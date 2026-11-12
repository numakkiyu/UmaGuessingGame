/** Compares fields read by frontend adapters with real game projections. Missing runtime fields may require phase-specific sampling. */
import { readFile, readdir, writeFile, mkdir } from "node:fs/promises";

const base = process.env.UMA_API_BASE;
if (!base) throw new Error("Set isolated UMA_API_BASE");

const apiDir = new URL("../src/lib/api/", import.meta.url);
const out = new URL("../../.cache/reports/projection-field-audit/", import.meta.url);
await mkdir(out, { recursive: true });

// ---- 1. 抓前端解析层读取的字面量 key ----
const frontendKeys = new Map(); // key -> Set(文件)
for (const name of await readdir(apiDir)) {
  if (!name.endsWith(".ts") || name.endsWith(".test.ts") || name === "schema.d.ts") continue;
  const src = await readFile(new URL(name, apiDir), "utf8");
  // 形如 item["foo"] / ps["foo"] / record["foo"] / x?.foo —— 只抓字符串下标与明确的字段读法
  for (const m of src.matchAll(/(?:item|ps|record|value|obj|entry|row|cell|node)\??\.?\["([A-Za-z_][A-Za-z0-9_]*)"\]/g)) {
    const key = m[1];
    if (!frontendKeys.has(key)) frontendKeys.set(key, new Set());
    frontendKeys.get(key).add(name);
  }
}

