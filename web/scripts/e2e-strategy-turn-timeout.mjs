#!/usr/bin/env node
/** Checks that a strategy turn advances without player actions after its deadline. This covers timeout progression, not a full played match. */
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const EVIDENCE = resolve(HERE, "../../.cache/reports");
const API = process.env.UMA_API_BASE ?? "http://127.0.0.1:8080";

const WAIT_MS = Number(process.env.UMA_TURN_WAIT_MS ?? 24_000);
const DECISION_SECONDS = 20;

const report = { at: new Date().toISOString(), api: API, waitMs: WAIT_MS, steps: [], failures: [] };
function assert(name, ok, detail) {
  report.steps.push({ name, ok: Boolean(ok), detail });
  if (!ok) {
    report.failures.push({ name, detail });
    console.error(`  ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail).slice(0, 300));
  } else {
    console.log(`  ✓ ${name}`);
  }
}

function makeClient() {
  let cookies = new Map();
  let csrf = null;
  const cookieHeader = () => [...cookies].map(([k, v]) => `${k}=${v}`).join("; ");

  async function call(method, path, body, idem = false) {
    const headers = { Origin: API, Cookie: cookieHeader() };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
    if (idem) headers["Idempotency-Key"] = crypto.randomUUID();
    const res = await fetch(`${API}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    for (const line of res.headers.getSetCookie?.() ?? []) {
      const [pair] = line.split(";");
      const i = pair.indexOf("=");
      if (i > 0) cookies.set(pair.slice(0, i).trim(), pair.slice(i + 1).trim());
    }
    const text = await res.text();
    let json;
    try {
      json = text ? JSON.parse(text) : null;
    } catch {
      json = { raw: text.slice(0, 300) };
    }
    return { status: res.status, json };
  }
  return { call, setCsrf: (t) => (csrf = t) };
}

async function main() {
  await mkdir(EVIDENCE, { recursive: true });

  const health = await fetch(`${API}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error("[turn-timeout] 后端未就绪");
    process.exitCode = 1;
    return;
  }

  const host = makeClient();
  const login = await host.call("POST", "/api/v1/sessions", { displayName: "超时验证-1" });
  host.setCsrf(login.json.csrfToken);

  const created = await host.call(
    "POST",
    "/api/v1/rooms",
    {
      name: `回合超时-${Date.now() % 100000}`,
      kind: "friend",
      modeConfig: {
        modeId: "strategy-race",
        rulesVersion: "2.0.0-draft.1",
        contentPackId: "pack-496dd9bbe6059311",
        informationStyle: "closed",
      },
      maxPlayers: 4,
      maxSpectators: 0,
      visibility: "invite",
      allowSpectators: false,
    },
    true,
  );
  if (created.status >= 400) {
    assert("建房成功", false, created.json);
    return;
  }
  const roomId = created.json.room.roomId;
  const inviteCode = created.json.inviteCode;

  // 第二个玩家：策略赛至少要 2 队才能开局。
  const peer = makeClient();
  const peerLogin = await peer.call("POST", "/api/v1/sessions", { displayName: "超时验证-2" });
  peer.setCsrf(peerLogin.json.csrfToken);
  const joined = await peer.call(
    "POST",
    `/api/v1/rooms/${roomId}/memberships`,
    { inviteCode, requestedRole: "player" },
    true,
  );
  assert("第二人加入成功", joined.status < 400, joined.json?.code ?? null);

  const round = await host.call(
    "POST",
    `/api/v1/rooms/${roomId}/rounds`,
    {
      modeConfig: {
        modeId: "strategy-race",
        rulesVersion: "2.0.0-draft.1",
        contentPackId: "pack-496dd9bbe6059311",
        informationStyle: "closed",
      },
    },
    true,
  );
  if (round.status >= 400) {
    assert("开局成功", false, round.json);
    return;
  }
  const roundId = round.json.roundId;
  let pv = round.json.phaseVersion ?? 0;
  report.roomId = roomId;
  report.roundId = roundId;

  for (const target of ["briefing", "collecting"]) {
    const res = await host.call(
      "POST",
      `/api/v1/rounds/${roundId}/transitions`,
      { target, expectedPhaseVersion: pv },
      true,
    );
    if (res.status >= 400) {
      assert(`推进到 ${target}`, false, res.json);
      return;
    }
    pv = res.json?.phaseVersion ?? pv + 1;
  }
  assert("推进到 collecting", true, { phaseVersion: pv });

  const readState = async () => {
    const snap = await host.call("GET", `/api/v1/rounds/${roundId}`);
    const pub = snap.json?.publicState ?? {};
    return {
      phase: snap.json?.round?.phase ?? null,
      currentTurn: pub.currentTurn ?? null,
      resolvedTurns: pub.resolvedTurns ?? null,
      decisionSeconds: pub.track?.decisionSeconds ?? pub.config?.decisionSeconds ?? null,
      deadlineAt: snap.json?.round?.deadlineAt ?? null,
      serverTime: snap.json?.serverTime ?? null,
    };
  };

  const before = await readState();
  report.before = before;
  console.log("  等待前:", JSON.stringify(before));
  assert("开局时在第 1 回合", before.currentTurn === 1, before);
  assert("声明每回合 20 秒", before.decisionSeconds === DECISION_SECONDS, before);

  // 关键：两人都不提交，只等一个回合窗口过去。
  console.log(`  两人都不提交，等待 ${WAIT_MS}ms…`);
  await new Promise((r) => setTimeout(r, WAIT_MS));

  const after = await readState();
  report.after = after;
  console.log("  等待后:", JSON.stringify(after));

  assert(
    "超过一个回合窗口后 currentTurn 前进（服务端自己推进，不靠提交）",
    typeof after.currentTurn === "number" && after.currentTurn > 1,
    { before: before.currentTurn, after: after.currentTurn },
  );
  assert("回合推进后仍是 collecting（整局没有结束）", after.phase === "collecting", after);
  assert(
    "deadline 被刷新成新的回合窗口",
    after.deadlineAt !== null && after.deadlineAt !== before.deadlineAt,
    { before: before.deadlineAt, after: after.deadlineAt },
  );

  report.summary = { steps: report.steps.length, failed: report.failures.length };
  await writeFile(
    resolve(EVIDENCE, "strategy-turn-timeout-live.json"),
    `${JSON.stringify(report, null, 2)}\n`,
    "utf8",
  );
  console.log(`\n[turn-timeout] ${report.steps.length} 项检查，${report.failures.length} 失败`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
