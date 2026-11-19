#!/usr/bin/env node
import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const base = process.env.UMA_WEB_BASE ?? "http://127.0.0.1:8081", output = resolve(process.env.UMA_EVIDENCE_DIR ?? "../.cache/reports");
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_CHROMIUM ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM } : {}) });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, acceptDownloads: true }), page = await context.newPage();
const checks = [], requests = [], errors = [];
page.on("request", r => { const path = new URL(r.url()).pathname; if (path.startsWith("/api/")) requests.push({ method: r.method(), path }); });
page.on("pageerror", e => errors.push(e.message));
const check = (name, ok, detail) => { checks.push({ name, ok, detail: structuredClone(detail) }); if (!ok) throw new Error(name + ": " + JSON.stringify(detail)); };
async function state(target = page) {
  return target.evaluate(async () => {
    const db = await new Promise((resolve, reject) => { const r = indexedDB.open("umaguess-memory-practice", 1); r.onsuccess = () => resolve(r.result); r.onerror = () => reject(r.error); });
    const g = await new Promise((resolve, reject) => { const r = db.transaction("games").objectStore("games").get(location.pathname.split("/").at(-1)); r.onsuccess = () => resolve(r.result); r.onerror = () => reject(r.error); }); db.close(); return g;
  });
}
const card = i => page.getByRole("button", { name: new RegExp(`^第 ${Math.floor(i / 4) + 1} 行第 ${i % 4 + 1} 列，`) });
async function waitRevision(revision) {
  await page.waitForFunction(async expected => {
    const db = await new Promise(resolve => { const r = indexedDB.open("umaguess-memory-practice", 1); r.onsuccess = () => resolve(r.result); });
    const g = await new Promise(resolve => { const r = db.transaction("games").objectStore("games").get(location.pathname.split("/").at(-1)); r.onsuccess = () => resolve(r.result); }); db.close(); return g?.revision === expected;
  }, revision);
}
try {
  await page.clock.install();
  await page.goto(base + "/play/memory"); await page.getByRole("button", { name: "准备翻牌资料", exact: true }).waitFor(); check("entry has zero business requests", requests.length === 0, requests);
  await page.getByRole("button", { name: "准备翻牌资料", exact: true }).click(); await page.getByRole("button", { name: "准备这一局", exact: true }).waitFor({ timeout: 30000 });
  check("only one public pack request and no identity", requests.length === 1 && requests[0].path === "/api/v1/practice-packs/memory", requests);
  await context.setOffline(true);
  await page.getByRole("button", { name: "准备这一局", exact: true }).click(); await page.waitForURL(/\/practice\/memory\/local-/); await page.getByRole("button", { name: "开始记忆", exact: true }).waitFor();
  check("before start no face is mounted or timer begun", (await state()).startedAt === null && await page.locator('[aria-label="记忆棋盘"] img').count() === 0);
  await page.getByRole("button", { name: "开始记忆", exact: true }).click(); await waitRevision(1); const g = await state(), url = page.url();
  check("viewing shows sixteen faces and prevents guessing", await page.locator('[aria-label="记忆棋盘"] img').count() === 16 && await card(0).isDisabled());
  await page.locator('[aria-label="记忆棋盘"]').evaluate(el => Promise.all(el.getAnimations({ subtree: true }).map(a => a.finished)));
  check("viewing images decode and front faces actually show", await page.locator('[aria-label="记忆棋盘"] img').evaluateAll(images => images.every(img => img.complete && img.naturalWidth > 0)));
  await page.screenshot({ path: resolve(output, "memory-practice-desktop.png"), fullPage: true });
  await page.clock.fastForward(8100); await card(0).waitFor();
  check("after eight seconds all unmatched faces are removed", await page.locator('[aria-label="记忆棋盘"] img').count() === 0 && !await card(0).isDisabled());
  const otherIndex = g.board.findIndex(id => id !== g.board[0]);
  await card(0).click(); await waitRevision(2); await card(otherIndex).click(); await waitRevision(3);
  check("one miss records two flips and one attempt", (await state()).moves.length === 2 && await page.getByText("这两张不一样，记住它们的位置。", { exact: true }).isVisible());
  await page.reload(); await page.getByText("这两张不一样，记住它们的位置。", { exact: true }).waitFor();
  check("offline refresh keeps board question and original clock", JSON.stringify((await state()).board) === JSON.stringify(g.board) && (await state()).startedAt === g.startedAt);
  await page.clock.fastForward(1000);
  const partner = g.board.findIndex((id, i) => i !== 0 && id === g.board[0]);
  await card(0).click(); await waitRevision(4); await card(partner).click(); await waitRevision(5);
  check("matched pair stays up with score derived from moves", await page.getByRole("button", { name: `第 1 行第 1 列，已配对`, exact: true }).isDisabled() && await page.locator('[aria-label="记忆棋盘"] img').count() === 2);
  await page.setViewportSize({ width: 320, height: 740 }); check("320px board and controls do not overflow", await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  await page.screenshot({ path: resolve(output, "memory-practice-narrow.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 }); await page.emulateMedia({ reducedMotion: "reduce" });
  check("reduced motion removes card transitions", await page.locator(".uma-flip-card__face").first().evaluate(el => getComputedStyle(el).transitionDuration.split(",").every(value => parseFloat(value) <= 0.001)));
  check("390px layout has no horizontal overflow", await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)); await page.screenshot({ path: resolve(output, "memory-practice-mobile.png"), fullPage: true });
  const other = await context.newPage(); await other.goto(url); await other.getByRole("heading", { name: "学园记忆翻牌", exact: true }).waitFor();
  const nextIndex = g.board.findIndex((_, i) => i !== 0 && i !== partner); await card(nextIndex).click(); await waitRevision(6);
  await other.getByText("另一页已经更新了这局翻牌", { exact: false }).waitFor(); check("stale tab cannot flip", await other.getByRole("button", { name: "结束并揭晓", exact: true }).isDisabled()); await other.close();
  page.once("dialog", d => d.accept()); await page.getByRole("button", { name: "结束并揭晓", exact: true }).click(); await page.getByText("本局得分 10 分", { exact: true }).waitFor();
  check("early finish scores only completed pairs and actual misses", (await state()).ended && await page.locator('[aria-label="记忆棋盘"] img').count() === 16);
  const downloading = page.waitForEvent("download"); await page.getByRole("button", { name: "导出翻牌记录", exact: true }).click(); const download = await downloading;
  check("offline export keeps complete recovery file", download.suggestedFilename().startsWith("umaguess-memory-"));
  await page.evaluate(() => scrollTo(0, 0)); await page.screenshot({ path: resolve(output, "memory-practice-result.png"), fullPage: true });
  const exportedPath = await download.path();
  await page.goto(base + "/play/memory");
  await page.locator('input[type="file"]').setInputFiles(exportedPath);
  await page.waitForURL(/\/practice\/memory\/local-/); await page.getByText("本局得分 10 分", { exact: true }).waitFor();
  check("offline import preserves original question clock moves and result", (await state()).startedAt === g.startedAt && (await state()).gameId !== g.gameId && (await state()).moves.length === 5);
  const importedUrl = page.url();
  await page.getByRole("button", { name: "再翻一局", exact: true }).click(); await page.waitForFunction(old => location.href !== old, importedUrl); await page.getByRole("button", { name: "开始记忆", exact: true }).waitFor();
  await page.getByRole("button", { name: "开始记忆", exact: true }).click(); await page.clock.fastForward(99000); await page.getByText("本局得分 0 分", { exact: true }).waitFor();
  await page.reload(); await page.getByText("本局得分 0 分", { exact: true }).waitFor(); check("deadline ends play and refresh cannot restart it", await page.getByText("本局结束", { exact: true }).isVisible());
  check("play and offline refresh add zero business requests", requests.length === 1, requests);
  check("player copy hides execution location", !/(本机|服务器|云端|本地|服务端|客户端)/.test(await page.locator("body").innerText()));
  check("no uncaught page errors", errors.length === 0, errors);
  const denied = await browser.newContext({ viewport: { width: 390, height: 844 }, acceptDownloads: true });
  await denied.addInitScript(() => Object.defineProperty(globalThis, "indexedDB", { get() { throw new DOMException("SQLite server private path", "SecurityError"); } }));
  const limited = await denied.newPage();
  await limited.goto(base + "/play/memory"); await limited.getByRole("button", { name: "准备翻牌资料", exact: true }).waitFor();
  await limited.getByRole("button", { name: "准备翻牌资料", exact: true }).click(); await limited.getByRole("button", { name: "准备这一局", exact: true }).waitFor();
  await limited.getByRole("button", { name: "准备这一局", exact: true }).click(); await limited.getByRole("button", { name: "开始记忆", exact: true }).waitFor();
  check("storage denial still allows play with clear export warning", await limited.getByText("本局仍可继续，离开或刷新前请导出翻牌记录。", { exact: true }).isVisible());
  check("raw storage exceptions never reach player copy", !/SQLite|private path|SecurityError|本机|服务器|云端/.test(await limited.locator("body").innerText()));
  const volatileDownload = limited.waitForEvent("download"); await limited.getByRole("button", { name: "导出翻牌记录", exact: true }).click();
  check("storage denied player can export recovery file", (await volatileDownload).suggestedFilename().startsWith("umaguess-memory-"));
  await denied.close();
} catch (error) {
  checks.push({ name: "execution", ok: false, detail: String(error) });
  await page.screenshot({ path: resolve(output, "memory-practice-failure.png"), fullPage: true }).catch(() => {});
  await writeFile(resolve(output, "memory-practice-failure.txt"), await page.locator("body").innerText());
} finally {
  await writeFile(resolve(output, "memory-practice-e2e.json"), JSON.stringify({ base, checks, requests, errors }, null, 2)); await browser.close();
}
console.log(JSON.stringify(checks, null, 2)); if (checks.some(c => !c.ok)) process.exitCode = 1;
