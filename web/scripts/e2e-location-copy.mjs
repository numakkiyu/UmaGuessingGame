/** Bounded, read-only checks of practice entry copy and accessible labels. */
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { chromium } from "playwright";
import { findExecutionLocationWords, readPlayerSurface } from "./player-copy-policy.mjs";

const base = process.env.UMA_WEB_BASE;
assert(base, "Set UMA_WEB_BASE to the isolated test instance");
const output = new URL("../../.cache/reports/2026-11-10-location-copy/", import.meta.url);
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM });
const report = { checkedAt: new Date().toISOString(), assertions: [], limits: ["Focused copy checks, not full gameplay, multiplayer or regional language review."] };
function check(name, ok, detail) {
  report.assertions.push({ name, ok, detail });
  console.log(`${ok ? "PASS" : "FAIL"} ${name}`);
}
async function scan(page, label) {
  const text = await readPlayerSurface(page);
  const words = findExecutionLocationWords(text);
  check(label, words.length === 0, words);
}
try {
  const fixture = await browser.newPage();
  await fixture.setContent('<title>检查样例</title><button title="SERVER mode" aria-label="Local practice">开始</button><img alt="Cloud game" src="data:," /><div hidden><button title="backend">隐藏</button></div>');
  const surface = await readPlayerSurface(fixture);
  check("Reads tooltips, accessible names and image descriptions", findExecutionLocationWords(surface).sort().join(",") === "cloud,local,server", surface);
  check("Excludes hidden diagnostic fixture", !surface.includes("backend"));
  await fixture.close();
  for (const width of [320, 1440]) {
    const context = await browser.newContext({ viewport: { width, height: 900 } });
    const page = await context.newPage();
    for (const path of ["/", "/play/classic", "/play/classic/verified", "/play/sort", "/play/memory", "/play/aptitude-grid", "/play/draft", "/play/race-rebuild", "/play/co-race-path", "/play/strategy-race", "/play/pedigree", "/play/audio"]) {
      await page.goto(`${base}${path}`, { waitUntil: "networkidle" });
      await page.locator("h1").first().waitFor();
      await scan(page, `${width}px ${path}`);
      if ((width === 320 && path === "/play/classic") || (width === 1440 && path === "/play/audio")) {
        await page.screenshot({ path: new URL(`${width}-${path.split("/").at(-1)}.png`, output).pathname, fullPage: true });
      }
    }
    await context.close();
  }
  const recovery = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const page = await recovery.newPage();
  await page.route("**/api/v1/practice-packs/classic", route => route.fulfill({
    status: 503, contentType: "application/problem+json",
    body: JSON.stringify({ status: 503, code: "unavailable", title: "服务器 cloud internal detail", requestId: "req-location-check" }),
  }));
  await page.goto(`${base}/play/classic`, { waitUntil: "networkidle" });
  await page.getByRole("button", { name: "准备练习资料", exact: true }).click();
  await page.getByText("请稍等片刻，再试一次。已确认的作答会保留。", { exact: true }).waitFor();
  await scan(page, "Content preparation failure hides raw execution details");
  await recovery.close();
  const storage = await browser.newContext();
  await storage.addInitScript(() => Object.defineProperty(globalThis, "indexedDB", { get() { throw new Error("本地 server storage"); } }));
  const storagePage = await storage.newPage();
  await storagePage.goto(`${base}/play/classic`, { waitUntil: "networkidle" });
  await storagePage.getByText("进度暂时无法保存。本局仍可游玩，离开或刷新前请导出记录。", { exact: true }).waitFor();
  await scan(storagePage, "Storage failure explains impact without execution location");
  await storage.close();
} finally {
  await browser.close();
  report.passed = report.assertions.filter(item => item.ok).length;
  report.total = report.assertions.length;
  await writeFile(new URL("report.json", output), `${JSON.stringify(report, null, 2)}\n`);
}
assert(report.passed === report.total, "Location copy check failed");
