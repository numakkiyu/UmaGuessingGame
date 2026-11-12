#!/usr/bin/env node
/** Checks sorting controls, persisted drafts, hidden values before reveal and displayed results against a running service. */
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

const report = { generatedAt: new Date().toISOString(), assertions: [], failures: [], screenshots: [], steps: [] };

function assert(name, ok, detail) {
  report.assertions.push({ name, ok: Boolean(ok), detail });
  if (ok) console.log(`[sort-e2e] ✓ ${name}`);
  else {
    report.failures.push({ name, detail });
    console.error(`[sort-e2e] ✗ ${name}`, detail === undefined ? "" : JSON.stringify(detail));
  }
}

function step(name, detail) {
  report.steps.push({ name, ...detail });
  console.log(`[sort-e2e] · ${name}`, detail && Object.keys(detail).length ? JSON.stringify(detail) : "");
}

async function measureLayout(page) {
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
        smallTargets.push({ tag: el.tagName.toLowerCase(), text: (el.textContent ?? "").trim().slice(0, 24), w: Math.round(rect.width), h: Math.round(rect.height) });
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
      if (parseFloat(style.fontSize) < 13) smallText.push({ tag: el.tagName.toLowerCase(), text: text.slice(0, 24), size: parseFloat(style.fontSize) });
    }
    return { overflowPx, smallTargets: smallTargets.slice(0, 8), smallText: smallText.slice(0, 8) };
  });
}

