#!/usr/bin/env node
/** Checks simultaneous reconnection, state catch-up, ordered events and revoked subscriptions. */
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
/* Go slog 默认写入 stderr，此路径应指向服务端错误输出日志 */
const SERVER_LOG = process.env.UMA_SERVER_LOG ?? "/tmp/uma-server.err";

/**
 * 客户端分布。
 *
 * 关键约束：各模式的 MaxSubjects 都远小于 20（实测 classic=16、memory/draft/co-race-path=8、
 * sort/race-rebuild=6）。所以「单房间 20 个客户端」根本开不了局。
 *
 * 改用 2 个房间 × 8 人 = 16 个客户端，这也更接近真实场景
 * （真实活动通常是多个房间各有连接，不是一个房间塞满）。
 *
 * 20% 断线比例照旧：16 × 20% ≈ 3，取 4 保证至少每房间 2 个。
 */
const ROOM_COUNT = 2;
const PLAYERS_PER_ROOM = 8;
const TOTAL_CLIENTS = ROOM_COUNT * PLAYERS_PER_ROOM;
const DISCONNECT_COUNT = Math.max(ROOM_COUNT * 2, Math.round(TOTAL_CLIENTS * 0.2));

const report = {
  generatedAt: new Date().toISOString(),
  environment: { webBase: WEB_BASE, apiBase: API_BASE, node: process.version },
  config: {
    totalClients: TOTAL_CLIENTS,
    disconnectCount: DISCONNECT_COUNT,
    offlineSeconds: 8,
    roomCount: ROOM_COUNT,
    playersPerRoom: PLAYERS_PER_ROOM,
    note: "各模式 MaxSubjects 远小于 20（classic=16、memory/draft/co-race-path=8、sort/race-rebuild=6），故用多房间而非单房间塞满",
  },
  assertions: [],
  failures: [],
  screenshots: [],
  limits: [],
  recovery: { attempted: DISCONNECT_COUNT, recovered: 0, failed: 0, details: [] },
};