// ---- 2. 对每个玩法真实开局，收集投影 key ----
class Client {
  constructor() {
    this.cookie = "";
    this.csrf = "";
  }
  async call(method, path, body, extraHeaders = {}) {
    const headers = { "Content-Type": "application/json", Origin: base, ...extraHeaders };
    if (this.cookie) headers.Cookie = this.cookie;
    if (this.csrf && method !== "GET") headers["X-CSRF-Token"] = this.csrf;
    const res = await fetch(`${base}/api/v1${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const setCookie = res.headers.getSetCookie?.() ?? [];
    for (const c of setCookie) {
      const pair = c.split(";")[0];
      if (pair.includes("=")) this.cookie = pair;
    }
    const text = await res.text();
    let json = null;
    try {
      json = text ? JSON.parse(text) : null;
    } catch {
      /* non-JSON */
    }
    return { status: res.status, json, text };
  }
  async newSession(name) {
    const res = await this.call("POST", "/sessions", { displayName: name });
    this.csrf = res.json?.csrfToken ?? "";
    return res;
  }
}

const key = () => crypto.randomUUID().replace(/-/g, "");

/** 递归收集所有对象 key（数组取第一个元素的 key，避免样本爆炸）。 */
function collectKeys(value, into = new Set(), depth = 0) {
  if (depth > 8 || value === null || typeof value !== "object") return into;
  if (Array.isArray(value)) {
    if (value.length > 0) collectKeys(value[0], into, depth + 1);
    return into;
  }
  for (const [k, v] of Object.entries(value)) {
    into.add(k);
    collectKeys(v, into, depth + 1);
  }
  return into;
}

const modesRes = await fetch(`${base}/api/v1/modes`, { headers: { Origin: base } });
const modesBody = await modesRes.json();
const available = (modesBody.items ?? [])
  .filter((m) => m.availability === "available")
  .map((m) => m.modeId);

const report = {
  generatedAt: new Date().toISOString(),
  base,
  modes: [],
  frontendKeyCount: frontendKeys.size,
  missing: [],
  errors: [],
};

for (const modeId of available) {
  const client = new Client();
  try {
    await client.newSession(`字段审计-${modeId}`);
    const made = await client.call("POST", "/rooms", {
      name: `审计 ${modeId}`,
      kind: "friend",
      modeConfig: { modeId, config: {} },
      maxPlayers: 8,
      maxSpectators: 0,
      allowSpectators: false,
      visibility: "invite",
    }, { "Idempotency-Key": key() });
    if (made.status !== 201) {
      report.errors.push({ modeId, where: "createRoom", status: made.status, body: made.text.slice(0, 200) });
      continue;
    }
    const rid = made.json.room.roomId;

    /*
     * 补足参与者。
     *
     * clue-puzzle 要 3–6 人一队、rapport 要 2 人以上、trade 要 3 人、strategy-race
     * 要两队——房主一个人开局会被 422 拒掉，那样这些玩法的投影一个字段都采不到，
     * 审计就会把它们的全部前端字段误判成缺失。
     */
    const NEEDS = { "clue-puzzle": 5, rapport: 4, trade: 4, "strategy-race": 3 };
    const extra = NEEDS[modeId] ?? 0;
    const members = [];
    for (let i = 0; i < extra; i += 1) {
      const guest = new Client();
      await guest.newSession(`字段审计-${modeId}-${i}`);
      const join = await guest.call(
        "POST",
        `/rooms/${rid}/memberships`,
        { inviteCode: made.json.inviteCode, requestedRole: "player" },
        { "Idempotency-Key": key() },
      );
      if (join.status === 201) members.push({ guest, membershipId: join.json.membershipId });
    }
    /* 每次分队前重取 roomVersion；成员变动会使旧版本失效 */
    /*
     * 每队人数必须整除总人数。
     *
     * clue-puzzle 要求「所有人都分队」且每队 3–6 人：房主 + 6 名加入者 = 7 人，
     * 若按 2 队 × 3 人只分了 6 人，第 7 人没队，开局就报「assign all players to teams」。
     * 所以 clue-puzzle 用 6 人（房主 + 5），恰好 2 队 × 3 人。
     */
    const TEAM_PLAN = {
      "clue-puzzle": { size: 3, teams: ["审计甲队", "审计乙队"], total: 6 },
      "strategy-race": { size: 2, teams: ["审计甲队", "审计乙队"], total: 4 },
    };
    const plan = TEAM_PLAN[modeId];
    if (plan) {
      const roomView = await client.call("GET", `/rooms/${rid}`);
      const hostMembership = roomView.json?.viewer?.membershipId;
      const everyone = hostMembership ? [{ membershipId: hostMembership }, ...members] : members;
      const all = everyone.slice(0, plan.total);
      let cursor = 0;
      for (const [teamIndex, label] of plan.teams.entries()) {
        const team = await client.call(
          "POST",
          `/rooms/${rid}/teams`,
          { label, colorToken: teamIndex === 0 ? "green" : "blue" },
          { "Idempotency-Key": key() },
        );
        if (team.status !== 201) {
          report.errors.push({ modeId, where: `createTeam-${label}`, status: team.status, body: team.text.slice(0, 160) });
          continue;
        }
        for (let k = 0; k < plan.size; k += 1) {
          const member = all[cursor];
          cursor += 1;
          if (!member) continue;
          const version = (await client.call("GET", `/rooms/${rid}`)).json?.roomVersion ?? 1;
          const patch = await client.call(
            "PATCH",
            `/rooms/${rid}/memberships/${member.membershipId}`,
            {
              expectedRoomVersion: version,
              teamId: team.json.teamId,
              requestedRole: k === 0 ? "team-captain" : "player",
            },
            { "Idempotency-Key": key() },
          );
          if (patch.status >= 400) {
            report.errors.push({
              modeId,
              where: `assign-${label}-${k}`,
              status: patch.status,
              body: patch.text.slice(0, 160),
            });
          }
        }
      }
    }

    const opened = await client.call("POST", `/rooms/${rid}/rounds`, {}, { "Idempotency-Key": key() });
    if (opened.status !== 201) {
      report.errors.push({ modeId, where: "createRound", status: opened.status, body: opened.text.slice(0, 200) });
      continue;
    }
    const roundId = opened.json.roundId;

    /* 推进到 collecting 后采样，此时才有候选、手牌、线索等收集期字段 */
    for (const target of ["briefing", "collecting"]) {
      const cur = await client.call("GET", `/rounds/${roundId}`);
      const pv = cur.json?.round?.phaseVersion ?? 1;
      const tr = await client.call(
        "POST",
        `/rounds/${roundId}/transitions`,
        { target, expectedPhaseVersion: pv },
        { "Idempotency-Key": key() },
      );
      // 有些玩法不允许跳过 briefing（例如 memory 只看 briefing），失败不致命：
      // 采集期采样才是目的，拿不到就如实记下阶段。
      if (tr.status >= 400) break;
    }
    /*
     * 在多个时点采样，而不是只看一个阶段。
     *
     * 很多字段只在特定阶段存在：`claim`/`answer`/`correct`/`attempt` 要有人提交后才有，
     * `rank`/`authorIds`/`result` 要揭晓后才有。只看采集期会把它们全判成「前端读了但
     * 服务端没有」，那是误报。这里依次推进到 locked、revealed 再各采一次。
     */
    const keys = new Set();
    const phases = [];
    const sample = async (label) => {
      const v = await client.call("GET", `/rounds/${roundId}`);
      if (v.status !== 200) return;
      collectKeys(v.json, keys);
      phases.push(`${label}:${v.json?.round?.phase ?? "?"}`);
    };
    await sample("collecting");

    /* 提交最小合法动作以采集反馈字段；人数不足等拒绝结果单独记录 */
    const ACTION = {
      classic: { kind: "classic.guess", characterId: "1001" },
      "aptitude-grid": null,
      draft: null,
      memory: null,
      "co-race-path": null,
      "race-rebuild": null,
      sort: null,
      pedigree: null,
      creative: { kind: "creative.work", text: "字段审计用的一段企划文字。" },
      "clue-puzzle": null,
      rapport: null,
      "strategy-race": null,
      trade: null,
    };
    const action = ACTION[modeId];
    if (action) {
      const cur = await client.call("GET", `/rounds/${roundId}`);
      const sub = await client.call(
        "POST",
        `/rounds/${roundId}/submissions`,
        {
          modeAction: action,
          phaseVersion: cur.json?.round?.phaseVersion ?? 1,
          submissionRevision: 0,
          controlGeneration: cur.json?.controlGeneration ?? 0,
        },
        { "Idempotency-Key": key() },
      );
      if (sub.status < 400) {
        collectKeys(sub.json, keys);
        phases.push(`after-submit:${sub.status}`);
        await sample("after-submit");
      } else {
        phases.push(`submit-refused:${sub.status}`);
      }
    }
    for (const target of ["locked", "revealed"]) {
      const cur = await client.call("GET", `/rounds/${roundId}`);
      const pv = cur.json?.round?.phaseVersion ?? 1;
      const tr = await client.call(
        "POST",
        `/rounds/${roundId}/transitions`,
        { target, expectedPhaseVersion: pv },
        { "Idempotency-Key": key() },
      );
      if (tr.status >= 400) break;
      await sample(target);
    }
    report.modes.push({ modeId, roundId, phases, keys: [...keys].sort() });
  } catch (err) {
    report.errors.push({ modeId, where: "exception", body: String(err) });
  }
}

// ---- 3. 比对 ----
const published = new Set();
for (const m of report.modes) for (const k of m.keys) published.add(k);

// 这些 key 属于请求体/本地状态，不是投影字段，不参与比对。
const NOT_PROJECTION = new Set([
  "kind", "text", "optionId", "cellId", "characterId", "assignments", "groups",
  "submissionRevision", "controlGeneration", "phaseVersion", "idempotencyKey",
]);

/*
 * 只比对「轮次投影」这一层。
 *
 * 有些解析文件读的不是 /rounds/{id} 的投影，而是房间快照、成员列表或本地存档：
 * `teams.ts` 读 `nickname`/`publicCode`（来自 membershipDTO）、`draft.ts` 读
 * `savedAt`（本地草稿）。它们本来就不会出现在轮次投影里，把它们算成缺失是误报。
 * 因此按文件归属排除，而不是把整个仓库的字段混在一起比。
 */
const NOT_ROUND_PROJECTION_FILES = new Set([
  "teams.ts",           // 房间成员/队伍快照
  "submissions-progress.ts", // 提交进度聚合，来自多处
]);

for (const [k, files] of frontendKeys) {
  if (NOT_PROJECTION.has(k)) continue;
  const relevant = [...files].filter((f) => !NOT_ROUND_PROJECTION_FILES.has(f));
  if (relevant.length === 0) continue;
  if (!published.has(k)) {
    report.missing.push({ key: k, readBy: relevant.sort() });
  }
}

report.summary = {
  modesProbed: report.modes.length,
  publishedKeys: published.size,
  frontendKeys: frontendKeys.size,
  missingCount: report.missing.length,
};
await writeFile(new URL("report.json", out).pathname, `${JSON.stringify(report, null, 2)}\n`);

console.log(`玩法数：${report.summary.modesProbed} / 服务端投影 key：${published.size} / 前端读取 key：${frontendKeys.size}`);
console.log(`前端读了但投影里没有的 key：${report.missing.length}`);
for (const m of report.missing) console.log(`  - ${m.key}  ← ${m.readBy.join(", ")}`);
if (report.errors.length) {
  console.log("错误：");
  for (const e of report.errors) console.log(`  - ${e.modeId} ${e.where}: ${e.status ?? ""} ${String(e.body).slice(0, 120)}`);
}
