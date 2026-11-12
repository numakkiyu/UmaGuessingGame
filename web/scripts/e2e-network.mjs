#!/usr/bin/env node
/** Tests CDP latency, bandwidth limits and disconnection separately. Recovery requires current state and no duplicated rows, not just an open stream. */
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
  /** 按类型分开记，不合并成单一结论 */
  byType: {
    latency: { assertions: [], limits: [] },
    bandwidth: { assertions: [], limits: [] },
    offline: { assertions: [], limits: [] },
    reconnectCatchUp: { assertions: [], limits: [] },
  },
  failures: [],
  screenshots: [],
  limits: [],
};

function assert(type, name, ok, detail) {
  const entry = { name, ok: Boolean(ok), detail };
  report.byType[type].assertions.push(entry);
  if (ok) console.log(`[net-e2e] ✓ [${type}] ${name}`);
  else {
    report.failures.push({ type, ...entry });
    console.error(`[net-e2e] ✗ [${type}] ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(message, detail) {
  console.log(`[net-e2e] · ${message}`, detail ? JSON.stringify(detail) : "");
}

async function resolveChromiumExecutable() {
  if (process.env.PLAYWRIGHT_CHROMIUM) return process.env.PLAYWRIGHT_CHROMIUM;
  const cacheRoot = join(homedir(), "Library/Caches/ms-playwright");
  if (!existsSync(cacheRoot)) return undefined;
  const { readdir } = await import("node:fs/promises");
  const entries = await readdir(cacheRoot);
  const dirs = entries
    .filter((name) => /^chromium-\d+$/.test(name))
    .sort((a, b) => Number(b.split("-")[1]) - Number(a.split("-")[1]));
  for (const dir of dirs) {
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
  await page.screenshot({ path });
  const buf = await readFile(path);
  const viewport = page.viewportSize();
  report.screenshots.push({
    name,
    path: `screenshots/${name}.png`,
    sha256: createHash("sha256").update(buf).digest("hex"),
    viewport: viewport ? `${viewport.width}x${viewport.height}` : null,
  });
}

/** API 客户端（用于制造「另一方提交」） */
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
      return { status: res.status, json, text };
    },
    async newSession(name) {
      const s = await this.call("POST", "/sessions", { displayName: name });
      csrf = s.json?.csrfToken ?? null;
      return s;
    },
  };
}

const key = () => crypto.randomUUID().replace(/-/g, "");
const CLASSIC_CFG = {
  modeId: "classic",
  maxWrongGuesses: 8,
  winPolicy: "first-correct",
  durationSeconds: 0,
  shadowEnabled: false,
};

/** 通过 CDP 设置网络条件 */
async function setNetwork(cdp, condition) {
  await cdp.send("Network.enable");
  await cdp.send("Network.emulateNetworkConditions", {
    offline: condition.offline ?? false,
    latency: condition.latency ?? 0,
    downloadThroughput: condition.downloadThroughput ?? -1,
    uploadThroughput: condition.uploadThroughput ?? -1,
  });
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`[net-e2e] 后端未就绪：${API_BASE}/health/ready`);
    process.exitCode = 1;
    return;
  }

  const executablePath = await resolveChromiumExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});

  /* ============ 准备一局（房主 + 客人，两人都在同一房间） ============ */
  const hostApi = makeClient();
  await hostApi.newSession("弱网房主");
  const roomRes = await hostApi.call(
    "POST",
    "/rooms",
    {
      name: "弱网测试房",
      kind: "friend",
      modeConfig: CLASSIC_CFG,
      maxPlayers: 2,
      maxSpectators: 0,
      visibility: "invite",
      allowSpectators: false,
    },
    key(),
  );
  const roomId = roomRes.json?.room?.roomId;
  const inviteCode = roomRes.json?.inviteCode;

  const guestApi = makeClient();
  await guestApi.newSession("弱网客人");
  await guestApi.call(
    "POST",
    `/rooms/${roomId}/memberships`,
    { inviteCode, requestedRole: "player" },
    key(),
  );

  const roundRes = await hostApi.call("POST", `/rooms/${roomId}/rounds`, { modeConfig: CLASSIC_CFG }, key());
  const roundId = roundRes.json?.roundId;
  let phaseVersion = roundRes.json?.phaseVersion ?? 1;
  for (const target of ["briefing", "collecting"]) {
    const t = await hostApi.call(
      "POST",
      `/rounds/${roundId}/transitions`,
      { target, expectedPhaseVersion: phaseVersion },
      key(),
    );
    phaseVersion = t.json?.phaseVersion ?? phaseVersion + 1;
  }
  note("准备完成", { roomId, roundId, phaseVersion });

  /* ============ A. 延迟型 ============ */
  {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [name, value] = guestApi.cookie.split("=");
    await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();
    const cdp = await context.newCDPSession(page);

    try {
      // 2000ms 延迟（4G 弱信号量级）
      await setNetwork(cdp, { latency: 2000, downloadThroughput: 400 * 1024, uploadThroughput: 400 * 1024 });

      const startedAt = Date.now();
      await page.goto(`${WEB_BASE}/rooms/${roomId}`, { waitUntil: "commit" });
      /*
       * 不能用 h1 作为就绪信号：header 里有一个在 390 宽下隐藏的 h1
       * （`hidden ... sm:block`），waitForSelector 默认要求可见，会一直等它。
       * 改用房间页的真实内容：连接状态条或成员列表。
       */
      await page.waitForSelector(".uma-connbar, #main-content", { timeout: 30000 });
      await page.waitForTimeout(500);
      const firstPaintMs = Date.now() - startedAt;
      note("[latency] 首屏可交互耗时", { ms: firstPaintMs });

      assert("latency", "2000ms 延迟下页面仍能打开", firstPaintMs < 30000, { ms: firstPaintMs });

      // 延迟下不应该崩，应该显示真实内容或加载态
      const bodyText = await page.locator("body").innerText();
      assert("latency", "2000ms 延迟下页面有可读内容（不是白屏）", bodyText.length > 20, {
        length: bodyText.length,
        excerpt: bodyText.slice(0, 120),
      });

      await shot(page, "net-latency-2000ms");
      report.byType.latency.limits.push(
        "CDP latency 只模拟往返延迟，不模拟丢包与乱序；真实移动网络（4G/5G 切换、隧道）未测",
      );
    } catch (error) {
      assert("latency", "延迟型测试可执行", false, String(error));
    } finally {
      await context.close();
    }
  }

  /* ============ B. 带宽型 ============ */
  {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [name, value] = guestApi.cookie.split("=");
    await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();
    const cdp = await context.newCDPSession(page);

    try {
      // 50KB/s 下行（接近 2G/极弱 3G）
      await setNetwork(cdp, { latency: 300, downloadThroughput: 50 * 1024, uploadThroughput: 20 * 1024 });

      const startedAt = Date.now();
      await page.goto(`${WEB_BASE}/rooms/${roomId}`, { waitUntil: "commit" });
      await page.waitForSelector(".uma-connbar, #main-content", { timeout: 40000 });
      await page.waitForTimeout(500);
      const loadMs = Date.now() - startedAt;
      note("[bandwidth] 50KB/s 下首屏耗时", { ms: loadMs });

      assert("bandwidth", "50KB/s 带宽下页面仍能打开", loadMs < 40000, { ms: loadMs });

      const bodyText = await page.locator("body").innerText();
      assert("bandwidth", "50KB/s 带宽下有可读内容", bodyText.length > 20, { length: bodyText.length });

      /*
       * 头像在低带宽下可能加载不出来，但不能阻塞游戏。
       * 断言：页面结构完整（有 h1、有操作区），不因图片失败而空白。
       */
      const hasMain = (await page.locator("#main-content").count()) > 0;
      assert("bandwidth", "50KB/s 下页面结构完整（头像失败不影响主结构）", hasMain, null);

      await shot(page, "net-bandwidth-50kbps");
      report.byType.bandwidth.limits.push(
        "CDP throughput 只限速，不模拟突发丢包；头像在低带宽下的逐张回退未逐张验证",
      );
    } catch (error) {
      assert("bandwidth", "带宽型测试可执行", false, String(error));
    } finally {
      await context.close();
    }
  }

  /* ============ C. 断连型 + 重连追状态 ============ */
  {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [name, value] = guestApi.cookie.split("=");
    await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();
    const cdp = await context.newCDPSession(page);

    try {
      await page.goto(`${WEB_BASE}/rooms/${roomId}`, { waitUntil: "commit" });
      await page.waitForSelector(".uma-connbar, #main-content", { timeout: 25000 });
      await page.waitForTimeout(2000);

      // 记录断线前的页面内容（用于重连后对比，检测是否重复渲染）
      const beforeText = await page.locator("body").innerText();
      const beforeLength = beforeText.length;

      /* --- 断网 --- */
      await setNetwork(cdp, { offline: true });

      /*
       * 等待断线状态出现。
       *
       * 实测：2.5s 不够 —— EventSource 的 error 事件不是立即触发的，
       * 浏览器要等当前连接真的失败（TCP 超时或对端 RST）才会 onerror。
       * 这里用轮询等待最多 12s，而不是固定 sleep 后断言一次。
       */
      let showsOffline = false;
      let offlineText = "";
      for (let attempt = 0; attempt < 24; attempt += 1) {
        await page.waitForTimeout(500);
        offlineText = await page.locator("body").innerText();
        if (/已断开|正在重连|连接中断|离线/.test(offlineText)) {
          showsOffline = true;
          break;
        }
      }
      note("[offline] 断线状态检测耗时", { ms: showsOffline ? "已检测到" : "12s 内未检测到" });
      assert("offline", "断网后显示连接状态（不是静默失败）", showsOffline, {
        excerpt: offlineText.slice(0, 200),
      });
      await shot(page, "net-offline");

      /*
       * 断网期间另一方（房主）提交一次猜测。
       * 这是重连后「状态真的追上了」的前提条件。
       */
      const hostSubmit = await hostApi.call(
        "POST",
        `/rounds/${roundId}/submissions`,
        {
          phaseVersion,
          submissionRevision: 0,
          controlGeneration: 0,
          modeAction: { kind: "classic.guess", characterId: "1001" },
        },
        key(),
      );
      assert("reconnectCatchUp", "断线期间另一方提交成功（制造待追状态）", hostSubmit.status === 201, {
        status: hostSubmit.status,
      });
      note("断线期间房主提交", { status: hostSubmit.status });

      /* --- 恢复网络 --- */
      await setNetwork(cdp, { offline: false, latency: 100, downloadThroughput: -1, uploadThroughput: -1 });
      await page.waitForTimeout(6000);

      const afterText = await page.locator("body").innerText();
      const afterLength = afterText.length;

      // 连接状态应恢复
      const recovered = /已连接/.test(afterText);
      assert("reconnectCatchUp", "恢复网络后连接状态回到已连接", recovered, {
        excerpt: afterText.slice(0, 150),
      });

      /*
       * 核心断言：状态真的追上了。
       * 断线期间房主提交了角色 1001，重连后客人页面应该能反映这一点
       * （publicState.subjects 里有 submitted=true，或页面显示对手已提交）。
       *
       * 注意：客人自己还没提交，所以不能断言「看到对手的具体猜测内容」——
       * 那是私有历史，本来就看不到。这里断言的是对手的提交进度。
       */
      /* classic 的参赛进度使用 attempts/solved，不使用 submitted */
      const catchUpState = await (async () => {
        const roundView = await guestApi.call("GET", `/rounds/${roundId}`);
        const subjects = roundView.json?.publicState?.subjects ?? [];
        const guestSubject = roundView.json?.publicState?.self?.subjectId;
        // 断线期间房主（非客人）提交了，所以要找「不是自己」且 attempts>0 的那条
        const opponent = subjects.find((sub) => sub.subjectId !== guestSubject);
        return {
          opponent,
          opponentAttempts: opponent?.attempts ?? 0,
          allSubjects: subjects,
        };
      })();
      note("重连后服务端 subjects", catchUpState);
      assert(
        "reconnectCatchUp",
        "重连后服务端状态包含断线期间的另一方提交（attempts>0）",
        catchUpState.opponentAttempts > 0,
        catchUpState,
      );

      /*
       * 重复渲染检测：重连后页面文本长度不应显著膨胀。
       * 若 cursor 从 0 重放所有事件，列表会重复累积，长度会接近翻倍。
       */
      const growthRatio = beforeLength > 0 ? afterLength / beforeLength : 1;
      note("重连前后页面文本长度", { before: beforeLength, after: afterLength, ratio: Number(growthRatio.toFixed(2)) });
      assert(
        "reconnectCatchUp",
        "重连后页面没有重复渲染（文本长度未异常膨胀）",
        growthRatio < 1.8,
        { before: beforeLength, after: afterLength, ratio: Number(growthRatio.toFixed(2)) },
      );

      await shot(page, "net-reconnect");

      report.byType.offline.limits.push(
        "CDP offline 只模拟完全断网，不模拟半断连、DNS 抖动、NAT 超时；真实网络切换未测",
      );
      report.byType.reconnectCatchUp.limits.push(
        "只验证了「对手提交进度」能被追上；对手的具体猜测内容属于私有历史，本就不该可见，未断言",
      );
    } catch (error) {
      assert("offline", "断连型测试可执行", false, String(error));
    } finally {
      await context.close();
    }
  }

  /* ============ E. 等待区断网 + 服务端不可达（navigator.onLine 盲区） ============ */
  {
    /*
     * 两个场景分开测，因为它们的失败方式不同：
     *
     * 1. 等待区（lobby）断网：这一页原来完全没有 useRealtime 与连接状态条，
     * 断网后界面毫无变化，玩家会一直等一个永不更新的「等待房主开局」。
     *
     * 2. 服务端不可达但本机有网：navigator.onLine 仍是 true，
     * 所以 offline 事件不会触发。这是 navigator.onLine 的盲区——
     * 它只反映本机网络接口，不反映服务端是否可达。
     */
    const lobbyHost = makeClient();
    await lobbyHost.newSession("等待区房主");
    const lobbyRoom = await lobbyHost.call(
      "POST",
      "/rooms",
      {
        name: "等待区断网测试",
        kind: "solo",
        modeConfig: CLASSIC_CFG,
        maxPlayers: 2,
        maxSpectators: 0,
        visibility: "invite",
        allowSpectators: false,
      },
      key(),
    );
    const lobbyRoomId = lobbyRoom.json?.room?.roomId;

    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [name, value] = lobbyHost.cookie.split("=");
    await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();
    const cdp = await context.newCDPSession(page);

    try {
      // 注意：这个房间没有开局，所以渲染的是等待区
      await page.goto(`${WEB_BASE}/rooms/${lobbyRoomId}`, { waitUntil: "commit" });
      await page.waitForSelector(".uma-connbar, #main-content", { timeout: 25000 });
      await page.waitForTimeout(2000);

      const initialText = await page.locator("body").innerText();
      assert(
        "offline",
        "等待区有连接状态条（之前完全没有）",
        /已连接|正在重连|已断开/.test(initialText),
        { excerpt: initialText.slice(0, 150) },
      );

      /* --- 场景 1：等待区断网（offline 事件会触发） --- */
      await setNetwork(cdp, { offline: true });
      let lobbyOffline = false;
      for (let attempt = 0; attempt < 16; attempt += 1) {
        await page.waitForTimeout(500);
        const txt = await page.locator("body").innerText();
        if (/已断开|正在重连|连接中断/.test(txt)) {
          lobbyOffline = true;
          break;
        }
      }
      assert("offline", "等待区断网后显示连接状态（不再静默）", lobbyOffline, null);
      await shot(page, "net-lobby-offline");

      /* --- 场景 2：服务端不可达但本机有网 --- */
      await setNetwork(cdp, { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
      await page.waitForTimeout(4000);

      // 屏蔽所有 API 请求：本机网络正常（navigator.onLine=true），但服务端不可达
      await cdp.send("Network.setBlockedURLs", { urls: ["*/api/v1/*"] });
      const onLineStill = await page.evaluate(() => navigator.onLine);
      assert("offline", "屏蔽 API 时 navigator.onLine 仍为 true（这正是盲区所在）", onLineStill === true, {
        onLine: onLineStill,
      });

      /*
       * 期望：可达性探测（每 8s 打 /health/live）会在 ~16s 内把状态降级为 stale。
       * 只依赖 navigator.onLine 的话这里会一直显示「已连接」。
       */
      let degraded = false;
      let degradedAt = 0;
      for (let attempt = 0; attempt < 40; attempt += 1) {
        await page.waitForTimeout(1000);
        const txt = await page.locator("body").innerText();
        if (/可能已过期|正在重连|已断开|画面更新较慢/.test(txt)) {
          degraded = true;
          degradedAt = attempt + 1;
          break;
        }
      }
      assert("offline", "服务端不可达时状态会降级（不只依赖 navigator.onLine）", degraded, {
        secondsToDegrade: degradedAt,
      });
      note("服务端不可达降级耗时", { seconds: degradedAt });
      await shot(page, "net-server-unreachable");

      report.byType.offline.limits.push(
        "服务端不可达用 CDP 屏蔽 /api/v1/* 模拟；真实进程崩溃/网络分区未测",
      );
    } catch (error) {
      assert("offline", "等待区与服务端不可达测试可执行", false, String(error));
    } finally {
      await context.close();
    }
  }

  /* ============ F. 截图哈希互异 ============ */
  {
    const hashes = report.screenshots.map((s) => s.sha256).filter(Boolean);
    const unique = new Set(hashes);
    assert("offline", "弱网各状态截图互不相同", unique.size === hashes.length, {
      total: hashes.length,
      unique: unique.size,
    });
  }

  await browser.close();

  /* ============ 汇总（按类型分开） ============ */
  const allAssertions = Object.values(report.byType).flatMap((group) => group.assertions);
  report.summary = {
    total: allAssertions.length,
    passed: allAssertions.filter((a) => a.ok).length,
    failed: report.failures.length,
    screenshots: report.screenshots.length,
    byType: Object.fromEntries(
      Object.entries(report.byType).map(([type, group]) => [
        type,
        {
          passed: group.assertions.filter((a) => a.ok).length,
          total: group.assertions.length,
          limits: group.limits.length,
        },
      ]),
    ),
  };

  const out = resolve(EVIDENCE, "network-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[net-e2e] ${report.summary.passed}/${report.summary.total} 断言通过，${report.summary.failed} 失败，${report.summary.screenshots} 张截图`,
  );
  console.log(`[net-e2e] 按类型：${JSON.stringify(report.summary.byType)}`);
  console.log(`[net-e2e] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
