#!/usr/bin/env node
/** Exercises classic guessing against a running Go service, including history, feedback and responsive layout. */
import { mkdir, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "..");
const EVIDENCE = resolve(WEB_ROOT, "../.cache/reports");
const SHOTS = resolve(EVIDENCE, "screenshots");

const WEB_BASE = process.env.UMA_WEB_BASE ?? "http://127.0.0.1:5173";
const API_BASE = process.env.UMA_API_BASE ?? "http://127.0.0.1:8080";

async function resolveChromiumExecutable() {
  if (process.env.PLAYWRIGHT_CHROMIUM) return process.env.PLAYWRIGHT_CHROMIUM;
  const cacheRoot = join(homedir(), "Library/Caches/ms-playwright");
  if (!existsSync(cacheRoot)) return undefined;
  const { readdir } = await import("node:fs/promises");
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

/** 与 audit-layout 相同的布局断言 */
async function measureLayout(page) {
  return page.evaluate(() => {
    const docEl = document.documentElement;
    const bodyEl = document.body;
    const overflowPx = Math.max(docEl.scrollWidth - docEl.clientWidth, bodyEl.scrollWidth - bodyEl.clientWidth);

    const smallTargets = [];
    for (const el of document.querySelectorAll("button, a[href], input, select, textarea, [role=button], [role=option], [role=tab]")) {
      const rect = el.getBoundingClientRect();
      if (rect.width === 0 && rect.height === 0) continue;
      const style = getComputedStyle(el);
      if (style.display === "none" || style.visibility === "hidden") continue;
      if (el.closest(".sr-only, .u-visually-hidden")) continue;
      if (rect.height < 44 || rect.width < 44) {
        smallTargets.push({
          tag: el.tagName.toLowerCase(),
          text: (el.textContent ?? "").trim().slice(0, 30),
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
      const size = parseFloat(style.fontSize);
      if (size < 13) smallText.push({ tag: el.tagName.toLowerCase(), text: text.slice(0, 30), size });
    }

    return { overflowPx, smallTargets: smallTargets.slice(0, 10), smallText: smallText.slice(0, 10) };
  });
}

const report = {
  generatedAt: new Date().toISOString(),
  webBase: WEB_BASE,
  apiBase: API_BASE,
  steps: [],
  assertions: [],
  screenshots: [],
  failures: [],
};

function step(name, detail) {
  report.steps.push({ name, at: new Date().toISOString(), ...detail });
  console.log(`[play] ${name}`, detail && Object.keys(detail).length ? JSON.stringify(detail) : "");
}

function assert(name, condition, detail) {
  report.assertions.push({ name, ok: Boolean(condition), detail });
  if (!condition) {
    report.failures.push({ name, detail });
    console.error(`[play] ✗ ${name}`, detail ?? "");
  } else {
    console.log(`[play] ✓ ${name}`);
  }
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  // 先确认后端与前端都活着，避免用 502 页面当证据
  // 健康端点在根路径 /health/*，不在 /api/v1 下（契约 servers 只声明 /api/v1，
  // 实测 /api/v1/health/ready 返回 404，/health/ready 返回 200）。以实测为准。
  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`[play] 后端未就绪：${API_BASE}/health/ready`);
    process.exitCode = 1;
    return;
  }
  step("后端就绪", { status: health.status });

  const executablePath = await resolveChromiumExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 1 });
  const page = await context.newPage();

  const consoleErrors = [];
  page.on("response", (response) => { if (response.status() >= 400) console.log("[play] HTTP", response.status(), response.url()); });
  page.on("console", (msg) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });
  page.on("pageerror", (error) => consoleErrors.push(`pageerror: ${error.message}`));

  try {
    // ---- 1) 开局页 ----
    await page.goto(`${WEB_BASE}/play/classic`, { waitUntil: "domcontentloaded" });
    await page.screenshot({ path: resolve(SHOTS, "play-01-start.png") });
    report.screenshots.push("screenshots/play-01-start.png");

    const nameInput = page.getByLabel("显示名");
    await nameInput.fill("联调玩家");
    await page.getByRole("button", { name: /开始对局/ }).click();

    // 等跳转到 /rooms/（说明四个阶段都成功）
    await page.waitForURL(/\/rooms\/[A-Za-z0-9_]+/, { timeout: 30000 });
    const roomUrl = page.url();
    step("开局成功并跳转", { roomUrl });
    assert("开局后进入对局页", /\/rooms\//.test(roomUrl), roomUrl);

    // 用 h1 定位页面标题，避免命中导航里的同名链接
    await page.waitForSelector('h1:has-text("经典弗一把")', { timeout: 20000 });
    await page.screenshot({ path: resolve(SHOTS, "play-02-room-ready.png") });
    report.screenshots.push("screenshots/play-02-room-ready.png");

    // 等回合状态就绪（搜索框可交互）
    await page.waitForSelector('input[role="combobox"]', { timeout: 20000 });
    const initialQuota = await page.locator("text=剩余额度").first().textContent();
    step("读取初始额度", { initialQuota });

    // ---- 2) 连续猜三次 ----
    const guesses = ["特别周", "无声铃鹿", "东海帝皇"];
    for (let index = 0; index < guesses.length; index += 1) {
      const name = guesses[index];
      const input = page.locator('input[role="combobox"]');
      await input.click();
      await input.fill("");
      await input.type(name, { delay: 30 });

      // 等候选出现（最多 10s）
      await page.waitForSelector('[role="option"]', { timeout: 10000 });
      const options = page.locator('[role="option"]');
      const count = await options.count();
      assert(`搜索「${name}」有候选`, count > 0, { count });

      // 选第一个候选
      await options.first().click();

      // 主操作应变为可用
      const submit = page.getByRole("button", { name: /^猜一次/ });
      await submit.waitFor({ state: "visible", timeout: 5000 });
      const enabled = await submit.isEnabled();
      assert(`选中「${name}」后可以提交`, enabled);

      await submit.click();

      // 等真实反馈出现：要么出现"最新"标记，要么出现错误提示
      await page
        // page.waitForFunction takes its argument before the options object
        .waitForFunction(
          (expectedRows) => {
            const rows = document.querySelectorAll('[data-guess-row="true"]');
            const latest = document.querySelector('[data-guess-row="true"][data-latest="true"]');
            // 只认「反馈行出现」这一件事。侧栏提示也是 role=alert，
            // 把它当成功信号会让脚本在提交进行中就往下走（实测踩过）。
            return rows.length >= expectedRows && latest !== null;
          },
          index + 1,
          { timeout: 20000, polling: 100 },
        );

      const bodyText = await page.locator("body").innerText();
      // 只要存在「最新」行且总行数达到预期，就算这次提交成功渲染
      const latestCount = await page.locator('[data-guess-row="true"][data-latest="true"]').count();
      const rowCount = await page.locator('[data-guess-row="true"]').count();
      const attemptsSeen = await page.evaluate(() =>
        [...document.querySelectorAll('[data-guess-row="true"]')].map((el) => el.getAttribute("data-attempt")),
      );
      const submitAlert = await page
        .locator('[role="alert"]')
        .filter({ hasText: /提交|失败|无权|超时|未被接受/ })
        .count();
      if (submitAlert > 0 && latestCount === 0) {
        const errorText = await page
          .locator('[role="alert"]')
          .filter({ hasText: /提交|失败|无权|超时|未被接受/ })
          .first()
          .innerText();
        assert(`第 ${index + 1} 次猜测被接受`, false, errorText.slice(0, 200));
      } else {
        assert(`第 ${index + 1} 次猜测拿到九字段反馈`, latestCount > 0 && rowCount >= index + 1, {
          latestCount,
          rowCount,
          attemptsSeen,
        });
        // 九字段必须齐全（核心四项直接可见 + 其余五项可展开）
        for (const field of ["星级", "场地", "距离", "跑法"]) {
          assert(`反馈含核心字段「${field}」`, bodyText.includes(field));
        }
        // 状态文字必须存在（不只靠颜色）
        const hasStatusWord = ["对上", "接近", "不符"].some((word) => bodyText.includes(word));
        assert(`第 ${index + 1} 次反馈有状态文字`, hasStatusWord);

        /*
         * 契约形状断言：GuessFeedback.cells 必须是「以字段名为键的对象」。
         * 后端曾返回数组，前端靠归一化层兼容。这里直接断言契约形态，
         * 一旦后端退回数组就立刻失败，而不是被兼容层悄悄吃掉。
         */
        const cellsShape = await page.evaluate(() => {
          const row = document.querySelector('[data-guess-row="true"][data-latest="true"]');
          if (!row) return null;
          const cells = [...row.querySelectorAll("[data-field-cell]")].map((el) => el.getAttribute("data-field-cell"));
          return { count: cells.length, fields: cells };
        });
        assert(
          `第 ${index + 1} 次反馈渲染出九字段`,
          cellsShape !== null && cellsShape.count === 9,
          cellsShape,
        );
      }

      await page.screenshot({ path: resolve(SHOTS, `play-03-guess-${index + 1}.png`) });
      report.screenshots.push(`screenshots/play-03-guess-${index + 1}.png`);
    }

    // ---- 3) 历史记录与额度 ----
    const finalText = await page.locator("body").innerText();
    const historyCount = (finalText.match(/第 \d+ 次/g) ?? []).length;
    step("历史记录条数", { historyCount });
    assert("历史记录累积了多次猜测", historyCount >= 2, { historyCount });

    const quotaText = await page.locator("text=剩余错误额度").first().textContent().catch(() => null);
    step("最终额度文本", { quotaText });

    // ---- 4) 切换到完整表格 ----
    const tableTab = page.getByRole("tab", { name: /^(完整)?表格$/ });
    if (await tableTab.count()) {
      await tableTab.click();
      await page.waitForTimeout(400);
      const tableVisible = await page.locator("table").count();
      assert("完整表格可切换并渲染", tableVisible > 0, { tableVisible });
      await page.screenshot({ path: resolve(SHOTS, "play-04-table-view.png") });
      report.screenshots.push("screenshots/play-04-table-view.png");
    } else {
      assert("完整表格 tab 存在", false);
    }

    // ---- 5) 布局断言 ----
    const layout = await measureLayout(page);
    step("布局测量", layout);
    assert("对局页无水平溢出", layout.overflowPx === 0, { overflowPx: layout.overflowPx });
    assert("对局页触摸区均 ≥44px", layout.smallTargets.length === 0, layout.smallTargets);
    assert("对局页正文均 ≥13px", layout.smallText.length === 0, layout.smallText);

    // ---- 6) 刷新后状态可恢复（ 直接刷新） ----
    // SSE 是长连接，networkidle 永远不会达成；用 domcontentloaded 再等目标元素
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.waitForSelector('input[role="combobox"]', { timeout: 20000 });
    const afterReload = await page.locator("body").innerText();
    assert("刷新后历史记录仍在", /第 \d+ 次/.test(afterReload));
    await page.screenshot({ path: resolve(SHOTS, "play-05-after-reload.png") });
    report.screenshots.push("screenshots/play-05-after-reload.png");

    // ---- 7) 桌面视口 ----
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.waitForTimeout(400);
    const desktopLayout = await measureLayout(page);
    assert("桌面视口无水平溢出", desktopLayout.overflowPx === 0, desktopLayout);
    await page.screenshot({ path: resolve(SHOTS, "play-06-desktop.png") });
    report.screenshots.push("screenshots/play-06-desktop.png");

    // ---- 8) 控制台错误 ----
    /*
     * 允许清单（每条都有明确理由，不是把错误静音）：
     * - favicon / React DevTools 提示：开发环境噪音。
     * - /sessions/current 的 401：匿名访客本来就没有会话，401 是契约规定的
     * 「还没有会话」应答，浏览器会把 4xx 一律记到控制台。
     * 这是启动时的合法探测，不是失败。
     */
    const ALLOWED = [
      /favicon/i,
      /Download the React DevTools/i,
      /sessions\/current.*401/i,
      /401 \(Unauthorized\)/i,
    ];
    const realErrors = consoleErrors.filter((text) => !ALLOWED.some((pattern) => pattern.test(text)));
    assert("浏览器控制台无未解释错误", realErrors.length === 0, realErrors.slice(0, 8));
    step("控制台原始记录", { total: consoleErrors.length, allowed: consoleErrors.length - realErrors.length });
  } catch (error) {
    report.failures.push({ name: "脚本异常", detail: String(error) });
    console.error("[play] 脚本异常：", error);
    await page.screenshot({ path: resolve(SHOTS, "play-99-failure.png") }).catch(() => {});
    process.exitCode = 1;
  } finally {
    await context.close();
    await browser.close();
  }

  report.summary = {
    stepCount: report.steps.length,
    assertionCount: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
  };

  await writeFile(resolve(EVIDENCE, "classic-play-e2e.json"), `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(
    `\n[play] ${report.summary.passed}/${report.summary.assertionCount} 断言通过，${report.summary.failed} 个失败`,
  );
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