function assert(name, ok, detail) {
  const entry = { name, ok: Boolean(ok), detail };
  report.assertions.push(entry);
  if (ok) console.log(`[reconnect] ✓ ${name}`);
  else {
    report.failures.push(entry);
    console.error(`[reconnect] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(message, detail) {
  console.log(`[reconnect] · ${message}`, detail ? JSON.stringify(detail) : "");
}

async function resolveChromiumExecutable() {
  if (process.env.PLAYWRIGHT_CHROMIUM) return process.env.PLAYWRIGHT_CHROMIUM;
  const cacheRoot = join(homedir(), "Library/Caches/ms-playwright");
  if (!existsSync(cacheRoot)) return undefined;
  const { readdir } = await import("node:fs/promises");
  const dirs = (await readdir(cacheRoot))
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

function makeClient() {
  let cookie = "";
  let csrf = null;
  return {
    get cookie() {
      return cookie;
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
      return { status: out.res.status, json: out.json };
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
  winPolicy: "window-attempts",
  durationSeconds: 0,
  shadowEnabled: false,
};

/** 从页面读连接状态与房间版本 */
async function readClientState(page) {
  return page.evaluate(() => {
    const text = document.body.innerText;
    /* 连接提示匹配 connection-bar.tsx 的实际状态文案 */
    const connMatch = text.match(/已连接|连接中断|正在重连|可能已过期|已断开/);
    // 房间版本不在界面上显示，从 DOM 里找 data 属性或用 performance 记录
    return {
      connection: connMatch ? connMatch[0] : "未知",
      // 页面文本长度用于检测重复渲染
      textLength: text.length,
      // 是否有「被移出/权限」提示
      // 撤销/无权限的文案可能是多种，宽一点匹配；页面文案不是权限本身，只作 UI 观察
      revoked: /已被移出|权限已失效|会话已结束|不再有权限|无法进入|没有权限|房间不存在/.test(text),
    };
  });
}


// Read instance limits before allocating players and spectators
async function readRoomLimits() {
  const res = await fetch(`${API_BASE}/api/v1/config`, {
    headers: { Accept: "application/json", Origin: API_BASE },
  }).catch(() => null);
  const json = res && res.ok ? await res.json().catch(() => null) : null;
  const members = Number(json?.maxRoomMembers) || 0;
  const spectators = Number(json?.maxRoomSpectators) || 0;
  if (!members) {
    throw new Error(
      `读不到实例的房间容量上限（/api/v1/config 的 maxRoomMembers，status=${res?.status ?? "network"}）；` +
        "无法保证建房不被拒，停止。",
    );
  }
  return { members, spectators };
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`[reconnect] 后端未就绪：${API_BASE}/health/ready`);
    process.exitCode = 1;
    return;
  }

  // 记录日志起点行号，用于把「本轮 5xx」从历史累积里切出来
  let logLineAtStart = 1;
  try {
    const { execFile } = await import("node:child_process");
    const { promisify } = await import("node:util");
    const exec = promisify(execFile);
    const { stdout } = await exec("bash", ["-c", `wc -l < ${SERVER_LOG} 2>/dev/null || true`]);
    logLineAtStart = Number(stdout.trim() || 0) + 1;
  } catch {
    /* 取不到就从第 1 行开始 */
  }

  /* ---------- 准备：多个房间，每个房间一批玩家 ---------- */
  // 容量上限必须先读：写死会让建房被 400 拒掉，而失败现象看起来像「页面都没连上」。
  const limits = await readRoomLimits();
  const playersPerRoom = PLAYERS_PER_ROOM;
  if (playersPerRoom + 1 > limits.members) {
    throw new Error(
      `每个房间需要 ${playersPerRoom + 1} 人（${playersPerRoom} 玩家 + 房主），` +
        `超过本实例上限 ${limits.members}；请调整 CLIENT_COUNT/ROOM_COUNT 或在部署策略里调高 playersPerRoom。`,
    );
  }
  console.log(
    `[reconnect] 实例上限：每房 ${limits.members} 人 / ${limits.spectators} 观众；` +
      `本次每房 ${playersPerRoom + 1} 人`,
  );

  const rooms = [];
  for (let roomIndex = 0; roomIndex < ROOM_COUNT; roomIndex += 1) {
    const roomHost = makeClient();
    await roomHost.newSession(`重连房主${roomIndex + 1}`);
    const roomRes = await roomHost.call(
      "POST",
      "/rooms",
      {
        name: `集中重连测试房${roomIndex + 1}`,
        kind: "event",
        modeConfig: CLASSIC_CFG,
        maxPlayers: Math.min(limits.members, 200),
        maxSpectators: Math.min(limits.spectators, 300),
        visibility: "invite",
        allowSpectators: true,
      },
      key(),
    );
    if (roomRes.status !== 201) {
      assert(`准备 建房${roomIndex + 1}成功`, false, { status: roomRes.status, code: roomRes.json?.code });
      continue;
    }
    const roomId = roomRes.json?.room?.roomId;
    const inviteCode = roomRes.json?.inviteCode;

    /*
     * 房主改观众。
     *
     * classic 上限 16 人，本测试每房间 8 个玩家，房主若保持 player 会占一个名额；
     * 更重要的是房主参赛会额外成为一个 subject，干扰「8 个玩家」的计数断言。
     */
    {
      const cur = await roomHost.call("GET", `/rooms/${roomId}`);
      const self = (cur.json?.members ?? []).find((m) => m.displayName === `重连房主${roomIndex + 1}`);
      await roomHost.call(
        "PATCH",
        `/rooms/${roomId}/memberships/${self.membershipId}`,
        { expectedRoomVersion: cur.json?.roomVersion, requestedRole: "spectator" },
        key(),
      );
    }

    const clients = [];
    for (let index = 0; index < PLAYERS_PER_ROOM; index += 1) {
      const client = makeClient();
      const name = `房${roomIndex + 1}玩家${index + 1}`;
      await client.newSession(name);
      const joined = await client.call(
        "POST",
        `/rooms/${roomId}/memberships`,
        { inviteCode, requestedRole: "player" },
        key(),
      );
      if (joined.status === 201) {
        clients.push({ client, membershipId: joined.json?.membershipId, name, roomIndex });
      }
    }
    assert(`准备 房间${roomIndex + 1} 有 ${PLAYERS_PER_ROOM} 个玩家加入`, clients.length === PLAYERS_PER_ROOM, {
      joined: clients.length,
    });

    const roundRes = await roomHost.call("POST", `/rooms/${roomId}/rounds`, { modeConfig: CLASSIC_CFG }, key());
    const roundId = roundRes.json?.roundId;
    /* 先断言开局成功，避免后续请求使用缺失的 roundId */
    assert(`准备 房间${roomIndex + 1} 开局成功（roundId 非空）`, roundRes.status === 201 && Boolean(roundId), {
      status: roundRes.status,
      roundId,
      code: roundRes.json?.code,
      detail: roundRes.json?.detail,
    });

    let phaseVersion = roundRes.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await roomHost.call(
        "POST",
        `/rounds/${roundId}/transitions`,
        { target, expectedPhaseVersion: phaseVersion },
        key(),
      );
      phaseVersion = t.json?.phaseVersion ?? phaseVersion + 1;
    }

    rooms.push({ roomId, roundId, host: roomHost, clients, phaseVersion });
  }

  assert(`准备 ${ROOM_COUNT} 个房间都就绪`, rooms.length === ROOM_COUNT, { rooms: rooms.length });
  note("房间与回合", rooms.map((r) => ({ roomId: r.roomId, roundId: r.roundId, players: r.clients.length })));

  /* ---------- 打开全部浏览器页面 ---------- */
  const executablePath = await resolveChromiumExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});
  const pages = [];
  const contexts = [];

  console.log(`[reconnect] 打开 ${TOTAL_CLIENTS} 个浏览器上下文（${ROOM_COUNT} 个房间）……`);
  for (const room of rooms) {
    for (const entry of room.clients) {
      const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
      const [cookieName, cookieValue] = entry.client.cookie.split("=");
      if (cookieName && cookieValue) {
        await context.addCookies([{ name: cookieName, value: cookieValue, domain: "127.0.0.1", path: "/" }]);
      }
      const page = await context.newPage();
      contexts.push(context);
      pages.push({
        page,
        client: entry,
        roomId: room.roomId,
        roundId: room.roundId,
        roomHost: room.host,
        roomIndex: entry.roomIndex,
        cdp: await context.newCDPSession(page),
      });
    }
  }

  // 并发打开房间页
  await Promise.all(
    pages.map(async (entry) => {
      await entry.page.goto(`${WEB_BASE}/rooms/${entry.roomId}`, { waitUntil: "commit" }).catch(() => {});
    }),
  );
  await new Promise((r) => setTimeout(r, 5000));

  const initialStates = await Promise.all(pages.map((entry) => readClientState(entry.page)));
  const connectedCount = initialStates.filter((s) => s.connection === "已连接").length;
  assert(`准备 ${TOTAL_CLIENTS} 个页面都显示已连接`, connectedCount === TOTAL_CLIENTS, {
    connected: connectedCount,
    total: TOTAL_CLIENTS,
  });

  const baselineLengths = initialStates.map((s) => s.textLength);

  /* ---------- 阶段 1：20% 同时断线 ---------- */
  console.log(`[reconnect] 阶段 1：${DISCONNECT_COUNT} 个客户端同时断线（占 ${TOTAL_CLIENTS} 的 ${Math.round((DISCONNECT_COUNT / TOTAL_CLIENTS) * 100)}%）`);

  /*
   * 断线目标要跨房间分布，不能全在第一个房间——
   * 真实场景是多个房间的客户端同时掉线，而不是一个房间全灭。
   */
  const disconnectTargets = [];
  const perRoom = Math.floor(DISCONNECT_COUNT / ROOM_COUNT);
  for (let roomIndex = 0; roomIndex < ROOM_COUNT; roomIndex += 1) {
    const roomPages = pages.filter((p) => p.roomIndex === roomIndex);
    disconnectTargets.push(...roomPages.slice(0, perRoom));
  }
  // 余数补到第一个房间
  const remainder = DISCONNECT_COUNT - disconnectTargets.length;
  if (remainder > 0) {
    const extra = pages.filter((p) => p.roomIndex === 0 && !disconnectTargets.includes(p));
    disconnectTargets.push(...extra.slice(0, remainder));
  }
  note("断线目标分布", disconnectTargets.reduce((acc, t) => {
    acc[`房${t.roomIndex + 1}`] = (acc[`房${t.roomIndex + 1}`] ?? 0) + 1;
    return acc;
  }, {}));

  await Promise.all(
    disconnectTargets.map((entry) =>
      entry.cdp.send("Network.enable").then(() =>
        entry.cdp.send("Network.emulateNetworkConditions", {
          offline: true,
          latency: 0,
          downloadThroughput: -1,
          uploadThroughput: -1,
        }),
      ),
    ),
  );

  // 等待断线状态被检测到（浏览器层 offline 事件）
  await new Promise((r) => setTimeout(r, 3000));
  const offlineStates = await Promise.all(disconnectTargets.map((entry) => readClientState(entry.page)));
  const detectedOffline = offlineStates.filter((s) => /连接中断|正在重连|已断开/.test(s.connection)).length;
  assert(
    "断线 断开的客户端都显示连接中断",
    detectedOffline === DISCONNECT_COUNT,
    { detected: detectedOffline, expected: DISCONNECT_COUNT, states: offlineStates.map((s) => s.connection) },
  );

  /* ---------- 阶段 2：断线期间发生的事 ---------- */
  console.log("[reconnect] 阶段 2：断线期间有人提交 + 撤销 2 个");

  // 2a. 每个房间留在线的人提交（制造待追状态）
  const onlinePages = pages.filter((p) => !disconnectTargets.includes(p));
  let submitCount = 0;
  const submitDetail = [];
  /* 每个房间都安排断线期间的提交，才能验证恢复后的快照同步 */
  const submitTargets = [];
  for (const room of rooms) {
    const roomOnline = onlinePages.filter((p) => p.roomId === room.roomId);
    submitTargets.push(...roomOnline.slice(0, 2));
  }
  for (const entry of submitTargets) {
    const view = await entry.client.client.call("GET", `/rounds/${entry.roundId}`);
    if (view.status !== 200) {
      submitDetail.push({ name: entry.client.name, readStatus: view.status, code: view.json?.code });
      continue;
    }
    const submit = await entry.client.client.call(
      "POST",
      `/rounds/${entry.roundId}/submissions`,
      {
        phaseVersion: view.json?.round?.phaseVersion ?? 1,
        submissionRevision: view.json?.submissionRevision ?? 0,
        controlGeneration: view.json?.controlGeneration ?? 0,
        modeAction: { kind: "classic.guess", characterId: String(1001 + submitCount) },
      },
      key(),
    );
    if (submit.status === 201) submitCount += 1;
    submitDetail.push({ name: entry.client.name, status: submit.status, code: submit.json?.code });
  }
  note("在线客户端提交", submitDetail);
  assert("断线期间 在线客户端提交成功（制造待追状态）", submitCount > 0, { submitCount, submitDetail });

  // 2b. 撤销 2 个断线中的客户端（最容易漏的一条）
  const revokeTargets = disconnectTargets.slice(0, 2);
  const revokedMembers = [];
  for (const entry of revokeTargets) {
    const remove = await entry.roomHost.call(
      "DELETE",
      `/rooms/${entry.roomId}/memberships/${entry.client.membershipId}`,
      undefined,
      key(),
    );
    if (remove.status === 204 || remove.status === 200) {
      revokedMembers.push({ name: entry.client.name, roomId: entry.roomId, membershipId: entry.client.membershipId });
    }
  }
  assert("撤销 断线期间成功撤销 2 个成员", revokedMembers.length === 2, {
    revoked: revokedMembers.length,
    detail: revokedMembers,
  });

  // 记录断线期间各房间的版本变化
  const versionAfterActions = [];
  for (const room of rooms) {
    const after = await room.host.call("GET", `/rounds/${room.roundId}`);
    versionAfterActions.push({ roomId: room.roomId, roomVersion: after.json?.roomVersion });
  }
  note("断线期间动作完成", { versionAfterActions, submitCount, revokedCount: revokedMembers.length });

  /* ---------- 阶段 3：同时重连 ---------- */
  console.log("[reconnect] 阶段 3：等待 8 秒后同时重连");
  await new Promise((r) => setTimeout(r, 8000));

  const reconnectStart = Date.now();
  await Promise.all(
    disconnectTargets.map((entry) =>
      entry.cdp.send("Network.emulateNetworkConditions", {
        offline: false,
        latency: 0,
        downloadThroughput: -1,
        uploadThroughput: -1,
      }),
    ),
  );

  // 等待重连完成（给足时间让 SSE 重建 + 拉快照）
  await new Promise((r) => setTimeout(r, 12000));
  const reconnectElapsed = Date.now() - reconnectStart;
  note("重连耗时", { ms: reconnectElapsed });

  /* ---------- 阶段 4：逐条断言 ---------- */

  // 4a. 实际恢复数（不能因为「没报错」就算全过）
  const recoveredStates = await Promise.all(disconnectTargets.map((entry) => readClientState(entry.page)));

  /* 被撤销的客户端应进入无权限状态，不计入连接恢复率 */
  const revokedNames = new Set(revokeTargets.map((t) => t.client.name));
  const shouldRecover = disconnectTargets.filter((t) => !revokedNames.has(t.client.name));
  const shouldRecoverStates = recoveredStates.filter((_, i) => !revokedNames.has(disconnectTargets[i].client.name));
  const reconnectedCount = shouldRecoverStates.filter((s) => s.connection === "已连接").length;

  report.recovery.recovered = reconnectedCount;
  report.recovery.shouldRecover = shouldRecover.length;
  report.recovery.failed = shouldRecover.length - reconnectedCount;
  report.recovery.revoked = revokedNames.size;
  report.recovery.details = recoveredStates.map((s, index) => ({
    name: disconnectTargets[index]?.client?.name,
    roomIndex: disconnectTargets[index]?.roomIndex,
    connection: s.connection,
    textLength: s.textLength,
    baselineLength: baselineLengths[pages.indexOf(disconnectTargets[index])],
  }));

  assert(
    `重连 应恢复的 ${shouldRecover.length} 个客户端全部恢复（被撤销的 ${revokedNames.size} 个不计）`,
    reconnectedCount === shouldRecover.length,
    {
      recovered: reconnectedCount,
      shouldRecover: shouldRecover.length,
      revokedExcluded: revokedNames.size,
      details: report.recovery.details,
    },
  );

  // 4b. 状态追上：用服务端版本号验证（不是只看「已连接」）
  const versionProgressed = [];
  for (const room of rooms) {
    const now = await room.host.call("GET", `/rounds/${room.roundId}`);
    versionProgressed.push({ roomId: room.roomId, roomVersion: now.json?.roomVersion });
  }
  const anyProgress = versionProgressed.some((v) => typeof v.roomVersion === "number" && v.roomVersion > 1);
  assert("重连 服务端版本号确实前进了（证明断线期间有变更）", anyProgress, { versionProgressed });

  // 每个重连客户端页面的文本长度不应异常膨胀（重复渲染检测）
  const growthRatios = recoveredStates.map((s, index) => {
    const baseline = baselineLengths[pages.indexOf(disconnectTargets[index])] ?? 1;
    return Number((s.textLength / baseline).toFixed(2));
  });
  const inflated = growthRatios.filter((ratio) => ratio >= 1.8);
  assert("重连 没有重复渲染（文本长度未异常膨胀）", inflated.length === 0, {
    growthRatios,
    inflatedCount: inflated.length,
  });

  // 4c. 被撤销的客户端重连后必须无法继续读取（权限边界）
  const revokedAccess = await Promise.all(
    revokeTargets.map(async (entry) => {
      const res = await entry.client.client.call("GET", `/rooms/${entry.roomId}`);
      const streamRes = await fetch(`${API_BASE}/api/v1/rooms/${entry.roomId}/stream`, {
        headers: { Accept: "text/event-stream", Cookie: entry.client.client.cookie, Origin: API_BASE },
      }).catch(() => null);
      return {
        name: entry.client.name,
        roomStatus: res.status,
        streamStatus: streamRes?.status ?? null,
      };
    }),
  );
  note("被撤销客户端的重连访问结果", { revokedAccess });

  const allDenied = revokedAccess.every(
    (r) => r.roomStatus >= 400 && (r.streamStatus === null || r.streamStatus >= 400),
  );
  assert("撤销 断线期间被撤销的客户端重连后无法继续读取房间（权限边界生效）", allDenied, {
    details: revokedAccess,
    note: "撤销帧在断线期间发不出去，重连后应由成员校验拒绝——这才是正确语义",
  });

  // 4d. 未被撤销的客户端能看到断线期间的提交
  /*
   * 未被撤销的重连客户端必须能看到断线期间的提交。
   *
   * 注意：断线期间提交发生在在线客户端所在的房间。
   * 如果某个重连客户端所在房间没人提交，它的 submittedCount 自然是 0 ——
   * 那不是缺陷。所以要按房间比对：只要该房间断线期间有过提交，
   * 该房间的重连客户端就必须看到。
   */
  const roomsWithSubmit = new Set(
    submitDetail.filter((d) => d.status === 201).map((d) => {
      const matched = onlinePages.find((p) => p.client.name === d.name);
      return matched?.roomId;
    }).filter(Boolean),
  );
  const stillValid = disconnectTargets.filter((t) => !revokedNames.has(t.client.name));
  const validStates = await Promise.all(
    stillValid.map(async (entry) => {
      const view = await entry.client.client.call("GET", `/rounds/${entry.roundId}`);
      const subjects = view.json?.publicState?.subjects ?? [];
      return {
        name: entry.client.name,
        roomId: entry.roomId,
        roomHadSubmit: roomsWithSubmit.has(entry.roomId),
        readStatus: view.status,
        submittedCount: subjects.filter((s) => s.submitted).length,
        total: subjects.length,
      };
    }),
  );
  const checkable = validStates.filter((s) => s.roomHadSubmit);
  const seesSubmissions = checkable.filter((s) => s.submittedCount > 0).length;
  assert(
    "重连 未被撤销的客户端能看到断线期间同房间的提交",
    checkable.length > 0 && seesSubmissions === checkable.length,
    {
      validStates,
      checkableRooms: [...roomsWithSubmit],
      note: "只比对「该房间断线期间确实有人提交」的客户端；房间没人提交则 submittedCount=0 是正常的",
    },
  );

  /*
   * 4e2. 重连后事件顺序。
   *
   * 断线期间积压的事件必须按 event_sequence 递增投递，乱序会导致状态错乱。
   * 服务端 ListRoomEvents 是 `ORDER BY event_sequence ASC`，这里验证客户端
   * 通过 SSE 实际收到的帧顺序与之相符。
   *
   * 测法：用 fetch 直连 SSE 读原始字节流，解析 `id:` 字段（就是 cursor/seq），
   * 断言严格递增。
   */
  {
    const probeEntry = disconnectTargets.find((t) => !revokedNames.has(t.client.name));
    if (probeEntry) {
      const controller = new AbortController();
      const streamRes = await fetch(`${API_BASE}/api/v1/rooms/${probeEntry.roomId}/stream`, {
        headers: {
          Accept: "text/event-stream",
          Cookie: probeEntry.client.client.cookie,
          Origin: API_BASE,
        },
        signal: controller.signal,
      }).catch(() => null);

      if (streamRes && streamRes.ok && streamRes.body) {
        const reader = streamRes.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        const seqs = [];
        const readUntil = Date.now() + 4000;
        try {
          while (Date.now() < readUntil) {
            const { done, value } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });
            // SSE 帧以空行分隔；id: 行携带 cursor（形如 e<epoch>-<seq> 或纯 seq）
            const frames = buffer.split("\n\n");
            buffer = frames.pop() ?? "";
            for (const frame of frames) {
              for (const line of frame.split("\n")) {
                if (!line.startsWith("id:")) continue;
                const raw = line.slice(3).trim();
                /* Hub cursor 是 base64url 编码的结构，按字段解码 event_sequence，不能匹配字符串末尾数字 */
                try {
                  const bytes = Buffer.from(raw, "base64url");
                  if (bytes.length >= 9) seqs.push(bytes[8]);
                } catch {
                  // 回退：若是纯数字 cursor（轮询版），直接取数值
                  if (/^\d+$/.test(raw)) seqs.push(Number(raw));
                }
              }
            }
            if (seqs.length >= 3) break;
          }
        } catch {
          /* 中止是预期行为 */
        } finally {
          controller.abort();
        }

        note("重连后 SSE 事件序列", { seqs, count: seqs.length });
        /* 同一批 outbox 事件可以共享序号；只要求序号非递减 */
        const nonDecreasing = seqs.every((v, i) => i === 0 || v >= seqs[i - 1]);
        const hasProgress = seqs.length < 2 || seqs[seqs.length - 1] > seqs[0];
        assert(
          "重连 事件 seq 非递减投递（无乱序）",
          nonDecreasing,
          {
            seqs,
            hasProgress,
            note:
              seqs.length < 2
                ? "该窗口内事件太少，无法判定顺序（不计为失败）"
                : "同批次事件共享 seq 是正常的；乱序表现为 seq 回退",
          },
        );
      } else {
        report.limits.push("重连后事件顺序未验证：SSE 探测流建立失败");
      }
    }
  }

  // Restrict log counts to this run using the recorded starting line
  const count5xxSince = async (fromLine) => {
    try {
      const { execFile } = await import("node:child_process");
      const { promisify } = await import("node:util");
      const exec = promisify(execFile);
      const { stdout } = await exec("bash", [
        "-c",
        `tail -n +${fromLine} ${SERVER_LOG} 2>/dev/null | grep -cE '"status":5[0-9][0-9]' || true`,
      ]);
      const parsed = Number(stdout.trim());
      return Number.isFinite(parsed) ? parsed : 0;
    } catch {
      return 0;
    }
  };
  const total5xx = await count5xxSince(1);
  const window5xx = await count5xxSince(logLineAtStart);
  note("服务端 5xx 计数", { 全文件累计: total5xx, 本轮窗口内: window5xx, 起点行: logLineAtStart });
  assert("重连 本轮窗口内服务端未出现 5xx", window5xx === 0, {
    本轮5xx: window5xx,
    全文件累计: total5xx,
    note: "全文件累计包含历史压测与其它 E2E 运行，不是本轮指标",
  });

  // Preserve collected assertions when a dependency or timeout aborts the run
  if (pages.length === 0) {
    console.error(
      "[reconnect] 没有可用的页面：入场阶段失败（房间没建起来或页面没连上）。" +
        "上面的断言已记录原因，这里不再截图。",
    );
    return;
  }
  await shot(pages[0].page, "reconnect-revoked-client");
  await shot(pages[pages.length - 1].page, "reconnect-online-client");

  /* ---------- 收尾 ---------- */
  await Promise.all(contexts.map((c) => c.close()));
  await browser.close();

  const hashes = report.screenshots.map((s) => s.sha256).filter(Boolean);
  const unique = new Set(hashes);
  assert("重连 截图内容互不相同", unique.size === hashes.length, {
    total: hashes.length,
    unique: unique.size,
  });

  report.summary = {
    assertions: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
    screenshots: report.screenshots.length,
  };

  const out = resolve(EVIDENCE, "reconnect-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[reconnect] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 失败`,
  );
  console.log(`[reconnect] 恢复统计：${report.recovery.recovered}/${report.recovery.attempted}`);
  console.log(`[reconnect] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
