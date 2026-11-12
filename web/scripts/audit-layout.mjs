#!/usr/bin/env node
/** Checks viewport overflow, touch targets and page states in Chromium. Reports and screenshots are local artifacts. */
import { mkdir, readdir, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

/**
 * 定位可用的 Chromium。
 * Playwright 版本与本地缓存版本可能不一致（缓存里已有更新的构建），
 * 所以优先用 Playwright 自带解析；失败时回退到本机 ms-playwright 缓存里的完整 Chromium。
 * 环境变量 PLAYWRIGHT_CHROMIUM 可显式指定。
 */
async function resolveChromiumExecutable() {
  if (process.env.PLAYWRIGHT_CHROMIUM) return process.env.PLAYWRIGHT_CHROMIUM;

  const cacheRoot = join(homedir(), "Library/Caches/ms-playwright");
  if (!existsSync(cacheRoot)) return undefined;

  const entries = await readdir(cacheRoot);
  // 只接受完整 Chromium 构建（headless shell 不支持部分 API）
  const chromiumDirs = entries
    .filter((name) => /^chromium-\d+$/.test(name))
    .sort((a, b) => Number(b.split("-")[1]) - Number(a.split("-")[1]));

  for (const dir of chromiumDirs) {
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

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "..");
const EVIDENCE = resolve(WEB_ROOT, "../.cache/reports");
const SHOTS = resolve(EVIDENCE, "screenshots");

const BASE = process.argv[2] ?? "http://127.0.0.1:5173";

/** 视口矩阵（最小必测集） */
const VIEWPORTS = [
  { name: "small-phone-320x568", width: 320, height: 568 },
  { name: "phone-360x800", width: 360, height: 800 },
  { name: "phone-390x844", width: 390, height: 844 },
  { name: "phone-430x932", width: 430, height: 932 },
  { name: "phone-landscape-844x390", width: 844, height: 390 },
  { name: "tablet-768x1024", width: 768, height: 1024 },
  { name: "tablet-820x1180", width: 820, height: 1180 },
  { name: "tablet-landscape-1024x768", width: 1024, height: 768 },
  { name: "laptop-1280x720", width: 1280, height: 720 },
  { name: "laptop-1366x768", width: 1366, height: 768 },
  { name: "desktop-1440x900", width: 1440, height: 900 },
  { name: "desktop-1920x1080", width: 1920, height: 1080 },
  { name: "split-600x900", width: 600, height: 900 },
];

/** Public routes render without fabricated IDs; game pages use real fixture rooms. */
const ROUTES = [
  // 公共页
  "/",
  "/help",
  "/sources",
  "/join",
  "/nonexistent-page",
  // 经典玩法三入口
  "/play/classic",
  "/play/classic/verified",
  "/play/classic/online",
  "/practice",
  // 其余玩法入口（每个玩法的 start / online 入口都要有一页）
  "/play/sort",
  "/play/sort/online",
  "/play/aptitude-grid",
  "/play/aptitude-grid/online",
  "/play/draft",
  "/play/draft/online",
  "/play/race-rebuild",
  "/play/race-rebuild/online",
  "/play/co-race-path",
  "/play/co-race-path/online",
  "/play/memory",
  "/play/memory/online",
  "/play/pedigree",
  "/play/pedigree/online",
  "/play/audio",
  "/play/audio/online",
  "/play/bluff",
  "/play/clue-puzzle",
  "/play/strategy-race",
  "/play/strategy-race/online",
  "/play/rapport",
  "/play/creative",
  "/play/trade",
  // 房间/活动/匹配入口
  "/rooms/new",
  "/matchmaking",
  "/events/new",
];

/** 真实对局页由脚本自己开局产生（见 main 里的 createAuditRoom），保证浏览器有权限 */

const report = {
  generatedAt: new Date().toISOString(),
  baseUrl: BASE,
  viewports: [],
  routes: [],
  issues: [],
  screenshots: [],
};

function record(issue) {
  report.issues.push(issue);
}

/** 在页面内测量：水平溢出、触摸区、字号 */
async function measure(page) {
  return page.evaluate(() => {
    const docEl = document.documentElement;
    const bodyEl = document.body;

    const horizontalOverflow = {
      documentScrollWidth: docEl.scrollWidth,
      documentClientWidth: docEl.clientWidth,
      bodyScrollWidth: bodyEl.scrollWidth,
      bodyClientWidth: bodyEl.clientWidth,
      overflowPx: Math.max(
        docEl.scrollWidth - docEl.clientWidth,
        bodyEl.scrollWidth - bodyEl.clientWidth,
      ),
    };

    // 找出溢出但既不是局部滚动容器、也没有在容器上真实解决的元素
    const offenders = [];
    for (const el of document.querySelectorAll("*")) {
      const style = getComputedStyle(el);
      const isLocalScroller =
        style.overflowX === "auto" ||
        style.overflowX === "scroll" ||
        style.overflowX === "clip" ||
        el.classList.contains("uma-scroll-x");
      if (isLocalScroller) continue;
      const overflow = el.scrollWidth - el.clientWidth;
      if (overflow > 1 && el.clientWidth > 0) {
        // 只在元素自身超出视口时才算问题
        const rect = el.getBoundingClientRect();
        if (rect.right > window.innerWidth + 1 || rect.left < -1) {
          offenders.push({
            tag: el.tagName.toLowerCase(),
            className: typeof el.className === "string" ? el.className.slice(0, 120) : "",
            scrollWidth: el.scrollWidth,
            clientWidth: el.clientWidth,
            right: Math.round(rect.right),
            viewportWidth: window.innerWidth,
          });
        }
      }
    }

    // 主操作与所有可交互元素的触摸区
    const smallTargets = [];
    const interactive = document.querySelectorAll(
      "button, a[href], input, select, textarea, [role=button], [role=option], [role=tab]",
    );
    for (const el of interactive) {
      const rect = el.getBoundingClientRect();
      if (rect.width === 0 && rect.height === 0) continue; // 隐藏元素跳过
      const style = getComputedStyle(el);
      if (style.display === "none" || style.visibility === "hidden") continue;
      // 屏幕阅读器专用元素（sr-only）视觉上不可点，不计入触摸区检查
      if (el.closest(".sr-only, .u-visually-hidden")) continue;
      if (style.clipPath && style.clipPath !== "none" && rect.height <= 2) continue;

      /* Measure the wrapping label as the touch target only when it provides a real 44px area. */
      const owningLabel = el.closest("label");
      if (owningLabel) {
        const labelRect = owningLabel.getBoundingClientRect();
        if (labelRect.height >= 44 && labelRect.width >= 44) continue;
      }

      if (rect.height < 44 || rect.width < 44) {
        smallTargets.push({
          tag: el.tagName.toLowerCase(),
          text: (el.textContent ?? "").trim().slice(0, 40),
          width: Math.round(rect.width),
          height: Math.round(rect.height),
        });
      }
    }

    // 正文最小字号（排除 sr-only 与装饰）
    const smallText = [];
    for (const el of document.querySelectorAll("p, li, span, td, th, label, dd, dt, h1, h2, h3, h4")) {
      const text = (el.textContent ?? "").trim();
      if (!text) continue;
      const rect = el.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) continue;
      const style = getComputedStyle(el);
      if (style.display === "none") continue;
      // sr-only 文字只给屏幕阅读器，不受视觉字号下限约束
      if (el.closest(".sr-only, .u-visually-hidden")) continue;
      const size = parseFloat(style.fontSize);
      // 辅助文字允许 13px；10–12px 挤核心内容不允许
      if (size < 13) {
        smallText.push({
          tag: el.tagName.toLowerCase(),
          text: text.slice(0, 40),
          fontSize: size,
        });
      }
    }

    return {
      horizontalOverflow,
      offenders: offenders.slice(0, 10),
      smallTargets: smallTargets.slice(0, 15),
      smallText: smallText.slice(0, 10),
      title: document.title,
      h1: document.querySelector("h1")?.textContent?.trim() ?? null,
    };
  });
}

/**
 * 用真实 UI 开一局经典对局，返回房间 URL。
 * 审查必须覆盖真实对局页，而不是只看占位页——但房间必须由当前浏览器会话创建，
 * 否则会被权限正确拒绝（403），那是另一种状态，不能替代对局页审查。
 */
async function createAuditRoom(browser, base) {
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const page = await context.newPage();
  try {
    await page.goto(`${base}/play/classic`, { waitUntil: "domcontentloaded" });
    await page.getByLabel("显示名").fill("布局审查");
    await page.getByRole("button", { name: /开始对局/ }).click();
    await page.waitForURL(/\/rooms\//, { timeout: 30000 });
    const url = new URL(page.url());
    // 顺便猜一次，让审查覆盖「有反馈行」的状态
    await page.waitForSelector('input[role="combobox"]', { timeout: 20000 });
    const input = page.locator('input[role="combobox"]');
    await input.click();
    await input.type("特别周", { delay: 20 });
    await page.waitForSelector('[role="option"]', { timeout: 10000 });
    await page.locator('[role="option"]').first().click();
    await page.getByRole("button", { name: /^猜一次/ }).click();
    await page
      .waitForFunction(() => document.querySelectorAll('[data-guess-row="true"]').length >= 1, { timeout: 20000 })
      .catch(() => {});
    // 保存 cookie 供其他视口复用：房间按会话投影，没有 cookie 只能看到 403
    const storageState = await context.storageState();
    return { path: url.pathname, storageState };
  } catch {
    await context.close();
    return null;
  }
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const executablePath = await resolveChromiumExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});
  console.log(`[audit] Chromium: ${executablePath ?? "Playwright 默认"}`);

  // 真实对局页：由脚本自己开局，并保存会话状态供各视口复用
  const auditRoom = await createAuditRoom(browser, BASE);
  if (auditRoom) {
    ROUTES.push(auditRoom.path);
    console.log(`[audit] 已创建真实对局页用于审查：${auditRoom.path}`);
  } else {
    console.warn("[audit] 无法创建真实对局页，对局页将不在本次审查范围内");
  }

  // ---- 1) 多视口 × 多路由：溢出 / 触摸区 / 字号 ----
  for (const viewport of VIEWPORTS) {
    const context = await browser.newContext({
      viewport: { width: viewport.width, height: viewport.height },
      deviceScaleFactor: 1,
      // 不伪造移动设备 UA： 明确「不能把桌面模拟视口写成物理设备通过」
      reducedMotion: "no-preference",
      // 复用开局会话，否则对局页只能看到 403，审查就变成了审 403 页
      ...(auditRoom ? { storageState: auditRoom.storageState } : {}),
    });
    const page = await context.newPage();

    for (const route of ROUTES) {
      await page.goto(`${BASE}${route}`, { waitUntil: "networkidle" });
      // 等待骨架消失（最长 5s），避免把 loading 骨架当成最终布局
      await page
        .waitForFunction(() => !document.querySelector(".uma-skeleton"), { timeout: 5000 })
        .catch(() => {});

      const result = await measure(page);
      const entry = { viewport: viewport.name, route, ...result };
      report.routes.push(entry);

      if (result.horizontalOverflow.overflowPx > 0) {
        record({
          type: "horizontal-overflow",
          viewport: viewport.name,
          route,
          detail: result.horizontalOverflow,
        });
      }
      for (const offender of result.offenders) {
        record({ type: "element-overflow", viewport: viewport.name, route, detail: offender });
      }
      for (const target of result.smallTargets) {
        record({ type: "small-touch-target", viewport: viewport.name, route, detail: target });
      }
      for (const text of result.smallText) {
        record({ type: "small-text", viewport: viewport.name, route, detail: text });
      }

      // 只给关键视口截图，避免证据目录爆炸
      const shotKeys = ["small-phone-320x568", "phone-390x844", "phone-landscape-844x390", "desktop-1440x900", "desktop-1920x1080"];
      if (shotKeys.includes(viewport.name) && route !== "/nonexistent-page") {
        const file = `${viewport.name}${route.replace(/\//g, "_") || "_root"}.png`;
        await page.screenshot({ path: resolve(SHOTS, file), fullPage: false });
        report.screenshots.push(`screenshots/${file}`);
      }
    }

    await context.close();
    console.log(`[audit] 视口 ${viewport.name} 完成`);
  }

  // ---- 2) 页面状态覆盖：用路由拦截制造 error / forbidden / empty ----
  // 每个用例必须用独立的 browser context：TanStack Query 会缓存上一次的响应，
  // 复用同一个 page 会让后续用例读到旧缓存，导致「看起来都通过」的假证据。

  const STATE_CASES = [
    { name: "loading", handler: () => {} },
    {
      name: "error-500",
      handler: (route) =>
        route.fulfill({
          status: 500,
          contentType: "application/problem+json",
          body: JSON.stringify({
            type: "about:blank",
            title: "服务器内部错误",
            status: 500,
            code: "INTERNAL",
            requestId: "audit-req-500",
          }),
        }),
    },
    {
      name: "forbidden-403",
      handler: (route) =>
        route.fulfill({
          status: 403,
          contentType: "application/problem+json",
          body: JSON.stringify({
            type: "about:blank",
            title: "无权访问",
            status: 403,
            code: "FORBIDDEN",
            requestId: "audit-req-403",
          }),
        }),
    },
    {
      name: "empty",
      handler: (route) =>
        route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ items: [], serverTime: new Date().toISOString() }),
        }),
    },
  ];

  report.states = [];
  for (const stateCase of STATE_CASES) {
    const stateContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const statePage = await stateContext.newPage();

    if (stateCase.name !== "loading") {
      await statePage.route("**/api/v1/modes", stateCase.handler);
      await statePage.route("**/api/v1/config", stateCase.handler);
    }
    await statePage.goto(`${BASE}/`, { waitUntil: "domcontentloaded" });
    await statePage.waitForTimeout(stateCase.name === "loading" ? 80 : 900);

    const snapshot = await statePage.evaluate(() => {
      const text = document.body.innerText;
      return {
        hasAlert: Boolean(document.querySelector('[role="alert"]')),
        hasSkeleton: Boolean(document.querySelector(".uma-skeleton")),
        textSample: text.slice(0, 400),
        mentionsNoData: /暂无/.test(text),
        mentionsError: /失败|错误|无权|加载失败/.test(text),
        mentionsEmpty: /没有可开局|暂时没有/.test(text),
      };
    });
    report.states.push({ state: stateCase.name, ...snapshot });

    const file = `state-${stateCase.name}.png`;
    // 状态差异可能出现在首屏下方，必须截整页，否则不同状态会得到相同图片（假证据）
    await statePage.screenshot({ path: resolve(SHOTS, file), fullPage: true });
    report.screenshots.push(`screenshots/${file}`);

    if (stateCase.name === "forbidden-403" && !snapshot.mentionsError) {
      record({ type: "state-mismatch", state: stateCase.name, detail: "403 未显示权限错误" });
    }
    if (stateCase.name === "error-500" && snapshot.mentionsNoData) {
      record({ type: "state-mismatch", state: stateCase.name, detail: "加载失败被显示成暂无数据" });
    }
    if (stateCase.name === "empty" && snapshot.hasAlert) {
      record({ type: "state-mismatch", state: stateCase.name, detail: "空列表被显示成错误" });
    }

    await stateContext.close();
  }

  /*
   * 状态之间必须渲染出不同内容。
   * 若多个状态截图字节完全相同，说明路由拦截没生效或视图没区分——
   * 这正是「假证据」最典型的形态，必须当成问题报出来。
   */
  {
    const { createHash } = await import("node:crypto");
    const { readFile } = await import("node:fs/promises");
    const seen = new Map();
    for (const item of report.states) {
      const buf = await readFile(resolve(SHOTS, `state-${item.state}.png`));
      const hash = createHash("sha256").update(buf).digest("hex");
      if (seen.has(hash)) {
        record({
          type: "state-not-distinct",
          state: item.state,
          detail: `与 ${seen.get(hash)} 的渲染结果完全相同，状态可能未生效`,
        });
      } else {
        seen.set(hash, item.state);
      }
    }
  }

  // ---- 3) 减少动态与 200% 缩放 ----
  const a11yContext = await browser.newContext({
    viewport: { width: 390, height: 844 },
    reducedMotion: "reduce",
  });
  const a11yPage = await a11yContext.newPage();
  await a11yPage.goto(`${BASE}/`, { waitUntil: "networkidle" });
  const reduced = await a11yPage.evaluate(() => ({
    dataMotion: document.documentElement.getAttribute("data-motion"),
    skeletonAnimation: (() => {
      const el = document.querySelector(".uma-skeleton");
      if (!el) return "no-skeleton";
      return getComputedStyle(el).animationName;
    })(),
  }));
  report.reducedMotion = reduced;
  if (reduced.dataMotion !== "reduced") {
    record({ type: "reduced-motion-missing", detail: reduced });
  }
  await a11yContext.close();

  const zoomContext = await browser.newContext({ viewport: { width: 720, height: 450 }, deviceScaleFactor: 2 });
  const zoomPage = await zoomContext.newPage();
  await zoomPage.goto(`${BASE}/`, { waitUntil: "networkidle" });
  const zoomed = await measure(zoomPage);
  report.zoom200 = {
    viewport: "720x450 @2x（等效 200% 缩放的可用宽度）",
    overflowPx: zoomed.horizontalOverflow.overflowPx,
  };
  if (zoomed.horizontalOverflow.overflowPx > 0) {
    record({ type: "zoom-overflow", detail: zoomed.horizontalOverflow });
  }
  await zoomContext.close();

  await browser.close();

  report.summary = {
    viewportCount: VIEWPORTS.length,
    routeCount: ROUTES.length,
    measurementCount: report.routes.length,
    issueCount: report.issues.length,
    issueTypes: [...new Set(report.issues.map((i) => i.type))],
  };

  await writeFile(
    resolve(EVIDENCE, "layout-audit-web.json"),
    `${JSON.stringify(report, null, 2)}\n`,
    "utf8",
  );

  console.log(
    `\n[audit] ${report.summary.measurementCount} 次测量，${report.issues.length} 个问题（${report.summary.issueTypes.join(", ") || "无"}）`,
  );
  if (report.issues.length > 0) {
    const byType = {};
    for (const issue of report.issues) byType[issue.type] = (byType[issue.type] ?? 0) + 1;
    console.log("[audit] 问题分布：", byType);
    process.exitCode = 1;
  }
}

await main();
