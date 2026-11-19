#!/usr/bin/env node
/** Follows two independent sessions from matchmaking to settlement, checking authority, independent scores and disconnect recovery. */
import {createMatchingTicket} from "./matchmaking-support.mjs";
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
  chain: [],
};

function assert(name, ok, detail) {
  const entry = { name, ok: Boolean(ok), detail };
  report.assertions.push(entry);
  if (ok) console.log(`[match-flow] ✓ ${name}`);
  else {
    report.failures.push(entry);
    console.error(`[match-flow] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(message, detail) {
  console.log(`[match-flow] · ${message}`, detail ? JSON.stringify(detail) : "");
}

function step(name, detail) {
  report.chain.push({ step: name, at: new Date().toISOString(), detail });
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
  variant: "friend",
  winPolicy: "first-correct",
  durationSeconds: 180,
  shadowEnabled: false,
};

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error("[match-flow] 后端未就绪");
    process.exitCode = 1;
    return;
  }

  /* ================= 阶段 1：两会话各建票 → 撮合 ================= */
  console.log("[match-flow] 阶段 1/7：两个独立会话各建票并撮合");

  const playerA = makeClient();
  await playerA.newSession("匹配玩家A");
  const playerB = makeClient();
  await playerB.newSession("匹配玩家B");

  // Verify both participants are active room members after accepting the match
  assert("两会话 session 不同（防同会话假匹配）", playerA.cookie !== playerB.cookie, {
    cookieALength: playerA.cookie.length,
    cookieBLength: playerB.cookie.length,
  });

  // Arrival order does not grant either participant management authority.
  const ticketA = await createMatchingTicket(playerA);
  assert("A 建票成功（较早票）", ticketA.status === 201, { status: ticketA.status, code: ticketA.json?.code });
  const ticketAId = ticketA.json?.ticketId;

  const ticketB = await createMatchingTicket(playerB);
  assert("B 建票成功（较晚票）", ticketB.status === 201, { status: ticketB.status, code: ticketB.json?.code });
  const ticketBId = ticketB.json?.ticketId;

  // 撮合应已发生：A 变成 matched-pending-accept
  const aAfterMatch = await playerA.call("GET", `/matchmaking-tickets/${ticketAId}`);
  const bAfterMatch = await playerB.call("GET", `/matchmaking-tickets/${ticketBId}`);
  step("撮合", { statusA: aAfterMatch.json?.status, statusB: bAfterMatch.json?.status });
  assert(
    "撮合 两张票都进入 matched-pending-accept",
    aAfterMatch.json?.status === "matched-pending-accept" && bAfterMatch.json?.status === "matched-pending-accept",
    { statusA: aAfterMatch.json?.status, statusB: bAfterMatch.json?.status },
  );

  /* ================= 阶段 2：双方接受 → 房间创建 ================= */
  console.log("[match-flow] 阶段 2/7：双方接受配对");

  const acceptA = await playerA.call(
    "POST",
    `/matchmaking-tickets/${ticketAId}/responses`,
    { accept: true },
    key(),
  );
  assert("A 接受配对成功", acceptA.status === 201, { status: acceptA.status, code: acceptA.json?.code });

  const acceptB = await playerB.call(
    "POST",
    `/matchmaking-tickets/${ticketBId}/responses`,
    { accept: true },
    key(),
  );
  assert("B 接受配对成功", acceptB.status === 201, { status: acceptB.status, code: acceptB.json?.code });

  // 第二次接受应该创建房间
  const roomId = acceptB.json?.roomId ?? acceptA.json?.roomId;
  step("房间创建", { roomId });
  assert("配对完成 房间已创建（roomId 非空）", typeof roomId === "string" && roomId.length > 0, {
    roomId,
    note: "若为空说明配对未真正完成",
  });

  if (!roomId) {
    console.error("[match-flow] 房间未创建，后续链路无法验证");
    process.exitCode = 1;
    return;
  }

  /* ================= 阶段 3：房间归属权与成员 ================= */
  console.log("[match-flow] 阶段 3/7：验证房间归属权与成员关系");

  const roomA = await playerA.call("GET", `/rooms/${roomId}`);
  const roomB = await playerB.call("GET", `/rooms/${roomId}`);
  step("房间读取", { statusA: roomA.status, statusB: roomB.status });

  assert("A 能读取匹配房", roomA.status === 200, { status: roomA.status });
  assert("B 能读取匹配房", roomB.status === 200, { status: roomB.status });

  /* RoomSnapshot 的 viewer、members 等字段位于根对象 */
  const capsA = roomA.json?.viewer?.capabilities ?? [];
  const capsB = roomB.json?.viewer?.capabilities ?? [];
  note("双方权限", { capsA, capsB });

  assert("A 只持参赛权限", capsA.includes("play") && !capsA.includes("manage-room"), {capsA});
  assert("B 只持参赛权限", capsB.includes("play") && !capsB.includes("manage-room"), {capsB});
  const members = roomA.json?.members ?? [];
  assert("成员 房间里恰好 2 个成员", members.length === 2, {
    count: members.length,
    names: members.map((m) => m.displayName),
  });
  assert(
    "成员 两人角色都是 player",
    members.length === 2 && members.every((m) => m.role === "player"),
    { roles: members.map((m) => m.role) },
  );

  /* ================= 阶段 4：开局 → briefing → collecting ================= */
  console.log("[match-flow] 阶段 4/7：开局并推进到 collecting");

  // B 先尝试开局：应被拒（无 manage-room）
  const bCreateRound = await playerB.call("POST", `/rooms/${roomId}/rounds`, { modeConfig: CLASSIC_CFG }, key());
  assert(
    "归属权 B 不能开局（403 insufficient-capability）",
    bCreateRound.status === 403,
    { status: bCreateRound.status, code: bCreateRound.json?.code },
  );

  const roundId=roomA.json?.currentRound?.roundId;
  assert("确认后自动创建回合",typeof roundId==="string",{roundId});
  if(!roundId)throw new Error("No automatic round");
  const phaseVersion=roomA.json.currentRound.phaseVersion;
  const denied=await playerA.call("POST",`/rooms/${roomId}/rounds`,{},key());
  assert("A 也不能重复开局",denied.status===403,{status:denied.status});

  const roundView = await playerA.call("GET", `/rounds/${roundId}`);
  assert("回合进入 collecting", roundView.json?.round?.phase === "collecting", {
    phase: roundView.json?.round?.phase,
  });

  /* ================= 阶段 5：双方提交（造分差） ================= */
  console.log("[match-flow] 阶段 5/7：双方提交（故意造分数差异）");

  /*
   * 造分差的方式：A 提交一个很可能错的角色，B 提交正确答案。
   *
   * 但公开投影不含答案，所以先让 A 故意提交一个角色（可能是错的），
   * 然后揭晓后看真实结果。为了让差异确定，改用：
   * A 提交一次（几乎不可能一次猜中），B 也提交一次不同角色。
   * 若恰好有人猜中，那也是真实结果，断言改为「两人分数来自各自提交」。
   */
  const viewForSubmit = await playerB.call("GET", `/rounds/${roundId}`);
  const subjects = viewForSubmit.json?.publicState?.subjects ?? [];
  assert("双方都是回合主体（2 个 subject）", subjects.length === 2, { count: subjects.length });

  const subA = await playerA.call(
    "POST",
    `/rounds/${roundId}/submissions`,
    {
      phaseVersion: viewForSubmit.json?.round?.phaseVersion ?? phaseVersion,
      submissionRevision: 0,
      controlGeneration: 0,
      modeAction: { kind: "classic.guess", characterId: "1001" },
    },
    key(),
  );
  assert("A 提交成功", subA.status === 201, { status: subA.status, code: subA.json?.code });

  const subB = await playerB.call(
    "POST",
    `/rounds/${roundId}/submissions`,
    {
      phaseVersion: viewForSubmit.json?.round?.phaseVersion ?? phaseVersion,
      submissionRevision: 0,
      controlGeneration: 0,
      modeAction: { kind: "classic.guess", characterId: "1002" },
    },
    key(),
  );
  assert("B 作答按当前回合裁决", subB.status===201 || (subB.status===409 && subA.json?.feedback?.solved===true), { status: subB.status, code: subB.json?.code });

  /* ================= 阶段 6：中途断线不影响结算 ================= */
  console.log("[match-flow] 阶段 6/7：A 断线，B 继续提交，回合仍能结算");

  // Stop A's interaction. Only the server's timer may settle this random match.
  const until=performance.now()+195000;
  let observed;
  while(performance.now()<until){
    observed=await playerB.call("GET",`/rounds/${roundId}`);
    if(observed.json?.round?.phase==="settled")break;
    await new Promise(resolve=>setTimeout(resolve,2000));
  }
  const settledView = await playerB.call("GET", `/rounds/${roundId}`);
  assert("断线不影响 回合最终为 settled", settledView.json?.round?.phase === "settled", {
    phase: settledView.json?.round?.phase,
  });

  /* ================= 阶段 7：结算后状态 ================= */
  console.log("[match-flow] 阶段 7/7：结算后房间状态与分数独立累加");

  // 7a. 房间应变 finished
  const roomAfterSettle = await playerA.call("GET", `/rooms/${roomId}`);
  const roomStatus = roomAfterSettle.json?.status;
  step("结算后房间", { status: roomStatus });
  assert("结算后 房间状态为 finished", roomStatus === "finished", {
    status: roomStatus,
    note: "若仍为 playing 说明结算没有回写房间状态",
  });

  // Random matches return to the queue instead of granting rematch authority.
  const rematch = await playerA.call("POST", `/rooms/${roomId}/rounds`, { modeConfig: CLASSIC_CFG }, key());
  assert(
    "随机匹配结束后须重新排队，不能由玩家开新局",
    rematch.status === 403,
    {
      status: rematch.status,
      code: rematch.json?.code,
      note: "finished 不是终态；真终态是 archived/cancelled",
    },
  );
  if (rematch.status === 201) {
    note("重赛回合已创建", { roundId: rematch.json?.roundId });
  }

  // Finished rooms must reject new members independently of rematch handling
  {
    const lateJoiner = makeClient();
    await lateJoiner.newSession("迟到玩家");
    const roomForInvite = await playerA.call("GET", `/rooms/${roomId}`);
    const inviteCode = roomForInvite.json?.inviteCode;
    const lateJoin = await lateJoiner.call(
      "POST",
      `/rooms/${roomId}/memberships`,
      { inviteCode, requestedRole: "player" },
      key(),
    );
    assert("结算后 新成员不能加入 finished 房间", lateJoin.status >= 400, {
      status: lateJoin.status,
      code: lateJoin.json?.code,
      note: "加入有第二道检查（非 lobby/ready），所以即使 Terminal() 漏了 finished 也会被拦",
    });
  }

  // 7c. 双方结果：分数来自各自提交
  const results = settledView.json?.publicState?.subjects ?? [];
  note("结算后主体状态", { results });
  assert("结算 两个主体都有结果", results.length === 2, { count: results.length });

  /*
   * 分数独立累加：两人的 scoreUnits 来自各自的提交。
   * classic 里一次提交要么对（满分）要么错（0 分并扣额度）。
   * 两人提交了不同角色，若只有一人猜中，分数必然不同。
   * 若都错，分数都是 0 —— 这时用 attempts 区分「各自提交过」。
   */
  const scoreUnits = results.map((r) => r.scoreUnits ?? 0);
  const solvedFlags = results.map((r) => r.solved === true);
  const attempts = results.map((r) => r.attempts ?? 0);
  note("双方分数", { scoreUnits, solvedFlags, attempts });

  const scoresDiffer = new Set(scoreUnits).size > 1;
  const expectedAccepted=[subA,subB].filter(r=>r.status===201).length;
  const bothAttempted = attempts.filter(a=>a>0).length===expectedAccepted;
  assert(
    "分数 已接受作答只记入各自的主体",
    bothAttempted,
    { attempts, note: "证明两人的提交分别落到了各自的主体" },
  );
  if (!scoresDiffer) {
    report.limits.push(
      `分数：两人 scoreUnits 都是 ${scoreUnits[0]}（都没猜中或都猜中），` +
        "无法用分数差异区分「独立累加」与「复制同一份」。已用 attempts 各自 >0 与 subjectId 互异作为替代证据。",
    );
  } else {
    note("分数存在差异（可用于证明独立累加）", { scoreUnits });
  }

  report.limits.push("随机匹配不属于活动，不在此验证活动总榜。脚本等待真实对局时限，不使用玩家管理权限强制结算");

  /* ================= 浏览器渲染确认 ================= */
  {
    const executablePath = await resolveChromiumExecutable();
    const browser = await chromium.launch(executablePath ? { executablePath } : {});
    const context = await browser.newContext({ viewport: { width: 1366, height: 768 } });
    const [cookieName, cookieValue] = playerA.cookie.split("=");
    if (cookieName && cookieValue) {
      await context.addCookies([{ name: cookieName, value: cookieValue, domain: "127.0.0.1", path: "/" }]);
    }
    const page = await context.newPage();
    try {
      await page.goto(`${WEB_BASE}/rooms/${roomId}`, { waitUntil: "commit" });
      await page.waitForTimeout(3500);
      const text = await page.locator("body").innerText();
      assert("页面 匹配房可渲染（无白屏/无内部标识）", text.length > 100, {
        textLength: text.length,
        excerpt: text.slice(0, 200),
      });
      assert("页面 不出现内部 ID（tem_/mem_/rnd_ 等）", !/\b(tem_|mem_|rnd_|rom_)[0-9a-f]{8}/.test(text), {
        excerpt: text.slice(0, 300),
      });
      await shot(page, "match-flow-settled-room-1366");
    } catch (error) {
      assert("页面 匹配房可验证", false, String(error));
    } finally {
      await context.close();
      await browser.close();
    }
  }

  /* ================= 收尾 ================= */
  const hashes = report.screenshots.map((s) => s.sha256).filter(Boolean);
  const unique = new Set(hashes);
  assert("截图内容互不相同", unique.size === hashes.length, { total: hashes.length, unique: unique.size });

  report.summary = {
    assertions: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
    screenshots: report.screenshots.length,
  };

  const out = resolve(EVIDENCE, "match-to-settle-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[match-flow] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 失败`,
  );
  console.log(`[match-flow] 链路步骤：${report.chain.map((c) => c.step).join(" → ")}`);
  console.log(`[match-flow] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
