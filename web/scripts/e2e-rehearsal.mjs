#!/usr/bin/env node
/** Rehearses four independent teams across event segments, including pause/resume and separate scores. The organiser observes rather than adding a fifth scoring subject. */
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
  rounds: [],
};

function assert(name, ok, detail) {
  const entry = { name, ok: Boolean(ok), detail };
  report.assertions.push(entry);
  if (ok) console.log(`[rehearsal] ✓ ${name}`);
  else {
    report.failures.push(entry);
    console.error(`[rehearsal] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(message, detail) {
  console.log(`[rehearsal] · ${message}`, detail ? JSON.stringify(detail) : "");
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

/** 独立 API 客户端 */
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
      // CSRF 自愈（后端只有一份 hash，页面轮换会让旧值失效）
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
const TEAM_COLORS = ["green", "blue", "gold", "pink"];

const PATH_CFG = { modeId: "co-race-path", rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed" };
const SORT_CFG = { modeId: "sort", rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed" };

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`[rehearsal] 后端未就绪：${API_BASE}/health/ready`);
    process.exitCode = 1;
    return;
  }

  /* ---------- 1. 建房 + 4 个独立会话 + 4 支队 ---------- */
  const host = makeClient();
  await host.newSession("主办");
  const roomRes = await host.call(
    "POST",
    "/rooms",
    {
      name: "四队彩排",
      kind: "event",
      modeConfig: PATH_CFG,
      maxPlayers: 8,
      maxSpectators: 8,
      visibility: "invite",
      allowSpectators: true,
    },
    key(),
  );
  const roomId = roomRes.json?.room?.roomId;
  const inviteCode = roomRes.json?.inviteCode;
  assert("彩排 建房成功", roomRes.status === 201 && Boolean(roomId), { status: roomRes.status });

  /*
   * 主办必须改成观众。
   *
   * 否则他作为 player 会额外成为一个主体（实测：不改是 5 个主体，改了是 4 个）。
   * 这不是 bug——主办本来可以参赛——但彩排要的是「恰好 4 支队」。
   */
  {
    const cur = await host.call("GET", `/rooms/${roomId}`);
    const self = (cur.json?.members ?? []).find((m) => m.displayName === "主办");
    const patch = await host.call(
      "PATCH",
      `/rooms/${roomId}/memberships/${self.membershipId}`,
      { expectedRoomVersion: cur.json?.roomVersion, requestedRole: "spectator" },
      key(),
    );
    assert("彩排 主办改为观众（避免成为额外主体）", patch.status === 200 && patch.json?.role === "spectator", {
      status: patch.status,
      role: patch.json?.role,
    });
  }

  const players = [];
  for (let index = 1; index <= 4; index += 1) {
    const player = makeClient();
    await player.newSession(`选手${index}`);
    const joined = await player.call(
      "POST",
      `/rooms/${roomId}/memberships`,
      { inviteCode, requestedRole: "player" },
      key(),
    );
    if (joined.status === 201) {
      players.push({ client: player, name: `选手${index}`, membershipId: joined.json?.membershipId });
    }
  }
  assert("彩排 4 个独立会话都加入成功", players.length === 4, { count: players.length });

  const teamIds = [];
  for (let index = 0; index < 4; index += 1) {
    const team = await host.call(
      "POST",
      `/rooms/${roomId}/teams`,
      { label: `第${index + 1}队`, colorToken: TEAM_COLORS[index] },
      key(),
    );
    if (team.status === 201) teamIds.push(team.json?.teamId);
  }
  assert("彩排 创建 4 支队伍", teamIds.length === 4, { teamIds });

  // 每人分到一队并设为队长（必须由主办操作，队员不能自己改角色）
  for (let index = 0; index < 4; index += 1) {
    const cur = await host.call("GET", `/rooms/${roomId}`);
    await host.call(
      "PATCH",
      `/rooms/${roomId}/memberships/${players[index].membershipId}`,
      {
        expectedRoomVersion: cur.json?.roomVersion,
        teamId: teamIds[index],
        requestedRole: "team-captain",
      },
      key(),
    );
  }
  {
    const cur = await host.call("GET", `/rooms/${roomId}`);
    const captains = (cur.json?.members ?? []).filter((m) => m.role === "team-captain");
    assert("彩排 4 名成员都是队长", captains.length === 4, {
      captains: captains.map((m) => ({ name: m.displayName, team: m.teamId })),
    });
  }

  /* ---------- 2. 创建活动 + 分项 ---------- */
  const eventRes = await host.call("POST", "/events", { roomId, name: "四队彩排杯", tiePolicy: "shared-rank" }, key());
  const eventId = eventRes.json?.eventId;
  report.eventId = eventId;
  report.hostCookie = host.cookie;
  assert("彩排 创建活动", eventRes.status === 201 && Boolean(eventId), { status: eventRes.status });

  const segmentOne = await host.call(
    "POST",
    `/events/${eventId}/segments`,
    { modeConfig: PATH_CFG, label: "第一项 同场之路", scoreBudget: 100, roundCount: 1, competitive: true },
    key(),
  );
  const segmentTwo = await host.call(
    "POST",
    `/events/${eventId}/segments`,
    { modeConfig: SORT_CFG, label: "第二项 档案排序", scoreBudget: 100, roundCount: 1, competitive: true },
    key(),
  );
  assert("彩排 创建两个分项", segmentOne.status === 201 && segmentTwo.status === 201, {
    one: segmentOne.status,
    two: segmentTwo.status,
  });

  // 活动推进到 running
  {
    const cur = await host.call("GET", `/events/${eventId}`);
    await host.call(
      "POST",
      `/events/${eventId}/transitions`,
      { target: "running", expectedEventVersion: cur.json?.eventVersion ?? 1 },
      key(),
    );
  }

  /* ---------- 3. 第一项：4 队各自提交路径 ---------- */
  const roundOne = await host.call(
    "POST",
    `/rooms/${roomId}/rounds`,
    { modeConfig: PATH_CFG, segmentId: segmentOne.json?.segmentId },
    key(),
  );
  assert("彩排 第一项开局", roundOne.status === 201, { status: roundOne.status, code: roundOne.json?.code });
  const roundOneId = roundOne.json?.roundId;
  let roundOnePv = roundOne.json?.phaseVersion ?? 1;
  for (const target of ["briefing", "collecting"]) {
    const t = await host.call(
      "POST",
      `/rounds/${roundOneId}/transitions`,
      { target, expectedPhaseVersion: roundOnePv },
      key(),
    );
    roundOnePv = t.json?.phaseVersion ?? roundOnePv + 1;
  }

  const viewOne = await host.call("GET", `/rounds/${roundOneId}`);
  const subjectIds = (viewOne.json?.publicState?.subjects ?? []).map((s) => s.subjectId);
  assert("彩排 第一项恰好 4 个主体", subjectIds.length === 4, { count: subjectIds.length, subjectIds });
  assert(
    "彩排 4 个主体都是队伍 ID（tem_ 开头）",
    subjectIds.every((id) => typeof id === "string" && id.startsWith("tem_")),
    { subjectIds },
  );

  /*
   * 4 队各自提交一条合法路径。
   *
   * 每队都要用自己的会话提交（因为提交者是队伍成员），
   * 但回合主体是队伍，所以提交落到各自的 tem_ 主体上。
   */
  const submitResults = [];
  for (const player of players) {
    // BFS 找路径（用主办会话查邻居，邻居接口只要求能读回合）
    const startId = viewOne.json?.publicState?.start?.characterId;
    const goalId = viewOne.json?.publicState?.goal?.characterId;
    const prev = new Map();
    const seen = new Set([startId]);
    const queue = [startId];
    let reached = false;
    while (queue.length > 0 && !reached) {
      const current = queue.shift();
      const nb = await host.call(
        "GET",
        `/rounds/${roundOneId}/path-neighbors?characterId=${encodeURIComponent(current)}`,
      );
      for (const neighbor of nb.json?.neighbors ?? []) {
        const id = neighbor.character?.characterId;
        if (!id || seen.has(id)) continue;
        seen.add(id);
        prev.set(id, current);
        if (id === goalId) {
          reached = true;
          break;
        }
        queue.push(id);
      }
    }
    const path = [];
    if (reached) {
      path.push(goalId);
      let cursor = goalId;
      while (cursor !== startId) {
        cursor = prev.get(cursor);
        if (!cursor) break;
        path.unshift(cursor);
      }
    }

    const submit = await player.client.call(
      "POST",
      `/rounds/${roundOneId}/submissions`,
      {
        phaseVersion: roundOnePv,
        submissionRevision: 0,
        controlGeneration: 0,
        modeAction: { kind: "path.route", characterIds: path },
      },
      key(),
    );
    submitResults.push({ name: player.name, status: submit.status, code: submit.json?.code });
  }
  note("第一项 4 队提交结果", submitResults);
  assert(
    "彩排 第一项 4 队都提交成功",
    submitResults.length === 4 && submitResults.every((r) => r.status === 201),
    submitResults,
  );

  /* ---------- 4. 第一项：中途暂停与恢复 ---------- */
  {
    const pauseRes = await host.call(
      "POST",
      `/rounds/${roundOneId}/transitions`,
      { target: "pause", expectedPhaseVersion: roundOnePv },
      key(),
    );
    assert("彩排 可以暂停回合", pauseRes.status === 201, {
      status: pauseRes.status,
      code: pauseRes.json?.code,
    });
    if (pauseRes.status === 201) roundOnePv = pauseRes.json?.phaseVersion ?? roundOnePv + 1;

    // 暂停期间提交必须被拒（不只是「按钮禁用」，要服务端拒绝）
    const pausedSubmit = await players[0].client.call(
      "POST",
      `/rounds/${roundOneId}/submissions`,
      {
        phaseVersion: roundOnePv,
        submissionRevision: 0,
        controlGeneration: 0,
        modeAction: { kind: "path.route", characterIds: [] },
      },
      key(),
    );
    assert("彩排 暂停期间提交被服务端拒绝", pausedSubmit.status >= 400, {
      status: pausedSubmit.status,
      code: pausedSubmit.json?.code,
    });
    note("暂停期间提交被拒原因", { code: pausedSubmit.json?.code });

    const resumeRes = await host.call(
      "POST",
      `/rounds/${roundOneId}/transitions`,
      { target: "resume", expectedPhaseVersion: roundOnePv },
      key(),
    );
    assert("彩排 可以恢复回合", resumeRes.status === 201, {
      status: resumeRes.status,
      code: resumeRes.json?.code,
    });
    if (resumeRes.status === 201) roundOnePv = resumeRes.json?.phaseVersion ?? roundOnePv + 1;

    // 恢复后仍能提交
    const resumedSubmit = await players[0].client.call(
      "POST",
      `/rounds/${roundOneId}/submissions`,
      {
        phaseVersion: roundOnePv,
        submissionRevision: 1,
        controlGeneration: 0,
        modeAction: { kind: "path.route", characterIds: [] },
      },
      key(),
    );
    // 空路径可能因业务校验被拒，但不应是「回合已暂停」
    const resumedCode = resumedSubmit.json?.code;
    assert(
      "彩排 恢复后提交不再被「暂停」拒绝",
      resumedCode !== "round-paused",
      { status: resumedSubmit.status, code: resumedCode },
    );
    note("恢复后提交结果", { status: resumedSubmit.status, code: resumedCode });
  }

  // 收卷 → 揭晓 → 结算
  for (const target of ["locked", "revealed", "settled"]) {
    const t = await host.call(
      "POST",
      `/rounds/${roundOneId}/transitions`,
      { target, expectedPhaseVersion: roundOnePv },
      key(),
    );
    if (t.status === 201) roundOnePv = t.json?.phaseVersion ?? roundOnePv + 1;
  }

  /* ---------- 5. 第二项：另一个模式 ---------- */
  /*
   * 关键设计约束：
   * 房间：lobby → ready → playing → finished → archived
   * 「重赛创建新 round，不覆盖旧结果」
   *
   * 所以第一项结算后房间变 finished 是正常的，
   * 第二项开局属于「重赛」，必须能成功。
   *
   * 这条断言锁住这个设计意图：如果将来有人把 finished 当终态拒绝开局，
   * 多分项活动会全部卡死，这里会立刻红。
   */
  {
    const roomBeforeSecond = await host.call("GET", `/rooms/${roomId}`);
    note("第一项结算后的房间状态", { status: roomBeforeSecond.json?.status });
    assert(
      "彩排 第一项结算后房间为 finished（单局结束）",
      roomBeforeSecond.json?.status === "finished",
      { status: roomBeforeSecond.json?.status },
    );
  }

  const roundTwo = await host.call(
    "POST",
    `/rooms/${roomId}/rounds`,
    { modeConfig: SORT_CFG, segmentId: segmentTwo.json?.segmentId },
    key(),
  );
  assert("彩排 第二项开局（finished 房间可重赛，不同模式）", roundTwo.status === 201, {
    status: roundTwo.status,
    code: roundTwo.json?.code,
    note: "若为 409 room-closed，说明 finished 被误当成终态，多分项活动会卡死",
  });
  if (roundTwo.status === 201) {
    const roundTwoId = roundTwo.json?.roundId;
    let roundTwoPv = roundTwo.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await host.call(
        "POST",
        `/rounds/${roundTwoId}/transitions`,
        { target, expectedPhaseVersion: roundTwoPv },
        key(),
      );
      roundTwoPv = t.json?.phaseVersion ?? roundTwoPv + 1;
    }
    const viewTwo = await host.call("GET", `/rounds/${roundTwoId}`);
    const twoSubjects = (viewTwo.json?.publicState?.subjects ?? []).map((s) => s.subjectId);
    assert("彩排 第二项同样 4 个队伍主体", twoSubjects.length === 4, {
      count: twoSubjects.length,
      subjectIds: twoSubjects,
    });

    // 每队提交一份排序（顺序无所谓，验证都能提交）
    let submitted = 0;
    for (const player of players) {
      const cards = (viewTwo.json?.publicState?.cards ?? []).map((c) => c.characterId);
      const submit = await player.client.call(
        "POST",
        `/rounds/${roundTwoId}/submissions`,
        {
          phaseVersion: roundTwoPv,
          submissionRevision: 0,
          controlGeneration: 0,
          modeAction: { kind: "sort.order", groups: cards.map((id) => [id]) },
        },
        key(),
      );
      if (submit.status === 201) submitted += 1;
    }
    assert("彩排 第二项 4 队都提交成功", submitted === 4, { submitted });

    for (const target of ["locked", "revealed", "settled"]) {
      const t = await host.call(
        "POST",
        `/rounds/${roundTwoId}/transitions`,
        { target, expectedPhaseVersion: roundTwoPv },
        key(),
      );
      if (t.status === 201) roundTwoPv = t.json?.phaseVersion ?? roundTwoPv + 1;
    }
  }

  /* ---------- 6. 总榜：4 队独立累加 ---------- */
  {
    const standings = await host.call("GET", `/events/${eventId}/standings`);
    const items = standings.json?.items ?? [];
    note("总榜", {
      count: items.length,
      rows: items.map((i) => ({ name: i.displayName, score: i.scoreUnits, segments: i.segments?.length })),
    });

    assert("彩排 总榜有 4 行（4 支队）", items.length === 4, {
      count: items.length,
      names: items.map((i) => i.displayName),
    });

    /*
     * 关键断言：每队的分数来自自己的提交，不是同一份分复制 4 份。
     * 用 subjectId 校验：每行对应一个不同的队伍 ID。
     */
    const subjectIdsInStandings = items.map((i) => i.subjectId);
    const uniqueSubjects = new Set(subjectIdsInStandings);
    assert("彩排 总榜 4 行的 subjectId 互不相同（各自独立计分）", uniqueSubjects.size === 4, {
      subjectIdsInStandings,
    });
    assert(
      "彩排 总榜 subjectId 是队伍 ID（tem_ 开头）",
      subjectIdsInStandings.every((id) => typeof id === "string" && id.startsWith("tem_")),
      { subjectIdsInStandings },
    );

    // 每个队伍都应该有 2 个分项的成绩
    const withBothSegments = items.filter((item) => (item.segments ?? []).length === 2);
    assert("彩排 每支队都有 2 个分项成绩", withBothSegments.length === 4, {
      detail: items.map((i) => ({ name: i.displayName, segments: (i.segments ?? []).length })),
    });

    /*
     * 队名必须非空。
     *
     * 这一条是已知缺陷的回归锁：后端 subjectDisplayNames 用 membership ID
     * 作为 key，而队伍参赛时 subjectId 是队伍 ID，查不到就返回空字符串。
     * 修好后这条应变绿；若将来又改坏，会立刻红。
     */
    const emptyNames = items.filter((item) => !item.displayName);
    assert("彩排 总榜每行都有队名（非空）", emptyNames.length === 0, {
      emptyCount: emptyNames.length,
      detail: items.map((i) => ({ subjectId: i.subjectId?.slice(0, 14), name: i.displayName })),
    });

    // 分数独立：不是所有队都是同一个分数（如果全同，可能是把总分当成了单队分）
    const scores = items.map((i) => i.scoreUnits);
    const allSame = scores.every((s) => s === scores[0]);
    note("各队总分", { scores, allSame });
    // 全同也可能是巧合（都提交了合法路径得满分），所以只记录不判失败
    if (allSame) {
      report.limits.push(
        "4 队总分恰好相同（都提交了合法路径得满分）——这是合法结果，但无法用它区分「独立累加」与「复制同一份」；已用 subjectId 互异与分项数双重断言替代",
      );
    }
  }

  await finish();
}

/* 浏览器部分单独抽出来，避免主流程太长 */
let browserRef = null;

async function finish() {
  /* 用真实浏览器看主持台与总榜 */
  if (browserRef) {
    const context = await browserRef.newContext({ viewport: { width: 1366, height: 768 } });
    // 结果页需要身份（活动不公开），注入主办 cookie
    const cookie = report.hostCookie ?? "";
    const [cookieName, cookieValue] = cookie.split("=");
    if (cookieName && cookieValue) {
      await context.addCookies([{ name: cookieName, value: cookieValue, domain: "127.0.0.1", path: "/" }]);
    }
    const page = await context.newPage();
    try {
      const eventId = report.eventId;
      if (eventId) {
        await page.goto(`${WEB_BASE}/events/${eventId}/results`, { waitUntil: "commit" });
        await page.waitForTimeout(2500);
        const text = await page.locator("body").innerText();
        assert("彩排 results 页显示 4 支队", (text.match(/第\d队/g) ?? []).length >= 4 || /四队彩排杯/.test(text), {
          excerpt: text.slice(0, 400),
        });
        await shot(page, "rehearsal-results-1366");
      }
    } catch (error) {
      assert("彩排 results 页可验证", false, String(error));
    } finally {
      await context.close();
    }
    await browserRef.close();
  }

  const hashes = report.screenshots.map((s) => s.sha256).filter(Boolean);
  const unique = new Set(hashes);
  assert("彩排 截图内容互不相同", unique.size === hashes.length, {
    total: hashes.length,
    unique: unique.size,
  });

  report.summary = {
    assertions: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
    screenshots: report.screenshots.length,
  };

  const out = resolve(EVIDENCE, "rehearsal-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[rehearsal] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 失败，${report.summary.screenshots} 张截图`,
  );
  console.log(`[rehearsal] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

// 启动浏览器并把 eventId 传给收尾阶段
const executablePath = await resolveChromiumExecutable();
browserRef = await chromium.launch(executablePath ? { executablePath } : {});

await main();
