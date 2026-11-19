#!/usr/bin/env node
import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const base = process.env.UMA_WEB_BASE ?? "http://127.0.0.1:8081", output = resolve(process.env.UMA_EVIDENCE_DIR ?? new URL("../../.cache/reports/", import.meta.url).pathname);
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_CHROMIUM ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM } : {}) });
const checks = [], requests = [], errors = [];
const check = (name, ok, detail) => { checks.push({ name, ok, detail }); if (!ok) throw new Error(name + ": " + JSON.stringify(detail)); };
let page;
async function archive(target, mode) {
  return target.evaluate(async mode => {
    const db = await new Promise((res, rej) => { const r = indexedDB.open(mode === "draft" ? "umaguess-draft-practice" : "umaguess-grid-practice"); r.onsuccess = () => res(r.result); r.onerror = () => rej(r.error); });
    const read = (table, key) => new Promise((res, rej) => { const r = db.transaction(table).objectStore(table).get(key); r.onsuccess = () => res(r.result); r.onerror = () => rej(r.error); });
    const game = await read("games", location.pathname.split("/").at(-1)), record = await read("packs", game.packKey); db.close(); return { game, pack: record.pack, content: JSON.parse(record.pack.contentJSON) };
  }, mode);
}
async function waitRevision(target, mode, revision) { await target.waitForFunction(async ({ mode, revision }) => {
  const db = await new Promise(res => { const r = indexedDB.open(mode === "draft" ? "umaguess-draft-practice" : "umaguess-grid-practice"); r.onsuccess = () => res(r.result); });
  const g = await new Promise(res => { const r = db.transaction("games").objectStore("games").get(location.pathname.split("/").at(-1)); r.onsuccess = () => res(r.result); }); db.close(); return g?.revision === revision;
}, { mode, revision }); }
function condition(g, i) { const d = ["短", "英", "中"], s = ["逃", "先", "差"], row = Math.floor(i / 3), col = i % 3; return { distance: d[g.rowAxis === "distance" ? row : col], style: s[g.rowAxis === "distance" ? col : row] }; }
function solution(cards, g) { const owners = new Map(), result = Array(9); function visit(i, seen) { const cond = condition(g, i); for (const c of cards) { if (seen.has(c.id) || c.aptitudes.distance[cond.distance] !== "A" || c.aptitudes.style[cond.style] !== "A") continue; seen.add(c.id); const previous = owners.get(c.id); if (previous === undefined || visit(previous, seen)) { owners.set(c.id, i); result[i] = c.id; return true; } } return false; } for (let i = 0; i < 9; i++) if (!visit(i, new Set())) throw new Error("unsolvable test board"); return result; }
async function downloadAndImport(target, mode) {
  const before = await archive(target, mode), pending = target.waitForEvent("download"); await target.getByRole("button", { name: "导出挑战记录", exact: true }).click(); const download = await pending, file = await download.path();
  await target.goto(base + "/play/" + mode); await target.locator('input[type="file"]').setInputFiles(file); await target.waitForURL(new RegExp(`/practice/${mode}/local-`)); await target.getByText("本局结束", { exact: true }).waitFor(); const after = await archive(target, mode);
  check(mode + " offline export/import preserves result and clock", after.game.gameId !== before.game.gameId && after.game.deadlineAt === before.game.deadlineAt && after.game.revision === before.game.revision);
}
try {
  for (const mode of ["aptitude-grid", "draft"]) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, acceptDownloads: true }); page = await context.newPage(); await page.clock.install();
    const seen = []; page.on("request", r => { if (new URL(r.url()).pathname.startsWith("/api/")) { const row = { mode, method: r.method(), path: new URL(r.url()).pathname }; seen.push(row); requests.push(row); } }); page.on("pageerror", e => errors.push(e.message));
    await page.goto(base + "/play/" + mode); const prepare = mode === "draft" ? "准备编队资料" : "准备占格资料", start = mode === "draft" ? "开始编队" : "开始占格";
    await page.getByRole("button", { name: prepare, exact: true }).waitFor(); check(mode + " entry creates no identity or room", seen.length === 0, [...seen]);
    await page.getByRole("button", { name: prepare, exact: true }).click(); await page.getByRole("button", { name: start, exact: true }).waitFor({ timeout: 30000 }); check(mode + " prepares using one public request", seen.length === 1 && seen[0].path === "/api/v1/practice-packs/" + mode, [...seen]);
    await context.setOffline(true); await page.getByRole("button", { name: start, exact: true }).click(); await page.waitForURL(new RegExp(`/practice/${mode}/local-`));
    await page.getByRole("heading", { name: mode === "draft" ? "四项任务，各派一位" : "这格派谁？", exact: true }).waitFor();
    const initial = await archive(page, mode), url = page.url();
    if (mode === "aptitude-grid") {
      const cells = page.locator('[aria-label="占格棋盘"] button'), answer = solution(initial.content.cards, initial.game);
      await cells.nth(0).click(); check("grid hides grade tables and fit feedback before confirmation", await page.getByRole("button", { name: "确认占格", exact: true }).count() === 1 && !/适配点数|符合条件的角色/.test(await page.locator("body").innerText()));
      const wrong = initial.content.cards.find(c => { const cond = condition(initial.game, 0); return c.aptitudes.distance[cond.distance] !== "A" || c.aptitudes.style[cond.style] !== "A"; });
      await page.getByLabel("搜索角色名字", { exact: true }).fill(wrong.name); await page.getByRole("button", { name: wrong.name, exact: true }).click(); await page.getByRole("button", { name: "确认占格", exact: true }).click(); await page.getByText(/这次不扣机会/).waitFor(); check("grid wrong confirmation consumes no turn or revision", (await archive(page, mode)).game.revision === 0);
      const first = initial.content.cards.find(c => c.id === answer[0]); await page.getByLabel("搜索角色名字", { exact: true }).fill(first.name); await page.getByRole("button", { name: first.name, exact: true }).click(); await page.getByRole("button", { name: "确认占格", exact: true }).click(); await waitRevision(page, mode, 1);
      await page.reload(); await page.getByRole("heading", { name: "这格派谁？", exact: true }).waitFor(); check("grid offline refresh preserves locked claim and original deadline", (await archive(page, mode)).game.deadlineAt === initial.game.deadlineAt && (await archive(page, mode)).game.claims[0].characterId === first.id && await page.locator('[aria-label="占格棋盘"] button').nth(0).isDisabled());
      const other = await context.newPage(); await other.goto(url); await other.getByRole("heading", { name: "这格派谁？", exact: true }).waitFor();
      for (let i = 1; i < 9; i++) {
        if (i === 1) { await page.setViewportSize({ width: 320, height: 740 }); check("grid 320px preserves three columns and 84px cells", await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth) && await page.locator('[aria-label="占格棋盘"] button').first().evaluate(el => el.getBoundingClientRect().width >= 84)); await page.evaluate(() => scrollTo(0, 0)); await page.screenshot({ path: resolve(output, "grid-practice-narrow.png"), fullPage: true }); }
        await page.locator('[aria-label="占格棋盘"] button').nth(i).click(); const scope = i === 1 ? page.getByRole("dialog") : page;
        const c = initial.content.cards.find(c => c.id === answer[i]); await scope.getByLabel("搜索角色名字", { exact: true }).fill(c.name); await scope.getByRole("button", { name: c.name, exact: true }).click(); await scope.getByRole("button", { name: "确认占格", exact: true }).click(); await waitRevision(page, mode, i + 1);
        if (i === 1) { await page.getByRole("dialog").waitFor({ state: "hidden" }); check("grid small screen picker returns focus to board", await page.locator("#grid-board-heading").evaluate(el => el === document.activeElement)); await other.getByText("另一页已经更新了这局挑战", { exact: false }).waitFor(); check("grid stale tab is read only", await other.getByRole("button", { name: "结束并揭晓", exact: true }).isDisabled()); await other.close(); await page.setViewportSize({ width: 1440, height: 1000 }); }
      }
      await page.getByText("九格集齐，漂亮！", { exact: true }).waitFor(); check("grid nine unique legal claims score 100 and finish", (await archive(page, mode)).game.claims.length === 9 && await page.getByText("100分", { exact: true }).isVisible());
      await page.evaluate(() => scrollTo(0, 0)); await page.screenshot({ path: resolve(output, "grid-practice-desktop-result.png"), fullPage: true });
    } else {
      check("draft begins with neutral pending slots and disabled final confirmation", await page.getByRole("button", { name: "确认编队并结束", exact: true }).isDisabled() && await page.locator("#draft-tasks").evaluate(el => [...el.closest("section").querySelectorAll("li")].every(li => !li.className.includes("color-danger"))));
      check("draft desktop uses visible table with twelve public cards", await page.getByRole("table").isVisible() && await page.locator("tbody tr").count() === 12);
      await page.evaluate(() => scrollTo(0, 0)); await page.screenshot({ path: resolve(output, "draft-practice-desktop.png"), fullPage: true });
      const market = initial.game.marketIds.map(id => initial.content.cards.find(c => c.id === id)); let chosen = null;
      for (let a = 0; a < 12 && !chosen; a++) for (let b = a + 1; b < 12 && !chosen; b++) for (let c = b + 1; c < 12 && !chosen; c++) for (let d = c + 1; d < 12 && !chosen; d++) if ([a,b,c,d].reduce((sum,i) => sum + market[i].price,0) <= 24) chosen = [a,b,c,d].map(i => market[i]);
      for (let i = 0; i < 4; i++) {
        await page.getByRole("button", { name: "选角色", exact: true }).first().click(); const dialog = page.getByRole("dialog"); await dialog.getByLabel("搜索角色名字", { exact: true }).fill(chosen[i].name); await dialog.getByRole("button", { name: "选她", exact: true }).click(); await waitRevision(page, mode, i + 1); await dialog.waitFor({ state: "hidden" });
        if (i === 0) {
          await page.reload(); await page.getByRole("heading", { name: "四项任务，各派一位", exact: true }).waitFor(); check("draft offline refresh keeps original tasks market and draft", JSON.stringify((await archive(page, mode)).game.marketIds) === JSON.stringify(initial.game.marketIds) && (await archive(page, mode)).game.deadlineAt === initial.game.deadlineAt && Object.values((await archive(page, mode)).game.draft)[0] === chosen[0].id);
          await page.setViewportSize({ width: 390, height: 844 }); check("draft phone shows cards and hides table without overflow", !await page.getByRole("table").isVisible() && await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)); await page.evaluate(() => scrollTo(0, 0)); await page.screenshot({ path: resolve(output, "draft-practice-mobile.png"), fullPage: true });
        }
      }
      await page.setViewportSize({ width: 320, height: 740 }); check("draft 320px controls and grades do not overflow", await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)); await page.evaluate(() => scrollTo(0, 0)); await page.screenshot({ path: resolve(output, "draft-practice-narrow.png"), fullPage: true });
      check("draft final confirmation enables only after complete legal lineup", !await page.getByRole("button", { name: "确认编队并结束", exact: true }).isDisabled());
      page.once("dialog", d => d.accept()); await page.getByRole("button", { name: "确认编队并结束", exact: true }).click(); await page.getByText("编队成绩出炉！", { exact: true }).waitFor(); const result = (await archive(page, mode)).game;
      check("draft final confirmation ends immediately and locks slots", !!result.submitted && result.ended && await page.getByRole("button", { name: "换一位", exact: true }).first().isDisabled()); await page.evaluate(() => scrollTo(0, 0)); await page.screenshot({ path: resolve(output, "draft-practice-result.png"), fullPage: true });
    }
    await downloadAndImport(page, mode); check(mode + " player text hides processing location", !/本机|服务器|云端|本地|服务端|客户端/.test(await page.locator("body").innerText()));
    const old = page.url(); await page.getByRole("button", { name: "再开一局", exact: true }).click(); await page.waitForFunction(old => location.href !== old, old); await page.getByText("结束并揭晓", { exact: true }).waitFor(); await page.clock.fastForward(mode === "draft" ? 241000 : 181000); await page.getByText("本局结束", { exact: true }).waitFor(); await page.reload(); await page.getByText("本局结束", { exact: true }).waitFor();
    check(mode + " deadline survives refresh and never submits draft", mode !== "draft" || (await archive(page, mode)).game.submitted === null);
    check(mode + " all play import export refresh produce zero extra API requests", seen.length === 1, [...seen]); await context.close();
  }
  for (const mode of ["aptitude-grid", "draft"]) {
    const denied = await browser.newContext({ viewport: { width: 390, height: 844 }, acceptDownloads: true });
    await denied.addInitScript(() => Object.defineProperty(globalThis, "indexedDB", { get() { throw new DOMException("private server database path", "SecurityError"); } }));
    const limited = await denied.newPage(); limited.on("pageerror", e => errors.push(e.message));
    await limited.goto(base + "/play/" + mode);
    const prepare = mode === "draft" ? "准备编队资料" : "准备占格资料", start = mode === "draft" ? "开始编队" : "开始占格";
    await limited.getByRole("button", { name: prepare, exact: true }).click(); await limited.getByRole("button", { name: start, exact: true }).waitFor({ timeout: 30000 }); await limited.getByRole("button", { name: start, exact: true }).click();
    await limited.getByText("本局仍可继续，离开或刷新前请导出挑战记录。", { exact: true }).waitFor();
    check(mode + " storage denial allows play without raw errors", !/private server|database path|SecurityError|服务器|本机|云端/.test(await limited.locator("body").innerText()));
    const pending = limited.waitForEvent("download"); await limited.getByRole("button", { name: "导出挑战记录", exact: true }).click();
    check(mode + " storage denial retains export recovery", (await pending).suggestedFilename().startsWith(mode === "draft" ? "umaguess-draft-" : "umaguess-grid-"));
    await denied.close();
  }
  check("no uncaught page errors", errors.length === 0, errors);
} catch (error) { checks.push({ name: "execution", ok: false, detail: String(error) }); if (page && !page.isClosed()) { await page.evaluate(() => scrollTo(0, 0)); await page.screenshot({ path: resolve(output, "aptitude-practice-failure.png"), fullPage: true }).catch(() => {}); await writeFile(resolve(output, "aptitude-practice-failure.txt"), await page.locator("body").innerText()); } }
finally { await writeFile(resolve(output, "aptitude-practice-e2e.json"), JSON.stringify({ checks, requests, errors }, null, 2)); await browser.close(); }
console.log(JSON.stringify(checks, null, 2)); if (checks.some(c => !c.ok)) process.exitCode = 1;
