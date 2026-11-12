#!/usr/bin/env node
/** Exercises preparation, submissions, phase transitions and settlement through REST with real content and independent sessions. */
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const EVIDENCE = resolve(HERE, "../../.cache/reports");
const API = process.env.UMA_API_BASE ?? "http://127.0.0.1:8080";

const report = { generatedAt: new Date().toISOString(), api: API, modes: [], failures: [] };

function assert(mode, name, ok, detail) {
  const row = { mode, name, ok: Boolean(ok), detail };
  const bucket = report.modes.find((m) => m.modeId === mode);
  if (bucket) bucket.assertions.push(row);
  if (!ok) {
    report.failures.push({ mode, name, detail });
    console.error(`  ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail).slice(0, 240));
  } else {
    console.log(`  ✓ ${name}`);
  }
}

/** 一个带 cookie 罐的极简客户端。 */
function makeClient() {
  let cookies = new Map();
  let csrf = null;

  function cookieHeader() {
    return [...cookies.entries()].map(([k, v]) => `${k}=${v}`).join("; ");
  }

  function absorb(res) {
    const raw = res.headers.getSetCookie?.() ?? [];
    for (const line of raw) {
      const [pair] = line.split(";");
      const idx = pair.indexOf("=");
      if (idx > 0) cookies.set(pair.slice(0, idx).trim(), pair.slice(idx + 1).trim());
    }
  }

  async function call(method, path, body, { idempotency = false } = {}) {
    const headers = { Origin: API, Cookie: cookieHeader() };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
    if (idempotency) headers["Idempotency-Key"] = crypto.randomUUID();
    const res = await fetch(`${API}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    absorb(res);
    const text = await res.text();
    let json;
    try {
      json = text ? JSON.parse(text) : null;
    } catch {
      json = { raw: text.slice(0, 400) };
    }
    return { status: res.status, json };
  }

  return {
    call,
    setCsrf(token) {
      csrf = token;
    },
    async login(displayName) {
      const res = await call("POST", "/api/v1/sessions", { displayName });
      const token = res.json?.csrfToken;
      if (!token) throw new Error(`创建会话失败: ${JSON.stringify(res.json).slice(0, 200)}`);
      csrf = token;
      return res.json;
    },
  };
}

/** 建一间房并让指定人数加入，返回 {roomId, clients}。 */
async function makeRoom(modeId, { players, modeConfig }) {
  const host = makeClient();
  await host.login(`结算-${modeId}-1`);

  const created = await host.call(
    "POST",
    "/api/v1/rooms",
    {
      name: `结算验证-${modeId}-${Date.now() % 100000}`,
      kind: players > 1 ? "friend" : "solo",
      modeConfig: { modeId, ...modeConfig },
      maxPlayers: Math.max(players, 2),
      maxSpectators: 0,
      visibility: "invite",
      allowSpectators: false,
    },
    { idempotency: true },
  );
  if (created.status >= 400) throw new Error(`建房失败: ${JSON.stringify(created.json).slice(0, 300)}`);

  const roomId = created.json.room.roomId;
  const inviteCode = created.json.inviteCode;
  const clients = [host];

  for (let i = 1; i < players; i += 1) {
    const c = makeClient();
    await c.login(`结算-${modeId}-${i + 1}`);
    const joined = await c.call(
      "POST",
      `/api/v1/rooms/${roomId}/memberships`,
      { inviteCode, requestedRole: "player" },
      { idempotency: true },
    );
    if (joined.status >= 400) throw new Error(`加入失败: ${JSON.stringify(joined.json).slice(0, 300)}`);
    clients.push(c);
  }
  return { roomId, clients };
}

/** 推进回合到目标阶段，返回最新 phaseVersion。 */
async function transition(client, roundId, target, phaseVersion) {
  const current = await snapshot(client, roundId);
  const phase = current.round?.phase ?? current.round?.status;
  const sequence = ["prepared", "briefing", "collecting", "locked", "revealed", "settled"];
  if (sequence.indexOf(phase) >= sequence.indexOf(target)) return current.round.phaseVersion;
  phaseVersion = current.round?.phaseVersion ?? phaseVersion;
  const res = await client.call(
    "POST",
    `/api/v1/rounds/${roundId}/transitions`,
    { target, expectedPhaseVersion: phaseVersion },
    { idempotency: true },
  );
  if (res.status >= 400) {
    throw new Error(`transition ${target} 失败: ${JSON.stringify(res.json).slice(0, 300)}`);
  }
  return res.json?.phaseVersion ?? phaseVersion + 1;
}

/** 读回合快照。 */
async function snapshot(client, roundId) {
  const res = await client.call("GET", `/api/v1/rounds/${roundId}`);
  if (res.status >= 400) throw new Error(`读回合失败: ${JSON.stringify(res.json).slice(0, 300)}`);
  return res.json;
}

/** 提交一个动作。 */
/* submissionRevision 按参赛方槽位递增，首次为 0；同一方多次提交时显式覆盖计数 */
async function submit(client, roundId, meta, modeAction, revisionOverride) {
  const res = await client.call(
    "POST",
    `/api/v1/rounds/${roundId}/submissions`,
    {
      // phaseVersion 位于快照的 round 子对象
      phaseVersion: meta.round?.phaseVersion ?? 0,
      submissionRevision: revisionOverride ?? meta.submissionRevision ?? 0,
      controlGeneration: meta.controlGeneration ?? 0,
      modeAction,
    },
    { idempotency: true },
  );
  return res;
}

/**
 * 每个玩法的驱动方式。
 *
 * 每个 drive 函数拿到 (clients, roundId, helpers)，负责提交本局需要的动作。
 * 载荷形状与 internal/modes/<mode>/mode.go 里的 payload 结构一一对应。
 */
const DRIVERS = {
  classic: {
    players: 1,
    modeConfig: { maxWrongGuesses: 8, winPolicy: "solo", durationSeconds: 0, shadowEnabled: false },
    async drive(clients, roundId, h) {
      /*
       * 猜哪个角色？
       *
       * 不能读 privateState —— classic 的答案池刻意不下发给玩家（防泄题），
       * 实测 `privateState` 是 `{}`。真实前端是让玩家从公开角色列表里搜，
       * 所以这里也走同一条路：GET /characters 取一个真实角色去猜。
       * 猜错也是合法提交，本脚本验的是「提交 → 收卷 → 结算」这条链路通不通。
       */
      const list = await clients[0].call("GET", "/api/v1/characters?limit=1");
      const guessId = list.json?.items?.[0]?.characterId;
      if (!guessId) return { submitted: 0, note: "公开角色列表为空" };
      const snap = await h.snapshot(clients[0], roundId);
      const res = await h.submit(clients[0], roundId, snap, {
        kind: "classic.guess",
        characterId: guessId,
      });
      return { submitted: res.status < 400 ? 1 : 0, status: res.status, body: res.json?.code ?? null };
    },
  },

  sort: {
    players: 1,
    modeConfig: {},
    async drive(clients, roundId, h) {
      const snap = await h.snapshot(clients[0], roundId);
      const cards = snap.publicState?.cards ?? [];
      // groups 是「从大到小」的分组数组。校验要求覆盖全部卡片
      // （incomplete_order 就是漏卡），字段是 characterId 不是 cardId。
      const groups = cards.map((c) => [c.characterId]);
      if (groups.length === 0) return { note: "公开卡为空", submitted: 0 };
      const res = await h.submit(clients[0], roundId, snap, { kind: "sort.order", groups });
      return { submitted: res.status < 400 ? 1 : 0, status: res.status, body: res.json?.code ?? null };
    },
  },

  pedigree: {
    players: 1,
    modeConfig: {},
    async drive(clients, roundId, h) {
      const snap = await h.snapshot(clients[0], roundId);
      const slots = snap.publicState?.slots ?? [];
      const candidates = snap.publicState?.candidates ?? [];
      if (slots.length === 0 || candidates.length === 0) {
        return { note: "槽位或候选为空", submitted: 0 };
      }
      const assignments = slots.map((slot, i) => ({
        slotId: slot.slotId,
        horseId: candidates[i % candidates.length].horseId ?? candidates[i % candidates.length].id,
      }));
      const res = await h.submit(clients[0], roundId, snap, {
        kind: "pedigree.assignment",
        assignments,
      });
      return { submitted: res.status < 400 ? 1 : 0, status: res.status, body: res.json?.code ?? null };
    },
  },

  "clue-puzzle": {
    players: 3,
    modeConfig: {},
    async drive(clients, roundId, h) {
      let submitted = 0;
      for (const c of clients) {
        const snap = await h.snapshot(c, roundId);
        const slots = snap.publicState?.slots ?? [];
        const candidates = snap.publicState?.cards ?? [];
        if (slots.length === 0 || candidates.length === 0) continue;
        // 槽位公开字段是 slotIndex；候选卡是 cardId（不是 characterId）。
        const assignments = slots.map((slot, i) => ({
          slotId: String(slot.slotIndex),
          characterId: candidates[i % candidates.length].cardId,
        }));
        const res = await h.submit(c, roundId, snap, { kind: "clue.assignment", assignments });
        if (res.status < 400) submitted += 1;
      }
      return { submitted };
    },
  },

  "strategy-race": {
    players: 2,
    modeConfig: {},
    async drive(clients, roundId, h) {
      // 每回合所有队伍都要提交，回合才会关闭。跑满 totalTurns。
      let submitted = 0;
      const first = await h.snapshot(clients[0], roundId);
      const totalTurns = first.publicState?.track?.totalTurns ?? 8;
      for (let turn = 1; turn <= totalTurns; turn += 1) {
        for (const c of clients) {
          const snap = await h.snapshot(c, roundId);
          const res = await h.submit(c, roundId, snap, {
            kind: "strategy.action",
            action: "steady",
            turn,
          });
          if (res.status < 400) submitted += 1;
        }
      }
      return { submitted, totalTurns };
    },
  },

  rapport: {
    players: 2,
    modeConfig: {},
    async drive(clients, roundId, h) {
      let submitted = 0;
      const first = await h.snapshot(clients[0], roundId);
      const questions = first.publicState?.questions ?? [];
      const total = questions.length;
      for (let q = 0; q < total; q += 1) {
        for (const c of clients) {
          const snap = await h.snapshot(c, roundId);
          // 选项挂在每一道题上：publicState.questions[i].options
          const qs = snap.publicState?.questions ?? [];
          const options = qs[q]?.options ?? [];
          const optionId = options[0]?.optionId ?? options[0]?.id;
          if (!optionId) continue;
          // rapport 每人每局只交一次答案（一题定胜负），所以 revision 直接用快照值。
          const res = await h.submit(c, roundId, snap, { kind: "rapport.answer", optionId });
          if (res.status < 400) submitted += 1;
        }
      }
      return { submitted, total };
    },
  },

  creative: {
    players: 2,
    modeConfig: {},
    /*
     * 创意企划两段式，两段都在 collecting 内完成。
     *
     * 防抄袭的正确边界是「还能改稿的人看不到候选」：一旦自己提交了，稿子就锁死
     * （Apply 拒绝任何后续修改），此时看到候选不可能帮助抄稿。所以投影对
     * 「本人已提交」的玩家公开 ballot，投票才真正可达。
     * （修复前：投票要求 collecting，候选要求非 collecting，玩家没有合法路径。）
     */
    async drive(clients, roundId, h) {
      let works = 0;
      for (const [i, c] of clients.entries()) {
        const snap = await h.snapshot(c, roundId);
        const res = await h.submit(c, roundId, snap, {
          kind: "creative.work",
          text: `第${i + 1}份企划：学园祭上的跑步接力，把接力棒稳稳交到下一位手里。`,
        });
        if (res.status < 400) works += 1;
      }

      // 写作结束后立刻投票：此时每人自己都已提交，ballot 对本人可见。
      let votes = 0;
      for (const c of clients) {
        const snap = await h.snapshot(c, roundId);
        const ballot = snap.publicState?.ballot ?? [];
        /*
         * 自己的作品 id 在 publicState.self.workId。
         *
         * publicState.workIds 实测是空对象（那是「主体 → 作品 id」的预备映射，
         * 投影里没有按这个形状下发），所以不能靠它判断"哪个是我的"。
         * 靠 workIds 判断时 ownId 恒为 null，第一票必然投给自己，
         * 服务端回 cannot_vote_own_work。
         */
        const ownId = snap.publicState?.self?.workId ?? null;
        const pick = ballot.find((b) => b.workId !== ownId);
        if (!pick) {
          console.log("    [creative] 无合法候选", JSON.stringify({
            ballotIds: ballot.map((b) => b.workId),
            ownId,
            hidden: snap.publicState?.ballotHiddenReason ?? null,
          }).slice(0, 260));
          continue;
        }
        const res = await h.submit(c, roundId, snap, {
          kind: "creative.vote",
          optionId: pick.workId,
        }, 0);
        if (res.status < 400) votes += 1;
        else console.log("    [creative] 投票被拒", res.status, res.json?.code ?? null,
          JSON.stringify({
            sentRevision: snap.submissionRevision ?? 0,
            sentPhaseVersion: snap.round?.phaseVersion ?? 0,
            detail: res.json?.detail ?? null,
          }).slice(0, 240));
      }
      return { works, votes };
    },
  },

  trade: {
    players: 3,
    modeConfig: { competitive: false },
    /*
     * 交易不是 submission，是独立端点：
     * POST /rounds/{id}/trades {phaseVersion, recipientMembershipId, offeredInstanceId, requestedInstanceId}
     * POST /rounds/{id}/trades/{tradeId}/responses {accept}
     * 首版手牌公开：publicState.hands = {subjectId: [instanceId...]}。
     * 注意 recipient 字段是 recipientMembershipId，不是 recipientId。
     */
    async drive(clients, roundId, h) {
      const a = clients[0];
      const snapA = await h.snapshot(a, roundId);
      const peer = clients[1];
      const snapB = await h.snapshot(peer, roundId);
      const selfId = snapA.publicState?.self?.subjectId;
      const hands = snapA.publicState?.hands ?? {};
      const others = Object.keys(hands).filter((id) => id !== selfId);
      const mine = hands[selfId] ?? [];
      if (mine.length === 0 || others.length === 0) {
        return { note: "手牌不可见或为空", proposals: 0, selfId, handKeys: Object.keys(hands) };
      }
      const peerId = snapB.publicState?.self?.subjectId;
      const theirCards = hands[peerId] ?? [];
      if (theirCards.length === 0) return { note: "对手手牌为空", proposals: 0 };

      const propose = await a.call(
        "POST",
        `/api/v1/rounds/${roundId}/trades`,
        {
          phaseVersion: snapA.round?.phaseVersion ?? 0,
          recipientMembershipId: peerId,
          offeredInstanceId: mine[0],
          requestedInstanceId: theirCards[0],
        },
        { idempotency: true },
      );
      let accepted = false;
      let respondStatus = null;
      let respondCode = null;
      // 提案响应的 tradeId 可能在顶层或 feedback 里；两个都试，取到为止。
      const tradeId = propose.json?.tradeId ?? propose.json?.id ?? propose.json?.feedback?.tradeId;
      if (propose.status < 400 && tradeId) {
        const respond = await peer.call(
          "POST",
          `/api/v1/rounds/${roundId}/trades/${tradeId}/responses`,
          { accept: true },
          { idempotency: true },
        );
        accepted = respond.status < 400;
        respondStatus = respond.status;
        respondCode = respond.json?.code ?? null;
      }
      const after = accepted ? await h.snapshot(a, roundId) : null;
      const afterHands = after?.publicState?.hands ?? {};
      const exchanged = accepted && (afterHands[selfId] ?? []).includes(theirCards[0])
        && !(afterHands[selfId] ?? []).includes(mine[0])
        && (afterHands[peerId] ?? []).includes(mine[0])
        && !(afterHands[peerId] ?? []).includes(theirCards[0]);
      return {
        proposals: propose.status < 400 ? 1 : 0,
        accepted,
        exchanged,
        proposeStatus: propose.status,
        proposeCode: propose.json?.code ?? null,
        tradeId: tradeId ?? null,
        respondStatus,
        respondCode,
      };
    },
  },

};

async function runMode(modeId) {
  const spec = DRIVERS[modeId];
  const bucket = { modeId, assertions: [], steps: [] };
  report.modes.push(bucket);
  console.log(`\n[settle] ${modeId}（${spec.players} 人）`);

  try {
    const { roomId, clients } = await makeRoom(modeId, spec);
    bucket.roomId = roomId;
    assert(modeId, "建房间成功", Boolean(roomId), { roomId });

    const created = await clients[0].call(
      "POST",
      `/api/v1/rooms/${roomId}/rounds`,
      { modeConfig: { modeId, ...spec.modeConfig } },
      { idempotency: true },
    );
    if (created.status >= 400) {
      assert(modeId, "开局成功", false, created.json);
      return;
    }
    const roundId = created.json.roundId;
    bucket.roundId = roundId;
    assert(modeId, "开局成功", Boolean(roundId), { roundId, phase: created.json.phase });

    let pv = created.json.phaseVersion ?? 0;
    pv = await transition(clients[0], roundId, "briefing", pv);
    pv = await transition(clients[0], roundId, "collecting", pv);
    assert(modeId, "推进到 collecting", true, { phaseVersion: pv });

    const helpers = { snapshot, submit };
    const driven = await spec.drive(clients, roundId, helpers);
    bucket.driven = driven;
    console.log(`  驱动结果`, JSON.stringify(driven).slice(0, 200));

    // Require a real submission before validating settlement
    const submitCount =
      (driven.submitted ?? 0) + (driven.works ?? 0) + (driven.votes ?? 0) + (driven.proposals ?? 0);
    assert(modeId, "本局有真实提交（不是空局）", submitCount > 0, { driven });
    if (modeId === "trade") {
      assert(modeId, "真正的交易对象确认成功", driven.accepted === true, { driven });
      assert(modeId, "交换后双方卡牌归属一致", driven.exchanged === true, { driven });
    }

    // 收卷 → （可选第二段投票）→ 揭晓 → 结算
    pv = await transition(clients[0], roundId, "locked", pv);

    /*
     * 两段式玩法在 locked 之后还有动作（creative 的投票）。
     * 放在这里而不是 drive 里，是因为候选只在写作阶段结束后才公开。
     */

    pv = await transition(clients[0], roundId, "revealed", pv);
    pv = await transition(clients[0], roundId, "settled", pv);

    const final = await snapshot(clients[0], roundId);
    bucket.finalPhase = final.round?.status ?? final.round?.phase;
    assert(modeId, "回合到达 settled", bucket.finalPhase === "settled", { phase: bucket.finalPhase });

    /*
     * 结算分数从回合视图本身读（契约里没有 /rounds/{id}/results 端点，
     * 结果是 round.results 字段）。单人练习局按规则不产生竞技分，那属于
     * 合法结果，如实记录，不当失败。
     */
    const rows = final.round?.results ?? final.results ?? [];
    bucket.results = rows;
    assert(modeId, "回合视图带 results 字段", Array.isArray(rows), {
      type: typeof rows,
      count: Array.isArray(rows) ? rows.length : null,
    });
    if (rows.length > 0) {
      const numeric = rows.filter((r) => typeof (r.scoreUnits ?? r.score ?? r.units) === "number");
      assert(modeId, "结算分数是真实数值（不是占位空值）", numeric.length > 0, {
        sample: rows.slice(0, 3),
      });
    } else {
      // 单人/练习局不产出竞技分是规则允许的；记录而不伪造。
      const practice = spec.players === 1;
      assert(modeId, "无竞技分时是单人练习局（符合规则）", practice, {
        players: spec.players,
        note: "多人局没有分数才是问题",
      });
    }
  } catch (error) {
    assert(modeId, "完整一局执行", false, String(error).slice(0, 300));
  }
}

async function main() {
  await mkdir(EVIDENCE, { recursive: true });
  const health = await fetch(`${API}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error("[settle] 后端未就绪");
    process.exitCode = 1;
    return;
  }

  for (const modeId of Object.keys(DRIVERS)) {
    await runMode(modeId);
  }

  const all = report.modes.flatMap((m) => m.assertions);
  report.summary = {
    modes: report.modes.length,
    assertions: all.length,
    passed: all.filter((a) => a.ok).length,
    failed: report.failures.length,
  };
  await writeFile(
    resolve(EVIDENCE, "modes-settle-e2e.json"),
    `${JSON.stringify(report, null, 2)}\n`,
    "utf8",
  );
  console.log(
    `\n[settle] ${report.modes.length} 个玩法，${report.summary.passed}/${report.summary.assertions} 断言通过，${report.failures.length} 失败`,
  );
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
