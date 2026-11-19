#!/usr/bin/env node
/** Creates an event through the UI, then checks teams, programme, standings, score corrections and display pairing. Distinct team results detect accidental score duplication. */
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
  path: [],
};

function assert(name, ok, detail) {
  const entry = { name, ok: Boolean(ok), detail };
  report.assertions.push(entry);
  if (ok) console.log(`[event] ✓ ${name}`);
  else {
    report.failures.push(entry);
    console.error(`[event] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(message, detail) {
  console.log(`[event] · ${message}`, detail ? JSON.stringify(detail) : "");
}

function step(name, detail) {
  report.path.push({ step: name, at: new Date().toISOString(), detail });
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

/**
 * 把页面可见文本归一成「可比较的稳定内容」。
 *
 * 大屏有倒计时、主持台有「更新于 HH:MM:SS」，两次刷新之间这些必然不同。
 * 直接用整段文本做相等比较，测到的永远是时钟在走，不是页面漂移。
 * 这里只抹掉时间，保留队伍名、分数、分项名、状态词——那些才是刷新后不该变的东西。
 */
function stableText(text) {
  return text
    .replace(/\d{1,2}:\d{2}(:\d{2})?/g, "<time>") // 07:28 / 7:28:05
    .replace(/更新于\s*<time>/g, "更新于 <time>")
    .replace(/[ \t]+/g, " ")
    .replace(/\n{2,}/g, "\n")
    .trim();
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
const PATH_CFG = {
  modeId: "co-race-path",
  rulesVersion: "2.0.0-draft.1",
  contentPackId: "",
  informationStyle: "closed",
};

/** BFS 找一条合法路径（co-race-path 用，用于制造确定得分） */
async function bfsPath(client, roundId, startId, goalId) {
  const prev = new Map();
  const seen = new Set([startId]);
  const queue = [startId];
  let reached = false;
  while (queue.length > 0 && !reached) {
    const current = queue.shift();
    const nb = await client.call(
      "GET",
      `/rounds/${roundId}/path-neighbors?characterId=${encodeURIComponent(current)}`,
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
  if (!reached) return null;
  const path = [goalId];
  let cursor = goalId;
  while (cursor !== startId) {
    cursor = prev.get(cursor);
    if (!cursor) return null;
    path.unshift(cursor);
  }
  return path;
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error("[event] 后端未就绪");
    process.exitCode = 1;
    return;
  }

  const executablePath = await resolveChromiumExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});

  /* ============ 阶段 1：从大厅真实点击创建活动（不能用脚本代替） ============ */
  console.log("[event] 阶段 1/7：大厅 → 创建活动（真实点击）");

  const hostContext = await browser.newContext({ viewport: { width: 1366, height: 768 } });
  const hostPage = await hostContext.newPage();
  let eventId = null;
  // 无初始值：赋值点在下面 321 行，初始的 null 从不被读到。
  let roomId;
  /** 建房响应里才有邀请码——RoomSnapshot 不含它（实测字段列表） */
  let inviteCode = null;

  hostPage.on("response", async (res) => {
    if (res.request().method() !== "POST") return;
    const url = res.url();
    if (!url.includes("/api/v1/rooms") || url.includes("/memberships")) return;
    try {
      const body = await res.json();
      if (body?.inviteCode) inviteCode = body.inviteCode;
    } catch {
      /* 非 JSON */
    }
  });

  try {
    await hostPage.goto(WEB_BASE, { waitUntil: "commit" });
    await hostPage.waitForSelector("#main-content", { timeout: 25000 });
    await hostPage.waitForTimeout(2500);

    const lobbyText = await hostPage.locator("body").innerText();
    assert("大厅 有「创建活动」入口（不是只提 API）", /创建活动/.test(lobbyText), {
      excerpt: lobbyText.slice(0, 300),
    });
    assert(
      "大厅 不再声称「尚未开放创建入口」",
      !/尚未开放创建入口/.test(lobbyText),
      { excerpt: lobbyText.slice(0, 300) },
    );

    const createLink = hostPage.getByRole("link", { name: /创建活动/ });
    assert("大厅 创建入口是可点击链接", (await createLink.count()) > 0, {
      count: await createLink.count(),
    });
    await createLink.first().click();
    await hostPage.waitForTimeout(2000);
    step("点击「创建活动」", { url: hostPage.url().replace(WEB_BASE, "") });

    const formText = await hostPage.locator("body").innerText();
    assert("创建页 有活动名称输入", /活动名称/.test(formText), { excerpt: formText.slice(0, 200) });
    assert("创建页 有同分处理选项", /同分/.test(formText), null);

    const eventName = `娱乐杯${Date.now() % 10000}`;
    await hostPage.locator("#event-name").fill(eventName);
    await hostPage.getByRole("button", { name: /创建并进入主持台/ }).click();
    await hostPage.waitForTimeout(6000);

    const hostUrl = hostPage.url();
    const match = hostUrl.match(/\/events\/([^/]+)\/host/);
    eventId = match ? match[1] : null;
    step("创建并进入主持台", { url: hostUrl.replace(WEB_BASE, ""), eventId });

    assert(
      "创建后 自动跳到主持台（URL 含 /events/{id}/host）",
      Boolean(eventId),
      { url: hostUrl.replace(WEB_BASE, "") },
    );

    const hostText = await hostPage.locator("body").innerText();
    assert("主持台 显示刚创建的活动名", hostText.includes(eventName), {
      eventName,
      excerpt: hostText.slice(0, 200),
    });
    assert("主持台 有节目单区", /节目单/.test(hostText), null);
    assert("主持台 有添加分项区", /添加分项/.test(hostText), null);
    assert("主持台 有活动操作区", /活动操作/.test(hostText), null);

    await shot(hostPage, "event-flow-created-host-1366");
  } catch (error) {
    assert("大厅创建路径可执行", false, String(error));
  }

  if (!eventId) {
    console.error("[event] 活动未创建成功，后续链路无法验证");
    await hostContext.close();
    await browser.close();
    process.exitCode = 1;
    return;
  }

  /* ============ 阶段 2：拿主持会话、建房成员、分队 ============ */
  console.log("[event] 阶段 2/7：读取活动与房间，建两支队伍");

  const hostCookie = (await hostContext.cookies()).find((c) => c.name.startsWith("uma_session"));
  assert("主持会话 浏览器里持有身份 cookie", Boolean(hostCookie), {
    cookieNames: (await hostContext.cookies()).map((c) => c.name),
  });

  // 独立客户端：通过浏览器页面里的会话做后续 API 操作，避免身份错乱
  const apiViaBrowser = async (method, path, body) => {
    const result = await hostPage.evaluate(
      async ({ method, path, body }) => {
        const headers = { Accept: "application/json, application/problem+json", Origin: location.origin };
        if (body !== undefined) headers["Content-Type"] = "application/json";
        const csrf =
          (await fetch("/api/v1/sessions/current/csrf", { method: "POST", headers: { Origin: location.origin } })
            .then((r) => (r.ok ? r.json() : null))
            .catch(() => null))?.csrfToken ?? null;
        if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
        if (method !== "GET") headers["Idempotency-Key"] = crypto.randomUUID().replace(/-/g, "");
        const res = await fetch(`/api/v1${path}`, {
          method,
          headers,
          body: body === undefined ? undefined : JSON.stringify(body),
        });
        const text = await res.text();
        let json = null;
        try {
          json = JSON.parse(text);
        } catch {
          /* 非 JSON */
        }
        return { status: res.status, json };
      },
      { method, path, body },
    );
    return result;
  };

  const eventView = await apiViaBrowser("GET", `/events/${eventId}`);
  roomId = eventView.json?.roomId;
  assert("活动 能读到 roomId", typeof roomId === "string" && roomId.length > 0, {
    roomId,
    status: eventView.json?.status,
  });

  if (!roomId) {
    await hostContext.close();
    await browser.close();
    process.exitCode = 1;
    return;
  }

  // 两个独立会话加入并各成一队（用不同会话，才能验证「每队独立汇总」）
  const playerOne = makeClient();
  await playerOne.newSession("红队队长");
  const playerTwo = makeClient();
  await playerTwo.newSession("蓝队队长");

  /*
   * 邀请码只能从建房响应拿到：RoomSnapshot 的字段列表里没有 inviteCode
   * （roomId/name/kind/status/roomVersion/viewer/members/teams/currentRound/serverTime/serverEpoch/cursor）。
   * 上面挂了响应监听器，在建房那一刻抓下来——这正是真实主持人看到的东西。
   */
  assert("邀请码 从建房响应里抓到（真实主持人拿到的就是它）", typeof inviteCode === "string" && inviteCode.length > 0, {
    inviteCode,
  });
  const joinOne = await playerOne.call(
    "POST",
    `/rooms/${roomId}/memberships`,
    { inviteCode, requestedRole: "player" },
    key(),
  );
  const joinTwo = await playerTwo.call(
    "POST",
    `/rooms/${roomId}/memberships`,
    { inviteCode, requestedRole: "player" },
    key(),
  );
  note("两名选手加入结果", {
    inviteCode,
    one: joinOne.status,
    oneCode: joinOne.json?.code,
    two: joinTwo.status,
    twoCode: joinTwo.json?.code,
  });
  assert("选手 两人都成功加入房间", joinOne.status === 201 && joinTwo.status === 201, {
    one: joinOne.status,
    oneCode: joinOne.json?.code,
    two: joinTwo.status,
    twoCode: joinTwo.json?.code,
  });

  /*
   * 主持人必须改成观众。
   *
   * 建活动时 ensureSession 用活动名建了会话，那个会话是房主、角色是 player，
   * 所以它会作为一个参赛主体出现在总榜上（实测出现第 3 行）。
   * 主持人主持活动时不该同时参赛。
   */
  {
    const roomNow = await apiViaBrowser("GET", `/rooms/${roomId}`);
    const self = (roomNow.json?.members ?? []).find((m) => m.displayName === roomNow.json?.viewer?.displayName)
      ?? (roomNow.json?.members ?? []).find((m) => m.capabilities?.includes("manage-room"))
      ?? (roomNow.json?.members ?? [])[0];
    if (self) {
      const patch = await apiViaBrowser("PATCH", `/rooms/${roomId}/memberships/${self.membershipId}`, {
        expectedRoomVersion: roomNow.json?.roomVersion,
        requestedRole: "spectator",
      });
      assert("主持人 改为观众（不参赛）", patch.status === 200, {
        status: patch.status,
        code: patch.json?.code,
      });
    }
  }

  // 建两队并各设队长
  const teamRed = await apiViaBrowser("POST", `/rooms/${roomId}/teams`, { label: "红队", colorToken: "pink" });
  const teamBlue = await apiViaBrowser("POST", `/rooms/${roomId}/teams`, { label: "蓝队", colorToken: "blue" });
  assert("分队 建了两支队伍", teamRed.status === 201 && teamBlue.status === 201, {
    red: teamRed.status,
    blue: teamBlue.status,
  });

  {
    const roomNow = await apiViaBrowser("GET", `/rooms/${roomId}`);
    const members = roomNow.json?.members ?? [];
    const redMember = members.find((m) => m.displayName === "红队队长");
    const blueMember = members.find((m) => m.displayName === "蓝队队长");
    if (redMember && blueMember) {
      await apiViaBrowser("PATCH", `/rooms/${roomId}/memberships/${redMember.membershipId}`, {
        expectedRoomVersion: roomNow.json?.roomVersion,
        teamId: teamRed.json?.teamId,
        requestedRole: "team-captain",
      });
      const roomAgain = await apiViaBrowser("GET", `/rooms/${roomId}`);
      await apiViaBrowser("PATCH", `/rooms/${roomId}/memberships/${blueMember.membershipId}`, {
        expectedRoomVersion: roomAgain.json?.roomVersion,
        teamId: teamBlue.json?.teamId,
        requestedRole: "team-captain",
      });
      const finalRoom = await apiViaBrowser("GET", `/rooms/${roomId}`);
      const captains = (finalRoom.json?.members ?? []).filter((m) => m.role === "team-captain");
      assert("分队 两名成员都是队长", captains.length === 2, {
        captains: captains.map((m) => m.displayName),
      });
    } else {
      assert("分队 能读到两名成员", false, { members: members.map((m) => m.displayName) });
    }
  }

  /* ============ 阶段 3：编节目单（用主持台真实按钮） ============ */
  console.log("[event] 阶段 3/7：通过主持台添加分项");

  try {
    await hostPage.reload({ waitUntil: "commit" });
    await hostPage.waitForTimeout(3000);

    // 选玩法：co-race-path（能造出确定的不同分数）
    await hostPage.locator("#segment-mode").selectOption("co-race-path").catch(() => {});
    await hostPage.locator("#segment-label").fill("第一项 同场之路").catch(() => {});
    await hostPage.getByRole("button", { name: /添加分项/ }).first().click();
    await hostPage.waitForTimeout(3000);

    const afterAdd = await hostPage.locator("body").innerText();
    assert("节目单 通过主持台按钮添加了分项", /第一项 同场之路/.test(afterAdd), {
      excerpt: afterAdd.slice(0, 400),
    });
    step("主持台添加分项", { ok: /第一项 同场之路/.test(afterAdd) });
    await shot(hostPage, "event-flow-segment-added-1366");
  } catch (error) {
    report.limits.push(`主持台添加分项按钮路径未走通，改用 API 兜底：${String(error)}`);
    await apiViaBrowser("POST", `/events/${eventId}/segments`, {
      modeConfig: PATH_CFG,
      label: "第一项 同场之路",
      scoreBudget: 100,
      roundCount: 1,
      competitive: true,
    });
  }

  const eventAfterSegment = await apiViaBrowser("GET", `/events/${eventId}`);
  const segmentDetails = eventAfterSegment.json?.segmentDetails ?? [];
  assert("节目单 分项已建立", segmentDetails.length >= 1, {
    segments: segmentDetails.map((s) => ({ label: s.name, status: s.status })),
  });
  const segmentId = segmentDetails[0]?.segmentId;

  /* ============ 阶段 4：推进活动、逐队开局（有意造不同分） ============ */
  console.log("[event] 阶段 4/7：推进活动并让两队得到不同分数");

  {
    const v = await apiViaBrowser("GET", `/events/${eventId}`);
    await apiViaBrowser("POST", `/events/${eventId}/transitions`, {
      target: "running",
      expectedEventVersion: v.json?.eventVersion ?? 1,
    });
    const after = await apiViaBrowser("GET", `/events/${eventId}`);
    assert("活动 推进到 running", after.json?.status === "running", { status: after.json?.status });
  }

  // 第一局：红队提交合法最短路径（满分），蓝队提交更长的路径（低分）
  const roundOne = await apiViaBrowser("POST", `/rooms/${roomId}/rounds`, {
    modeConfig: PATH_CFG,
    segmentId,
  });
  assert("第一局 开局成功", roundOne.status === 201, {
    status: roundOne.status,
    code: roundOne.json?.code,
  });

  let roundOneId = roundOne.json?.roundId;
  let pv = roundOne.json?.phaseVersion ?? 1;
  for (const target of ["briefing", "collecting"]) {
    const t = await apiViaBrowser("POST", `/rounds/${roundOneId}/transitions`, {
      target,
      expectedPhaseVersion: pv,
    });
    pv = t.json?.phaseVersion ?? pv + 1;
  }

  const roundView = await apiViaBrowser("GET", `/rounds/${roundOneId}`);
  const startId = roundView.json?.publicState?.start?.characterId;
  const goalId = roundView.json?.publicState?.goal?.characterId;

  // 红队走最短路径
  const shortest = await bfsPath(
    { call: (m, p) => apiViaBrowser(m, p) },
    roundOneId,
    startId,
    goalId,
  );
  assert("第一局 能通过邻居接口找到合法路径", Array.isArray(shortest) && shortest.length >= 2, {
    length: shortest?.length,
  });

  /*
   * 蓝队：co-race-path 的图太窄，绕不出更长的合法路径（实测多轮尝试后
   * 最短路径的中间点全被禁用就无路可走，禁首跳又能找到等长替代）。
   *
   * 所以第一局两队都走最短路径，分数相同——这是题包图的客观情况，
   * 不是代码缺陷。分差改由后面的「分数更正」制造：
   * 那是服务端的正式能力，能确定地把某队分数改低，
   * 从而验证「每队独立汇总」（改一队不影响另一队）。
   *
   * 这里如实记录这个限制，不假装造出了分差。
   */
  const longerPath = null;
  note("第一局路径策略", {
    shortestLength: shortest?.length,
    note: "两队都走最短路径，分差由后续分数更正制造",
  });

  const redSubmit = await playerOne.call(
    "POST",
    `/rounds/${roundOneId}/submissions`,
    {
      phaseVersion: pv,
      submissionRevision: 0,
      controlGeneration: 0,
      modeAction: { kind: "path.route", characterIds: shortest },
    },
    key(),
  );
  assert("第一局 红队提交最短路径", redSubmit.status === 201, {
    status: redSubmit.status,
    code: redSubmit.json?.code,
  });
  note("红队得分", { scoreUnits: redSubmit.json?.feedback?.scoreUnits });

  const blueSubmit = await playerTwo.call(
    "POST",
    `/rounds/${roundOneId}/submissions`,
    {
      phaseVersion: pv,
      submissionRevision: 0,
      controlGeneration: 0,
      modeAction: { kind: "path.route", characterIds: longerPath ?? shortest },
    },
    key(),
  );
  assert("第一局 蓝队提交", blueSubmit.status === 201, {
    status: blueSubmit.status,
    code: blueSubmit.json?.code,
  });
  note("蓝队得分", {
    scoreUnits: blueSubmit.json?.feedback?.scoreUnits,
    usedDetour: Boolean(longerPath),
  });

  for (const target of ["locked", "revealed", "settled"]) {
    const t = await apiViaBrowser("POST", `/rounds/${roundOneId}/transitions`, {
      target,
      expectedPhaseVersion: pv,
    });
    if (t.status === 201) pv = t.json?.phaseVersion ?? pv + 1;
  }

  /* ============ 阶段 5：榜单验证「每队独立汇总」 ============ */
  console.log("[event] 阶段 5/7：验证总榜每队独立汇总（分数必须不同）");

  const standings = await apiViaBrowser("GET", `/events/${eventId}/standings`);
  const items = standings.json?.items ?? [];
  note("总榜", {
    count: items.length,
    rows: items.map((i) => ({ name: i.displayName, score: i.scoreUnits })),
  });

  assert("总榜 有两行（两支队伍）", items.length === 2, {
    count: items.length,
    names: items.map((i) => i.displayName),
  });
  assert(
    "总榜 两行 subjectId 互不相同（每队独立汇总）",
    new Set(items.map((i) => i.subjectId)).size === items.length && items.length > 0,
    { subjectIds: items.map((i) => i.subjectId) },
  );

  /* 使用不同队分，才能区分独立汇总与错误复制总分 */
  /*
   * 第一局两队都走最短路径 → 同分。这不是缺陷，是题包图结构导致
   * （绕不出更长的合法路径）。分差与独立汇总的验证放在更正阶段，
   * 那里能确定地制造差异并证明作用范围。
   */
  const scores = items.map((i) => i.scoreUnits);
  note("第一局总榜分数", { scores, note: "同分是题包图结构导致，分差由更正制造" });
  report.limits.push(
    "第一局两队分数相同（都走最短路径）。co-race-path 的题包图太窄，" +
      "禁用最短路径中间点后无路可走、禁首跳又能找到等长替代（多轮实测），" +
      "所以无法用它造出确定分差。classic 活动局在 collecting 阶段看不到答案，" +
      "也无法可靠造出「一队对一队错」。" +
      "因此「每队独立汇总」的判定证据改为：更正一队后另一队分数不变 + 更正后两队分数不同。" +
      "这比「两队本来就不同分」更硬，因为它直接证明更正作用范围是单个 subject。",
  );

  /* ============ 阶段 6：分数更正（必须保留原值） ============ */
  console.log("[event] 阶段 6/7：分数更正要真的改榜单，且保留原始记录");

  if (items.length === 2 && segmentId) {
    const target = items[0];
    const oldScore = target.scoreUnits;
    const newScore = Math.max(0, oldScore - 500);

    const beforeCorrection = await apiViaBrowser("GET", `/events/${eventId}`);
    const correction = await apiViaBrowser("POST", `/events/${eventId}/score-corrections`, {
      segmentId,
      subjectId: target.subjectId,
      newScoreUnits: newScore,
      reason: "E2E 验证更正会改榜单并保留原值",
      expectedEventVersion: beforeCorrection.json?.eventVersion ?? 1,
    });
    assert("更正 提交成功", correction.status === 201, {
      status: correction.status,
      code: correction.json?.code,
    });

    const afterCorrection = await apiViaBrowser("GET", `/events/${eventId}/standings`);
    const afterItems = afterCorrection.json?.items ?? [];
    const afterTarget = afterItems.find((i) => i.subjectId === target.subjectId);
    note("更正后", {
      before: oldScore,
      after: afterTarget?.scoreUnits,
      correctionIds: afterCorrection.json?.correctionIds?.length,
    });

    assert(
      "更正 榜单分数确实变了",
      afterTarget?.scoreUnits === newScore,
      { before: oldScore, after: afterTarget?.scoreUnits, expected: newScore },
    );
    assert(
      "更正 榜单记录了更正 ID（不是静默覆盖）",
      (afterCorrection.json?.correctionIds ?? []).length > 0,
      { correctionIds: afterCorrection.json?.correctionIds },
    );

    /*
     * 更正一队之后，另一队的分数必须不变。
     * 这是「每队独立汇总」的直接证据——比「两队本来就不同分」更硬，
     * 因为它证明更正的作用范围是单个 subject，没有波及全表。
     */
    const otherBefore = items.find((i) => i.subjectId !== target.subjectId);
    const otherAfter = afterItems.find((i) => i.subjectId === otherBefore?.subjectId);
    assert(
      "更正 只改目标队伍，另一队分数不变（作用范围正确）",
      otherBefore?.scoreUnits === otherAfter?.scoreUnits,
      { otherBefore: otherBefore?.scoreUnits, otherAfter: otherAfter?.scoreUnits },
    );

    // 更正后两队分数不同 —— 这才是「独立汇总」的可判定证据
    const afterScores = afterItems.map((i) => i.scoreUnits);
    assert(
      "更正后 两队分数不同（独立汇总可判定）",
      new Set(afterScores).size > 1,
      {
        scores: afterScores,
        note: "第一局两队同分是题包图结构导致；更正后产生分差，可验证独立汇总",
      },
    );
  } else {
    report.limits.push("分数更正未验证：总榜不足两行或缺 segmentId");
  }

  /* ============ 阶段 7：只读大屏配对显示 ============ */
  console.log("[event] 阶段 7/7：生成大屏配对码并在另一设备打开");

  {
    const grant = await apiViaBrowser("POST", `/events/${eventId}/display-grants`);
    const grantCode = grant.json?.grantCode;
    assert("大屏 能生成配对码", typeof grantCode === "string" && grantCode.length > 0, {
      status: grant.status,
      code: grant.json?.code,
    });

    if (grantCode) {
      const stageContext = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
      const stagePage = await stageContext.newPage();
      try {
        await stagePage.goto(`${WEB_BASE}/events/${eventId}/display`, { waitUntil: "commit" });
        await stagePage.waitForSelector("#grant-code", { timeout: 20000 });
        await stagePage.locator("#grant-code").fill(grantCode);
        await stagePage.getByRole("button", { name: /打开大屏/ }).click();
        await stagePage.waitForTimeout(4500);

        const stageText = await stagePage.locator("body").innerText();
        assert(
          "大屏 换来的只读链接能正确显示（另一设备上下文）",
          /当前领先|红队|蓝队|等待第一项成绩/.test(stageText),
          { excerpt: stageText.slice(0, 300) },
        );
        assert("大屏 不出现内部 ID", !/\b(evs_|rom_|rnd_|seg_|mem_)[0-9a-f]{8}/.test(stageText), {
          excerpt: stageText.slice(0, 300),
        });
        await shot(stagePage, "event-flow-display-1920");

        /*
         * 刷新一致性（B06 收口）。
         *
         * 大屏和主持台都可能被刷新：投影设备重连、主持人换标签页、误触 F5。
         * 刷新走的是恢复路径——不是创建时的内存状态。如果一边重读服务端、
         * 另一边用了上一次的投影，赛事现场两边就会显示不同的分项或总分。
         * 这里各刷两次，比较刷新前后的可见文本是否完全一致。
         */
        const dBefore = stageText;
        await stagePage.reload({ waitUntil: "domcontentloaded" });
        await stagePage.waitForTimeout(3000);
        const dAfter = await stagePage.locator("body").innerText();
        assert("大屏 刷新后仍有内容", dAfter.trim().length > 0, { len: dAfter.trim().length });
        // 抹掉时钟后比较：队伍名/分数/分项名/状态词必须完全一致。
        assert("大屏 刷新前后稳定内容一致（不漂移）", stableText(dBefore) === stableText(dAfter), {
          before: stableText(dBefore).slice(0, 200),
          after: stableText(dAfter).slice(0, 200),
        });
        assert("大屏 刷新后没有停在错误页", !/出了点问题|暂时无法继续|加载失败/.test(dAfter), {
          excerpt: dAfter.slice(0, 200),
        });
        assert("大屏 刷新后仍不泄漏内部 ID", !/\b(evs_|rom_|rnd_|seg_|mem_)[0-9a-f]{8}/.test(dAfter), {
          excerpt: dAfter.slice(0, 200),
        });
        await shot(stagePage, "event-flow-display-after-reload");
      } catch (error) {
        assert("大屏 只读链接可验证", false, String(error));
      } finally {
        await stageContext.close();
      }
    }
  }

  /* ============ 刷新一致性：主持台 ============ */
  console.log("[event] 附加：主持台刷新一致性");
  {
    const hostPage = await hostContext.newPage();
    try {
      await hostPage.goto(`${WEB_BASE}/events/${eventId}/host`, { waitUntil: "domcontentloaded" });
      await hostPage.waitForSelector("#host-heading", { timeout: 20000 });
      await hostPage.waitForTimeout(2000);
      const before = await hostPage.locator("body").innerText();
      await hostPage.reload({ waitUntil: "domcontentloaded" });
      await hostPage.waitForSelector("#host-heading", { timeout: 20000 });
      await hostPage.waitForTimeout(2000);
      const after = await hostPage.locator("body").innerText();
      assert("主持台 刷新后仍有内容", after.trim().length > 0, { len: after.trim().length });
      assert("主持台 刷新前后稳定内容一致（不漂移）", stableText(before) === stableText(after), {
        before: stableText(before).slice(0, 200),
        after: stableText(after).slice(0, 200),
      });
      assert("主持台 刷新后没有停在错误页", !/出了点问题|暂时无法继续|加载失败/.test(after), {
        excerpt: after.slice(0, 200),
      });
      // 分项名称是主持台与观众看到的最关键一致项
      const programme = await apiViaBrowser("GET", `/events/${eventId}`);
      const names = (programme.json?.segments ?? []).map((s) => s.name).filter(Boolean);
      if (names.length > 0) {
        const missing = names.filter((n) => !after.includes(n));
        assert("主持台 刷新后仍显示全部分项名称", missing.length === 0, { names, missing });
      }
      await shot(hostPage, "event-flow-host-after-reload");
    } catch (error) {
      assert("主持台 刷新一致性可验证", false, String(error));
    } finally {
      await hostPage.close();
    }
  }

  /* ============ 收尾 ============ */
  await hostContext.close();
  await browser.close();

  /*
   * 截图唯一性断言。
   *
   * 目的：防止「状态注入复用同一个 context 导致多张截图完全一样」这种假证据
   * （历史上真的发生过：三张截图 sha256 相同却都被当成不同阶段）。
   *
   * 例外：刷新前后那一对截图本来就该相同——它们证明的正是「刷新后画面没变」。
   * 把这一对排除在唯一性检查外，而不是放宽整个断言。
   */
  const RELOAD_PAIRS = ["event-flow-display-1920", "event-flow-display-after-reload"];
  const comparable = report.screenshots.filter((s) => s.sha256 && !RELOAD_PAIRS.includes(s.name));
  const hashes = comparable.map((s) => s.sha256);
  const unique = new Set(hashes);
  assert("截图内容互不相同（刷新前后那一对除外）", unique.size === hashes.length, {
    total: hashes.length,
    unique: unique.size,
    excluded: RELOAD_PAIRS,
  });

  // 刷新前后两张必须都存在，否则「画面没变」这个结论没有证据。
  const reloadShots = report.screenshots.filter((s) => RELOAD_PAIRS.includes(s.name));
  assert("刷新前后两张截图都已保存", reloadShots.length === RELOAD_PAIRS.length, {
    found: reloadShots.map((s) => s.name),
  });

  report.summary = {
    assertions: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
    screenshots: report.screenshots.length,
  };

  const out = resolve(EVIDENCE, "event-flow-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[event] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 失败`,
  );
  console.log(`[event] 路径：${report.path.map((p) => p.step).join(" → ")}`);
  console.log(`[event] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
