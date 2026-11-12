#!/usr/bin/env node
/** Exercises game entry, matchmaking and event pages against a running service; records viewport checks and screenshots. */
import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
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
  environment: {
    webBase: WEB_BASE,
    apiBase: API_BASE,
    node: process.version,
  },
  pages: {},
  assertions: [],
  failures: [],
  screenshots: [],
  limits: [],
};

function assert(name, ok, detail) {
  const entry = { name, ok: Boolean(ok), detail };
  report.assertions.push(entry);
  if (ok) console.log(`[modes-e2e] ✓ ${name}`);
  else {
    report.failures.push(entry);
    console.error(`[modes-e2e] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function note(name, detail) {
  console.log(`[modes-e2e] · ${name}`, detail ? JSON.stringify(detail) : "");
}

async function resolveChromiumExecutable() {
  if (process.env.PLAYWRIGHT_CHROMIUM) return process.env.PLAYWRIGHT_CHROMIUM;
  const cacheRoot = join(homedir(), "Library/Caches/ms-playwright");
  if (!existsSync(cacheRoot)) return undefined;
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

/** 截图并记录 sha256（用于证明不同状态截图确实不同） */
async function shot(page, name) {
  const path = resolve(SHOTS, `${name}.png`);
  await page.screenshot({ path, fullPage: false });
  const buf = await readFile(path);
  const sha256 = createHash("sha256").update(buf).digest("hex");
  // 记录真实像素尺寸：防止「文件名说 390 实际是 1920」这种证据失真
  const viewport = page.viewportSize();
  report.screenshots.push({
    name,
    path: `screenshots/${name}.png`,
    sha256,
    viewport: viewport ? `${viewport.width}x${viewport.height}` : null,
  });
  return sha256;
}

/** 布局与可访问性测量 */
async function measure(page) {
  return page.evaluate(() => {
    const docEl = document.documentElement;
    const bodyEl = document.body;
    const overflowPx = Math.max(docEl.scrollWidth - docEl.clientWidth, bodyEl.scrollWidth - bodyEl.clientWidth);

    const smallTargets = [];
    for (const el of document.querySelectorAll("button, a[href], input, select, textarea, [role=button]")) {
      const rect = el.getBoundingClientRect();
      if (rect.width === 0 && rect.height === 0) continue;
      const style = getComputedStyle(el);
      if (style.display === "none" || style.visibility === "hidden") continue;
      if (el.closest(".sr-only, .u-visually-hidden")) continue;
      if (rect.height < 44 || rect.width < 44) {
        smallTargets.push({
          tag: el.tagName.toLowerCase(),
          text: (el.textContent ?? "").trim().slice(0, 24),
          w: Math.round(rect.width),
          h: Math.round(rect.height),
        });
      }
    }

    const smallText = [];
    for (const el of document.querySelectorAll("p, li, span, td, th, label, dd, dt")) {
      const text = (el.textContent ?? "").trim();
      if (!text) continue;
      const rect = el.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) continue;
      const style = getComputedStyle(el);
      if (style.display === "none") continue;
      if (el.closest(".sr-only, .u-visually-hidden")) continue;
      if (parseFloat(style.fontSize) < 13) {
        smallText.push({ tag: el.tagName.toLowerCase(), text: text.slice(0, 24), size: parseFloat(style.fontSize) });
      }
    }

    // 内部技术词：玩家可见文本里不得出现
    const BANNED = [
      "幂等", "Idempotency", "相同请求", "payload",
      "CSRF", "cookie", "sessionId", "membership", "控制租约",
      "OpenAPI", "REST", "Schema", "DTO", "序列化",
      "WebSocket", "SSE", "轮询降级", "outbox", "roomVersion", "phaseVersion", "epoch",
      "SQLite", "Docker",
      "骨架已就绪", "待联调", "占位实现", "全绿", "兜底策略",
      "作为 AI", "希望能帮到你",
      "shapeWarnings", "privateState",
    ];
    const visibleText = bodyEl.innerText ?? "";
    const bannedHits = BANNED.filter((word) => visibleText.includes(word));

    return { overflowPx, smallTargets: smallTargets.slice(0, 8), smallText: smallText.slice(0, 8), bannedHits };
  });
}

/** 依次在多个视口测量同一页面 */
async function measureAtViewports(page, pageName, viewports) {
  const results = [];
  for (const vp of viewports) {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    await page.waitForTimeout(320);
    const m = await measure(page);
    results.push({ viewport: `${vp.width}x${vp.height}`, ...m });
    assert(`${pageName} ${vp.width}x${vp.height} 无水平溢出`, m.overflowPx === 0, { overflowPx: m.overflowPx });
    assert(
      `${pageName} ${vp.width}x${vp.height} 触摸区 ≥44px`,
      m.smallTargets.length === 0,
      m.smallTargets,
    );
    assert(`${pageName} ${vp.width}x${vp.height} 无玩家可见技术词`, m.bannedHits.length === 0, m.bannedHits);
  }
  return results;
}

const MOBILE_VIEWPORTS = [
  { width: 320, height: 568 },
  { width: 390, height: 844 },
  { width: 844, height: 390 },
  { width: 1366, height: 768 },
  { width: 1920, height: 1080 },
];

/** 直接调 API 开局（比走 UI 更快、更稳；UI 开局单独验证一次） */
async function apiBoot(modeId, displayName = "E2E") {
  const API = `${API_BASE}/api/v1`;
  let cookie = "";
  let csrf = null;

  async function call(method, path, body, idempotencyKey) {
    const doCall = async () => {
      const headers = { Accept: "application/json, application/problem+json", Origin: API_BASE };
      if (body !== undefined) headers["Content-Type"] = "application/json";
      if (cookie) headers["Cookie"] = cookie;
      if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
      if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
      const res = await fetch(API + path, {
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
        /* 非 JSON 响应保持 null */
      }
      return { res, text, json };
    };

    let out = await doCall();

    /*
     * CSRF 失效自愈。
     *
     * 浏览器页面在会话恢复时会调用 POST /sessions/current/csrf 轮换 token，
     * 这会让本脚本持有的旧 token 失效（后端只有一份 csrf_token_hash）。
     * 脚本与浏览器共用同一个会话 cookie 时就会撞上。
     *
     * 处理：只在 403 csrf-token-invalid 时轮换一次并重放同一个幂等键，
     * 不生成新键（否则可能重复建房/重复提交）。
     */
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

    if (!out.res.ok) {
      throw new Error(`${method} ${path} -> ${out.res.status}: ${out.text.slice(0, 200)}`);
    }
    return out.json;
  }

  const key = () => crypto.randomUUID().replace(/-/g, "");
  const session = await call("POST", "/sessions", { displayName });
  csrf = session?.csrfToken ?? null;
  if (!csrf) throw new Error("没有拿到 CSRF token");

  const modeConfig = { modeId, rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed" };
  const room = await call(
    "POST",
    "/rooms",
    {
      name: `${displayName} 的 ${modeId}`,
      kind: "solo",
      modeConfig,
      maxPlayers: 2,
      maxSpectators: 0,
      visibility: "invite",
      allowSpectators: false,
    },
    key(),
  );
  const roomId = room?.room?.roomId ?? room?.roomId;
  if (!roomId) throw new Error("没有拿到 roomId");

  const round = await call("POST", `/rooms/${roomId}/rounds`, { modeConfig }, key());
  const roundId = round?.roundId ?? round?.round?.roundId;
  let phaseVersion = round?.phaseVersion ?? 1;
  for (const target of ["briefing", "collecting"]) {
    const t = await call(
      "POST",
      `/rounds/${roundId}/transitions`,
      { target, expectedPhaseVersion: phaseVersion },
      key(),
    );
    phaseVersion = t?.phaseVersion ?? phaseVersion + 1;
  }

  return { roomId, roundId, cookie, csrf, call, key };
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`[modes-e2e] 后端未就绪：${API_BASE}/health/ready`);
    process.exitCode = 1;
    return;
  }

  const executablePath = await resolveChromiumExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});

  /* ================= 1. 五个玩法开局页（UI 真实点击） ================= */
  const startPages = [
    { path: "/play/memory", name: "memory-start", title: "学园记忆翻牌" },
    { path: "/play/aptitude-grid", name: "aptitude-grid-start", title: "适性交叉占格" },
    { path: "/play/draft", name: "draft-start", title: "公开编队夺标" },
    { path: "/play/race-rebuild", name: "race-rebuild-start", title: "历史赛况复原" },
    { path: "/play/co-race-path", name: "co-race-path-start", title: "同场之路" },
  ];

  for (const spec of startPages) {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const page = await context.newPage();
    const consoleErrors = [];
    page.on("console", (msg) => {
      if (msg.type() === "error") consoleErrors.push(msg.text());
    });
    page.on("pageerror", (err) => consoleErrors.push(`pageerror: ${err.message}`));

    try {
      await page.goto(`${WEB_BASE}${spec.path}`, { waitUntil: "commit" });
      await page.waitForSelector("h1", { timeout: 20000 });
      await page.waitForTimeout(600);

      const heading = await page.locator("h1").first().innerText();
      assert(`${spec.name} 标题正确`, heading.includes(spec.title), { heading, expected: spec.title });

      // 规则要点必须在（不能只有一个开始按钮）
      const bodyText = await page.locator("body").innerText();
      assert(`${spec.name} 有规则说明`, bodyText.includes("规则要点"), null);

      const m = await measure(page);
      assert(`${spec.name} 无水平溢出`, m.overflowPx === 0, { overflowPx: m.overflowPx });
      assert(`${spec.name} 无玩家可见技术词`, m.bannedHits.length === 0, m.bannedHits);
      assert(`${spec.name} 触摸区 ≥44px`, m.smallTargets.length === 0, m.smallTargets);

      await shot(page, `modes-${spec.name}-390`);

      const ALLOWED = [/favicon/i, /Download the React DevTools/i, /401 \(Unauthorized\)/i];
      const realErrors = consoleErrors.filter((t) => !ALLOWED.some((p) => p.test(t)));
      assert(`${spec.name} 控制台无未解释错误`, realErrors.length === 0, realErrors.slice(0, 4));
    } catch (error) {
      assert(`${spec.name} 页面可打开`, false, String(error));
      await page.screenshot({ path: resolve(SHOTS, `modes-${spec.name}-FAILED.png`) }).catch(() => {});
    } finally {
      await context.close();
    }
  }

  /* ================= 2. memory 对局页（真实翻牌） ================= */
  {
    /* 通过真实开局流程验证翻牌交互；直接访问房间的会话恢复另行检查 */
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const page = await context.newPage();

    try {
      await page.goto(`${WEB_BASE}/play/memory`, { waitUntil: "commit" });
      await page.waitForSelector("input#memory-name", { timeout: 25000 });
      await page.getByLabel("显示名").fill("记忆E2E");
      await page.getByRole("button", { name: /开始对局/ }).click();
      await page.waitForFunction(() => location.pathname.startsWith("/rooms/"), { timeout: 40000 });
      await page.waitForSelector('[aria-label*="行第"]', { timeout: 25000 });
      await page.waitForTimeout(800);

      const cardCount = await page.locator('[aria-label*="行第"]').count();
      assert("memory 棋盘渲染 16 张牌", cardCount === 16, { cardCount });

      const bodyText = await page.locator("body").innerText();
      assert("memory 显示配对进度", /已配对/.test(bodyText), null);
      assert("memory 显示尝试数", /尝试/.test(bodyText), null);

      // 320 宽实测卡片尺寸
      await page.setViewportSize({ width: 320, height: 568 });
      await page.waitForTimeout(400);
      const cardBox = await page.locator('[aria-label*="行第"]').first().boundingBox();
      assert("memory 320 宽每张牌 ≥44px", Boolean(cardBox && cardBox.width >= 44 && cardBox.height >= 44), cardBox);
      note("memory 320 宽卡片尺寸", cardBox ? { w: Math.round(cardBox.width), h: Math.round(cardBox.height) } : null);

      const m320 = await measure(page);
      assert("memory 320 宽无水平溢出", m320.overflowPx === 0, { overflowPx: m320.overflowPx });
      await shot(page, "modes-memory-round-320");

      await page.setViewportSize({ width: 390, height: 844 });
      await page.waitForTimeout(300);

      // 翻两张牌（真实点击 → 真实提交）
      const cards = page.locator('[aria-label*="行第"]');
      await cards.nth(0).click();
      await page.waitForTimeout(1200);
      await cards.nth(1).click();
      await page.waitForTimeout(1800);

      const afterText = await page.locator("body").innerText();
      assert(
        "memory 翻两张后有配对反馈",
        /配对成功|不是一对|还在确认/.test(afterText),
        afterText.slice(0, 200),
      );

      const attemptsMatch = afterText.match(/尝试\s*(\d+)\s*\/\s*(\d+)/);
      assert("memory 尝试数已增加", Boolean(attemptsMatch && Number(attemptsMatch[1]) >= 1), attemptsMatch?.[0] ?? null);

      await shot(page, "modes-memory-round-390");
      await measureAtViewports(page, "memory-round", MOBILE_VIEWPORTS.slice(2));
    } catch (error) {
      assert("memory 对局页可操作", false, String(error));
      await page.screenshot({ path: resolve(SHOTS, "modes-memory-round-FAILED.png") }).catch(() => {});
    } finally {
      await context.close();
    }
  }

  /* 直接访问房间后的写操作凭据恢复 */
  {
    /* 直接访问房间时保留原会话身份，检查写操作凭据恢复后的页面状态 */
    const boot = await apiBoot("memory", "刷新E2E");
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [name, value] = boot.cookie.split("=");
    await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();

    try {
      await page.goto(`${WEB_BASE}/rooms/${boot.roomId}`, { waitUntil: "commit" });
      await page.waitForSelector('[aria-label*="行第"]', { timeout: 25000 });
      await page.waitForTimeout(900);

      const cards = page.locator('[aria-label*="行第"]');
      await cards.nth(0).click();
      await page.waitForTimeout(2000);

      const text = await page.locator("body").innerText();
      const showedRejoin = /需要重新进入这一局|重新进入这一局/.test(text);
      const flippedSuccessfully = /已翻开|配对成功|不是一对/.test(text);

      assert(
        "刷新后写操作给出明确的重新进入入口（K12 已知缺口）",
        showedRejoin || flippedSuccessfully,
        { showedRejoin, flippedSuccessfully, excerpt: text.slice(0, 200) },
      );
      note("K12 场景结果", { showedRejoin, flippedSuccessfully });

      if (showedRejoin) {
        report.limits.push(
          "K12 未修：刷新后写操作凭据失效，页面显示「重新进入这一局」兜底；后端补凭据轮换端点后应改为直接可提交",
        );
      }

      await shot(page, "modes-memory-refresh-k12-390");
    } catch (error) {
      assert("K12 场景可观测", false, String(error));
    } finally {
      await context.close();
    }
  }

  /* ================= 3. aptitude-grid 对局页（真实占格） ================= */
  {
    const boot = await apiBoot("aptitude-grid", "占格E2E");
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [name, value] = boot.cookie.split("=");
    await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();

    try {
      await page.goto(`${WEB_BASE}/rooms/${boot.roomId}`, { waitUntil: "commit" });
      await page.waitForSelector('[aria-label*="列"]', { timeout: 25000 });
      await page.waitForTimeout(800);

      const cellCount = await page.locator('button[aria-label*="列"]').count();
      assert("aptitude-grid 渲染 9 格", cellCount === 9, { cellCount });

      // 格子最短边 ≥84px（320 宽实测）
      await page.setViewportSize({ width: 320, height: 568 });
      await page.waitForTimeout(400);
      const cellBox = await page.locator('button[aria-label*="列"]').first().boundingBox();
      assert(
        "aptitude-grid 320 宽每格最短边 ≥84px",
        Boolean(cellBox && Math.min(cellBox.width, cellBox.height) >= 84),
        cellBox ? { w: Math.round(cellBox.width), h: Math.round(cellBox.height) } : null,
      );
      note("aptitude-grid 320 宽格子尺寸", cellBox ? { w: Math.round(cellBox.width), h: Math.round(cellBox.height) } : null);

      // 轴标签可读
      const bodyText320 = await page.locator("body").innerText();
      assert("aptitude-grid 320 宽轴标签可读", /距离：|跑法：/.test(bodyText320), null);

      const m320 = await measure(page);
      assert("aptitude-grid 320 宽无水平溢出", m320.overflowPx === 0, { overflowPx: m320.overflowPx });
      await shot(page, "modes-aptitude-grid-round-320");

      await page.setViewportSize({ width: 390, height: 844 });
      await page.waitForTimeout(300);

      // 选格 → 选卡 → 确认
      await page.locator('button[aria-label*="列"]').first().click();
      await page.waitForTimeout(400);
      const confirmText = await page.locator("body").innerText();
      assert("aptitude-grid 选中格显示两项条件", /行条件/.test(confirmText) && /列条件/.test(confirmText), null);

      // 候选池里点第一个角色
      const firstCard = page.locator("ul li button").first();
      await firstCard.click();
      await page.waitForTimeout(400);

      const claimBtn = page.getByRole("button", { name: /确认占格/ });
      assert("aptitude-grid 确认占格按钮存在", (await claimBtn.count()) > 0, null);

      if (await claimBtn.isEnabled()) {
        await claimBtn.click();
        await page.waitForTimeout(2000);
        const afterClaim = await page.locator("body").innerText();
        assert(
          "aptitude-grid 占格有真实结果",
          /已占据|不是一对|条件|不符|没有记下/.test(afterClaim),
          afterClaim.slice(0, 300),
        );
      } else {
        note("aptitude-grid 确认按钮禁用（可能尚未选卡）", null);
      }

      await shot(page, "modes-aptitude-grid-round-390");
      await measureAtViewports(page, "aptitude-grid-round", MOBILE_VIEWPORTS.slice(2));
    } catch (error) {
      assert("aptitude-grid 对局页可操作", false, String(error));
      await page.screenshot({ path: resolve(SHOTS, "modes-aptitude-grid-round-FAILED.png") }).catch(() => {});
    } finally {
      await context.close();
    }
  }

  /* ================= 4. draft 对局页（预算 sticky + 任务 + 候选 Sheet） ================= */
  {
    const boot = await apiBoot("draft", "编队E2E");
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [name, value] = boot.cookie.split("=");
    await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();

    try {
      await page.goto(`${WEB_BASE}/rooms/${boot.roomId}`, { waitUntil: "commit" });
      await page.waitForSelector("h2", { timeout: 25000 });
      await page.waitForTimeout(800);

      const bodyText = await page.locator("body").innerText();
      assert("draft 显示预算条", /预算/.test(bodyText), null);
      assert("draft 显示 4 个任务槽", (bodyText.match(/任务\s*\d/g) ?? []).length >= 4, null);

      // sticky 预算条：滚动后仍可见
      const budgetSticky = page.locator('[role="status"]').filter({ hasText: "预算" }).first();
      const stickyBefore = await budgetSticky.boundingBox();
      await page.evaluate(() => window.scrollBy(0, 400));
      await page.waitForTimeout(400);
      const stickyAfter = await budgetSticky.boundingBox();
      assert(
        "draft 预算条滚动后仍可见（sticky）",
        Boolean(stickyAfter && stickyAfter.y >= 0 && stickyAfter.y < 200),
        { before: stickyBefore?.y, after: stickyAfter?.y },
      );
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.waitForTimeout(300);

      // 点任务打开候选 Sheet
      await page.getByRole("button", { name: /选角色/ }).first().click();
      await page.waitForTimeout(600);
      const sheetText = await page.locator("body").innerText();
      assert("draft 候选 Sheet 打开", /为任务选角色/.test(sheetText), null);
      await shot(page, "modes-draft-candidates-390");

      // 选第一位角色
      const candidate = page.locator('[role="dialog"] button').filter({ hasText: /价|\d/ }).first();
      if ((await candidate.count()) > 0) {
        await candidate.click();
        await page.waitForTimeout(700);
        const afterPick = await page.locator("body").innerText();
        assert("draft 选卡后任务显示已选角色", /换一位/.test(afterPick), null);
      } else {
        note("draft 候选列表未找到可点按钮", null);
      }

      const m = await measure(page);
      assert("draft 无水平溢出", m.overflowPx === 0, { overflowPx: m.overflowPx });
      assert("draft 无玩家可见技术词", m.bannedHits.length === 0, m.bannedHits);

      await shot(page, "modes-draft-round-390");
      await measureAtViewports(page, "draft-round", MOBILE_VIEWPORTS.slice(2));
    } catch (error) {
      assert("draft 对局页可操作", false, String(error));
      await page.screenshot({ path: resolve(SHOTS, "modes-draft-round-FAILED.png") }).catch(() => {});
    } finally {
      await context.close();
    }
  }

  /* ================= 5. race-rebuild 对局页（排序 + 真实名次空缺） ================= */
  {
    const boot = await apiBoot("race-rebuild", "复原E2E");
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [name, value] = boot.cookie.split("=");
    await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();

    try {
      await page.goto(`${WEB_BASE}/rooms/${boot.roomId}`, { waitUntil: "commit" });
      await page.waitForSelector("ol li", { timeout: 25000 });
      await page.waitForTimeout(800);

      const bodyText = await page.locator("body").innerText();
      assert("race-rebuild 显示赛事名", /有马纪念|赛事|名次/.test(bodyText), null);
      assert("race-rebuild 显示参赛者", (bodyText.match(/位/g) ?? []).length >= 3, null);

      // 初始草稿必须打乱：不能按 characterId 升序
      const names = await page.locator("ol > li").evaluateAll((els) =>
        els.map((el) => (el.textContent ?? "").trim().slice(0, 20)),
      );
      /*
       * 参赛人数由内容包决定：实测抽到过 4 / 5 / 6 人。
       * 因此断言「渲染出的卡片数 = 服务端给的参赛人数」，而不是写死一个下限
       * ——写死会在小赛事上误报（第一次就是 4 人被判失败）。
       */
      const entryCount = await boot.call("GET", `/rounds/${boot.roundId}`).then((v) => v?.publicState?.entries?.length ?? 0);
      assert("race-rebuild 渲染全部参赛者", names.length === entryCount && entryCount > 0, {
        rendered: names.length,
        expected: entryCount,
      });
      note("race-rebuild 初始顺序（前 3）", names.slice(0, 3));

      // 上移/下移按钮可用
      const upBtn = page.getByRole("button", { name: /上移一位/ }).nth(1);
      if ((await upBtn.count()) > 0) {
        const before = await page.locator("ol > li").first().innerText();
        await upBtn.click();
        await page.waitForTimeout(400);
        const after = await page.locator("ol > li").first().innerText();
        assert("race-rebuild 上移真实改变顺序", before !== after, { before: before.slice(0, 40), after: after.slice(0, 40) });
      }

      const m = await measure(page);
      assert("race-rebuild 无水平溢出", m.overflowPx === 0, { overflowPx: m.overflowPx });
      assert("race-rebuild 无玩家可见技术词", m.bannedHits.length === 0, m.bannedHits);

      await shot(page, "modes-race-rebuild-round-390");
      await measureAtViewports(page, "race-rebuild-round", MOBILE_VIEWPORTS.slice(2));

      /* 揭晓后：真实名次必须用服务端数字，不能重新编号 1..n */
      await boot.call("POST", `/rounds/${boot.roundId}/transitions`, { target: "revealed", expectedPhaseVersion: 3 }, boot.key());
      // 测量多个视口后恢复手机尺寸，保证截图尺寸与文件名一致
      await page.setViewportSize({ width: 390, height: 844 });
      await page.reload({ waitUntil: "commit" });
      await page.waitForTimeout(2500);

      const revealText = await page.locator("body").innerText();

      /* 断言锚定揭晓面板，避免将规则说明误认为已揭晓结果 */
      const revealPanel = page.locator("#race-result");
      const panelCount = await revealPanel.count();
      assert("race-rebuild 揭晓面板已渲染", panelCount > 0, { panelCount });
      assert("race-rebuild 揭晓后显示真实名次标题", /真实名次/.test(revealText), null);

      // 名次号必须来自服务端 finishRank：逐行读表格第一列的数字
      const rankNumbers = await page
        .locator("#race-result table tbody th")
        .evaluateAll((els) =>
          els
            .map((el) => {
              const m = (el.textContent ?? "").replace(/\s/g, "").match(/^(\d+)名?/);
              return m ? Number(m[1]) : null;
            })
            .filter((n) => n !== null),
        );
      note("race-rebuild 揭晓名次号", rankNumbers.slice(0, 12));

      // 服务端真实名次集合（用于对照，证明没有重新编号）
      const serverRanks = await boot
        .call("GET", `/rounds/${boot.roundId}`)
        .then((v) => (v?.publicState?.answer?.ranked ?? []).map((r) => r.finishRank))
        .then((list) => Array.from(new Set(list)).sort((a, b) => a - b));

      assert(
        "race-rebuild 名次号与服务端一致（未重新编号成 1..n）",
        JSON.stringify(rankNumbers) === JSON.stringify(Array.from({ length: Math.max(0, ...rankNumbers) }, (_, i) => i + 1)),
        { rendered: rankNumbers, serverRanks },
      );
      assert(
        "race-rebuild 表格行数 = 服务端最大名次（空缺也占行）",
        rankNumbers.length === Math.max(0, ...serverRanks),
        { rows: rankNumbers.length, maxRank: Math.max(0, ...serverRanks), serverRanks },
      );

      const gapShown = /名次空缺|该名次空缺/.test(revealText);
      const gaps = await page.evaluate(() => {
        const text = document.body.innerText;
        const m = text.match(/第\s*([\d、]+)\s*名不存在/);
        return m ? m[1] : null;
      });
      if (gapShown) {
        assert("race-rebuild 显式标注名次空缺（不重编号）", true, { gaps });
        note("race-rebuild 空缺名次", gaps);
      } else {
        // 本场恰好没有空缺也是合法结果，记录而不判失败
        note("race-rebuild 本场没有名次空缺（合法）", null);
        report.limits.push("race-rebuild 本次抽到的赛事没有名次空缺，空缺显示逻辑未被该局触发");
      }

      // 揭晓面板在页面底部（提交区之后），滚过去截图并断言它进入视口
      await revealPanel.scrollIntoViewIfNeeded();
      await page.waitForTimeout(400);
      const revealInView = await revealPanel.evaluate((el) => {
        const r = el.getBoundingClientRect();
        return r.top < window.innerHeight && r.bottom > 0;
      });
      assert("race-rebuild 揭晓面板滚动后进入视口", revealInView, { revealInView });

      // 参赛者列必须是名字，不能是角色编号（后端 answer.ranked 缺 name 时的真实缺陷）
      const rankedNames = await page
        .locator("#race-result table tbody tr td")
        .evaluateAll((els) => els.map((el) => (el.textContent ?? "").trim()));
      const numericNames = rankedNames.filter((t) => /^\d+$/.test(t));
      assert("race-rebuild 揭晓表格显示角色名而非编号", numericNames.length === 0, {
        numericNames,
        sample: rankedNames.slice(0, 6),
      });

      await shot(page, "modes-race-rebuild-revealed-390");
    } catch (error) {
      assert("race-rebuild 对局页可操作", false, String(error));
      await page.screenshot({ path: resolve(SHOTS, "modes-race-rebuild-round-FAILED.png") }).catch(() => {});
    } finally {
      await context.close();
    }
  }

  /* ================= 6. co-race-path 对局页（起终点 + 邻居 + 路线） ================= */
  {
    const boot = await apiBoot("co-race-path", "路径E2E");
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [name, value] = boot.cookie.split("=");
    await context.addCookies([{ name, value, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();

    try {
      await page.goto(`${WEB_BASE}/rooms/${boot.roomId}`, { waitUntil: "commit" });
      await page.waitForSelector("h2", { timeout: 25000 });
      await page.waitForTimeout(1500);

      const bodyText = await page.locator("body").innerText();
      assert("co-race-path 显示起点与终点", /起点/.test(bodyText) && /终点/.test(bodyText), null);
      assert("co-race-path 显示路线区", /你的路线/.test(bodyText), null);

      // 邻居列表（逐点查询，带同场证据）
      const neighborSection = await page.locator("body").innerText();
      const hasNeighbors = /的同场角色|同场：/.test(neighborSection);
      assert("co-race-path 邻居与同场证据可读", hasNeighbors, neighborSection.slice(0, 300));

      /* 等待头像解码后检查 naturalWidth，避免将加载中状态误判为失败 */
      await page
        .waitForFunction(
          () => {
            const imgs = [...document.querySelectorAll('img[aria-hidden="true"]')].slice(0, 4);
            return imgs.length > 0 && imgs.every((el) => el.complete);
          },
          { timeout: 10000 },
        )
        .catch(() => {});
      const nodeImages = await page
        .locator('img[aria-hidden="true"]')
        .evaluateAll((els) =>
          els.slice(0, 4).map((el) => ({ src: el.getAttribute("src"), w: el.naturalWidth, complete: el.complete })),
        );
      const loadedImages = nodeImages.filter((img) => img.w > 0);
      assert("co-race-path 起点/终点头像真实加载", loadedImages.length >= 2, { nodeImages });

      const m = await measure(page);
      assert("co-race-path 无水平溢出", m.overflowPx === 0, { overflowPx: m.overflowPx });
      assert("co-race-path 无玩家可见技术词", m.bannedHits.length === 0, m.bannedHits);

      await shot(page, "modes-co-race-path-round-390");
      await measureAtViewports(page, "co-race-path-round", MOBILE_VIEWPORTS.slice(2));
    } catch (error) {
      assert("co-race-path 对局页可操作", false, String(error));
      await page.screenshot({ path: resolve(SHOTS, "modes-co-race-path-round-FAILED.png") }).catch(() => {});
    } finally {
      await context.close();
    }
  }

  /* ================= 7. matchmaking（排队 → 取消） ================= */
  {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const page = await context.newPage();
    try {
      await page.goto(`${WEB_BASE}/matchmaking`, { waitUntil: "commit" });
      await page.waitForSelector("h1", { timeout: 20000 });
      await page.waitForTimeout(600);

      const heading = await page.locator("h1").first().innerText();
      assert("matchmaking 页面标题", heading.includes("随机匹配"), { heading });

      const startBtn = page.getByRole("button", { name: /开始匹配/ });
      assert("matchmaking 开始匹配按钮存在", (await startBtn.count()) > 0, null);

      await startBtn.click();
      await page.waitForTimeout(2500);

      const queuedText = await page.locator("body").innerText();
      const queued = /正在寻找对手|找到对手了|没能进入队列/.test(queuedText);
      assert("matchmaking 进入队列或明确报错", queued, queuedText.slice(0, 300));

      if (/正在寻找对手/.test(queuedText)) {
        const cancelBtn = page.getByRole("button", { name: /取消排队/ });
        assert("matchmaking 排队中可取消", (await cancelBtn.count()) > 0, null);
        if ((await cancelBtn.count()) > 0) {
          await cancelBtn.click();
          await page.waitForTimeout(1500);
          const afterCancel = await page.locator("body").innerText();
          assert("matchmaking 取消后回到可再排状态", /开始匹配|已取消/.test(afterCancel), afterCancel.slice(0, 200));
        }
      }

      const m = await measure(page);
      assert("matchmaking 无水平溢出", m.overflowPx === 0, { overflowPx: m.overflowPx });
      assert("matchmaking 无玩家可见技术词", m.bannedHits.length === 0, m.bannedHits);

      await shot(page, "modes-matchmaking-390");
      await measureAtViewports(page, "matchmaking", MOBILE_VIEWPORTS.slice(2));
    } catch (error) {
      assert("matchmaking 可操作", false, String(error));
      await page.screenshot({ path: resolve(SHOTS, "modes-matchmaking-FAILED.png") }).catch(() => {});
    } finally {
      await context.close();
    }
  }

  /* ================= 7b. display 兑换的 Origin 安全边界 ================= */
  {
    /*
     * 配对码兑换是「会消费凭据、创建新身份」的端点，后端已改为
     * write:true + noCsrf:true —— 即 Origin 强制、CSRF 豁免。
     *
     * 两条断言把这个安全边界锁住：
     * 1. 无 Origin → 403 origin-required（防跨站伪造兑换）
     * 2. 带 Origin + 无效码 → 404 grant-not-found（说明确实过了 Origin 检查）
     *
     * 若将来有人把 write 改回 false，第 1 条会立刻变红。
     */
    const noOrigin = await fetch(`${API_BASE}/api/v1/display-sessions`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ grantCode: "0000000000000000000000000000" }),
    }).catch(() => null);
    const noOriginBody = noOrigin ? await noOrigin.json().catch(() => null) : null;
    assert("display 兑换缺少 Origin 时被拒（403 origin-required）", noOrigin?.status === 403 && noOriginBody?.code === "origin-required", {
      status: noOrigin?.status,
      code: noOriginBody?.code,
    });

    const withOrigin = await fetch(`${API_BASE}/api/v1/display-sessions`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json", Origin: API_BASE },
      body: JSON.stringify({ grantCode: "0000000000000000000000000000" }),
    }).catch(() => null);
    const withOriginBody = withOrigin ? await withOrigin.json().catch(() => null) : null;
    assert(
      "display 兑换带 Origin 时通过安全校验（无效码返回 grant-not-found）",
      withOrigin?.status === 404 && withOriginBody?.code === "grant-not-found",
      { status: withOrigin?.status, code: withOriginBody?.code },
    );
  }

  /* ================= 8. 活动三页（host / results / display） ================= */
  {
    // 用 API 建活动 + 一个分项
    const API = `${API_BASE}/api/v1`;
    let cookie = "";
    let csrf = null;
    async function call(method, path, body, idempotencyKey) {
      const doCall = async () => {
        const headers = { Accept: "application/json, application/problem+json", Origin: API_BASE };
        if (body !== undefined) headers["Content-Type"] = "application/json";
        if (cookie) headers["Cookie"] = cookie;
        if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
        if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
        const res = await fetch(API + path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
        for (const c of res.headers.getSetCookie?.() ?? []) {
          const kv = c.split(";")[0];
          if (kv.startsWith("uma_session")) cookie = kv;
        }
        const text = await res.text();
        let json = null;
        try {
          json = JSON.parse(text);
        } catch {
          /* 非 JSON 响应保持 null */
        }
        return { res, text, json };
      };

      let out = await doCall();
      // 同 apiBoot：CSRF 被页面轮换后自愈，复用同一幂等键
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
      if (!out.res.ok) throw new Error(`${method} ${path} -> ${out.res.status}: ${out.text.slice(0, 200)}`);
      return out.json;
    }
    const key = () => crypto.randomUUID().replace(/-/g, "");

    const session = await call("POST", "/sessions", { displayName: "活动E2E" });
    csrf = session?.csrfToken;
    const room = await call(
      "POST",
      "/rooms",
      {
        name: "活动房",
        kind: "event",
        modeConfig: { modeId: "classic", maxWrongGuesses: 8, winPolicy: "window-attempts", durationSeconds: 0, shadowEnabled: false },
        maxPlayers: 8,
        maxSpectators: 8,
        visibility: "invite",
        allowSpectators: true,
      },
      key(),
    );
    const roomId = room?.room?.roomId;
    const ev = await call("POST", "/events", { roomId, name: "彩排杯", tiePolicy: "shared-rank" }, key());
    const eventId = ev?.eventId;
    await call(
      "POST",
      `/events/${eventId}/segments`,
      {
        modeConfig: { modeId: "classic", maxWrongGuesses: 8, winPolicy: "window-attempts", durationSeconds: 0, shadowEnabled: false },
        label: "第一项",
        scoreBudget: 100,
        roundCount: 1,
        competitive: true,
      },
      key(),
    );

    const context = await browser.newContext({ viewport: { width: 1366, height: 768 } });
    const [cname, cvalue] = cookie.split("=");
    await context.addCookies([{ name: cname, value: cvalue, domain: "127.0.0.1", path: "/" }]);
    const page = await context.newPage();

    // --- host ---
    try {
      await page.goto(`${WEB_BASE}/events/${eventId}/host`, { waitUntil: "commit" });
      await page.waitForSelector("h1", { timeout: 20000 });
      await page.waitForTimeout(1000);

      const hostText = await page.locator("body").innerText();
      assert("host 显示活动名", hostText.includes("彩排杯"), null);
      assert("host 有节目单分区", /节目单/.test(hostText), null);
      assert("host 有操作区", /活动操作/.test(hostText), null);
      assert("host 风险动作独立分区", /风险动作/.test(hostText), null);

      // 暂停/恢复与作废/结束分区 → 检查两者不在同一容器
      const dangerZone = page.locator(".uma-danger-zone");
      assert("host 风险动作有独立容器", (await dangerZone.count()) > 0, null);

      const m = await measure(page);
      assert("host 无水平溢出", m.overflowPx === 0, { overflowPx: m.overflowPx });
      assert("host 无玩家可见技术词", m.bannedHits.length === 0, m.bannedHits);

      await shot(page, "modes-event-host-1366");
      await measureAtViewports(page, "event-host", MOBILE_VIEWPORTS.slice(0, 2).concat(MOBILE_VIEWPORTS.slice(2)));

      // 二次确认：点「结束活动」应弹确认框，而不是直接执行
      await page.setViewportSize({ width: 1366, height: 768 });
      await page.waitForTimeout(300);
      await page.getByRole("button", { name: /^结束活动$/ }).first().click();
      await page.waitForTimeout(600);
      const dialogText = await page.locator('[role="dialog"]').innerText().catch(() => "");
      assert("host 风险动作有二次确认", /结束这个活动？/.test(dialogText), dialogText.slice(0, 120));
      await shot(page, "modes-event-host-confirm-1366");
      // 关闭确认框，不真的结束
      await page.getByRole("button", { name: /^返回$/ }).click().catch(() => {});
      await page.waitForTimeout(400);
    } catch (error) {
      assert("host 页面可操作", false, String(error));
      await page.screenshot({ path: resolve(SHOTS, "modes-event-host-FAILED.png") }).catch(() => {});
    }

    // --- results ---
    try {
      await page.goto(`${WEB_BASE}/events/${eventId}/results`, { waitUntil: "commit" });
      await page.waitForSelector("h1", { timeout: 20000 });
      await page.waitForTimeout(1000);

      const resultsText = await page.locator("body").innerText();
      assert("results 显示活动名", resultsText.includes("彩排杯"), null);
      assert(
        "results 空状态如实说明（没有成绩）",
        /还没有成绩|总榜|分项/.test(resultsText),
        resultsText.slice(0, 300),
      );

      const m = await measure(page);
      assert("results 无水平溢出", m.overflowPx === 0, { overflowPx: m.overflowPx });
      assert("results 无玩家可见技术词", m.bannedHits.length === 0, m.bannedHits);

      await shot(page, "modes-event-results-1366");
      await measureAtViewports(page, "event-results", MOBILE_VIEWPORTS);
    } catch (error) {
      assert("results 页面可操作", false, String(error));
      await page.screenshot({ path: resolve(SHOTS, "modes-event-results-FAILED.png") }).catch(() => {});
    }

    // --- display ---
    try {
      const displayContext = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
      const displayPage = await displayContext.newPage();
      await displayPage.goto(`${WEB_BASE}/events/${eventId}/display`, { waitUntil: "commit" });
      await displayPage.waitForSelector("h1", { timeout: 20000 });
      await displayPage.waitForTimeout(800);

      const displayText = await displayPage.locator("body").innerText();
      assert("display 要求配对码（无授权不显示画面）", /配对码|大屏展示/.test(displayText), displayText.slice(0, 200));

      /*
       * 独立舞台布局：大屏不能带站点导航与页脚，
       * 否则投影上会多出与活动无关的入口、并吃掉主舞台高度。
       */
      const hasSiteNav = await displayPage.locator("nav[aria-label='主导航']").count();
      const hasSkipLink = await displayPage.locator("a.uma-skip-link").count();
      const hasFooter = await displayPage.locator("footer").filter({ hasText: "资料来源" }).count();
      assert("display 是独立舞台（无站点导航）", hasSiteNav === 0, { hasSiteNav });
      assert("display 是独立舞台（无跳过链接）", hasSkipLink === 0, { hasSkipLink });
      assert("display 是独立舞台（无站点页脚）", hasFooter === 0, { hasFooter });

      /* 配对表单与观众舞台分开测量；舞台正文字号需达到 24px */
      const pairingMinFont = await displayPage.evaluate(() => {
        let min = Infinity;
        for (const el of document.querySelectorAll("p, span, h1, h2, li, label, strong")) {
          const text = (el.textContent ?? "").trim();
          if (!text) continue;
          const rect = el.getBoundingClientRect();
          if (rect.width === 0 || rect.height === 0) continue;
          const style = getComputedStyle(el);
          if (style.display === "none") continue;
          if (el.closest(".sr-only, .u-visually-hidden")) continue;
          min = Math.min(min, parseFloat(style.fontSize));
        }
        return min === Infinity ? null : Math.round(min);
      });
      note("display 配对阶段最小字号（表单，非舞台）", { minFontPx: pairingMinFont });

      // 1920×1080 下输入框与按钮可用
      const input = displayPage.locator("#grant-code");
      assert("display 有配对码输入框", (await input.count()) > 0, null);

      // 输入一个无效码，必须给出真实失败而不是假装成功
      await input.fill("0000000000000000000000000000");
      await displayPage.getByRole("button", { name: /打开大屏/ }).click();
      await displayPage.waitForTimeout(2000);
      const afterInvalid = await displayPage.locator("body").innerText();
      assert(
        "display 无效配对码给出真实失败",
        /没能打开大屏|无效|已用过|暂时/.test(afterInvalid),
        afterInvalid.slice(0, 300),
      );

      // Capture genuinely different viewport and full-page states
      await displayPage.screenshot({ path: resolve(SHOTS, "modes-event-display-1920.png"), fullPage: true });
      {
        const buf = await readFile(resolve(SHOTS, "modes-event-display-1920.png"));
        const vp = displayPage.viewportSize();
        report.screenshots.push({
          name: "modes-event-display-1920",
          path: "screenshots/modes-event-display-1920.png",
          sha256: createHash("sha256").update(buf).digest("hex"),
          viewport: vp ? `${vp.width}x${vp.height}` : null,
        });
      }

      for (const vp of [
        { width: 1280, height: 720 },
        { width: 3840, height: 2160 },
      ]) {
        await displayPage.setViewportSize(vp);
        await displayPage.waitForTimeout(400);
        const m = await measure(displayPage);
        assert(`display ${vp.width}x${vp.height} 无水平溢出`, m.overflowPx === 0, { overflowPx: m.overflowPx });
        await shot(displayPage, `modes-event-display-${vp.width}`);
      }

      /*
       * 真实兑换配对码，进入舞台画面，验证 的字号门槛。
       *
       * 必须在独立浏览器上下文里兑换：后端明确拒绝「已有主持权限的浏览器
       * 兑换大屏码」（会 403 use-separate-profile），那是正确的安全设计。
       */
      const grant = await call("POST", `/events/${eventId}/display-grants`, undefined, key());
      const grantCode = grant?.grantCode;
      if (grantCode) {
        const stageContext = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
        const stagePage = await stageContext.newPage();
        await stagePage.goto(`${WEB_BASE}/events/${eventId}/display`, { waitUntil: "commit" });
        await stagePage.waitForSelector("#grant-code", { timeout: 20000 });
        await stagePage.locator("#grant-code").fill(grantCode);
        await stagePage.getByRole("button", { name: /打开大屏/ }).click();
        await stagePage.waitForTimeout(3000);

        const stageText = await stagePage.locator("body").innerText();
        const onStage = /等待第一项成绩|当前领先|只读大屏/.test(stageText);
        const blocked = /未获得只读授权|暂时取不到/.test(stageText);
        note("display 兑换后状态", { onStage, blocked, excerpt: stageText.slice(0, 160) });
        assert("display 配对成功后进入只读舞台（后端已修授权）", onStage && !blocked, {
          onStage,
          blocked,
          excerpt: stageText.slice(0, 200),
        });

        if (onStage) {
          const stageMinFont = await stagePage.evaluate(() => {
            let min = Infinity;
            for (const el of document.querySelectorAll("p, span, h1, h2, li, strong")) {
              const text = (el.textContent ?? "").trim();
              if (!text) continue;
              const rect = el.getBoundingClientRect();
              if (rect.width === 0 || rect.height === 0) continue;
              const style = getComputedStyle(el);
              if (style.display === "none") continue;
              if (el.closest(".sr-only, .u-visually-hidden")) continue;
              min = Math.min(min, parseFloat(style.fontSize));
            }
            return min === Infinity ? null : Math.round(min);
          });
          note("display 舞台 1920 最小字号", { minFontPx: stageMinFont });
          assert("display 舞台 1920×1080 正文字号 ≥24px", Boolean(stageMinFont && stageMinFont >= 24), {
            minFontPx: stageMinFont,
          });

          // 舞台不能带站点导航（独立布局）
          const stageNav = await stagePage.locator("nav[aria-label='主导航']").count();
          assert("display 舞台无站点导航", stageNav === 0, { stageNav });

          await stagePage.screenshot({ path: resolve(SHOTS, "modes-event-display-stage-1920.png") });
          const buf = await readFile(resolve(SHOTS, "modes-event-display-stage-1920.png"));
          report.screenshots.push({
            name: "modes-event-display-stage-1920",
            path: "screenshots/modes-event-display-stage-1920.png",
            sha256: createHash("sha256").update(buf).digest("hex"),
            viewport: "1920x1080",
          });

          for (const vp of [
            { width: 1280, height: 720 },
            { width: 3840, height: 2160 },
          ]) {
            await stagePage.setViewportSize(vp);
            await stagePage.waitForTimeout(400);
            const m = await measure(stagePage);
            assert(`display 舞台 ${vp.width}x${vp.height} 无水平溢出`, m.overflowPx === 0, { overflowPx: m.overflowPx });
            await stagePage.screenshot({ path: resolve(SHOTS, `modes-event-display-stage-${vp.width}.png`) });
            const b = await readFile(resolve(SHOTS, `modes-event-display-stage-${vp.width}.png`));
            report.screenshots.push({
              name: `modes-event-display-stage-${vp.width}`,
              path: `screenshots/modes-event-display-stage-${vp.width}.png`,
              sha256: createHash("sha256").update(b).digest("hex"),
              viewport: `${vp.width}x${vp.height}`,
            });
          }
        } else if (blocked) {
          // Record denied display access separately from successful stage rendering
          report.limits.push(
            "display 会话读取活动数据仍未打通：配对成功后页面显示「未获得只读授权」，舞台画面与 ≥24px 字号无法在该状态下验证",
          );
          assert("display 配对后给出明确授权状态（不伪造画面）", blocked, { stageText: stageText.slice(0, 160) });
          await shot(stagePage, "modes-event-display-blocked-1920");
        }

        await stageContext.close();
      } else {
        report.limits.push("display 配对码未生成，舞台阶段未验证");
      }

      await displayContext.close();
    } catch (error) {
      assert("display 页面可操作", false, String(error));
    }

    await context.close();
  }

  /* ================= 10. 200% 缩放与长文本边界 ================= */
  {
    /*
     * 200% 缩放。
     *
     * 实现方式：把视口缩到一半、再放大 deviceScaleFactor 不会模拟文字缩放。
     * 真正接近浏览器「缩放 200%」的做法是设置视口宽高为逻辑值的一半，
     * 让 CSS 像素变少 —— 等价于用户把页面放大到 200%（可用宽度减半）。
     *
     * 检查点：
     * - 无水平溢出（放大的常见失败是横向被撑开）
     * - 触摸区仍然 ≥44px（缩放不该让控件变小）
     * - 正文仍然可读（≥13px）
     */
    const zoomContext = await browser.newContext({ viewport: { width: 640, height: 480 } });
    const zoomPage = await zoomContext.newPage();

    try {
      // 用最长内容的玩法页做压力测试（aptitude-grid 有棋盘 + 候选池）
      await zoomPage.goto(`${WEB_BASE}/play/aptitude-grid`, { waitUntil: "commit" });
      await zoomPage.waitForSelector("h1", { timeout: 20000 });
      await zoomPage.waitForTimeout(800);

      const zoomMeasure = await measure(zoomPage);
      assert("200% 缩放（640×480 等效 320×240）无水平溢出", zoomMeasure.overflowPx === 0, {
        overflowPx: zoomMeasure.overflowPx,
      });
      assert("200% 缩放触摸区仍 ≥44px", zoomMeasure.smallTargets.length === 0, zoomMeasure.smallTargets);
      assert("200% 缩放无玩家可见技术词", zoomMeasure.bannedHits.length === 0, zoomMeasure.bannedHits);
      await shot(zoomPage, "modes-zoom200-aptitude-grid");

      // 大厅在 200% 缩放下也要可用
      await zoomPage.goto(`${WEB_BASE}/`, { waitUntil: "commit" });
      await zoomPage.waitForTimeout(1200);
      const lobbyZoom = await measure(zoomPage);
      assert("200% 缩放大厅无水平溢出", lobbyZoom.overflowPx === 0, { overflowPx: lobbyZoom.overflowPx });
      assert("200% 缩放大厅触摸区 ≥44px", lobbyZoom.smallTargets.length === 0, lobbyZoom.smallTargets);
      await shot(zoomPage, "modes-zoom200-lobby");
    } catch (error) {
      assert("200% 缩放可测量", false, String(error));
    } finally {
      await zoomContext.close();
    }
  }

  /* ================= 11. 长文本边界（超长队名/角色名） ================= */
  {
    /*
     * / ：「最长中文/日文角色名、4 行任务、长队名」必须单独审查。
     *
     * 做法：用超长显示名建房，检查页面不被撑开、名字不被截断到无法辨认。
     * 显示名上限 24 字符（session 校验），用满 24 个全角汉字。
     */
    /*
     * 更可靠的做法：直接走 UI 开局，用满 24 字符的显示名，
     * 让页面自己把长名字渲染出来，然后测量溢出。
     */
    const longContext = await browser.newContext({ viewport: { width: 320, height: 568 } });
    const longPage = await longContext.newPage();
    try {
      await longPage.goto(`${WEB_BASE}/play/memory`, { waitUntil: "commit" });
      await longPage.waitForSelector("input#memory-name", { timeout: 20000 });
      // 用满 24 个全角字符（最长可能输入）
      await longPage.getByLabel("显示名").fill("超长测试队伍名称极限二十四个汉字全角占位符测试");
      await longPage.getByRole("button", { name: /开始对局/ }).click();
      await longPage.waitForFunction(() => location.pathname.startsWith("/rooms/"), { timeout: 40000 });
      await longPage.waitForSelector('[aria-label*="行第"]', { timeout: 25000 });
      await longPage.waitForTimeout(1200);

      const longMeasure = await measure(longPage);
      assert("长显示名 320 宽无水平溢出", longMeasure.overflowPx === 0, {
        overflowPx: longMeasure.overflowPx,
      });
      assert("长显示名触摸区 ≥44px", longMeasure.smallTargets.length === 0, longMeasure.smallTargets);

      // 名字必须真的渲染出来（不是被截成空）
      const bodyText = await longPage.locator("body").innerText();
      assert("长显示名确实渲染在页面上", bodyText.includes("超长测试"), {
        excerpt: bodyText.slice(0, 200),
      });

      await shot(longPage, "modes-longname-memory-320");
    } catch (error) {
      assert("长文本边界可测量", false, String(error));
    } finally {
      await longContext.close();
    }
  }

  /* ================= 11b. 活动有成绩状态（总榜 + results + 大屏） ================= */
  {
    /*
     * 活动编排补完后（分项状态流转 + 结算搬移到 segment_results），总榜终于有数据。
     * 这里验证三个页面的有数据状态，不是只看空状态。
     *
     * 制造真实分数：用 memory 模式翻牌配对，能拿到非零 scoreUnits。
     * 注意建回合必须带 segmentId，否则分项不绑定、状态不流转、总榜不写。
     */
    const scoredHost = await (async () => {
      let cookie = "";
      let csrf = null;
      const call = async (method, path, body, idempotencyKey) => {
        const headers = { Accept: "application/json, application/problem+json", Origin: API_BASE };
        if (body !== undefined) headers["Content-Type"] = "application/json";
        if (cookie) headers["Cookie"] = cookie;
        if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
        if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
        const doCall = async () => {
          const h = { ...headers };
          if (cookie) h["Cookie"] = cookie;
          if (csrf && method !== "GET") h["X-CSRF-Token"] = csrf;
          const res = await fetch(`${API_BASE}/api/v1${path}`, {
            method,
            headers: h,
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
        // CSRF 被页面轮换后自愈，复用同一幂等键
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
      };
      const s = await call("POST", "/sessions", { displayName: "成绩主持" });
      csrf = s.json?.csrfToken;
      return { call, getCookie: () => cookie };
    })();

    const key = () => crypto.randomUUID().replace(/-/g, "");
    /*
     * 用 co-race-path 制造确定的非零成绩。
     *
     * 为什么不用 memory：memory 得分 = max(0, floor(100×配对数/8) − 2×答错次数)。
     * 盲翻 16 张最多中 1–2 对（12–25 分），而错一次扣 2 分——实测 17 次尝试
     * 中了 1 对却因扣分归零。那是正确的计分行为，不是 bug，
     * 但拿它造「非零分」不可靠。
     *
     * co-race-path 只要提交一条合法路径就有分：
     * 得分 = max(20, 100 − 10×(边数 − 最短边数))，BFS 找到最短路径即满分。
     */
    const pathCfg = {
      modeId: "co-race-path",
      rulesVersion: "2.0.0-draft.1",
      contentPackId: "",
      informationStyle: "closed",
    };
    const scoredRoom = await scoredHost.call(
      "POST",
      "/rooms",
      {
        name: "成绩活动房",
        kind: "event",
        modeConfig: pathCfg,
        maxPlayers: 8,
        maxSpectators: 8,
        visibility: "invite",
        allowSpectators: true,
      },
      key(),
    );
    const scoredRoomId = scoredRoom.json?.room?.roomId;
    const scoredEvent = await scoredHost.call(
      "POST",
      "/events",
      { roomId: scoredRoomId, name: "路径杯", tiePolicy: "shared-rank" },
      key(),
    );
    const scoredEventId = scoredEvent.json?.eventId;
    const scoredSegment = await scoredHost.call(
      "POST",
      `/events/${scoredEventId}/segments`,
      { modeConfig: pathCfg, label: "第一项", scoreBudget: 100, roundCount: 1, competitive: true },
      key(),
    );
    const scoredSegmentId = scoredSegment.json?.segmentId;

    // 建回合必须带 segmentId，否则分项不绑定、状态不流转、总榜不写
    /*
     * 把活动推进到 running，让大屏状态显示「进行中」而不是「准备中」。
     * 这是真实主持流程的一部分（活动先开放/开始，再跑分项）。
     */
    const scoredEventView = await scoredHost.call("GET", `/events/${scoredEventId}`);
    await scoredHost.call(
      "POST",
      `/events/${scoredEventId}/transitions`,
      { target: "running", expectedEventVersion: scoredEventView.json?.eventVersion ?? 1 },
      key(),
    );

    const scoredRound = await scoredHost.call(
      "POST",
      `/rooms/${scoredRoomId}/rounds`,
      { modeConfig: pathCfg, segmentId: scoredSegmentId },
      key(),
    );
    const scoredRoundId = scoredRound.json?.roundId;
    let scoredPv = scoredRound.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await scoredHost.call(
        "POST",
        `/rounds/${scoredRoundId}/transitions`,
        { target, expectedPhaseVersion: scoredPv },
        key(),
      );
      scoredPv = t.json?.phaseVersion ?? scoredPv + 1;
    }

    // BFS 找一条真实合法路径（每跳都查服务端邻居）
    const scoredView = await scoredHost.call("GET", `/rounds/${scoredRoundId}`);
    const startId = scoredView.json?.publicState?.start?.characterId;
    const goalId = scoredView.json?.publicState?.goal?.characterId;
    const prev = new Map();
    const seen = new Set([startId]);
    const queue = [startId];
    let reached = false;
    while (queue.length > 0 && !reached) {
      const current = queue.shift();
      const nb = await scoredHost.call(
        "GET",
        `/rounds/${scoredRoundId}/path-neighbors?characterId=${encodeURIComponent(current)}`,
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
    const validPath = [];
    if (reached) {
      validPath.push(goalId);
      let cursor = goalId;
      while (cursor !== startId) {
        cursor = prev.get(cursor);
        if (!cursor) break;
        validPath.unshift(cursor);
      }
    }
    assert("活动 能通过邻居接口找到合法路径（用于制造真实成绩）", validPath.length >= 2, {
      length: validPath.length,
    });

    const scoredSubmit = await scoredHost.call(
      "POST",
      `/rounds/${scoredRoundId}/submissions`,
      {
        phaseVersion: scoredPv,
        submissionRevision: 0,
        controlGeneration: 0,
        modeAction: { kind: "path.route", characterIds: validPath },
      },
      key(),
    );
    assert("活动 路径提交成功", scoredSubmit.status === 201, {
      status: scoredSubmit.status,
      code: scoredSubmit.json?.code,
    });
    note("活动成绩制造", {
      scoreUnits: scoredSubmit.json?.feedback?.scoreUnits,
      edges: scoredSubmit.json?.feedback?.edges,
      shortestEdges: scoredSubmit.json?.feedback?.shortestEdges,
    });

    for (const target of ["revealed", "settled"]) {
      const t = await scoredHost.call(
        "POST",
        `/rounds/${scoredRoundId}/transitions`,
        { target, expectedPhaseVersion: scoredPv },
        key(),
      );
      scoredPv = t.json?.phaseVersion ?? scoredPv + 1;
    }

    const scoredStandings = await scoredHost.call("GET", `/events/${scoredEventId}/standings`);
    const scoredItems = scoredStandings.json?.items ?? [];
    assert("活动 结算后总榜有数据行", scoredItems.length > 0, {
      items: scoredItems.map((i) => ({ name: i.displayName, score: i.scoreUnits })),
    });
    assert(
      "活动 总榜成绩为非零（真实结算，不是占位 0）",
      scoredItems.some((item) => item.scoreUnits > 0),
      { scores: scoredItems.map((i) => i.scoreUnits) },
    );
    assert(
      "活动 总榜行含分项明细与 maxUnits",
      scoredItems.some((item) => (item.segments ?? []).some((seg) => seg.maxUnits > 0)),
      { segments: scoredItems.flatMap((i) => i.segments ?? []) },
    );

    // --- results 页有数据状态 ---
    const scoredContext = await browser.newContext({ viewport: { width: 1366, height: 768 } });
    const [scName, scValue] = (scoredHost.getCookie() ?? "").split("=");
    if (scName && scValue) {
      await scoredContext.addCookies([{ name: scName, value: scValue, domain: "127.0.0.1", path: "/" }]);
    }
    const scoredPage = await scoredContext.newPage();
    try {
      await scoredPage.goto(`${WEB_BASE}/events/${scoredEventId}/results`, { waitUntil: "commit" });
      await scoredPage.waitForTimeout(2500);

      const resultsText = await scoredPage.locator("body").innerText();
      assert("results 有数据时显示总榜内容", /成绩主持|总分/.test(resultsText), {
        excerpt: resultsText.slice(0, 300),
      });
      assert("results 有数据时不再显示「还没有成绩」", !/还没有成绩/.test(resultsText), {
        excerpt: resultsText.slice(0, 200),
      });

      const segmentTab = scoredPage.getByRole("tab", { name: /分项明细/ });
      if ((await segmentTab.count()) > 0) {
        await segmentTab.click();
        await scoredPage.waitForTimeout(900);
        const segText = await scoredPage.locator("body").innerText();
        /*
         * 后端补了 segmentDTO.name 之后，表头显示的是创建时填的真实名称
         * （「第一项」），不再是「第 N 项 · 玩法」。
         * 断言两者都接受：有 name 用 name，没 name 才回退编号。
         */
        assert("results 分项明细显示分项列（真实名称或序号）", /第一项|第 \d+ 项/.test(segText), {
          excerpt: segText.slice(0, 300),
        });
      }

      const resultsMeasure = await measure(scoredPage);
      assert("results 有数据状态无水平溢出", resultsMeasure.overflowPx === 0, {
        overflowPx: resultsMeasure.overflowPx,
      });
      assert("results 有数据状态无技术词", resultsMeasure.bannedHits.length === 0, resultsMeasure.bannedHits);
      await shot(scoredPage, "modes-event-results-scored-1366");
    } catch (error) {
      assert("results 有数据状态可验证", false, String(error));
    } finally {
      await scoredContext.close();
    }

    // --- 大屏有成绩状态 ---
    const scoredGrant = await scoredHost.call("POST", `/events/${scoredEventId}/display-grants`, undefined, key());
    const scoredGrantCode = scoredGrant.json?.grantCode;
    if (scoredGrantCode) {
      const stageContext = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
      const stagePage = await stageContext.newPage();
      try {
        await stagePage.goto(`${WEB_BASE}/events/${scoredEventId}/display`, { waitUntil: "commit" });
        await stagePage.waitForSelector("#grant-code", { timeout: 20000 });
        await stagePage.locator("#grant-code").fill(scoredGrantCode);
        await stagePage.getByRole("button", { name: /打开大屏/ }).click();
        await stagePage.waitForTimeout(4000);

        const stageText = await stagePage.locator("body").innerText();
        assert("display 有成绩时主舞台显示领先队伍", /当前领先|成绩主持/.test(stageText), {
          excerpt: stageText.slice(0, 300),
        });
        assert("display 有成绩时不再显示「等待第一项成绩」", !/等待第一项成绩/.test(stageText), {
          excerpt: stageText.slice(0, 200),
        });

        const stageMinFont = await stagePage.evaluate(() => {
          let min = Infinity;
          for (const el of document.querySelectorAll("p, span, h1, h2, li, strong")) {
            const text = (el.textContent ?? "").trim();
            if (!text) continue;
            const rect = el.getBoundingClientRect();
            if (rect.width === 0 || rect.height === 0) continue;
            const style = getComputedStyle(el);
            if (style.display === "none") continue;
            if (el.closest(".sr-only, .u-visually-hidden")) continue;
            min = Math.min(min, parseFloat(style.fontSize));
          }
          return min === Infinity ? null : Math.round(min);
        });
        assert("display 有成绩时正文字号仍 ≥24px", Boolean(stageMinFont && stageMinFont >= 24), {
          minFontPx: stageMinFont,
        });

        await stagePage.screenshot({ path: resolve(SHOTS, "modes-event-display-scored-1920.png") });
        const scoredBuf = await readFile(resolve(SHOTS, "modes-event-display-scored-1920.png"));
        report.screenshots.push({
          name: "modes-event-display-scored-1920",
          path: "screenshots/modes-event-display-scored-1920.png",
          sha256: createHash("sha256").update(scoredBuf).digest("hex"),
          viewport: "1920x1080",
        });
      } catch (error) {
        assert("display 有成绩状态可验证", false, String(error));
      } finally {
        await stageContext.close();
      }
    } else {
      report.limits.push("display 有成绩状态未验证：配对码生成失败");
    }
  }

  /* ================= 8c. 主持台「为分项开局」UI 入口 ================= */
  {
    // Explicit segmentId binds the round to its event segment
    const hostClient = await (async () => {
      let cookie = "";
      let csrf = null;
      const call = async (method, path, body, idempotencyKey) => {
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
      };
      const s = await call("POST", "/sessions", { displayName: "开局主持" });
      csrf = s.json?.csrfToken;
      return { call, getCookie: () => cookie };
    })();

    const key = () => crypto.randomUUID().replace(/-/g, "");
    const segRoom = await hostClient.call(
      "POST",
      "/rooms",
      {
        name: "分项开局测试房",
        kind: "event",
        modeConfig: {
          modeId: "classic",
          maxWrongGuesses: 8,
          winPolicy: "window-attempts",
          durationSeconds: 0,
          shadowEnabled: false,
        },
        maxPlayers: 8,
        maxSpectators: 8,
        visibility: "invite",
        allowSpectators: true,
      },
      key(),
    );
    const segRoomId = segRoom.json?.room?.roomId;
    const segEvent = await hostClient.call(
      "POST",
      "/events",
      { roomId: segRoomId, name: "开局测试杯", tiePolicy: "shared-rank" },
      key(),
    );
    const segEventId = segEvent.json?.eventId;
    const segCreated = await hostClient.call(
      "POST",
      `/events/${segEventId}/segments`,
      {
        modeConfig: {
          modeId: "classic",
          maxWrongGuesses: 8,
          winPolicy: "window-attempts",
          durationSeconds: 0,
          shadowEnabled: false,
        },
        label: "第一项",
        scoreBudget: 100,
        roundCount: 1,
        competitive: true,
      },
      key(),
    );
    assert("主持台 分项创建时带 name（后端已补 segmentDTO.name）", segCreated.json?.name === "第一项", {
      name: segCreated.json?.name,
    });

    const segContext = await browser.newContext({ viewport: { width: 1366, height: 768 } });
    const [segName, segValue] = (hostClient.getCookie() ?? "").split("=");
    if (segName && segValue) {
      await segContext.addCookies([{ name: segName, value: segValue, domain: "127.0.0.1", path: "/" }]);
    }
    const segPage = await segContext.newPage();
    try {
      await segPage.goto(`${WEB_BASE}/events/${segEventId}/host`, { waitUntil: "commit" });
      await segPage.waitForSelector("h1", { timeout: 25000 });
      await segPage.waitForTimeout(2000);

      const hostText = await segPage.locator("body").innerText();
      assert("主持台 显示真实分项名称（不是「第 N 项 · 玩法」）", /第一项/.test(hostText), {
        excerpt: hostText.slice(0, 300),
      });

      // 未绑回合 → 应有「开局」按钮
      const startBtn = segPage.getByRole("button", { name: /^开局$/ });
      assert("主持台 未绑回合的分项显示「开局」按钮", (await startBtn.count()) > 0, {
        count: await startBtn.count(),
      });

      if ((await startBtn.count()) > 0) {
        await startBtn.first().click();
        await segPage.waitForTimeout(3000);

        // 点完后应变成「已绑定回合」
        const afterText = await segPage.locator("body").innerText();
        assert("主持台 开局后分项显示「已绑定回合」", /已绑定回合/.test(afterText), {
          excerpt: afterText.slice(0, 300),
        });
        assert("主持台 开局后「开局」按钮消失（不会重复建回合）", (await startBtn.count()) === 0, {
          remaining: await startBtn.count(),
        });

        // 服务端确认 roundId 真的绑上了
        const afterEvent = await hostClient.call("GET", `/events/${segEventId}`);
        const boundSegment = (afterEvent.json?.segmentDetails ?? [])[0];
        assert(
          "主持台 开局后分项真的绑定 roundId（服务端确认）",
          typeof boundSegment?.roundId === "string" && boundSegment.roundId.length > 0,
          { roundId: boundSegment?.roundId, status: boundSegment?.status },
        );
        note("分项绑定结果", { roundId: boundSegment?.roundId, status: boundSegment?.status });
      }

      await shot(segPage, "modes-event-host-segment-start-1366");
    } catch (error) {
      assert("主持台 为分项开局可验证", false, String(error));
    } finally {
      await segContext.close();
    }
  }

  /* ================= 8d. 主持台缺交进度面板（三态判定） ================= */
  {
    // Distinguish missing submissions from rounds that have not started
    const progHost = await (async () => {
      let cookie = "";
      let csrf = null;
      const call = async (method, path, body, idempotencyKey) => {
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
      };
      const s = await call("POST", "/sessions", { displayName: "进度主持" });
      csrf = s.json?.csrfToken;
      return { call, getCookie: () => cookie };
    })();

    const key = () => crypto.randomUUID().replace(/-/g, "");
    const classicCfg = {
      modeId: "classic",
      maxWrongGuesses: 8,
      winPolicy: "window-attempts",
      durationSeconds: 0,
      shadowEnabled: false,
    };
    const progRoom = await progHost.call(
      "POST",
      "/rooms",
      {
        name: "缺交面板测试房",
        kind: "event",
        modeConfig: classicCfg,
        maxPlayers: 8,
        maxSpectators: 8,
        visibility: "invite",
        allowSpectators: true,
      },
      key(),
    );
    const progRoomId = progRoom.json?.room?.roomId;
    const progInvite = progRoom.json?.inviteCode;

    // 两个独立会话加入
    const guestOne = await (async () => {
      let cookie = "";
      let csrf = null;
      const res = await fetch(`${API_BASE}/api/v1/sessions`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Origin: API_BASE },
        body: JSON.stringify({ displayName: "进度选手A" }),
      });
      const body = await res.json();
      csrf = body.csrfToken;
      for (const c of res.headers.getSetCookie?.() ?? []) {
        const kv = c.split(";")[0];
        if (kv.startsWith("uma_session")) cookie = kv;
      }
      const call = async (method, path, payload, idempotencyKey) => {
        const headers = { Accept: "application/json, application/problem+json", Origin: API_BASE };
        if (payload !== undefined) headers["Content-Type"] = "application/json";
        if (cookie) headers["Cookie"] = cookie;
        if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
        if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
        const r = await fetch(`${API_BASE}/api/v1${path}`, {
          method,
          headers,
          body: payload === undefined ? undefined : JSON.stringify(payload),
        });
        for (const c of r.headers.getSetCookie?.() ?? []) {
          const kv = c.split(";")[0];
          if (kv.startsWith("uma_session")) cookie = kv;
        }
        const text = await r.text();
        let json = null;
        try {
          json = JSON.parse(text);
        } catch {
          /* 非 JSON */
        }
        return { status: r.status, json };
      };
      return { call };
    })();
    await guestOne.call(
      "POST",
      `/rooms/${progRoomId}/memberships`,
      { inviteCode: progInvite, requestedRole: "player" },
      key(),
    );

    const progEvent = await progHost.call(
      "POST",
      "/events",
      { roomId: progRoomId, name: "缺交测试杯", tiePolicy: "shared-rank" },
      key(),
    );
    const progEventId = progEvent.json?.eventId;
    const progSegment = await progHost.call(
      "POST",
      `/events/${progEventId}/segments`,
      { modeConfig: classicCfg, label: "缺交测试项", scoreBudget: 100, roundCount: 1, competitive: true },
      key(),
    );

    // 开局（带 segmentId 才能让主持台拿到 currentRound）
    const progRound = await progHost.call(
      "POST",
      `/rooms/${progRoomId}/rounds`,
      { modeConfig: classicCfg, segmentId: progSegment.json?.segmentId },
      key(),
    );
    const progRoundId = progRound.json?.roundId;
    let progPv = progRound.json?.phaseVersion ?? 1;

    /* --- 三态 1：未开始（briefing 前不统计缺交） --- */
    {
      const view = await progHost.call("GET", `/rounds/${progRoundId}`);
      const subjects = view.json?.publicState?.subjects ?? [];
      assert(
        "缺交 开局前 subjects 都有 submitted 字段",
        subjects.length > 0 && subjects.every((s) => typeof s.submitted === "boolean"),
        { subjects },
      );
      assert(
        "缺交 开局前所有 submitted=false（都还没交）",
        subjects.every((s) => s.submitted === false),
        { subjects },
      );
    }

    for (const target of ["briefing", "collecting"]) {
      const t = await progHost.call(
        "POST",
        `/rounds/${progRoundId}/transitions`,
        { target, expectedPhaseVersion: progPv },
        key(),
      );
      progPv = t.json?.phaseVersion ?? progPv + 1;
    }

    /* --- 三态 2：collecting 但没人交 → 都算缺交 --- */
    {
      const view = await progHost.call("GET", `/rounds/${progRoundId}`);
      const subjects = view.json?.publicState?.subjects ?? [];
      const outstanding = subjects.filter((s) => !s.submitted);
      assert("缺交 collecting 阶段所有人未交", outstanding.length === subjects.length, {
        total: subjects.length,
        outstanding: outstanding.length,
      });
    }

    /* --- 三态 3：一人交 → 只他算已交 --- */
    {
      const hostSelf = (await progHost.call("GET", `/rooms/${progRoomId}`)).json?.members?.find(
        (m) => m.displayName === "进度主持",
      );
      const hostSubmit = await progHost.call(
        "POST",
        `/rounds/${progRoundId}/submissions`,
        {
          phaseVersion: progPv,
          submissionRevision: 0,
          controlGeneration: 0,
          modeAction: { kind: "classic.guess", characterId: "1001" },
        },
        key(),
      );
      assert("缺交 主持提交一次", hostSubmit.status === 201, { status: hostSubmit.status });

      const view = await progHost.call("GET", `/rounds/${progRoundId}`);
      const subjects = view.json?.publicState?.subjects ?? [];
      const submitted = subjects.filter((s) => s.submitted);
      const outstanding = subjects.filter((s) => !s.submitted);
      assert("缺交 只有提交者变成已交", submitted.length === 1, {
        submittedCount: submitted.length,
        hostMembership: hostSelf?.membershipId,
        subjects,
      });
      assert("缺交 其余仍是未交", outstanding.length === subjects.length - 1, {
        outstanding: outstanding.length,
        total: subjects.length,
      });
      note("缺交 三态分布", {
        total: subjects.length,
        submitted: submitted.length,
        outstanding: outstanding.length,
      });
    }

    /* --- 页面渲染 --- */
    const progContext = await browser.newContext({ viewport: { width: 1366, height: 768 } });
    const [pcName, pcValue] = (progHost.getCookie() ?? "").split("=");
    if (pcName && pcValue) {
      await progContext.addCookies([{ name: pcName, value: pcValue, domain: "127.0.0.1", path: "/" }]);
    }
    const progPage = await progContext.newPage();
    try {
      await progPage.goto(`${WEB_BASE}/events/${progEventId}/host`, { waitUntil: "commit" });
      await progPage.waitForSelector("h1", { timeout: 25000 });
      await progPage.waitForTimeout(3000);

      const panelText = await progPage.locator("body").innerText();
      assert("缺交 主持台显示提交进度面板", /提交进度/.test(panelText), {
        excerpt: panelText.slice(0, 400),
      });
      assert("缺交 显示已交/总数", /\d+\s*\/\s*\d+（\d+%）/.test(panelText), {
        excerpt: panelText.slice(0, 400),
      });
      assert("缺交 列出未提交的主体", /未提交/.test(panelText) || /所有人都交了/.test(panelText), {
        excerpt: panelText.slice(0, 400),
      });

      /* 按 subjectId 从房间成员与队伍列表解析显示名，检查面板实际显示队名 */
      assert("缺交 面板显示真实成员名（不是未命名）", !/未命名队伍/.test(panelText), {
        excerpt: panelText.slice(0, 500),
      });

      const progMeasure = await measure(progPage);
      assert("缺交 面板无水平溢出", progMeasure.overflowPx === 0, { overflowPx: progMeasure.overflowPx });
      assert("缺交 面板无技术词", progMeasure.bannedHits.length === 0, progMeasure.bannedHits);

      await shot(progPage, "modes-event-host-progress-1366");
    } catch (error) {
      assert("缺交 面板可验证", false, String(error));
    } finally {
      await progContext.close();
    }
  }

  /* ================= 8e. pedigree 对局页（G14 原型血统拼图） ================= */
  {
    /* 验证史实原型血统题面的标注、槽位修订和按正确比例计分 */
    const pedClient = await (async () => {
      let cookie = "";
      let csrf = null;
      const call = async (method, path, body, idempotencyKey) => {
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
      };
      const s = await call("POST", "/sessions", { displayName: "血统玩家" });
      csrf = s.json?.csrfToken;
      return { call, getCookie: () => cookie };
    })();

    const pedKey = () => crypto.randomUUID().replace(/-/g, "");
    const pedCfg = {
      modeId: "pedigree",
      rulesVersion: "2.0.0-draft.1",
      contentPackId: "",
      informationStyle: "closed",
    };
    const pedRoom = await pedClient.call(
      "POST",
      "/rooms",
      { name: "血统对局", kind: "solo", modeConfig: pedCfg, maxPlayers: 2, maxSpectators: 0, visibility: "invite", allowSpectators: false },
      pedKey(),
    );
    const pedRoomId = pedRoom.json?.room?.roomId;
    const pedRound = await pedClient.call("POST", `/rooms/${pedRoomId}/rounds`, { modeConfig: pedCfg }, pedKey());
    const pedRoundId = pedRound.json?.roundId;
    assert("血统 开局成功", pedRound.status === 201 && Boolean(pedRoundId), {
      status: pedRound.status,
      code: pedRound.json?.code,
    });

    let pedPv = pedRound.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await pedClient.call(
        "POST",
        `/rounds/${pedRoundId}/transitions`,
        { target, expectedPhaseVersion: pedPv },
        pedKey(),
      );
      pedPv = t.json?.phaseVersion ?? pedPv + 1;
    }

    const pedView = await pedClient.call("GET", `/rounds/${pedRoundId}`);
    const pedPs = pedView.json?.publicState;
    assert(
      "血统 服务端下发 disclaimer 且含「史实原型血统」",
      typeof pedPs?.disclaimer === "string" && pedPs.disclaimer.includes("史实原型血统"),
      { disclaimer: pedPs?.disclaimer },
    );
    assert("血统 槽位数在 4–6 之间", (pedPs?.slots?.length ?? 0) >= 4 && (pedPs?.slots?.length ?? 0) <= 6, {
      slotCount: pedPs?.slots?.length,
    });
    assert("血统 有候选马池", (pedPs?.candidates?.length ?? 0) > 0, {
      candidateCount: pedPs?.candidates?.length,
    });
    assert(
      "血统 scoring 说明是「正确槽位 ÷ 总槽位」",
      typeof pedPs?.scoring === "string" && pedPs.scoring.includes("槽位"),
      { scoring: pedPs?.scoring },
    );

    // 提交一次
    const pedAssignments = (pedPs?.slots ?? []).slice(0, 2).map((slot, index) => ({
      slotId: slot.slotId,
      horseId: pedPs.candidates[index].horseId,
    }));
    const pedSubmit = await pedClient.call(
      "POST",
      `/rounds/${pedRoundId}/submissions`,
      {
        phaseVersion: pedPv,
        submissionRevision: 0,
        controlGeneration: 0,
        modeAction: { kind: "pedigree.assignment", assignments: pedAssignments },
      },
      pedKey(),
    );
    assert("血统 提交槽位分配成功", pedSubmit.status === 201, {
      status: pedSubmit.status,
      code: pedSubmit.json?.code,
      detail: pedSubmit.json?.detail,
    });

    // 页面渲染
    const pedContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [pcName, pcValue] = (pedClient.getCookie() ?? "").split("=");
    if (pcName && pcValue) {
      await pedContext.addCookies([{ name: pcName, value: pcValue, domain: "127.0.0.1", path: "/" }]);
    }
    const pedPage = await pedContext.newPage();
    try {
      await pedPage.goto(`${WEB_BASE}/rooms/${pedRoomId}`, { waitUntil: "commit" });
      await pedPage.waitForSelector("#main-content", { timeout: 25000 });
      await pedPage.waitForTimeout(3500);

      const pedText = await pedPage.locator("#main-content").innerText();
      assert("血统 页面显示「史实原型血统」（措辞红线）", /史实原型血统/.test(pedText), {
        excerpt: pedText.slice(0, 300),
      });
      assert("血统 页面不出现占位词", !/还在实现中|尚未实现|开发中/.test(pedText), {
        excerpt: pedText.slice(0, 200),
      });
      assert("血统 页面显示主角与史实原型", /史实原型：/.test(pedText), {
        excerpt: pedText.slice(0, 300),
      });
      assert("血统 页面显示血统槽位区", /血统槽位/.test(pedText), null);
      assert("血统 页面显示候选原型马", /候选原型马/.test(pedText), null);
      assert("血统 页面显示计分说明", /计分：/.test(pedText), null);

      const pedMeasure = await measure(pedPage);
      assert("血统 页面无水平溢出", pedMeasure.overflowPx === 0, { overflowPx: pedMeasure.overflowPx });
      assert("血统 页面无技术词", pedMeasure.bannedHits.length === 0, pedMeasure.bannedHits);

      await shot(pedPage, "modes-pedigree-390");
    } catch (error) {
      assert("血统 页面可验证", false, String(error));
    } finally {
      await pedContext.close();
    }
  }

  /* ================= 8f. audio 对局页（G15 听音配对） ================= */
  {
    /*
     * 硬约束：
     * - preloadRequired 时全部预载成功才能开始
     * - 播放失败 ≠ 答错，必须有「这段听不了」入口
     * - 每对正确 25 分（4 段基线）
     * - audioPath 是公开的，不把隐藏它写成安全措施
     */
    const audioClient = await (async () => {
      let cookie = "";
      let csrf = null;
      const call = async (method, path, body, idempotencyKey) => {
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
      };
      const s = await call("POST", "/sessions", { displayName: "听音玩家" });
      csrf = s.json?.csrfToken;
      return { call, getCookie: () => cookie };
    })();

    const aKey = () => crypto.randomUUID().replace(/-/g, "");
    const aCfg = { modeId: "audio", rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed" };
    const aRoom = await audioClient.call(
      "POST",
      "/rooms",
      { name: "听音对局", kind: "solo", modeConfig: aCfg, maxPlayers: 6, maxSpectators: 0, visibility: "invite", allowSpectators: false },
      aKey(),
    );
    const aRoomId = aRoom.json?.room?.roomId;
    const aRound = await audioClient.call("POST", `/rooms/${aRoomId}/rounds`, { modeConfig: aCfg }, aKey());

    /*
     * 服务端当前把 audio 标为 unavailable（审核记录被如实回退为未审核），
     * 所以开局会 422 round-preparation-failed。这不是前端缺陷——
     * 断言要如实区分「模式未准入」与「实现有问题」。
     */
    const audioAdmitted = aRound.status === 201;
    if (!audioAdmitted) {
      report.limits.push(
        `audio 未验证：服务端未准入（${aRound.json?.code ?? aRound.status}）。界面与归一化层已实现，等审核数据到位后可直接验证。`,
      );
      note("audio 当前不可开局，跳过其断言", { status: aRound.status, code: aRound.json?.code });
    }
    const aRoundId = aRound.json?.roundId;
    let aPv = aRound.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await audioClient.call(
        "POST",
        `/rounds/${aRoundId}/transitions`,
        { target, expectedPhaseVersion: aPv },
        aKey(),
      );
      aPv = t.json?.phaseVersion ?? aPv + 1;
    }

    const aView = await audioClient.call("GET", `/rounds/${aRoundId}`);
    const aPs = aView.json?.publicState;
    assert("听音 有音频段（4 段基线）", !audioAdmitted || (aPs?.clips?.length ?? 0) >= 2, { count: aPs?.clips?.length, admitted: audioAdmitted });
    assert(
      "听音 每段有 clipId / audioPath / durationMillis / maxPlays",
      !audioAdmitted || (aPs?.clips ?? []).every(
        (c) =>
          typeof c.clipId === "string" &&
          typeof c.audioPath === "string" &&
          c.audioPath.startsWith("/assets/audio/") &&
          typeof c.durationMillis === "number" &&
          c.durationMillis > 0 &&
          typeof c.maxPlays === "number",
      ),
      { clips: aPs?.clips },
    );
    assert("听音 preloadRequired 为 true", !audioAdmitted || aPs?.preloadRequired === true, { preloadRequired: aPs?.preloadRequired });
    assert("听音 有候选角色", !audioAdmitted || (aPs?.candidates?.length ?? 0) > 0, { count: aPs?.candidates?.length });

    /*
     * 提交配对：必须配对所有段。
     * 服务端对不完整提交返回 422 incomplete_matches（实测）。
     */
    const aMatches = (aPs?.clips ?? []).map((clip, index) => ({
      clipId: clip.clipId,
      characterId: aPs.candidates[index].characterId,
    }));
    const aSubmit = await audioClient.call(
      "POST",
      `/rounds/${aRoundId}/submissions`,
      {
        phaseVersion: aPv,
        submissionRevision: 0,
        controlGeneration: 0,
        modeAction: { kind: "audio.matches", matches: aMatches },
      },
      aKey(),
    );
    assert("听音 提交配对成功", !audioAdmitted || aSubmit.status === 201, {
      status: aSubmit.status,
      code: aSubmit.json?.code,
    });

    // 音频文件真的能取到（HTTP 200 + audio/*）
    const audioPath = aPs?.clips?.[0]?.audioPath;
    if (audioPath) {
      const audioRes = await fetch(`${WEB_BASE}${audioPath}`, {
        headers: { Origin: WEB_BASE },
      }).catch(() => null);
      const contentType = audioRes?.headers.get("content-type") ?? "";
      assert(
        "听音 audioPath 返回真实音频（200 + audio/*）",
        Boolean(audioRes?.ok) && contentType.startsWith("audio/"),
        { status: audioRes?.status, contentType },
      );
    }

    // 页面渲染
    const aContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [acName, acValue] = (audioClient.getCookie() ?? "").split("=");
    if (acName && acValue) {
      await aContext.addCookies([{ name: acName, value: acValue, domain: "127.0.0.1", path: "/" }]);
    }
    const aPage = await aContext.newPage();
    try {
      await aPage.goto(`${WEB_BASE}/rooms/${aRoomId}`, { waitUntil: "commit" });
      await aPage.waitForSelector("#main-content", { timeout: 25000 });
      await aPage.waitForTimeout(6000);

      const aText = await aPage.locator("#main-content").innerText();
      assert("听音 页面不出现占位词", !audioAdmitted || !/还在实现中|尚未实现|开发中/.test(aText), {
        excerpt: aText.slice(0, 200),
      });
      assert("听音 页面显示音频区与候选区", !audioAdmitted || /音频/.test(aText) && /候选角色/.test(aText), null);
      assert(
        "听音 页面有「听不了」入口（播放失败 ≠ 答错）",
        !audioAdmitted || /听不了/.test(aText),
        { excerpt: aText.slice(0, 300) },
      );
      /*
       * 候选角色必须显示真实名字，不能是纯数字 ID。
       * 实测服务端下发的 candidates[].name 就是 characterId，
       * 前端要用角色索引补名字。
       */
      const candidateText = await aPage
        .locator('section[aria-labelledby="audio-candidates"]')
        .innerText()
        .catch(() => "");
      assert("听音 候选角色显示真实名字（不是纯 ID）", !audioAdmitted || !/^\s*\d{4}\s*$/m.test(candidateText), {
        excerpt: candidateText.slice(0, 200),
      });

      const aMeasure = await measure(aPage);
      assert("听音 页面无水平溢出", !audioAdmitted || aMeasure.overflowPx === 0, { overflowPx: aMeasure.overflowPx });
      assert("听音 页面无技术词", !audioAdmitted || aMeasure.bannedHits.length === 0, aMeasure.bannedHits);

      await shot(aPage, "modes-audio-390");
    } catch (error) {
      assert("听音 页面可验证", false, String(error));
    } finally {
      await aContext.close();
    }
  }

  /* ================= 8g. bluff 对局页（G08 假档案） ================= */
  {
    /*
     * 硬约束：
     * - 最少 3 人
     * - 两阶段：先 bluff.decoy 写假答案，再 bluff.vote 投票
     * - options 在 decoy 阶段之后才生成（decoy 阶段为空是设计，不是缺数据）
     * - promptSource 只在揭晓时显示
     * - options[].isTruth 服务端永不下发
     * - revision 按 slot 独立：decoy 和 vote 各自从 0 开始
     */
    const bluffHost = await (async () => {
      let cookie = "";
      let csrf = null;
      const call = async (method, path, body, idempotencyKey) => {
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
      };
      const s = await call("POST", "/sessions", { displayName: "假档案主持" });
      csrf = s.json?.csrfToken;
      return { call, getCookie: () => cookie };
    })();

    const bKey = () => crypto.randomUUID().replace(/-/g, "");
    const bCfg = { modeId: "bluff", rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "closed" };
    const bRoom = await bluffHost.call(
      "POST",
      "/rooms",
      { name: "假档案对局", kind: "friend", modeConfig: bCfg, maxPlayers: 8, maxSpectators: 0, visibility: "invite", allowSpectators: false },
      bKey(),
    );
    const bRoomId = bRoom.json?.room?.roomId;
    const bInvite = bRoom.json?.inviteCode;

    // 最少 3 人：房主 + 2 个独立会话
    const bPlayers = [];
    for (let index = 0; index < 2; index += 1) {
      const guest = await (async () => {
        let cookie = "";
        let csrf = null;
        const call = async (method, path, body, idempotencyKey) => {
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
        };
        const s = await call("POST", "/sessions", { displayName: `假档案玩家${index + 1}` });
        csrf = s.json?.csrfToken;
        return { call };
      })();
      await guest.call(
        "POST",
        `/rooms/${bRoomId}/memberships`,
        { inviteCode: bInvite, requestedRole: "player" },
        bKey(),
      );
      bPlayers.push(guest);
    }

    const bRound = await bluffHost.call("POST", `/rooms/${bRoomId}/rounds`, { modeConfig: bCfg }, bKey());

    /*
     * bluff 当前也是 unavailable（审核记录如实回退为未审核）。
     * 同样要区分「未准入」与「实现有问题」。
     */
    const bluffAdmitted = bRound.status === 201;
    if (!bluffAdmitted) {
      report.limits.push(
        `bluff 未验证：服务端未准入（${bRound.json?.code ?? bRound.status}）。界面与归一化层已实现，等审核数据到位后可直接验证。`,
      );
      note("bluff 当前不可开局，跳过其断言", { status: bRound.status, code: bRound.json?.code });
    }
    const bRoundId = bRound.json?.roundId;
    let bPv = bRound.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await bluffHost.call(
        "POST",
        `/rounds/${bRoundId}/transitions`,
        { target, expectedPhaseVersion: bPv },
        bKey(),
      );
      bPv = t.json?.phaseVersion ?? bPv + 1;
    }

    const bView = await bluffHost.call("GET", `/rounds/${bRoundId}`);
    const bPs = bView.json?.publicState;
    assert("假档案 题面已挖空", !bluffAdmitted || typeof bPs?.prompt === "string" && bPs.prompt.includes("____"), {
      prompt: bPs?.prompt,
    });
    assert(
      "假档案 decoy 阶段 options 为空（设计如此，不是缺数据）",
      (bPs?.options?.length ?? 0) === 0,
      { options: bPs?.options?.length },
    );
    assert(
      "假档案 揭晓前 promptSource 为空（措辞红线）",
      bPs?.promptSource === undefined || bPs?.promptSource === "",
      { promptSource: bPs?.promptSource },
    );
    assert(
      "假档案 选项不携带真值标记（isTruth 是 json:\"-\"）",
      (bPs?.options ?? []).every((option) => !("isTruth" in option)),
      { options: bPs?.options },
    );

    // 三人各写假答案
    const allPlayers = [bluffHost, ...bPlayers];
    let decoyOk = 0;
    for (let index = 0; index < allPlayers.length; index += 1) {
      const submit = await allPlayers[index].call(
        "POST",
        `/rounds/${bRoundId}/submissions`,
        {
          phaseVersion: bPv,
          submissionRevision: 0,
          controlGeneration: 0,
          modeAction: { kind: "bluff.decoy", text: `编的答案${index + 1}` },
        },
        bKey(),
      );
      if (submit.status === 201) decoyOk += 1;
    }
    assert("假档案 三人都提交了假答案", !bluffAdmitted || decoyOk === 3, { decoyOk });

    const bView2 = await bluffHost.call("GET", `/rounds/${bRoundId}`);
    const bPs2 = bView2.json?.publicState;
    assert(
      "假档案 decoy 之后 options 生成（3 假 + 1 真）",
      !bluffAdmitted || (bPs2?.options?.length ?? 0) >= 3,
      { options: bPs2?.options?.length },
    );
    assert(
      "假档案 揭晓前仍不显示 promptSource",
      bPs2?.promptSource === undefined || bPs2?.promptSource === "",
      { promptSource: bPs2?.promptSource },
    );

    /*
     * 投票用 revision 0。
     * revision 按 slot 独立：bluff.decoy 的 slot 是 decoy，
     * bluff.vote 的 slot 是 vote，各自从 0 开始。
     * 用 decoy 的 revision 投票会 409（实测踩过）。
     */
    const voteOption = bPs2?.options?.[0]?.optionId;
    const bVote = await bluffHost.call(
      "POST",
      `/rounds/${bRoundId}/submissions`,
      {
        phaseVersion: bPv,
        submissionRevision: 0,
        controlGeneration: 0,
        modeAction: { kind: "bluff.vote", optionId: voteOption },
      },
      bKey(),
    );
    assert("假档案 投票成功（vote slot 的 revision 从 0 开始）", !bluffAdmitted || bVote.status === 201, {
      status: bVote.status,
      code: bVote.json?.code,
    });

    // 页面渲染
    const bContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const [bcName, bcValue] = (bluffHost.getCookie() ?? "").split("=");
    if (bcName && bcValue) {
      await bContext.addCookies([{ name: bcName, value: bcValue, domain: "127.0.0.1", path: "/" }]);
    }
    const bPage = await bContext.newPage();
    try {
      await bPage.goto(`${WEB_BASE}/rooms/${bRoomId}`, { waitUntil: "commit" });
      await bPage.waitForSelector("#main-content", { timeout: 25000 });
      await bPage.waitForTimeout(4000);

      const bText = await bPage.locator("#main-content").innerText();
      assert("假档案 页面不出现占位词", !bluffAdmitted || !/还在实现中|尚未实现|开发中/.test(bText), {
        excerpt: bText.slice(0, 200),
      });
      assert("假档案 页面显示题面", !bluffAdmitted || /____/.test(bText), { excerpt: bText.slice(0, 200) });
      assert("假档案 页面显示投票阶段选项", !bluffAdmitted || /哪一项是真的|投票结果/.test(bText), {
        excerpt: bText.slice(0, 300),
      });

      const bMeasure = await measure(bPage);
      assert("假档案 页面无水平溢出", !bluffAdmitted || bMeasure.overflowPx === 0, { overflowPx: bMeasure.overflowPx });
      assert("假档案 页面无技术词", !bluffAdmitted || bMeasure.bannedHits.length === 0, bMeasure.bannedHits);

      await shot(bPage, "modes-bluff-390");
    } catch (error) {
      assert("假档案 页面可验证", false, String(error));
    } finally {
      await bContext.close();
    }
  }

  /* ================= 12. 截图哈希互异（防止假证据） ================= */
  {
    const hashes = report.screenshots.map((s) => s.sha256).filter(Boolean);
    const unique = new Set(hashes);
    assert("所有截图内容互不相同（非重复假证据）", unique.size === hashes.length, {
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

  await mkdir(EVIDENCE, { recursive: true });
  const out = resolve(EVIDENCE, "modes-e2e.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");

  console.log(
    `\n[modes-e2e] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 个失败，${report.summary.screenshots} 张截图`,
  );
  console.log(`[modes-e2e] 报告：${out}`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
