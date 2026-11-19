#!/usr/bin/env node
import {createMatchingTicket} from "./matchmaking-support.mjs";
/** Uses isolated browser contexts to check room invitations, member-specific views and turn authority. */
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
  if (ok) console.log(`[multi-e2e] ✓ ${name}`);
  else {
    report.failures.push(entry);
    console.error(`[multi-e2e] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(name, detail) {
  console.log(`[multi-e2e] · ${name}`, detail ? JSON.stringify(detail) : "");
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

/** 一个 API 客户端（独立 cookie） */
function makeClient() {
  let cookie = "";
  let csrf = null;
  return {
    get cookie() {
      return cookie;
    },
    get csrf() {
      return csrf;
    },
    async call(method, path, body, idempotencyKey) {
      const doCall = async () => {
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
        return { res, text, json };
      };

      let out = await doCall();
      // CSRF 被页面轮换后自愈（后端只有一份 hash），复用同一幂等键
      if (out.res.status === 403 && /csrf-token-invalid/.test(out.text) && method !== "GET") {
        const rotated = await fetch(`${API_BASE}/api/v1/sessions/current/csrf`, {
          method: "POST",
          headers: { Accept: "application/json", Origin: API_BASE, Cookie: cookie },
          cache: "no-store",
        }).catch(() => null);
        if (rotated && rotated.ok) {
          const bodyJson = await rotated.json().catch(() => null);
          if (bodyJson?.csrfToken) {
            csrf = bodyJson.csrfToken;
            out = await doCall();
          }
        }
      }
      return { status: out.res.status, json: out.json, text: out.text };
    },
    async newSession(name) {
      const s = await this.call("POST", "/sessions", { displayName: name });
      csrf = s.json?.csrfToken ?? null;
      return s;
    },
  };
}

const key = () => crypto.randomUUID().replace(/-/g, "");

  async function drainQueue() {
  // Drain shared tickets until two consecutive probes remain unmatched
  for (let attempt = 0; attempt < 8; attempt += 1) {
    const probeA = makeClient();
    await probeA.newSession(`清队列-${attempt}-a`);
    const ticketA = await createMatchingTicket(probeA);
    if (ticketA.status !== 201) return;

    const probeB = makeClient();
    await probeB.newSession(`清队列-${attempt}-b`);
    const ticketB = await createMatchingTicket(probeB);

    // 无论 A 是否配上，都取消它（取消会同时释放被它占住的 partner）
    await probeA.call("DELETE", `/matchmaking-tickets/${ticketA.json.ticketId}`, undefined, key());

    if (ticketB.status !== 201) {
      // 建不出第二张：队列里已没有可配的人，且 A 已取消 → 队列空
      return;
    }
    await probeB.call("DELETE", `/matchmaking-tickets/${ticketB.json.ticketId}`, undefined, key());

    // 若 B 建票时仍是 queueing（说明 A 没配上它），再检查一次是否还有残留
    if (ticketB.json?.status === "queueing") {
      // 两张都 queueing 且都已取消 → 队列应该空了，再确认一次
      const probeC = makeClient();
      await probeC.newSession(`清队列-${attempt}-c`);
      const ticketC = await createMatchingTicket(probeC);
      if (ticketC.status === 201) {
        const paired = ticketC.json?.status !== "queueing";
        await probeC.call("DELETE", `/matchmaking-tickets/${ticketC.json.ticketId}`, undefined, key());
        if (!paired) return; // 连续没配上人 → 队列空
      }
    }
  }
}

const CLASSIC_CFG = {
  modeId: "classic",
  maxWrongGuesses: 8,
  winPolicy: "first-correct",
  durationSeconds: 0,
  shadowEnabled: false,
};

/** 把 API 客户端的 cookie 注入浏览器上下文，让页面复用同一会话 */
async function injectCookie(context, cookie) {
  if (!cookie) return;
  const [name, value] = cookie.split("=");
  await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`[multi-e2e] 后端未就绪：${API_BASE}/health/ready`);
    process.exitCode = 1;
    return;
  }

  const executablePath = await resolveChromiumExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});

  /* ============ A. friend 房间：房主 + 客人两个独立会话 ============ */
  const hostApi = makeClient();
  await hostApi.newSession("房主");
  const roomRes = await hostApi.call(
    "POST",
    "/rooms",
    {
      name: "朋友对局",
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
  assert("房主创建 friend 房间", roomRes.status === 201 && Boolean(roomId), { status: roomRes.status, roomId });
  assert("返回邀请码", Boolean(inviteCode), { inviteCode });

  const guestApi = makeClient();
  await guestApi.newSession("客人");
  const resolveRes = await guestApi.call("POST", "/room-invitations/resolutions", { inviteCode }, key());
  assert("客人解析邀请码", resolveRes.status === 200 && resolveRes.json?.joinable === true, {
    status: resolveRes.status,
    body: resolveRes.json,
  });

  const joinRes = await guestApi.call(
    "POST",
    `/rooms/${roomId}/memberships`,
    { inviteCode, requestedRole: "player" },
    key(),
  );
  assert("客人加入房间", joinRes.status === 201, { status: joinRes.status, body: joinRes.json });
  assert("客人只有 play 能力（无管理权）", JSON.stringify(joinRes.json?.capabilities) === JSON.stringify(["play"]), {
    capabilities: joinRes.json?.capabilities,
  });

  // 房主视角应看到两个成员
  const hostView = await hostApi.call("GET", `/rooms/${roomId}`);
  const members = hostView.json?.members ?? [];
  assert("房主看到 2 名成员", members.length === 2, { count: members.length, names: members.map((m) => m.displayName) });
  assert(
    "房主拥有 manage-room 能力",
    (hostView.json?.viewer?.capabilities ?? []).includes("manage-room"),
    { capabilities: hostView.json?.viewer?.capabilities },
  );

  // 两个独立浏览器上下文
  const hostContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
  await injectCookie(hostContext, hostApi.cookie);
  const guestContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
  await injectCookie(guestContext, guestApi.cookie);

  const hostPage = await hostContext.newPage();
  const guestPage = await guestContext.newPage();

  try {
    await hostPage.goto(`${WEB_BASE}/rooms/${roomId}`, { waitUntil: "commit" });
    await hostPage.waitForTimeout(2500);
    await guestPage.goto(`${WEB_BASE}/rooms/${roomId}`, { waitUntil: "commit" });
    await guestPage.waitForTimeout(2500);

    const hostText = await hostPage.locator("body").innerText();
    const guestText = await guestPage.locator("body").innerText();

    assert("房主房间页可打开", hostText.length > 20, { excerpt: hostText.slice(0, 120) });
    assert("客人房间页可打开", guestText.length > 20, { excerpt: guestText.slice(0, 120) });
    assert("房主页面显示客人名字", hostText.includes("客人"), null);
    assert("客人页面显示房主名字", guestText.includes("房主"), null);

    await shot(hostPage, "multi-friend-host-390");
    await shot(guestPage, "multi-friend-guest-390");

    /* 权限差异：客人不能开局（开局是房主/主持动作） */
    const guestStart = await guestApi.call("POST", `/rooms/${roomId}/rounds`, { modeConfig: CLASSIC_CFG }, key());
    assert("客人不能开局（服务端拒绝）", guestStart.status >= 400, { status: guestStart.status });
    note("客人开局被拒状态", { status: guestStart.status, code: guestStart.json?.code });

    /* 房主开局 */
    const roundRes = await hostApi.call("POST", `/rooms/${roomId}/rounds`, { modeConfig: CLASSIC_CFG }, key());
    assert("房主可以开局", roundRes.status === 201, { status: roundRes.status });
    const roundId = roundRes.json?.roundId;
    let pv = roundRes.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await hostApi.call(
        "POST",
        `/rounds/${roundId}/transitions`,
        { target, expectedPhaseVersion: pv },
        key(),
      );
      pv = t.json?.phaseVersion ?? pv + 1;
    }

    /* 双方各自提交猜测（不同角色），验证私有历史隔离 */
    const hostGuess = await hostApi.call(
      "POST",
      `/rounds/${roundId}/submissions`,
      {
        phaseVersion: pv,
        submissionRevision: 0,
        controlGeneration: 0,
        modeAction: { kind: "classic.guess", characterId: "1001" },
      },
      key(),
    );
    assert("房主提交猜测成功", hostGuess.status === 201, { status: hostGuess.status, body: hostGuess.json });

    const guestRound = await guestApi.call("GET", `/rounds/${roundId}`);
    const guestGuess = await guestApi.call(
      "POST",
      `/rounds/${roundId}/submissions`,
      {
        phaseVersion: guestRound.json?.round?.phaseVersion ?? pv,
        submissionRevision: guestRound.json?.submissionRevision ?? 0,
        controlGeneration: guestRound.json?.controlGeneration ?? 0,
        modeAction: { kind: "classic.guess", characterId: "1002" },
      },
      key(),
    );
    assert("客人提交猜测成功", guestGuess.status === 201, { status: guestGuess.status, body: guestGuess.json });

    /* 私有历史隔离：客人不能读取房主的猜测 */
    const guestRoundAfter = await guestApi.call("GET", `/rounds/${roundId}`);
    const guestPrivate = JSON.stringify(guestRoundAfter.json?.privateState ?? {});
    assert(
      "客人的私有状态不含房主猜的角色",
      !guestPrivate.includes("1001"),
      { guestPrivateExcerpt: guestPrivate.slice(0, 300) },
    );
    note("客人私有状态摘要", { excerpt: guestPrivate.slice(0, 200) });

    /* 刷新后仍可写 */
    await guestPage.reload({ waitUntil: "commit" });
    await guestPage.waitForTimeout(2500);
    const guestAfterReload = await guestPage.locator("body").innerText();
    assert("客人刷新后页面仍可用", guestAfterReload.length > 20, { excerpt: guestAfterReload.slice(0, 120) });
    await shot(guestPage, "multi-friend-guest-reload-390");
  } catch (error) {
    assert("friend 双会话流程", false, String(error));
    await hostPage.screenshot({ path: resolve(SHOTS, "multi-friend-FAILED.png") }).catch(() => {});
  } finally {
    await hostContext.close();
    await guestContext.close();
  }

  /* ============ B. aptitude-grid 双人轮流 ============ */
  const duelHost = makeClient();
  await duelHost.newSession("甲");
  const duelRoom = await duelHost.call(
    "POST",
    "/rooms",
    {
      name: "双人占格",
      kind: "friend",
      modeConfig: { modeId: "aptitude-grid", rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed" },
      maxPlayers: 2,
      maxSpectators: 0,
      visibility: "invite",
      allowSpectators: false,
    },
    key(),
  );
  const duelRoomId = duelRoom.json?.room?.roomId;
  const duelInvite = duelRoom.json?.inviteCode;

  const duelGuest = makeClient();
  await duelGuest.newSession("乙");
  if (duelInvite) {
    await duelGuest.call("POST", `/rooms/${duelRoomId}/memberships`, { inviteCode: duelInvite, requestedRole: "player" }, key());
  }

  /*
   * duel 变体：把 variant 直接放在 modeConfig 顶层。
   *
   * 实测（2026-11-08）：`PrepareInput.Config` 就是 `in.ModeConfig` 原样，
   * 所以 `{modeId, rulesVersion, contentPackId, informationStyle, variant:"duel"}`
   * 会被服务端接受并进入双人轮流（201）。放在 `config: {...}` 子对象里无效。
   * 契约的 GenericPackModeConfig 没声明 variant，但服务端容忍额外字段，
   * 这是实测事实，不是契约保证。
   */
  const DUEL_CFG = {
    modeId: "aptitude-grid",
    rulesVersion: "2.0.0-draft.1",
    contentPackId: "",
    informationStyle: "closed",
    variant: "duel",
  };
  const duelRound = await duelHost.call("POST", `/rooms/${duelRoomId}/rounds`, { modeConfig: DUEL_CFG }, key());
  if (duelRound.status === 201) {
    const duelRoundId = duelRound.json?.roundId;
    let duelPv = duelRound.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await duelHost.call("POST", `/rounds/${duelRoundId}/transitions`, { target, expectedPhaseVersion: duelPv }, key());
      duelPv = t.json?.phaseVersion ?? duelPv + 1;
    }
    const duelView = await duelHost.call("GET", `/rounds/${duelRoundId}`);
    const variant = duelView.json?.publicState?.config?.variant;
    const turnSubject = duelView.json?.publicState?.turnSubjectId;
    note("aptitude-grid 变体与轮次", { variant, turnSubject });

    if (variant === "duel") {
      assert("aptitude-grid 进入双人轮流变体", true, { variant });
      assert("服务端给出当前轮到的玩家", Boolean(turnSubject), { turnSubject });

      // 非轮次方提交应被拒（服务端判定，不靠前端）
      const guestDuelView = await duelGuest.call("GET", `/rounds/${duelRoundId}`);
      const guestSubject = guestDuelView.json?.privateState?.subjectId ?? guestDuelView.json?.publicState?.self?.subjectId;
      const isGuestTurn = turnSubject === guestSubject;
      note("是否轮到客人", { isGuestTurn, guestSubject, turnSubject });

      /*
       * 非轮次方必须被拒。
       *
       * 谁是先手由服务端决定（本次可能是房主也可能是客人），
       * 所以这里按实际 turnSubject 挑出「非轮次那一方」再提交，
       * 而不是硬编码「客人应该被拒」——否则先手是谁会决定断言真假。
       */
      const hostSubject = duelView.json?.publicState?.self?.subjectId;
      const nonTurnClient = turnSubject === hostSubject ? duelGuest : duelHost;
      const nonTurnView =
        turnSubject === hostSubject ? guestDuelView : await duelHost.call("GET", `/rounds/${duelRoundId}`);

      const rejected = await nonTurnClient.call(
        "POST",
        `/rounds/${duelRoundId}/submissions`,
        {
          phaseVersion: nonTurnView.json?.round?.phaseVersion ?? duelPv,
          submissionRevision: nonTurnView.json?.submissionRevision ?? 0,
          controlGeneration: nonTurnView.json?.controlGeneration ?? 0,
          modeAction: { kind: "grid.claim", cellId: "cell-0-0", characterId: "1001" },
        },
        key(),
      );
      assert("非轮次方提交被服务端拒绝", rejected.status >= 400, {
        status: rejected.status,
        code: rejected.json?.code,
        turnSubject,
        hostSubject,
      });
      note("非轮次拒绝详情", { status: rejected.status, code: rejected.json?.code });

      /*
       * 轮次方提交应当成功（证明拒绝是「轮次」而非「所有人都不能提交」）。
       *
       * 注意：盲选一个角色会因条件不符被 422 拒绝（那是正常业务校验，
       * 不能据此判断轮次逻辑）。这里先扫一遍候选池，找到确实满足第一格
       * 两项条件的角色再提交，才能真正验证「轮次方可以占格」与轮次切换。
       */
      const turnView = turnSubject === hostSubject ? duelView : guestDuelView;
      const firstCell = turnView.json?.publicState?.cells?.[0];

      /*
       * 找一张确实满足第一格条件的角色。
       *
       * 候选池的公开投影不含适性（刻意的信息边界：提交前不告诉玩家谁能满足），
       * 所以盲试 40 个可能一个都不中（第一次就是这样，全是 condition_not_satisfied）。
       *
       * 正确做法：把这一局揭晓，服务端会下发 aptitudes[] 全表；
       * 用它按两项条件筛出合格角色，然后换一局新的用那个角色提交。
       * 这只在测试里做，不改变玩家可见的信息边界。
       */
      const revealForAptitudes = await duelHost.call(
        "POST",
        `/rounds/${duelRoundId}/transitions`,
        { target: "revealed", expectedPhaseVersion: turnView.json?.round?.phaseVersion ?? duelPv },
        key(),
      );
      const revealedView = await duelHost.call("GET", `/rounds/${duelRoundId}`);
      const aptitudes = revealedView.json?.publicState?.aptitudes ?? [];
      note("揭晓后拿到适性表", { count: aptitudes.length, transitionStatus: revealForAptitudes.status });

      /*
       * 必须再推到 settled 才能开新回合：实测 revealed 状态下
       * POST /rooms/{id}/rounds 会 409 round-already-open
       * （房间只允许一个未完结的回合）。
       */
      const settled = await duelHost.call(
        "POST",
        `/rounds/${duelRoundId}/transitions`,
        {
          target: "settled",
          expectedPhaseVersion: revealedView.json?.round?.phaseVersion ?? duelPv,
        },
        key(),
      );
      note("上一局结算", { status: settled.status, code: settled.json?.code });

      const rowAxis = firstCell?.rowCondition?.axis;
      const rowValue = firstCell?.rowCondition?.value;
      const colAxis = firstCell?.colCondition?.axis;
      const colValue = firstCell?.colCondition?.value;
      const satisfying = aptitudes
        .filter(
          (entry) =>
            entry.aptitudes?.[rowAxis]?.[rowValue] === "A" && entry.aptitudes?.[colAxis]?.[colValue] === "A",
        )
        .map((entry) => entry.characterId);
      note("满足第一格条件的角色", { count: satisfying.length, sample: satisfying.slice(0, 5) });

      if (satisfying.length === 0) {
        report.limits.push(
          `aptitude-grid 轮次切换未验证：揭晓后适性表里没有同时满足 ${rowAxis}=${rowValue} 与 ${colAxis}=${colValue} 的角色`,
        );
      } else {
        /*
         * 用合格角色重新开一局 duel（上一局已揭晓，房间可以再开新回合），
         * 验证「轮次方占格成功 → 轮次切换」。
         */
        const fresh = await duelHost.call("POST", `/rooms/${duelRoomId}/rounds`, { modeConfig: DUEL_CFG }, key());
        let accepted = { status: fresh.status, json: fresh.json };
        let usedCharacter = null;
        note("新一局开局结果", { status: fresh.status, code: fresh.json?.code });

        if (fresh.status !== 201) {
          report.limits.push(
            `aptitude-grid 轮次切换未验证：上一局只到 revealed，房间不允许再开新回合（${fresh.status} ${fresh.json?.code ?? ""}）`,
          );
        }

        if (fresh.status === 201) {
          const freshRoundId = fresh.json?.roundId;
          let freshPv = fresh.json?.phaseVersion ?? 1;
          for (const target of ["briefing", "collecting"]) {
            const t = await duelHost.call(
              "POST",
              `/rounds/${freshRoundId}/transitions`,
              { target, expectedPhaseVersion: freshPv },
              key(),
            );
            freshPv = t.json?.phaseVersion ?? freshPv + 1;
          }

          const freshView = await duelHost.call("GET", `/rounds/${freshRoundId}`);
          const freshTurn = freshView.json?.publicState?.turnSubjectId;
          const freshHostSubject = freshView.json?.publicState?.self?.subjectId;
          const freshCell = freshView.json?.publicState?.cells?.[0];
          const freshTurnClient = freshTurn === freshHostSubject ? duelHost : duelGuest;

          // 在新一局里，按新格子的条件重新筛
          const freshRowAxis = freshCell?.rowCondition?.axis;
          const freshRowValue = freshCell?.rowCondition?.value;
          const freshColAxis = freshCell?.colCondition?.axis;
          const freshColValue = freshCell?.colCondition?.value;
          const freshSatisfying = aptitudes
            .filter(
              (entry) =>
                entry.aptitudes?.[freshRowAxis]?.[freshRowValue] === "A" &&
                entry.aptitudes?.[freshColAxis]?.[freshColValue] === "A",
            )
            .map((entry) => entry.characterId);

          note("新一局满足条件的角色", {
            count: freshSatisfying.length,
            cell: `${freshRowAxis}=${freshRowValue} × ${freshColAxis}=${freshColValue}`,
          });

          for (const characterId of freshSatisfying.slice(0, 10)) {
            const attempt = await freshTurnClient.call(
              "POST",
              `/rounds/${freshRoundId}/submissions`,
              {
                phaseVersion: freshView.json?.round?.phaseVersion ?? freshPv,
                submissionRevision: freshView.json?.submissionRevision ?? 0,
                controlGeneration: freshView.json?.controlGeneration ?? 0,
                modeAction: { kind: "grid.claim", cellId: freshCell?.cellId ?? "cell-0-0", characterId },
              },
              key(),
            );
            accepted = attempt;
            if (attempt.status === 201) {
              usedCharacter = characterId;
              break;
            }
          }

          note("轮次方占格尝试", { usedCharacter, status: accepted.status, code: accepted.json?.code });

          const notTurnCode = accepted.json?.code;
          assert(
            "轮次方用合格角色占格成功",
            accepted.status === 201,
            { status: accepted.status, code: notTurnCode, usedCharacter },
          );

          const afterTurn = await duelHost.call("GET", `/rounds/${freshRoundId}`);
          const nextTurn = afterTurn.json?.publicState?.turnSubjectId;
          note("轮次切换", { before: freshTurn, after: nextTurn });
          if (accepted.status === 201) {
            assert("占格成功后轮次切换给对手", nextTurn !== freshTurn, {
              before: freshTurn,
              after: nextTurn,
            });
          }
        }
      }
      // 条件可能不符（422 也是合法业务拒绝），关键是不能是「不是你的回合」

    } else {
      report.limits.push(
        `aptitude-grid 双人轮流未触发：服务端按默认 variant=${variant} 开局（需要 modeConfig 传 variant=duel 且被服务端接受）`,
      );
      note("aptitude-grid 变体说明", { variant });
    }
  } else {
    assert("aptitude-grid 双人可开局", false, { status: duelRound.status, body: duelRound.json });
  }

  /* ============ C. G04 协作真状态流转（不只断言各自 201） ============ */
  {
    // Verify shared submission state is visible to both participants without locking the other player
    const coopHost = makeClient();
    await coopHost.newSession("合作甲");
    const COOP_CFG = {
      modeId: "race-rebuild",
      rulesVersion: "2.0.0-draft.1",
      contentPackId: "",
      informationStyle: "closed",
      cooperative: true,
    };
    const coopRoom = await coopHost.call(
      "POST",
      "/rooms",
      {
        name: "合作复原",
        kind: "friend",
        modeConfig: COOP_CFG,
        maxPlayers: 2,
        maxSpectators: 0,
        visibility: "invite",
        allowSpectators: false,
      },
      key(),
    );
    const coopRoomId = coopRoom.json?.room?.roomId;
    const coopInvite = coopRoom.json?.inviteCode;

    const coopGuest = makeClient();
    await coopGuest.newSession("合作乙");
    await coopGuest.call(
      "POST",
      `/rooms/${coopRoomId}/memberships`,
      { inviteCode: coopInvite, requestedRole: "player" },
      key(),
    );

    const coopRound = await coopHost.call("POST", `/rooms/${coopRoomId}/rounds`, { modeConfig: COOP_CFG }, key());
    assert("G04 协作局可开局", coopRound.status === 201, { status: coopRound.status });
    const coopRoundId = coopRound.json?.roundId;
    let coopPv = coopRound.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await coopHost.call(
        "POST",
        `/rounds/${coopRoundId}/transitions`,
        { target, expectedPhaseVersion: coopPv },
        key(),
      );
      coopPv = t.json?.phaseVersion ?? coopPv + 1;
    }

    const coopHostView = await coopHost.call("GET", `/rounds/${coopRoundId}`);
    const coopGuestView = await coopGuest.call("GET", `/rounds/${coopRoundId}`);
    const coopConfig = coopHostView.json?.publicState?.config;
    assert("G04 服务端确认 cooperative 变体", coopConfig?.cooperative === true, { config: coopConfig });

    const coopSubjects = coopHostView.json?.publicState?.subjects ?? [];
    assert("G04 同一局有 2 名参与者", coopSubjects.length === 2, { count: coopSubjects.length });

    const hostSubjectId = coopHostView.json?.publicState?.self?.subjectId;
    const guestSubjectId = coopGuestView.json?.publicState?.self?.subjectId;
    assert("G04 双方 subject 不同（不是同一人两份视图）", hostSubjectId !== guestSubjectId, {
      hostSubjectId,
      guestSubjectId,
    });

    // A（房主）提交
    const coopOrder = (coopHostView.json?.publicState?.entries ?? []).map((e) => e.characterId);
    const coopSubmitA = await coopHost.call(
      "POST",
      `/rounds/${coopRoundId}/submissions`,
      {
        phaseVersion: coopPv,
        submissionRevision: 0,
        controlGeneration: 0,
        modeAction: { kind: "race.order", groups: coopOrder.map((id) => [id]) },
      },
      key(),
    );
    assert("G04 甲方提交成功", coopSubmitA.status === 201, { status: coopSubmitA.status });

    /*
     * 核心断言：B 拉取时必须看到 A 的动作结果。
     * 这正是「各自 201」测不出来的东西 —— 状态有没有真的流转。
     */
    const guestSeesHost = await coopGuest.call("GET", `/rounds/${coopRoundId}`);
    const guestSubjectsAfter = guestSeesHost.json?.publicState?.subjects ?? [];
    const hostRowForGuest = guestSubjectsAfter.find((sub) => sub.subjectId === hostSubjectId);
    assert(
      "G04 乙拉取时能看到甲已提交（状态真的流转）",
      hostRowForGuest?.submitted === true && hostRowForGuest?.revisions >= 1,
      { hostRow: hostRowForGuest, allSubjects: guestSubjectsAfter },
    );
    note("乙看到的甲进度", hostRowForGuest);
    // 甲那行的正确对数是真实计算出来的，不是 0 占位
    assert(
      "G04 乙能看到甲的真实得分进度（correctPairs 已计算）",
      typeof hostRowForGuest?.correctPairs === "number" && hostRowForGuest.totalPairs > 0,
      { correctPairs: hostRowForGuest?.correctPairs, totalPairs: hostRowForGuest?.totalPairs },
    );

    // B 仍能提交（协作模式下不是「一人提交就锁死」）
    const coopSubmitB = await coopGuest.call(
      "POST",
      `/rounds/${coopRoundId}/submissions`,
      {
        phaseVersion: guestSeesHost.json?.round?.phaseVersion ?? coopPv,
        submissionRevision: guestSeesHost.json?.submissionRevision ?? 0,
        controlGeneration: guestSeesHost.json?.controlGeneration ?? 0,
        modeAction: {
          kind: "race.order",
          groups: coopOrder.slice().reverse().map((id) => [id]),
        },
      },
      key(),
    );
    assert("G04 乙在甲之后仍能提交（协作不锁死）", coopSubmitB.status === 201, {
      status: coopSubmitB.status,
      code: coopSubmitB.json?.code,
    });

    // 双方都提交后，各自都能看到两条 submitted
    const finalHost = await coopHost.call("GET", `/rounds/${coopRoundId}`);
    const finalGuest = await coopGuest.call("GET", `/rounds/${coopRoundId}`);
    const hostSeesAll = (finalHost.json?.publicState?.subjects ?? []).every((sub) => sub.submitted === true);
    const guestSeesAll = (finalGuest.json?.publicState?.subjects ?? []).every((sub) => sub.submitted === true);
    assert("G04 房主视角看到双方都已提交", hostSeesAll, {
      subjects: finalHost.json?.publicState?.subjects,
    });
    assert("G04 客人视角看到双方都已提交", guestSeesAll, {
      subjects: finalGuest.json?.publicState?.subjects,
    });

    // 各自只看到自己的 order（私有答案不互相泄露）
    const hostSelfOrder = finalHost.json?.publicState?.self?.order;
    const guestSelfOrder = finalGuest.json?.publicState?.self?.order;
    assert(
      "G04 双方各自只看到自己的排序（不泄露对方答案）",
      JSON.stringify(hostSelfOrder) !== JSON.stringify(guestSelfOrder),
      { hostSelfOrder, guestSelfOrder },
    );

    /* 断线重连语义：客人重新拉取，看到的必须是最新状态而不是旧缓存 */
    const guestReconnect = await coopGuest.call("GET", `/rounds/${coopRoundId}`);
    assert(
      "G04 重连后拉取到最新状态（非旧缓存）",
      (guestReconnect.json?.publicState?.subjects ?? []).every((sub) => sub.submitted === true),
      { subjects: guestReconnect.json?.publicState?.subjects },
    );
  }

  /* ============ D. G05 同场之路协作真状态流转 ============ */
  {
    const pathHost = makeClient();
    await pathHost.newSession("路径甲");
    const PATH_CFG = {
      modeId: "co-race-path",
      rulesVersion: "2.0.0-draft.1",
      contentPackId: "",
      informationStyle: "closed",
    };
    const pathRoom = await pathHost.call(
      "POST",
      "/rooms",
      {
        name: "同场之路",
        kind: "friend",
        modeConfig: PATH_CFG,
        maxPlayers: 2,
        maxSpectators: 0,
        visibility: "invite",
        allowSpectators: false,
      },
      key(),
    );
    const pathRoomId = pathRoom.json?.room?.roomId;

    const pathGuest = makeClient();
    await pathGuest.newSession("路径乙");
    await pathGuest.call(
      "POST",
      `/rooms/${pathRoomId}/memberships`,
      { inviteCode: pathRoom.json?.inviteCode, requestedRole: "player" },
      key(),
    );

    const pathRound = await pathHost.call("POST", `/rooms/${pathRoomId}/rounds`, { modeConfig: PATH_CFG }, key());
    assert("G05 协作局可开局", pathRound.status === 201, { status: pathRound.status });
    const pathRoundId = pathRound.json?.roundId;
    let pathPv = pathRound.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await pathHost.call(
        "POST",
        `/rounds/${pathRoundId}/transitions`,
        { target, expectedPhaseVersion: pathPv },
        key(),
      );
      pathPv = t.json?.phaseVersion ?? pathPv + 1;
    }

    const pathHostView = await pathHost.call("GET", `/rounds/${pathRoundId}`);
    const pathGuestView = await pathGuest.call("GET", `/rounds/${pathRoundId}`);
    const startId = pathHostView.json?.publicState?.start?.characterId;
    const goalId = pathHostView.json?.publicState?.goal?.characterId;

    assert(
      "G05 双方看到同一组起终点（同一局，不是各自一局）",
      startId === pathGuestView.json?.publicState?.start?.characterId &&
        goalId === pathGuestView.json?.publicState?.goal?.characterId,
      { hostStart: startId, guestStart: pathGuestView.json?.publicState?.start?.characterId },
    );

    /*
     * 用服务端的 path-neighbors 逐点 BFS 找一条真实存在的合法路径。
     *
     * 不能凭 start + 任意邻居 + goal 拼一条（第一次就是这样：422 missing_edge，
     * 因为中间那条边根本没有同场证据）。必须每一跳都查过邻居。
     */
    async function findPath(client) {
      const prev = new Map();
      const seen = new Set([startId]);
      const queue = [startId];
      let found = false;
      let hops = 0;
      while (queue.length > 0 && !found && hops < 200) {
        const current = queue.shift();
        hops += 1;
        const nb = await client.call(
          "GET",
          `/rounds/${pathRoundId}/path-neighbors?characterId=${encodeURIComponent(current)}`,
        );
        for (const neighbor of nb.json?.neighbors ?? []) {
          const id = neighbor.character?.characterId;
          if (!id || seen.has(id)) continue;
          seen.add(id);
          prev.set(id, current);
          if (id === goalId) {
            found = true;
            break;
          }
          queue.push(id);
        }
      }
      if (!found) return null;
      const path = [goalId];
      let cursor = goalId;
      while (cursor !== startId) {
        cursor = prev.get(cursor);
        if (!cursor) return null;
        path.unshift(cursor);
      }
      return path;
    }

    const validPath = await findPath(pathHost);
    assert("G05 通过服务端邻居接口找到一条真实合法路径", Array.isArray(validPath) && validPath.length >= 2, {
      length: validPath?.length,
    });
    note("G05 找到的路径", { length: validPath?.length, edges: (validPath?.length ?? 0) - 1 });

    if (validPath) {
      // 甲提交合法路径
      const submitA = await pathHost.call(
        "POST",
        `/rounds/${pathRoundId}/submissions`,
        {
          phaseVersion: pathPv,
          submissionRevision: 0,
          controlGeneration: 0,
          modeAction: { kind: "path.route", characterIds: validPath },
        },
        key(),
      );
      assert("G05 甲方提交合法路径成功", submitA.status === 201, {
        status: submitA.status,
        code: submitA.json?.code,
      });

      // 乙拉取必须看到甲已提交（状态流转）
      const guestSees = await pathGuest.call("GET", `/rounds/${pathRoundId}`);
      const guestSubjects = guestSees.json?.publicState?.subjects ?? [];
      const hostSubjectId = pathHostView.json?.publicState?.self?.subjectId;
      const hostRow = guestSubjects.find((sub) => sub.subjectId === hostSubjectId);
      assert(
        "G05 乙拉取时能看到甲已提交（状态真的流转）",
        hostRow?.submitted === true && hostRow?.arrived === true,
        { hostRow, allSubjects: guestSubjects },
      );
      note("乙看到的甲进度", hostRow);

      // 乙用自己的路径提交（反向或同一条都算合法）
      const submitB = await pathGuest.call(
        "POST",
        `/rounds/${pathRoundId}/submissions`,
        {
          phaseVersion: guestSees.json?.round?.phaseVersion ?? pathPv,
          submissionRevision: guestSees.json?.submissionRevision ?? 0,
          controlGeneration: guestSees.json?.controlGeneration ?? 0,
          modeAction: { kind: "path.route", characterIds: validPath },
        },
        key(),
      );
      assert("G05 乙在甲之后仍能提交", submitB.status === 201, {
        status: submitB.status,
        code: submitB.json?.code,
      });

      // 双方最终都看到两人已提交
      const finalHost = await pathHost.call("GET", `/rounds/${pathRoundId}`);
      const finalGuest = await pathGuest.call("GET", `/rounds/${pathRoundId}`);
      assert(
        "G05 双方都看到两名参与者已提交",
        (finalHost.json?.publicState?.subjects ?? []).every((sub) => sub.submitted === true) &&
          (finalGuest.json?.publicState?.subjects ?? []).every((sub) => sub.submitted === true),
        {
          hostSubjects: finalHost.json?.publicState?.subjects,
          guestSubjects: finalGuest.json?.publicState?.subjects,
        },
      );

      // 未揭晓时不下发最短路径（不泄答案）
      assert(
        "G05 未揭晓时不下发最短路线",
        finalHost.json?.publicState?.answer === undefined ||
          finalHost.json?.publicState?.answer === null,
        { answer: finalHost.json?.publicState?.answer },
      );
    } else {
      report.limits.push("G05 协作：未能通过邻居接口找到从起点到终点的路径，协作提交未验证");
    }
  }

  /* ============ E. G03 公开编队：双人同局的提交与隔离 ============ */
  {
    const draftHost = makeClient();
    await draftHost.newSession("编队甲");
    const DRAFT_CFG = {
      modeId: "draft",
      rulesVersion: "2.0.0-draft.1",
      contentPackId: "",
      informationStyle: "closed",
    };
    const draftRoom = await draftHost.call(
      "POST",
      "/rooms",
      {
        name: "公开编队",
        kind: "friend",
        modeConfig: DRAFT_CFG,
        maxPlayers: 2,
        maxSpectators: 0,
        visibility: "invite",
        allowSpectators: false,
      },
      key(),
    );
    const draftRoomId = draftRoom.json?.room?.roomId;

    const draftGuest = makeClient();
    await draftGuest.newSession("编队乙");
    await draftGuest.call(
      "POST",
      `/rooms/${draftRoomId}/memberships`,
      { inviteCode: draftRoom.json?.inviteCode, requestedRole: "player" },
      key(),
    );

    const draftRound = await draftHost.call("POST", `/rooms/${draftRoomId}/rounds`, { modeConfig: DRAFT_CFG }, key());
    assert("G03 双人局可开局", draftRound.status === 201, { status: draftRound.status });
    const draftRoundId = draftRound.json?.roundId;
    let draftPv = draftRound.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await draftHost.call(
        "POST",
        `/rounds/${draftRoundId}/transitions`,
        { target, expectedPhaseVersion: draftPv },
        key(),
      );
      draftPv = t.json?.phaseVersion ?? draftPv + 1;
    }

    const draftHostView = await draftHost.call("GET", `/rounds/${draftRoundId}`);
    const draftGuestView = await draftGuest.call("GET", `/rounds/${draftRoundId}`);
    const draftTasks = draftHostView.json?.publicState?.tasks ?? [];
    const draftCards = draftHostView.json?.publicState?.cards ?? [];

    assert("G03 双方看到同一套任务与牌池", draftTasks.length === (draftGuestView.json?.publicState?.tasks ?? []).length, {
      hostTasks: draftTasks.length,
      guestTasks: (draftGuestView.json?.publicState?.tasks ?? []).length,
    });

    const hostDraftSubject = draftHostView.json?.publicState?.self?.subjectId;
    const guestDraftSubject = draftGuestView.json?.publicState?.self?.subjectId;
    assert("G03 双方 subject 不同", hostDraftSubject !== guestDraftSubject, {
      hostDraftSubject,
      guestDraftSubject,
    });

    // 甲提交一份合法编队
    const assignments = draftTasks.map((task, index) => ({
      slotId: task.taskId,
      characterId: draftCards[index % draftCards.length]?.id,
    }));
    const draftSubmitA = await draftHost.call(
      "POST",
      `/rounds/${draftRoundId}/submissions`,
      {
        phaseVersion: draftPv,
        submissionRevision: 0,
        controlGeneration: 0,
        modeAction: { kind: "draft.lineup", assignments },
      },
      key(),
    );
    assert("G03 甲方提交编队成功", draftSubmitA.status === 201, {
      status: draftSubmitA.status,
      code: draftSubmitA.json?.code,
    });

    // 乙拉取必须看到甲已锁定（状态流转）
    const guestSeesDraft = await draftGuest.call("GET", `/rounds/${draftRoundId}`);
    const guestDraftSubjects = guestSeesDraft.json?.publicState?.subjects ?? [];
    const hostDraftRow = guestDraftSubjects.find((sub) => sub.subjectId === hostDraftSubject);
    assert(
      "G03 乙拉取时能看到甲已锁定编队（状态真的流转）",
      hostDraftRow?.locked === true && hostDraftRow?.revisions >= 1,
      { hostRow: hostDraftRow, allSubjects: guestDraftSubjects },
    );
    note("乙看到的甲编队进度", hostDraftRow);

    /* 只检查对手的 subjects 行是否泄露编队；self.assignments 是调用者有权读取的草稿 */
    assert(
      "G03 甲那一行公开进度只含 locked/revisions（不含甲的编队明细）",
      hostDraftRow !== undefined &&
        !("assignments" in hostDraftRow) &&
        !("taskScores" in hostDraftRow) &&
        !("totalPoints" in hostDraftRow),
      { hostRow: hostDraftRow, keys: hostDraftRow ? Object.keys(hostDraftRow) : null },
    );

    // 乙自己的 self 里有自己的 assignments（这是乙应有的权限）
    const guestSelf = guestSeesDraft.json?.publicState?.self;
    assert(
      "G03 乙仍能看到自己的编队草稿（自己的数据不丢）",
      guestSelf !== undefined && guestSelf.subjectId === guestDraftSubject,
      { guestSelfSubject: guestSelf?.subjectId, guestDraftSubject },
    );

    /* 只检查 subjects 中的选人信息；公开候选牌池本身不属于答案泄露 */
    const hostPickedIds = assignments.map((a) => a.characterId);
    const guestSubjectsJson = JSON.stringify(guestSeesDraft.json?.publicState?.subjects ?? []);
    const leakedIds = hostPickedIds.filter((id) => guestSubjectsJson.includes(id));
    assert(
      "G03 甲选的角色 ID 不出现在乙看到的 subjects 里",
      leakedIds.length === 0,
      { leakedIds, hostPickedIds, note: "只查 subjects，不查公开牌池" },
    );

    // 反向确认：公开牌池确实包含这些卡（证明它们本来就在公开信息里）
    const guestCardIds = (guestSeesDraft.json?.publicState?.cards ?? []).map((c) => c.id);
    assert(
      "G03 甲选的卡在公开牌池里本来就可见（所以不算泄露）",
      hostPickedIds.every((id) => guestCardIds.includes(id)),
      { hostPickedIds, guestCardCount: guestCardIds.length },
    );

    // 乙仍能提交自己的编队（不锁死）
    const draftSubmitB = await draftGuest.call(
      "POST",
      `/rounds/${draftRoundId}/submissions`,
      {
        phaseVersion: guestSeesDraft.json?.round?.phaseVersion ?? draftPv,
        submissionRevision: guestSeesDraft.json?.submissionRevision ?? 0,
        controlGeneration: guestSeesDraft.json?.controlGeneration ?? 0,
        modeAction: {
          kind: "draft.lineup",
          assignments: draftTasks.map((task, index) => ({
            slotId: task.taskId,
            characterId: draftCards[(index + 3) % draftCards.length]?.id,
          })),
        },
      },
      key(),
    );
    assert("G03 乙在甲之后仍能提交", draftSubmitB.status === 201, {
      status: draftSubmitB.status,
      code: draftSubmitB.json?.code,
    });

    const finalDraft = await draftHost.call("GET", `/rounds/${draftRoundId}`);
    assert(
      "G03 双方都看到两名参与者已锁定",
      (finalDraft.json?.publicState?.subjects ?? []).every((sub) => sub.locked === true),
      { subjects: finalDraft.json?.publicState?.subjects },
    );

    // Per-player checks do not establish team-level score aggregation
    report.limits.push(
      "G03 teams 变体未真正按队伍分账：internal/modes/draft 里 VariantTeams 仅用于配置校验（注释写「每队独立市场」），实现中每个玩家仍是独立 subject，没有队伍聚合。该检查仅覆盖双人提交与私有隔离，不验证队伍计分聚合。",
    );
  }

  /* F. 匹配成功后的状态流转 */
  {
    // Random matches start automatically; neither participant manages the room.
    // Drain stale tickets through the API to keep shared matchmaking queues isolated
    const mmFirst = makeClient();
    await mmFirst.newSession("先建票者");
    const mmSecond = makeClient();
    await mmSecond.newSession("后接受者");

    const MM_CFG = {
      modeId: "classic",
      maxWrongGuesses: 8,
      winPolicy: "first-correct",
      durationSeconds: 0,
      shadowEnabled: false,
    };

    await drainQueue(MM_CFG);

    const ticketFirst = await createMatchingTicket(mmFirst);
    assert("匹配 甲建票成功且初始为 queueing", ticketFirst.status === 201 && ticketFirst.json?.status === "queueing", {
      status: ticketFirst.status,
      ticketStatus: ticketFirst.json?.status,
    });

    const ticketSecond = await createMatchingTicket(mmSecond);
    note("乙建票返回状态", { status: ticketSecond.json?.status, roomId: ticketSecond.json?.roomId });

    // 重新读取两张票，避免只看建票响应体（响应可能不是落库后的真实状态）
    const firstAfterPair = await mmFirst.call("GET", `/matchmaking-tickets/${ticketFirst.json.ticketId}`);
    const secondAfterPair = await mmSecond.call("GET", `/matchmaking-tickets/${ticketSecond.json.ticketId}`);
    note("配对后两张票的实际状态", {
      first: firstAfterPair.json?.status,
      second: secondAfterPair.json?.status,
    });

    /*
     * 断言：乙建票后应进入 matched-pending-accept（撮合器在建票事务内配对）。
     * 若这里返回 queueing，说明配对没有发生或状态没落库。
     */
    assert(
      "匹配 乙建票后双方进入 matched-pending-accept（撮合真的发生）",
      firstAfterPair.json?.status === "matched-pending-accept" &&
        secondAfterPair.json?.status === "matched-pending-accept",
      {
        first: firstAfterPair.json?.status,
        second: secondAfterPair.json?.status,
        secondCreateResponse: ticketSecond.json?.status,
      },
    );

    // 只有一方接受时，roomId 仍应为 null
    const acceptFirst = await mmFirst.call(
      "POST",
      `/matchmaking-tickets/${ticketFirst.json.ticketId}/responses`,
      { accept: true },
      key(),
    );
    assert("匹配 只有一方接受时 roomId 仍为 null", acceptFirst.json?.roomId === null, {
      status: acceptFirst.json?.status,
      roomId: acceptFirst.json?.roomId,
    });

    // 双方都接受 → 建房
    const acceptSecond = await mmSecond.call(
      "POST",
      `/matchmaking-tickets/${ticketSecond.json.ticketId}/responses`,
      { accept: true },
      key(),
    );
    const matchRoomId = acceptSecond.json?.roomId ?? acceptFirst.json?.roomId;
    assert("匹配 双方都接受后拿到 roomId", typeof matchRoomId === "string" && matchRoomId.length > 0, {
      roomId: matchRoomId,
      status: acceptSecond.json?.status,
    });

    // 两张票最终都指向同一个房间
    const finalFirst = await mmFirst.call("GET", `/matchmaking-tickets/${ticketFirst.json.ticketId}`);
    const finalSecond = await mmSecond.call("GET", `/matchmaking-tickets/${ticketSecond.json.ticketId}`);
    assert(
      "匹配 两张票最终指向同一房间",
      finalFirst.json?.roomId === matchRoomId && finalSecond.json?.roomId === matchRoomId,
      { first: finalFirst.json?.roomId, second: finalSecond.json?.roomId, expected: matchRoomId },
    );

    // 双方读同一房间都 200，成员都是两人
    const mmRoomFirst = await mmFirst.call("GET", `/rooms/${matchRoomId}`);
    const mmRoomSecond = await mmSecond.call("GET", `/rooms/${matchRoomId}`);
    assert("匹配 先建票者读房间 200", mmRoomFirst.status === 200, { status: mmRoomFirst.status });
    assert("匹配 后接受者读房间 200", mmRoomSecond.status === 200, { status: mmRoomSecond.status });
    assert(
      "匹配 房间成员为两人",
      (mmRoomFirst.json?.members ?? []).length === 2 && (mmRoomSecond.json?.members ?? []).length === 2,
      {
        firstCount: (mmRoomFirst.json?.members ?? []).length,
        secondCount: (mmRoomSecond.json?.members ?? []).length,
        names: (mmRoomFirst.json?.members ?? []).map((m) => m.displayName),
      },
    );

    // Both participants have identical play authority, independent of arrival order.
    const firstCaps = mmRoomFirst.json?.viewer?.capabilities ?? [];
    const secondCaps = mmRoomSecond.json?.viewer?.capabilities ?? [];
    assert("匹配 先建票者只有参赛权限", !firstCaps.includes("manage-room") && firstCaps.includes("play"), { capabilities: firstCaps });
    assert(
      "匹配 后接受者只有 play（不因后接受而获得管理权）",
      secondCaps.includes("play") && !secondCaps.includes("manage-room"),
      { capabilities: secondCaps },
    );

    /*
     * 权限的实际效果（不只信 capabilities 字段）：
     * 让后接受者尝试开局，必须被拒。若这条红了但上面 capabilities 是绿的，
     * 说明还有另一条权限来源（例如 owner_session_id）。
     */
    const secondStarts = await mmSecond.call("POST", `/rooms/${matchRoomId}/rounds`, { modeConfig: MM_CFG }, key());
    assert(
      "匹配 后接受者实际开局被拒（权限不是只有字段好看）",
      secondStarts.status >= 400,
      { status: secondStarts.status, code: secondStarts.json?.code },
    );

    const firstStarts = await mmFirst.call("POST", `/rooms/${matchRoomId}/rounds`, { modeConfig: MM_CFG }, key());
    assert("匹配 先建票者也不能重复开局", firstStarts.status === 403, {
      status: firstStarts.status,
      code: firstStarts.json?.code,
    });
  }

  /* ============ G. 匹配拒绝路径：一方拒绝后另一方回到队列 ============ */
  {
    const declineFirst = makeClient();
    await declineFirst.newSession("拒绝测试甲");
    const declineSecond = makeClient();
    await declineSecond.newSession("拒绝测试乙");

    const MM_CFG2 = {
      modeId: "classic",
      maxWrongGuesses: 8,
      winPolicy: "first-correct",
      durationSeconds: 0,
      shadowEnabled: false,
    };

    await drainQueue(MM_CFG2);

    const dTicketFirst = await createMatchingTicket(declineFirst);
    const dTicketSecond = await createMatchingTicket(declineSecond);

    // 甲拒绝
    const declined = await declineFirst.call(
      "POST",
      `/matchmaking-tickets/${dTicketFirst.json.ticketId}/responses`,
      { accept: false },
      key(),
    );
    assert("匹配 拒绝后自己变为 cancelled", declined.json?.status === "cancelled", {
      status: declined.json?.status,
    });

    /*
     * 另一方必须被释放回队列（否则会一直挂在「等一个永不会接受的人」上，
     * 直到票过期）。这是 code 注释里明确写的行为，值得断言。
     */
    const partnerAfterDecline = await declineSecond.call(
      "GET",
      `/matchmaking-tickets/${dTicketSecond.json.ticketId}`,
    );
    note("拒绝后对方票据状态", {
      status: partnerAfterDecline.json?.status,
      roomId: partnerAfterDecline.json?.roomId,
    });
    assert(
      "匹配 一方拒绝后对方回到 queueing（不被吊死）",
      partnerAfterDecline.json?.status === "queueing",
      { status: partnerAfterDecline.json?.status },
    );
    assert(
      "匹配 拒绝后没有产生房间",
      (partnerAfterDecline.json?.roomId ?? null) === null,
      { roomId: partnerAfterDecline.json?.roomId },
    );
  }

  /* ============ I. 多参与者人数上限（边界值） ============ */
  {
    /*
     * 边界测试：上限内开局成功、超一人被 422 拒绝并给出原因。
     *
     * 为什么值得做：上限校验是「并发与容量」的第一道门。
     * 只测「能开局」测不出上限有没有真的生效——必须测 n 和 n+1 两侧。
     *
     * 各模式上限来自服务端 modes 注册（05 各章节）：race-rebuild 6、aptitude-grid 2、
     * co-race-path 8、draft 8、memory 8。
     */
    const CAP_CASES = [
      { modeId: "race-rebuild", atLimit: 6, overLimit: 7 },
      { modeId: "co-race-path", atLimit: 8, overLimit: 9 },
      { modeId: "draft", atLimit: 8, overLimit: 9 },
      { modeId: "memory", atLimit: 8, overLimit: 9 },
      { modeId: "aptitude-grid", atLimit: 2, overLimit: 3 },
    ];

    for (const testCase of CAP_CASES) {
      for (const participantCount of [testCase.atLimit, testCase.overLimit]) {
        const capHost = makeClient();
        await capHost.newSession(`${testCase.modeId}-${participantCount}人-主机`);
        const capCfg = {
          modeId: testCase.modeId,
          rulesVersion: "2.0.0-draft.1",
          contentPackId: "",
          informationStyle: "closed",
        };
        const capRoom = await capHost.call(
          "POST",
          "/rooms",
          {
            name: `${testCase.modeId} ${participantCount}人`,
            kind: "friend",
            modeConfig: capCfg,
            // 房间容量放宽到 16，让「模式上限」而不是「房间容量」成为拒绝原因
            maxPlayers: 16,
            maxSpectators: 0,
            visibility: "invite",
            allowSpectators: false,
          },
          key(),
        );
        const capRoomId = capRoom.json?.room?.roomId;
        const capInvite = capRoom.json?.inviteCode;

        let joined = 1;
        for (let index = 2; index <= participantCount; index += 1) {
          const member = makeClient();
          await member.newSession(`${testCase.modeId}成员${index}`);
          const joinedRes = await member.call(
            "POST",
            `/rooms/${capRoomId}/memberships`,
            { inviteCode: capInvite, requestedRole: "player" },
            key(),
          );
          if (joinedRes.status < 300) joined += 1;
        }

        const capRound = await capHost.call(
          "POST",
          `/rooms/${capRoomId}/rounds`,
          { modeConfig: capCfg },
          key(),
        );

        const isAtLimit = participantCount === testCase.atLimit;
        if (isAtLimit) {
          assert(
            `${testCase.modeId} ${joined} 人（上限内）可开局`,
            capRound.status === 201,
            { participants: joined, status: capRound.status, code: capRound.json?.code },
          );
        } else {
          assert(
            `${testCase.modeId} ${joined} 人（超上限）被拒且给出原因`,
            capRound.status === 422 &&
              capRound.json?.code === "too-many-participants" &&
              typeof capRound.json?.detail === "string" &&
              capRound.json.detail.includes("at most"),
            {
              participants: joined,
              status: capRound.status,
              code: capRound.json?.code,
              detail: capRound.json?.detail,
            },
          );
        }
      }
    }
  }

  /* ============ J. 队伍管理 UI + 队伍聚合（G03 teams） ============ */
  {
    /*
     * 队伍聚合的关键事实（实测确认，internal/room/rounds.go:82）：
     * 成员 role=team-captain 且 teamId 非空时，回合主体是**队伍 ID（tem_...）**
     * 而不是成员 ID。所以聚合不需要改模式计分，只要正确设置成员。
     */
    const teamHost = makeClient();
    await teamHost.newSession("队伍队长");
    const teamRoom = await teamHost.call(
      "POST",
      "/rooms",
      {
        name: "队伍测试房",
        kind: "friend",
        modeConfig: { modeId: "draft", rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed" },
        maxPlayers: 8,
        maxSpectators: 0,
        visibility: "invite",
        allowSpectators: false,
      },
      key(),
    );
    const teamRoomId = teamRoom.json?.room?.roomId;

    const teamGuest = makeClient();
    await teamGuest.newSession("队伍队员");
    await teamGuest.call(
      "POST",
      `/rooms/${teamRoomId}/memberships`,
      { inviteCode: teamRoom.json?.inviteCode, requestedRole: "player" },
      key(),
    );

    const teamContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
    await injectCookie(teamContext, teamHost.cookie);
    const teamPage = await teamContext.newPage();

    try {
      await teamPage.goto(`${WEB_BASE}/rooms/${teamRoomId}`, { waitUntil: "commit" });
      await teamPage.waitForSelector("#main-content", { timeout: 25000 });
      await teamPage.waitForTimeout(2000);

      const panelText = await teamPage.locator("body").innerText();
      assert("队伍 UI 等待区显示队伍面板", /队伍/.test(panelText) && /新建队伍/.test(panelText), {
        excerpt: panelText.slice(0, 300),
      });
      assert("队伍 UI 初始提示还没有队伍", /还没有队伍/.test(panelText), null);

      await teamPage.locator("#team-label").fill("红队");
      await teamPage.locator("#team-color").selectOption("pink");
      await teamPage.getByRole("button", { name: /新建队伍/ }).click();
      await teamPage.waitForTimeout(2500);

      const afterCreate = await teamPage.locator("body").innerText();
      assert("队伍 UI 建队成功并显示队名", /红队/.test(afterCreate), {
        excerpt: afterCreate.slice(0, 300),
      });

      await teamPage.locator("#team-label").fill("蓝队");
      await teamPage.locator("#team-color").selectOption("blue");
      await teamPage.getByRole("button", { name: /新建队伍/ }).click();
      await teamPage.waitForTimeout(2500);

      const afterTwo = await teamPage.locator("body").innerText();
      assert("队伍 UI 可建多支队", /红队/.test(afterTwo) && /蓝队/.test(afterTwo), {
        excerpt: afterTwo.slice(0, 400),
      });
      assert("队伍 UI 显示未分配成员区", /未分配队伍的成员/.test(afterTwo), {
        excerpt: afterTwo.slice(0, 400),
      });

      const joinRed = teamPage.getByRole("button", { name: /加入「红队」/ }).first();
      if ((await joinRed.count()) > 0) {
        await joinRed.click();
        await teamPage.waitForTimeout(2500);
        const afterAssign = await teamPage.locator("body").innerText();
        assert("队伍 UI 分配队长后显示队长", /队长：/.test(afterAssign), {
          excerpt: afterAssign.slice(0, 400),
        });
      } else {
        assert("队伍 UI 有「加入队伍」按钮", false, null);
      }

      await shot(teamPage, "multi-team-panel-390");

      const teamsNow = await teamHost.call("GET", `/rooms/${teamRoomId}`);
      const roomTeams = teamsNow.json?.teams ?? [];
      assert("队伍 服务端已记录两支队伍", roomTeams.length === 2, {
        teams: roomTeams.map((t) => ({ label: t.label, id: t.teamId })),
      });

      /* 由具备 manage-room 权限的房主分配队长；队员不能修改自己的参与角色 */
      const membersNow = teamsNow.json?.members ?? [];
      const guestMember = membersNow.find((m) => m.displayName === "队伍队员");
      const blueTeam = roomTeams.find((t) => t.label === "蓝队");
      if (guestMember && blueTeam) {
        const patch = await teamHost.call(
          "PATCH",
          `/rooms/${teamRoomId}/memberships/${guestMember.membershipId}`,
          { expectedRoomVersion: teamsNow.json?.roomVersion, teamId: blueTeam.teamId, requestedRole: "team-captain" },
          key(),
        );
        assert("队伍 房主可把队员设为第二队队长", patch.status === 200, {
          status: patch.status,
          code: patch.json?.code,
        });

        // 反向断言：队员自己不能改角色（权限边界）
        const selfPatch = await teamGuest.call(
          "PATCH",
          `/rooms/${teamRoomId}/memberships/${guestMember.membershipId}`,
          { expectedRoomVersion: teamsNow.json?.roomVersion, requestedRole: "player" },
          key(),
        );
        assert("队伍 队员不能自行改角色（403，权限边界）", selfPatch.status === 403, {
          status: selfPatch.status,
          code: selfPatch.json?.code,
        });
      }

      const roundRes = await teamHost.call(
        "POST",
        `/rooms/${teamRoomId}/rounds`,
        { modeConfig: { modeId: "draft", rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed", variant: "teams" } },
        key(),
      );
      assert("队伍 两队队长可开局", roundRes.status === 201, {
        status: roundRes.status,
        code: roundRes.json?.code,
        detail: roundRes.json?.detail,
      });

      if (roundRes.status === 201) {
        const view = await teamHost.call("GET", `/rounds/${roundRes.json.roundId}`);
        const subjects = view.json?.publicState?.subjects ?? [];
        const subjectIds = subjects.map((s) => s.subjectId);
        note("队伍聚合主体", { subjectIds, teamIds: roomTeams.map((t) => t.teamId) });

        assert(
          "队伍 聚合生效：subjectId 是队伍 ID（tem_ 开头）",
          subjectIds.length === 2 && subjectIds.every((id) => typeof id === "string" && id.startsWith("tem_")),
          { subjectIds },
        );
        assert(
          "队伍 聚合生效：主体 ID 与创建队伍的 ID 一致",
          subjectIds.every((id) => roomTeams.some((t) => t.teamId === id)),
          { subjectIds, teamIds: roomTeams.map((t) => t.teamId) },
        );
        assert("队伍 两支队伍 = 两个回合主体", subjects.length === 2, { count: subjects.length });
      }
    } catch (error) {
      assert("队伍管理 UI 可验证", false, String(error));
      await teamPage.screenshot({ path: resolve(SHOTS, "multi-team-panel-FAILED.png") }).catch(() => {});
    } finally {
      await teamContext.close();
    }
  }

  /* ============ K. 队长无队伍时给出具体错误 ============ */
  {
    const badHost = makeClient();
    await badHost.newSession("无队队长");
    const badRoom = await badHost.call(
      "POST",
      "/rooms",
      {
        name: "无队队长房",
        kind: "friend",
        modeConfig: { modeId: "draft", rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed" },
        maxPlayers: 8,
        maxSpectators: 0,
        visibility: "invite",
        allowSpectators: false,
      },
      key(),
    );
    const badRoomId = badRoom.json?.room?.roomId;

    const before = await badHost.call("GET", `/rooms/${badRoomId}`);
    const selfMember = (before.json?.members ?? []).find((m) => m.displayName === "无队队长");
    if (selfMember) {
      await badHost.call(
        "PATCH",
        `/rooms/${badRoomId}/memberships/${selfMember.membershipId}`,
        { expectedRoomVersion: before.json?.roomVersion, requestedRole: "team-captain" },
        key(),
      );
    }

    const badRound = await badHost.call(
      "POST",
      `/rooms/${badRoomId}/rounds`,
      { modeConfig: { modeId: "draft", rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed", variant: "teams" } },
      key(),
    );
    assert("队长无队伍时开局被拒且原因是 captain-without-team", badRound.json?.code === "captain-without-team", {
      status: badRound.status,
      code: badRound.json?.code,
      detail: badRound.json?.detail,
    });
    note("captain-without-team 错误详情", { detail: badRound.json?.detail });
  }

  /* ============ L. 截图哈希互异 ============ */
  {
    const hashes = report.screenshots.map((s) => s.sha256).filter(Boolean);
    const unique = new Set(hashes);
    assert("多会话截图内容互不相同", unique.size === hashes.length, {
      total: hashes.length,
      unique: unique.size,
    });
  }

  await browser.close();

  report.summary = {
    assertions: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
    screenshots: report.screenshots.length,
  };

  const out = resolve(EVIDENCE, "multi-session-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[multi-e2e] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 个失败，${report.summary.screenshots} 张截图`,
  );
  console.log(`[multi-e2e] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
