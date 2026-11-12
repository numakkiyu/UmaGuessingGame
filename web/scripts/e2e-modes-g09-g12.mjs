#!/usr/bin/env node
/** Exercises clue puzzle, strategy race, rapport and creative rounds. Uses PLAYWRIGHT_CHROMIUM or an installed Chromium without downloading a browser. */
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "..");
const EVIDENCE = resolve(WEB_ROOT, "../.cache/reports");
const SHOTS = resolve(EVIDENCE, "screenshots");

const WEB_BASE = process.env.UMA_WEB_BASE ?? "http://127.0.0.1:8080";
const API_BASE = process.env.UMA_API_BASE ?? "http://127.0.0.1:8080";

const report = {
  generatedAt: new Date().toISOString(),
  environment: { webBase: WEB_BASE, apiBase: API_BASE, node: process.version },
  assertions: [],
  failures: [],
  screenshots: [],
  limits: [],
};

function assert(name, ok, detail) {
  const entry = { name, ok: Boolean(ok), detail };
  report.assertions.push(entry);
  if (ok) console.log(`[g09-g12] ✓ ${name}`);
  else {
    report.failures.push(entry);
    console.error(`[g09-g12] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(message, detail) {
  console.log(`[g09-g12] · ${message}`, detail ? JSON.stringify(detail) : "");
}

/**
 * 找本机已有的浏览器，不下载。
 */
async function resolveChromiumExecutable() {
  if (process.env.PLAYWRIGHT_CHROMIUM) return process.env.PLAYWRIGHT_CHROMIUM;
  const cacheRoot = join(homedir(), "Library/Caches/ms-playwright");
  if (!existsSync(cacheRoot)) return undefined;

  const { readdir } = await import("node:fs/promises");
  const entries = await readdir(cacheRoot);

  // 1. 优先 headless shell（体积小、启动快）
  const shellDirs = entries
    .filter((name) => /^chromium_headless_shell-\d+$/.test(name))
    .sort((a, b) => Number(b.split("-").pop()) - Number(a.split("-").pop()));
  for (const dir of shellDirs) {
    const candidates = [
      join(cacheRoot, dir, "chrome-headless-shell-mac-arm64", "chrome-headless-shell"),
      join(cacheRoot, dir, "chrome-headless-shell-mac-x64", "chrome-headless-shell"),
      join(cacheRoot, dir, "chrome-headless-shell-linux64", "chrome-headless-shell"),
    ];
    for (const candidate of candidates) {
      if (existsSync(candidate)) return candidate;
    }
  }

  // 2. 退回完整 Chrome for Testing
  const chromeDirs = entries
    .filter((name) => /^chromium-\d+$/.test(name))
    .sort((a, b) => Number(b.split("-").pop()) - Number(a.split("-").pop()));
  for (const dir of chromeDirs) {
    const candidates = [
      join(cacheRoot, dir, "chrome-mac-arm64", "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing"),
      join(cacheRoot, dir, "chrome-mac", "Google Chrome for Testing.app", "Contents", "MacOS", "Google Chrome for Testing"),
      join(cacheRoot, dir, "chrome-linux", "chrome"),
    ];
    for (const candidate of candidates) {
      if (existsSync(candidate)) return candidate;
    }
  }
  return undefined;
}

async function shot(page, name) {
  const path = resolve(SHOTS, `${name}.png`);
  await page.screenshot({ path, fullPage: true });
  const buf = await readFile(path);
  report.screenshots.push({
    name,
    path: `screenshots/${name}.png`,
    sha256: createHash("sha256").update(buf).digest("hex"),
    viewport: page.viewportSize() ? `${page.viewportSize().width}x${page.viewportSize().height}` : null,
  });
}

function makeClient() {
  let cookie = "";
  let csrf = null;
  return {
    get cookie() {
      return cookie;
    },
    async call(method, path, body, idempotencyKey) {
      const headers = { Accept: "application/json, application/problem+json", Origin: API_BASE };
      if (body !== undefined) headers["Content-Type"] = "application/json";
      if (cookie) headers["Cookie"] = cookie;
      if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
      if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
      const res = await fetch(`${API_BASE}/api/v1${path}`, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      for (const c of res.headers.getSetCookie?.() ?? []) {
        const kv = c.split(";")[0];
        if (kv.startsWith("uma_session")) cookie = kv;
      }
      const text = await res.text();
      let json = null;
      try {
        json = JSON.parse(text);
      } catch {
        /* 非 JSON */
      }
      return { status: res.status, json };
    },
    async newSession(name) {
      const s = await this.call("POST", "/sessions", { displayName: name });
      csrf = s.json?.csrfToken ?? null;
      return s;
    },
  };
}

const key = () => crypto.randomUUID().replace(/-/g, "");

/** 建房间、加 N 个玩家、开局到 collecting */
async function setupRound(modeId, playerCount) {
  const cfg = { modeId, rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed" };
  const host = makeClient();
  await host.newSession(`${modeId} 主持`);
  const room = await host.call(
    "POST",
    "/rooms",
    {
      name: `${modeId} 对局`,
      kind: "friend",
      modeConfig: cfg,
      maxPlayers: 8,
      maxSpectators: 0,
      visibility: "invite",
      allowSpectators: false,
    },
    key(),
  );
  const roomId = room.json?.room?.roomId;
  const inviteCode = room.json?.inviteCode;

  const players = [];
  for (let index = 1; index <= playerCount; index += 1) {
    const c = makeClient();
    await c.newSession(`${modeId} 选手${index}`);
    const joined = await c.call(
      "POST",
      `/rooms/${roomId}/memberships`,
      { inviteCode, requestedRole: "player" },
      key(),
    );
    if (joined.status === 201) {
      players.push({ client: c, membershipId: joined.json?.membershipId });
    }
  }

  const round = await host.call("POST", `/rooms/${roomId}/rounds`, { modeConfig: cfg }, key());
  if (round.status !== 201) {
    return { host, roomId, players, round, roundId: null, phaseVersion: 0 };
  }
  let phaseVersion = round.json?.phaseVersion ?? 1;
  for (const target of ["briefing", "collecting"]) {
    const t = await host.call(
      "POST",
      `/rounds/${round.json.roundId}/transitions`,
      { target, expectedPhaseVersion: phaseVersion },
      key(),
    );
    phaseVersion = t.json?.phaseVersion ?? phaseVersion + 1;
  }
  return { host, roomId, players, round, roundId: round.json?.roundId, phaseVersion };
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error("[g09-g12] 后端未就绪");
    process.exitCode = 1;
    return;
  }

  const executablePath = await resolveChromiumExecutable();
  if (!executablePath) {
    console.error(
      "[g09-g12] 找不到可用浏览器。请设置 PLAYWRIGHT_CHROMIUM 指向已安装的 Chromium，或先安装 Playwright 浏览器。",
    );
    process.exitCode = 1;
    return;
  }
  note("使用本机浏览器", { executablePath });
  const browser = await chromium.launch({ executablePath });

  /* ================= G09 clue-puzzle ================= */
  console.log("[g09-g12] G09 clue-puzzle");
  {
    const setup = await setupRound("clue-puzzle", 3);
    assert("拼图 三人开局成功", setup.round.status === 201, {
      status: setup.round.status,
      code: setup.round.json?.code,
    });

    if (setup.roundId) {
      const view = await setup.host.call("GET", `/rounds/${setup.roundId}`);
      const ps = view.json?.publicState;
      assert("拼图 有卡片与位置", (ps?.cards?.length ?? 0) > 0 && (ps?.slots?.length ?? 0) > 0, {
        cards: ps?.cards?.length,
        slots: ps?.slots?.length,
      });
      assert("拼图 本人有私有线索", (ps?.privateClues?.length ?? 0) > 0, {
        clues: ps?.privateClues?.length,
      });
      assert(
        "拼图 私有线索带约束 ID 与文本（不是空壳）",
        (ps?.privateClues ?? []).every((c) => typeof c.constraintId === "string" && typeof c.text === "string" && c.text.length > 0),
        { clues: ps?.privateClues },
      );
      assert("拼图 有约束总数", (ps?.constraintCount ?? 0) > 0, { constraintCount: ps?.constraintCount });
      assert("拼图 标明当前控制者", typeof ps?.controlSubjectId === "string" && ps.controlSubjectId.length > 0, {
        controlSubjectId: ps?.controlSubjectId,
        selfIsController: ps?.selfIsController,
      });
      note("拼图投影", {
        cards: ps?.cards?.length,
        slots: ps?.slots?.length,
        privateClues: ps?.privateClues?.length,
        shared: ps?.shared?.clues?.length ?? 0,
        sharedEmptyReason: ps?.sharedEmptyReason?.slice(0, 40),
        constraintCount: ps?.constraintCount,
        selfIsController: ps?.selfIsController,
      });

      // 提交完整分配（slotIndex 当 slotId，cardId 当 characterId）
      const assignments = (ps?.slots ?? []).map((slot, index) => ({
        slotId: String(slot.slotIndex),
        characterId: ps.cards[index]?.cardId,
      }));
      const submit = await setup.host.call(
        "POST",
        `/rounds/${setup.roundId}/submissions`,
        {
          phaseVersion: setup.phaseVersion,
          submissionRevision: 0,
          controlGeneration: 0,
          modeAction: { kind: "clue.assignment", assignments },
        },
        key(),
      );
      assert("拼图 控制者提交完整分配成功", submit.status === 201, {
        status: submit.status,
        code: submit.json?.code,
        detail: submit.json?.detail,
      });

      // 页面渲染
      const context = await browser.newContext({ viewport: { width: 360, height: 640 } });
      const [cookieName, cookieValue] = setup.host.cookie.split("=");
      if (cookieName && cookieValue) {
        await context.addCookies([{ name: cookieName, value: cookieValue, domain: "127.0.0.1", path: "/" }]);
      }
      const page = await context.newPage();
      try {
        await page.goto(`${WEB_BASE}/rooms/${setup.roomId}`, { waitUntil: "commit" });
        await page.waitForSelector("#main-content", { timeout: 25000 });
        await page.waitForTimeout(3000);
        const text = await page.locator("#main-content").innerText();
        assert("拼图 页面显示私有线索区", /只有你能看到的线索/.test(text), {
          excerpt: text.slice(0, 300),
        });
        assert("拼图 页面显示位置与卡片区", /位置/.test(text) && /卡片/.test(text), null);
        assert("拼图 页面不出现占位词", !/还在实现中|尚未实现|开发中/.test(text), {
          excerpt: text.slice(0, 200),
        });
        assert("拼图 页面不出现「界面还没做好」", !/界面还没做好/.test(text), null);
        const overflow = await page.evaluate(
          () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
        );
        assert("拼图 360×640 不横向溢出", overflow <= 0, { overflowPx: overflow });
        await shot(page, "g09-clue-puzzle-360");
      } catch (error) {
        assert("拼图 页面可验证", false, String(error));
      } finally {
        await context.close();
      }
    }
  }

  /* ================= G10 strategy-race ================= */
  console.log("[g09-g12] G10 strategy-race");
  {
    const setup = await setupRound("strategy-race", 2);
    assert("赛道 两人开局成功", setup.round.status === 201, {
      status: setup.round.status,
      code: setup.round.json?.code,
    });

    if (setup.roundId) {
      const view = await setup.host.call("GET", `/rounds/${setup.roundId}`);
      const ps = view.json?.publicState;
      const actions = ps?.track?.actions ?? [];
      assert("赛道 有四个动作（稳进/冲刺/恢复/跟跑）", actions.length === 4, {
        actions: actions.map((a) => a.action),
      });
      assert(
        "赛道 动作数值来自服务端（不是前端硬编码）",
        actions.every((a) => typeof a.positionGain === "number"),
        { actions: actions.map((a) => ({ action: a.action, gain: a.positionGain, stamina: a.staminaDelta })) },
      );
      assert("赛道 开局体力 6、上限 8", ps?.track?.startStamina === 6 && ps?.track?.staminaCap === 8, {
        startStamina: ps?.track?.startStamina,
        staminaCap: ps?.track?.staminaCap,
      });
      assert("赛道 标明默认动作（无行动时用）", typeof ps?.track?.defaultAction === "string", {
        defaultAction: ps?.track?.defaultAction,
      });
      assert("赛道 8 回合", ps?.track?.totalTurns === 8, { totalTurns: ps?.track?.totalTurns });

      const submit = await setup.host.call(
        "POST",
        `/rounds/${setup.roundId}/submissions`,
        {
          phaseVersion: setup.phaseVersion,
          submissionRevision: 0,
          controlGeneration: 0,
          modeAction: { kind: "strategy.action", action: "steady", turn: ps?.currentTurn ?? 1 },
        },
        key(),
      );
      assert("赛道 提交行动成功", submit.status === 201, {
        status: submit.status,
        code: submit.json?.code,
      });

      const context = await browser.newContext({ viewport: { width: 360, height: 640 } });
      const [cookieName, cookieValue] = setup.host.cookie.split("=");
      if (cookieName && cookieValue) {
        await context.addCookies([{ name: cookieName, value: cookieValue, domain: "127.0.0.1", path: "/" }]);
      }
      const page = await context.newPage();
      try {
        await page.goto(`${WEB_BASE}/rooms/${setup.roomId}`, { waitUntil: "commit" });
        await page.waitForSelector("#main-content", { timeout: 25000 });
        await page.waitForTimeout(3000);
        const text = await page.locator("#main-content").innerText();
        assert("赛道 页面显示动作选项", /稳进/.test(text) && /冲刺/.test(text), {
          excerpt: text.slice(0, 300),
        });
        assert("赛道 页面告知「没选会默认稳进」", /默认稳进|稳进.*处理/.test(text), {
          excerpt: text.slice(0, 400),
        });
        // 检查赛道存在，且可访问名称包含回合数与终点格数
        const track = await page.locator('[aria-label*="赛道"]').first();
        const trackCount = await track.count();
        assert("赛道 渲染了赛道可视化（替代数字卡）", trackCount > 0, { trackCount });
        if (trackCount > 0) {
          const label = await track.getAttribute("aria-label");
          assert("赛道 无障碍标签说明了回合与终点", /回合/.test(label ?? "") && /终点/.test(label ?? ""), { label });
        }
        // 体力上限必须在页面上有出处，否则玩家不知道 6/8 的 8 是什么
        assert("赛道 说明了体力上限", /体力/.test(text), { excerpt: text.slice(0, 200) });
        const overflow = await page.evaluate(
          () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
        );
        assert("赛道 360×640 不横向溢出", overflow <= 0, { overflowPx: overflow });
        await shot(page, "g10-strategy-race-360");
      } catch (error) {
        assert("赛道 页面可验证", false, String(error));
      } finally {
        await context.close();
      }
    }
  }

  /* ================= G11 rapport ================= */
  console.log("[g09-g12] G11 rapport");
  {
    const setup = await setupRound("rapport", 2);
    assert("默契 两人开局成功", setup.round.status === 201, {
      status: setup.round.status,
      code: setup.round.json?.code,
    });

    if (setup.roundId) {
      const view = await setup.host.call("GET", `/rounds/${setup.roundId}`);
      const ps = view.json?.publicState;
      const questions = ps?.questions ?? [];
      assert("默契 有题目", questions.length > 0, { count: questions.length });
      assert("默契 每题 4 个选项", questions.every((q) => q.options?.length === 4), {
        optionCounts: questions.map((q) => q.options?.length),
      });
      assert("默契 开局公布总预测槽位", (ps?.totalSlots ?? 0) > 0, { totalSlots: ps?.totalSlots });
      /*
       * 服务端在未锁定时把 revealedHits/revealedSlots 发成 0，
       * 字段注释明确写「A zero here means "nothing has been revealed yet"」。
       * 所以不能断言「字段不存在」，要断言「还没揭晓时计数为 0」。
       */
      assert(
        "默契 未揭晓时命中计数为 0（服务端语义：0 表示还没揭晓）",
        (ps?.revealedHits ?? 0) === 0 && (ps?.revealedSlots ?? 0) === 0,
        { revealedHits: ps?.revealedHits, revealedSlots: ps?.revealedSlots },
      );
      assert(
        "默契 每题带 locked 标记（决定能否显示结果）",
        questions.every((q) => typeof q.locked === "boolean"),
        { locked: questions.map((q) => q.locked) },
      );

      const first = questions[0];
      const submit = await setup.host.call(
        "POST",
        `/rounds/${setup.roundId}/submissions`,
        {
          phaseVersion: setup.phaseVersion,
          submissionRevision: 0,
          controlGeneration: 0,
          modeAction: { kind: "rapport.answer", optionId: first.options[0].optionId },
        },
        key(),
      );
      assert("默契 提交回答成功", submit.status === 201, {
        status: submit.status,
        code: submit.json?.code,
      });

      const context = await browser.newContext({ viewport: { width: 360, height: 640 } });
      const [cookieName, cookieValue] = setup.host.cookie.split("=");
      if (cookieName && cookieValue) {
        await context.addCookies([{ name: cookieName, value: cookieValue, domain: "127.0.0.1", path: "/" }]);
      }
      const page = await context.newPage();
      try {
        await page.goto(`${WEB_BASE}/rooms/${setup.roomId}`, { waitUntil: "commit" });
        await page.waitForSelector("#main-content", { timeout: 25000 });
        await page.waitForTimeout(3000);
        const text = await page.locator("#main-content").innerText();
        assert("默契 页面显示题目与选项", /第 1 题/.test(text), { excerpt: text.slice(0, 300) });
        assert("默契 页面告知「不答也占分母」", /分母/.test(text), { excerpt: text.slice(0, 400) });
        assert("默契 页面不显示他人选择", !/别人选了|队友选了/.test(text), null);
        const overflow = await page.evaluate(
          () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
        );
        assert("默契 360×640 不横向溢出", overflow <= 0, { overflowPx: overflow });
        await shot(page, "g11-rapport-360");
      } catch (error) {
        assert("默契 页面可验证", false, String(error));
      } finally {
        await context.close();
      }
    }
  }

  /* ================= G12 creative ================= */
  console.log("[g09-g12] G12 creative");
  {
    const setup = await setupRound("creative", 2);
    assert("创意 两人开局成功", setup.round.status === 201, {
      status: setup.round.status,
      code: setup.round.json?.code,
    });

    if (setup.roundId) {
      const view = await setup.host.call("GET", `/rounds/${setup.roundId}`);
      const ps = view.json?.publicState;
      assert("创意 有题面（角色/场景/限制）", Boolean(ps?.prompt?.characterName), {
        prompt: ps?.prompt,
      });
      assert("创意 码点上限 80", ps?.maxCodePoints === 80, { maxCodePoints: ps?.maxCodePoints });
      /*
       * 服务端 2026-11-09 改了文案：不再要求玩家自己做 NFC 归一化
       * （归一化改为程序内部完成，见 mode.go:622 的注释）。
       * 所以断言改为「规则说明面向玩家，不提编码细节」。
       */
      assert(
        "创意 规则面向玩家（不提 NFC/码点等实现细节）",
        typeof ps?.textRules === "string" && !/NFC|Unicode|码点/.test(ps.textRules),
        { textRules: ps?.textRules },
      );
      assert(
        "创意 规则明确「不会被悄悄截掉」",
        typeof ps?.textRules === "string" && /不会被悄悄截掉/.test(ps.textRules),
        { textRules: ps?.textRules },
      );
      assert(
        "创意 规则说明怎么数字数（一个汉字/字母/表情都算 1）",
        typeof ps?.textRules === "string" && /都算 1/.test(ps.textRules),
        { textRules: ps?.textRules },
      );
      assert("创意 写作阶段投票区为空并说明原因", (ps?.ballot?.length ?? 0) === 0 && Boolean(ps?.ballotHiddenReason), {
        ballot: ps?.ballot?.length,
        reason: ps?.ballotHiddenReason?.slice(0, 50),
      });

      /*
       * 写作阶段的界面检查用独立房间做。
       *
       * 不能在同一个房间上先开浏览器再提交：浏览器上下文会轮换 CSRF，
       * 之后 API 客户端用旧 token 提交就 403（实测踩过）。
       * 所以拆成两步：一个房间只验写作阶段界面，另一个房间走提交链路。
       */
      {
        const writeSetup = await setupRound("creative", 2);
        if (writeSetup.roundId) {
          const wCtx = await browser.newContext({ viewport: { width: 360, height: 640 } });
          const [wName, wValue] = writeSetup.host.cookie.split("=");
          if (wName && wValue) {
            await wCtx.addCookies([{ name: wName, value: wValue, domain: "127.0.0.1", path: "/" }]);
          }
          const wPage = await wCtx.newPage();
          try {
            await wPage.goto(`${WEB_BASE}/rooms/${writeSetup.roomId}`, { waitUntil: "commit" });
            await wPage.waitForSelector("#main-content", { timeout: 25000 });
            await wPage.waitForTimeout(3000);
            const wText = await wPage.locator("#main-content").innerText();
            assert("创意 写作阶段显示码点计数（0 / 80）", /\d+ \/ 80/.test(wText), {
              excerpt: wText.slice(0, 400),
            });
            assert("创意 写作阶段显示输入框", (await wPage.locator("#creative-text").count()) > 0, null);
            const wOverflow = await wPage.evaluate(
              () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
            );
            assert("创意 360×640 不横向溢出（写作阶段）", wOverflow <= 0, { overflowPx: wOverflow });
            await shot(wPage, "g12-creative-write-360");
          } catch (error) {
            assert("创意 写作阶段可验证", false, String(error));
          } finally {
            await wCtx.close();
          }
        }
      }

      // 提交作品（用 NFC 安全的普通中文）
      const workText = "她把这个道具放在桌上，笑着说这就是今天的答案。";
      const submit = await setup.host.call(
        "POST",
        `/rounds/${setup.roundId}/submissions`,
        {
          phaseVersion: setup.phaseVersion,
          submissionRevision: 0,
          controlGeneration: 0,
          modeAction: { kind: "creative.work", text: workText },
        },
        key(),
      );
      assert("创意 提交作品成功", submit.status === 201, {
        status: submit.status,
        code: submit.json?.code,
        detail: submit.json?.detail,
      });

      const context = await browser.newContext({ viewport: { width: 360, height: 640 } });
      const [cookieName, cookieValue] = setup.host.cookie.split("=");
      if (cookieName && cookieValue) {
        await context.addCookies([{ name: cookieName, value: cookieValue, domain: "127.0.0.1", path: "/" }]);
      }
      const page = await context.newPage();
      try {
        await page.goto(`${WEB_BASE}/rooms/${setup.roomId}`, { waitUntil: "commit" });
        await page.waitForSelector("#main-content", { timeout: 25000 });
        await page.waitForTimeout(3000);
        const text = await page.locator("#main-content").innerText();
        assert("创意 页面显示题面", /场景/.test(text) && /限制/.test(text), { excerpt: text.slice(0, 300) });
        assert("创意 页面显示字数规则（面向玩家，无编码术语）", /都算 1/.test(text) && !/NFC/.test(text), {
          excerpt: text.slice(0, 400),
        });
        assert("创意 提交后显示「作品已提交」", /作品已提交/.test(text), { excerpt: text.slice(0, 400) });
        assert("创意 提交后告知「不能投自己」", /不能投自己/.test(text), null);
        const overflow = await page.evaluate(
          () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
        );
        assert("创意 360×640 不横向溢出", overflow <= 0, { overflowPx: overflow });
        await shot(page, "g12-creative-360");
      } catch (error) {
        assert("创意 页面可验证", false, String(error));
      } finally {
        await context.close();
      }
    }
  }


  /* ================= G14 trade ================= */
  console.log("[g09-g12] G14 trade");
  {
    const setup = await setupRound("trade", 3);
    assert("交易 三人开局成功", setup.round.status === 201, {
      status: setup.round.status,
      code: setup.round.json?.code,
    });

    if (setup.roundId) {
      const view = await setup.host.call("GET", `/rounds/${setup.roundId}`);
      const ps = view.json?.publicState;
      assert("交易 有卡实例目录", (ps?.instances?.length ?? 0) > 0, {
        instances: ps?.instances?.length,
      });
      assert(
        "交易 实例带 label（不必二次请求）",
        (ps?.instances ?? []).every((i) => typeof i.instanceId === "string" && typeof i.label === "string"),
        { sample: ps?.instances?.[0] },
      );
      assert("交易 有卡种目录（抽象卡种，不绑定角色）", (ps?.cardKinds?.length ?? 0) > 0, {
        cardKinds: ps?.cardKinds?.length,
      });
      assert("交易 目标全部公开", (ps?.objectives?.length ?? 0) > 0, {
        objectives: ps?.objectives?.length,
      });
      assert("交易 自己有两个目标槽", (ps?.selfObjectives?.length ?? 0) === 2, {
        selfObjectives: ps?.selfObjectives?.length,
      });
      assert("交易 自己的手牌有 4 张", (ps?.self?.hand?.length ?? 0) === 4, {
        hand: ps?.self?.hand?.length,
      });
      assert(
        "交易 hands 是公开映射（每人一手）",
        Object.keys(ps?.hands ?? {}).length >= 3,
        { hands: Object.keys(ps?.hands ?? {}).length },
      );
      assert("交易 初始无提案并说明原因", (ps?.trades?.length ?? 0) === 0 && Boolean(ps?.tradesEmptyReason), {
        trades: ps?.trades?.length,
        reason: ps?.tradesEmptyReason?.slice(0, 40),
      });

      // Check proposal creation and its subsequent public projection
      const selfHand = ps?.self?.hand ?? [];
      const otherId = Object.keys(ps?.hands ?? {}).find((id) => id !== ps?.self?.subjectId);
      const theirHand = otherId ? (ps.hands[otherId] ?? []) : [];
      if (selfHand.length > 0 && theirHand.length > 0 && otherId) {
        const trade = await setup.host.call(
          "POST",
          `/rounds/${setup.roundId}/trades`,
          {
            phaseVersion: setup.phaseVersion,
            recipientMembershipId: otherId,
            offeredInstanceId: selfHand[0],
            requestedInstanceId: theirHand[0],
          },
          key(),
        );
        assert("交易 提案成功（前两层已修，能拿到 tradeId）", trade.status === 201 && Boolean(trade.json?.tradeId), {
          status: trade.status,
          code: trade.json?.code,
          tradeId: trade.json?.tradeId,
        });

        // Verify the proposal is visible to its recipient and can be accepted
        const afterPropose = await setup.host.call("GET", `/rounds/${setup.roundId}`);
        const tradesAfter = afterPropose.json?.publicState?.trades ?? [];
        assert("交易 提案出现在公开投影里（第三层已修）", tradesAfter.length > 0, {
          count: tradesAfter.length,
          first: tradesAfter[0],
        });
        assert(
          "交易 提案含双方 id 与两个实例 id",
          tradesAfter.length > 0 &&
            typeof tradesAfter[0].tradeId === "string" &&
            typeof tradesAfter[0].senderId === "string" &&
            typeof tradesAfter[0].recipientId === "string" &&
            typeof tradesAfter[0].offeredInstanceId === "string" &&
            typeof tradesAfter[0].requestedInstanceId === "string",
          { trade: tradesAfter[0] },
        );
        assert("交易 提案初始状态为 pending", tradesAfter[0]?.status === "pending", {
          status: tradesAfter[0]?.status,
        });

        // 对方（被提案人）也能看到这条提案
        const otherClient = setup.players.find((p) => p.membershipId === otherId);
        if (otherClient) {
          const otherView = await otherClient.client.call("GET", `/rounds/${setup.roundId}`);
          const otherTrades = otherView.json?.publicState?.trades ?? [];
          assert("交易 对方能看到这条提案", otherTrades.length > 0, {
            count: otherTrades.length,
          });

          // 记录交换前的双方手牌
          const beforeHands = afterPropose.json?.publicState?.hands ?? {};
          const proposerBefore = [...(beforeHands[ps?.self?.subjectId] ?? [])];
          const recipientBefore = [...(beforeHands[otherId] ?? [])];

          // 接受
          const acceptRes = await otherClient.client.call(
            "POST",
            `/rounds/${setup.roundId}/trades/${trade.json.tradeId}/responses`,
            { accept: true },
            key(),
          );
          assert("交易 对方接受提案成功", acceptRes.status === 201, {
            status: acceptRes.status,
            code: acceptRes.json?.code,
          });

          // 交换后
          const afterAccept = await setup.host.call("GET", `/rounds/${setup.roundId}`);
          const handsAfter = afterAccept.json?.publicState?.hands ?? {};
          const proposerAfter = handsAfter[ps?.self?.subjectId] ?? [];
          const recipientAfter = handsAfter[otherId] ?? [];

          assert(
            "交易 接受后双方手牌真的交换了",
            proposerAfter.includes(theirHand[0]) && recipientAfter.includes(selfHand[0]),
            {
              proposerBefore,
              proposerAfter,
              recipientBefore,
              recipientAfter,
              expectedProposerGain: theirHand[0],
              expectedRecipientGain: selfHand[0],
            },
          );
          assert(
            "交易 手牌数量不变（原子交换，不是增减）",
            proposerAfter.length === proposerBefore.length &&
              recipientAfter.length === recipientBefore.length,
            {
              proposerCount: [proposerBefore.length, proposerAfter.length],
              recipientCount: [recipientBefore.length, recipientAfter.length],
            },
          );
          assert(
            "交易 提案状态变为 accepted",
            afterAccept.json?.publicState?.trades?.[0]?.status === "accepted",
            { status: afterAccept.json?.publicState?.trades?.[0]?.status },
          );
        }
      }

      // 页面渲染
      const context = await browser.newContext({ viewport: { width: 360, height: 640 } });
      const [cookieName, cookieValue] = setup.host.cookie.split("=");
      if (cookieName && cookieValue) {
        await context.addCookies([{ name: cookieName, value: cookieValue, domain: "127.0.0.1", path: "/" }]);
      }
      const page = await context.newPage();
      try {
        await page.goto(`${WEB_BASE}/rooms/${setup.roomId}`, { waitUntil: "commit" });
        await page.waitForSelector("#main-content", { timeout: 25000 });
        await page.waitForTimeout(3000);
        const text = await page.locator("#main-content").innerText();
        assert("交易 页面显示「我的秘密目标」", /我的秘密目标/.test(text), { excerpt: text.slice(0, 300) });
        assert("交易 页面显示「我的手牌」", /我的手牌/.test(text), null);
        assert("交易 页面显示「发起提案」", /发起提案/.test(text), null);
        assert("交易 页面显示「交易记录」", /交易记录/.test(text), null);
        assert("交易 页面显示全部目标（公开，默认收起）", /全部目标（公开）/.test(text), null);
        /*
         * 全部目标默认收起：16 条展开会把 360×640 页面撑到 3000px 以上。
         * 断言「默认收起」——目标条目不应在首屏就全部铺开。
         */
        const objectiveRows = await page
          .locator('section[aria-labelledby="trade-all-objectives"] li')
          .count();
        assert("交易 全部目标默认收起（不一次性铺开）", objectiveRows === 0, { rows: objectiveRows });
        // 展开后能查到全部
        await page.getByRole("button", { name: /展开 \d+ 条/ }).click();
        await page.waitForTimeout(600);
        const expandedRows = await page
          .locator('section[aria-labelledby="trade-all-objectives"] li')
          .count();
        assert("交易 展开后能看到全部目标", expandedRows > 0, { rows: expandedRows });
        const pageHeight = await page.evaluate(() => document.documentElement.scrollHeight);
        note("交易页面高度", { px: pageHeight, expandedRows });
        assert("交易 页面不出现占位词", !/还在实现中|尚未实现|开发中/.test(text), {
          excerpt: text.slice(0, 200),
        });
        assert("交易 页面不出现「界面还没做好」", !/界面还没做好/.test(text), null);
        const overflow = await page.evaluate(
          () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
        );
        assert("交易 360×640 不横向溢出", overflow <= 0, { overflowPx: overflow });

        // 触摸区 ≥44px（抽查所有按钮）
        const smallTargets = await page.evaluate(() => {
          const out = [];
          for (const el of document.querySelectorAll("button, a[role='button']")) {
            const rect = el.getBoundingClientRect();
            if (rect.width === 0 || rect.height === 0) continue;
            if (rect.height < 44) out.push({ text: (el.textContent ?? "").trim().slice(0, 20), h: Math.round(rect.height) });
          }
          return out.slice(0, 5);
        });
        assert("交易 触摸区 ≥44px", smallTargets.length === 0, { smallTargets });

        await shot(page, "g14-trade-360");
      } catch (error) {
        assert("交易 页面可验证", false, String(error));
      } finally {
        await context.close();
      }
    }
  }

  await browser.close();

  const hashes = report.screenshots.map((s) => s.sha256).filter(Boolean);
  const unique = new Set(hashes);
  assert("截图内容互不相同", unique.size === hashes.length, {
    total: hashes.length,
    unique: unique.size,
  });

  report.summary = {
    assertions: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
    screenshots: report.screenshots.length,
  };

  const out = resolve(EVIDENCE, "modes-g09-g12-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[g09-g12] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 失败`,
  );
  console.log(`[g09-g12] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