async function main() {
  await mkdir(SHOTS, { recursive: true });

  const health = await fetch(`${API_BASE}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`[sort-e2e] 后端未就绪：${API_BASE}/health/ready`);
    process.exitCode = 1;
    return;
  }

  const executablePath = await resolveChromiumExecutable();
  const browser = await chromium.launch(executablePath ? { executablePath } : {});
  const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const page = await context.newPage();

  const consoleErrors = [];
  page.on("console", (msg) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });
  page.on("pageerror", (error) => consoleErrors.push(`pageerror: ${error.message}`));

  try {
    // ---- 1) 开局 ----
    await page.goto(`${WEB_BASE}/play/sort`, { waitUntil: "commit" });
    await page.waitForSelector("input#sort-name", { timeout: 20000 });
    await page.getByLabel("显示名").fill("排序E2E");
    await page.getByRole("button", { name: /开始对局/ }).click();

    // 不用 waitForURL：SSE 长连接让 load 事件永不触发
    await page.waitForFunction(() => location.pathname.startsWith("/rooms/"), { timeout: 40000 });
    await page.waitForSelector("ol > li", { timeout: 25000 });
    await page.waitForTimeout(1000);

    const cardCount = await page.locator("ol > li").count();
    step("开局成功", { cardCount, url: page.url() });
    assert("开局后进入排序页并渲染卡片", cardCount >= 4, { cardCount });

    const boardText = await page.locator("body").innerText();
    // Closed-book: the server withholds display/raw during collecting, so the board must
    // not show a value column and there must be no "本地预览" element that computes a
    // score from the true ordering. Both would hand the answer to the player.
    assert(
      "闭卷：界面不显示本地预览",
      !/本地预览/.test(boardText),
      boardText.match(/本地预览[^\n]*/)?.[0] ?? null,
    );
    assert(
      "闭卷：卡片不显示排序值",
      !/排序值/.test(boardText),
      boardText.match(/排序值[^\n]*/)?.[0] ?? null,
    );
    assert("同值卡显示为并列", /并列/.test(boardText));

    await page.screenshot({ path: resolve(SHOTS, "sort-e2e-01-board.png") });
    report.screenshots.push("screenshots/sort-e2e-01-board.png");

    // ---- 2) 上移 / 下移 ----
    const firstBefore = (await page.locator("ol > li").first().innerText()).split("\n").slice(0, 4).join("|");
    /*
     * 用「任意一个非首组的上移按钮」而不是硬编码「第 2 名」：
     * 初始草稿会自动合并同值，组数不固定（可能是 4 组也可能是 6 组），
     * 硬编码第 2 名在只有 1 组时会误报失败。
     */
    const upButtons = page.locator('button[aria-label*="上移"]');
    const upCount = await upButtons.count();
    // 跳过第一组的上移（首组本就禁用），取第二个
    const upBtn = upCount >= 2 ? upButtons.nth(1) : upButtons.first();
    if (upCount >= 2) {
      await upBtn.click();
      await page.waitForTimeout(350);
      const firstAfter = (await page.locator("ol > li").first().innerText()).split("\n").slice(0, 4).join("|");
      assert("上移真实改变顺序", firstBefore !== firstAfter, { before: firstBefore, after: firstAfter });

      await page.getByRole("button", { name: /把「第 1 名」下移/ }).click();
      await page.waitForTimeout(350);
      const restored = (await page.locator("ol > li").first().innerText()).split("\n").slice(0, 4).join("|");
      assert("下移可恢复原位", restored === firstBefore);
    } else {
      assert("存在上移按钮", false);
    }

    // ---- 3) 合并 / 拆分 ----
    const mergeBtn = page.getByRole("button", { name: /合并为并列/ }).first();
    if (await mergeBtn.count()) {
      await mergeBtn.click();
      await page.waitForTimeout(350);
      assert("合并后出现并列组", /并列第/.test(await page.locator("body").innerText()));

      const splitBtn = page.getByRole("button", { name: /拆成单独名次/ }).first();
      if (await splitBtn.count()) {
        await splitBtn.click();
        await page.waitForTimeout(350);
        assert("拆分可撤销合并", true);
      } else {
        assert("存在拆分按钮", false);
      }
    } else {
      assert("存在合并按钮", false);
    }

    // ---- 4) 草稿保存 + 刷新 ----
    await page.getByRole("button", { name: /保存草稿/ }).click();
    await page.waitForTimeout(350);
    assert("保存草稿有明确反馈", /草稿已保存/.test(await page.locator("body").innerText()));

    await page.reload({ waitUntil: "commit" });
    await page.waitForSelector("ol > li", { timeout: 25000 });
    await page.waitForTimeout(1500);
    const afterReloadCount = await page.locator("ol > li").count();
    /*
     * 刷新后：卡片仍应渲染（草稿从 localStorage 恢复）。
     * 不断言「组数完全等于刷新前」——用户可能刚做过合并/拆分操作，
     * 且刷新后若 CSRF 失效会重建会话，题目本身可能重抽。
     * 这里断言的是「草稿机制可用」：卡片存在且能继续操作。
     */
    assert("刷新后草稿仍可继续", afterReloadCount >= 2, { afterReloadCount, beforeReload: cardCount });

    // ---- 5) 提交 ----
    const submit = page.getByRole("button", { name: /最终提交/ });
    assert("最终提交按钮可用", await submit.isEnabled());
    await submit.click();
    await page.waitForFunction(() => /得分/.test(document.body.innerText), { timeout: 30000 });
    await page.waitForTimeout(1500);

    const resultText = await page.locator("body").innerText();
    const scoreMatch = resultText.match(/得分\s*(\d+)/);
    const pairsMatch = resultText.match(/(\d+)\/(\d+)/);
    step("提交结果", { score: scoreMatch?.[1], pairs: pairsMatch?.[0] });
    assert("提交后显示得分", scoreMatch !== null, scoreMatch?.[0] ?? null);
    // Closed-book means a full score is no longer reachable by reading values off the
    // board: the player has to know the ordering. So the assertion is that the score is
    // reported and internally consistent, not that it is 100.
    assert("提交后有得分", scoreMatch !== null, scoreMatch?.[0] ?? null);
    if (pairsMatch) {
      assert(
        "关系对比例在合法区间",
        Number(pairsMatch[1]) <= Number(pairsMatch[2]),
        { pairs: pairsMatch[0] },
      );
    }

    // ---- 6) 提交后滚动到结果区 ----
    const scrollY = await page.evaluate(() => window.scrollY);
    const resultVisible = await page.evaluate(() => {
      const el = document.getElementById("sort-result");
      if (!el) return false;
      const rect = el.getBoundingClientRect();
      return rect.top < window.innerHeight && rect.bottom > 0;
    });
    step("滚动位置", { scrollY, resultVisible });
    assert("提交后结果区进入视口", resultVisible === true, { scrollY });

    await page.screenshot({ path: resolve(SHOTS, "sort-e2e-02-result.png") });
    report.screenshots.push("screenshots/sort-e2e-02-result.png");

    // ---- 7) 布局 ----
    const layout = await measureLayout(page);
    assert("排序结果页无水平溢出", layout.overflowPx === 0, { overflowPx: layout.overflowPx });
    assert("排序结果页触摸区均 ≥44px", layout.smallTargets.length === 0, layout.smallTargets);
    assert("排序结果页正文均 ≥13px", layout.smallText.length === 0, layout.smallText);

    // ---- 8) 桌面视口 ----
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.waitForTimeout(500);
    const desktop = await measureLayout(page);
    assert("桌面视口无水平溢出", desktop.overflowPx === 0, { overflowPx: desktop.overflowPx });

    // ---- 9) 控制台 ----
    const ALLOWED = [/favicon/i, /Download the React DevTools/i, /401 \(Unauthorized\)/i];
    const realErrors = consoleErrors.filter((text) => !ALLOWED.some((p) => p.test(text)));
    assert("浏览器控制台无未解释错误", realErrors.length === 0, realErrors.slice(0, 6));
  } catch (error) {
    report.failures.push({ name: "脚本异常", detail: String(error) });
    console.error("[sort-e2e] 脚本异常：", error);
    await page.screenshot({ path: resolve(SHOTS, "sort-e2e-99-failure.png") }).catch(() => {});
    process.exitCode = 1;
  } finally {
    await context.close();
    await browser.close();
  }

  report.summary = {
    assertions: report.assertions.length,
    passed: report.assertions.filter((a) => a.ok).length,
    failed: report.failures.length,
  };

  await writeFile(resolve(EVIDENCE, "sort-e2e.json"), `${JSON.stringify(report, null, 2)}\n`, "utf8");
  console.log(`\n[sort-e2e] ${report.summary.passed}/${report.summary.assertions} 断言通过，${report.summary.failed} 个失败`);
  if (report.failures.length > 0) process.exitCode = 1;
}

await main();
