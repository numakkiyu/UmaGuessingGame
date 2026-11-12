#!/usr/bin/env node
/** Checks rendered text for internal identifiers and terminology, including values inserted at runtime. */
import { mkdir, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { findExecutionLocationWords, readPlayerSurface } from "./player-copy-policy.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "..");
const EVIDENCE = resolve(WEB_ROOT, "../.cache/reports");
const SHOTS = resolve(EVIDENCE, "screenshots");

const WEB_BASE = process.env.UMA_WEB_BASE ?? "http://127.0.0.1:8080";
const API_BASE = process.env.UMA_API_BASE ?? "http://127.0.0.1:8080";

/**
 * 内部 ID 前缀 + 至少 8 位标识。
 *
 * 用前缀而不是裸 `\b[a-z]+_[0-9a-f]{8}`，避免误报正常文案里的下划线组合。
 */
const INTERNAL_ID_PATTERN = /\b(?:rom|rnd|ses|mem|tem|evs|seg|tkt|grn|evt)_[0-9a-zA-Z]{8,}/g;

/** 禁用词（运行时补查，静态检查只覆盖字面量） */
const BANNED_WORDS = [
  "幂等键",
  "Idempotency-Key",
  "CSRF",
  "sessionId",
  "membership",
  "roomVersion",
  "phaseVersion",
  "WebSocket",
  "轮询降级",
  "SQLite",
  "Schema",
  "DTO",
  "OpenAPI",
  "诊断编号",
];

const report = {
  generatedAt: new Date().toISOString(),
  environment: { webBase: WEB_BASE, apiBase: API_BASE },
  pagesChecked: [],
  assertions: [],
  failures: [],
  screenshots: [],
  limits: [],
};

function assert(name, ok, detail) {
  const entry = { name, ok: Boolean(ok), detail };
  report.assertions.push(entry);
  if (ok) console.log(`[visible] ✓ ${name}`);
  else {
    report.failures.push(entry);
    console.error(`[visible] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(message, detail) {
  console.log(`[visible] · ${message}`, detail ? JSON.stringify(detail) : "");
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
const CLASSIC_CFG = {
  modeId: "classic",
  maxWrongGuesses: 8,
  winPolicy: "window-attempts",
  durationSeconds: 0,
  shadowEnabled: false,
};

/** 扫一个页面的可见文本 */
async function scanPage(page, label, url) {
  await page.goto(url, { waitUntil: "commit" });
  await page.waitForTimeout(2500);

  const text = await readPlayerSurface(page);
  const foundIds = [...new Set(text.match(INTERNAL_ID_PATTERN) ?? [])];
  const foundBanned = [...BANNED_WORDS.filter((word) => text.includes(word)), ...findExecutionLocationWords(text)];

  report.pagesChecked.push({
    label,
    url: url.replace(WEB_BASE, ""),
    textLength: text.length,
    internalIds: foundIds,
    bannedWords: foundBanned,
  });

  return { text, foundIds, foundBanned };
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error("[visible] 后端未就绪");
    process.exitCode = 1;
    return;
  }

  /* ---------- 准备数据：房间、回合、活动、票 ---------- */
  const host = makeClient();
  await host.newSession("可见性检查主持");

  // 一个 lobby 房间
  const lobbyRoom = await host.call(
    "POST",
    "/rooms",
    { name: "可见性大厅房", kind: "friend", modeConfig: CLASSIC_CFG, maxPlayers: 8, maxSpectators: 4, visibility: "invite", allowSpectators: true },
    key(),
  );
  const lobbyRoomId = lobbyRoom.json?.room?.roomId;
  const lobbyInvite = lobbyRoom.json?.inviteCode;

  // 加入一个成员，让成员列表非空
  const guest = makeClient();
  await guest.newSession("可见性玩家");
  await guest.call(
    "POST",
    `/rooms/${lobbyRoomId}/memberships`,
    { inviteCode: lobbyInvite, requestedRole: "player" },
    key(),
  );

  // 一个活动 + 分项（host 页与 results 页需要）
  const eventRes = await host.call("POST", "/events", { roomId: lobbyRoomId, name: "可见性杯", tiePolicy: "shared-rank" }, key());
  const eventId = eventRes.json?.eventId;
  await host.call(
    "POST",
    `/events/${eventId}/segments`,
    { modeConfig: CLASSIC_CFG, label: "第一项", scoreBudget: 100, roundCount: 1, competitive: true },
    key(),
  );

  // 一个已开局的房间（对局页）
  const playRoom = await host.call(
    "POST",
    "/rooms",
    { name: "可见性对局房", kind: "solo", modeConfig: CLASSIC_CFG, maxPlayers: 2, maxSpectators: 0, visibility: "invite", allowSpectators: false },
    key(),
  );
  const playRoomId = playRoom.json?.room?.roomId;
  const roundRes = await host.call("POST", `/rooms/${playRoomId}/rounds`, { modeConfig: CLASSIC_CFG }, key());
  const roundId = roundRes.json?.roundId;
  let pv = roundRes.json?.phaseVersion ?? 1;
  for (const target of ["briefing", "collecting"]) {
    const t = await host.call(
      "POST",
      `/rounds/${roundId}/transitions`,
      { target, expectedPhaseVersion: pv },
      key(),
    );
    pv = t.json?.phaseVersion ?? pv + 1;
  }

  // 一个匹配票（匹配页）
  await guest.call("POST", "/matchmaking-tickets", { modeConfig: CLASSIC_CFG }, key());

  /* ---------- 逐页扫描 ---------- */
  const executablePath = await resolveChromiumExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});
  const context = await browser.newContext({ viewport: { width: 1366, height: 768 } });
  const [cookieName, cookieValue] = host.cookie.split("=");
  if (cookieName && cookieValue) {
    await context.addCookies([{ name: cookieName, value: cookieValue, domain: "127.0.0.1", path: "/" }]);
  }
  const page = await context.newPage();

  /** 要扫的页面清单 */
  const targets = [
    { label: "大厅", path: "/" },
    { label: "等待区", path: `/rooms/${lobbyRoomId}` },
    { label: "对局页", path: `/rooms/${playRoomId}` },
    { label: "匹配页", path: "/matchmaking" },
    { label: "主持台", path: `/events/${eventId}/host` },
    { label: "结果页", path: `/events/${eventId}/results` },
    { label: "规则说明", path: "/rules" },
  ];

  try {
    for (const target of targets) {
      const { text, foundIds, foundBanned } = await scanPage(page, target.label, `${WEB_BASE}${target.path}`);

      assert(
        `可见文本 无内部 ID：${target.label}`,
        foundIds.length === 0,
        { found: foundIds, excerpt: text.slice(0, 250) },
      );
      assert(
        `可见文本无禁用词：${target.label}`,
        foundBanned.length === 0,
        { found: foundBanned, excerpt: text.slice(0, 250) },
      );
    }

    // 额外：大屏页（独立路由，需要配对码）
    const grant = await host.call("POST", `/events/${eventId}/display-grants`, undefined, key());
    const grantCode = grant.json?.grantCode;
    if (grantCode) {
      await page.goto(`${WEB_BASE}/events/${eventId}/display`, { waitUntil: "commit" });
      await page.waitForSelector("#grant-code", { timeout: 20000 });
      await page.locator("#grant-code").fill(grantCode);
      await page.getByRole("button", { name: /打开大屏/ }).click();
      await page.waitForTimeout(3500);
      const stageText = await readPlayerSurface(page);
      const stageIds = [...new Set(stageText.match(INTERNAL_ID_PATTERN) ?? [])];
      const stageBanned = [...BANNED_WORDS.filter((word) => stageText.includes(word)), ...findExecutionLocationWords(stageText)];
      report.pagesChecked.push({
        label: "大屏",
        url: `/events/${eventId}/display`,
        textLength: stageText.length,
        internalIds: stageIds,
        bannedWords: stageBanned,
      });
      assert("可见文本 无内部 ID：大屏", stageIds.length === 0, { found: stageIds });
      assert("可见文本无禁用词：大屏", stageBanned.length === 0, { found: stageBanned });
      await page.screenshot({ path: resolve(SHOTS, "visible-display-1920.png") });
    } else {
      report.limits.push("大屏页未扫描：配对码生成失败");
    }
  } catch (error) {
    assert("页面扫描可执行", false, String(error));
  } finally {
    await context.close();
    await browser.close();
  }

  /* ---------- 汇总 ---------- */
  const totalIds = report.pagesChecked.reduce((sum, p) => sum + p.internalIds.length, 0);
  const totalBanned = report.pagesChecked.reduce((sum, p) => sum + p.bannedWords.length, 0);
  note("扫描汇总", {
    pages: report.pagesChecked.length,
    totalInternalIds: totalIds,
    totalBannedWords: totalBanned,
  });

  report.summary = {
    pages: report.pagesChecked.length,
    assertions: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
    screenshots: report.screenshots.length,
  };

  const out = resolve(EVIDENCE, "player-visible-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[visible] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 失败（扫描 ${report.pagesChecked.length} 个页面）`,
  );
  console.log(`[visible] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
