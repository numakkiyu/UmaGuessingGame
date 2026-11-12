/** Synthetic signals exercise browser mechanics; never approve real character media. */
import { chromium } from "playwright";
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { findExecutionLocationWords, readPlayerSurface } from "./player-copy-policy.mjs";
const base = process.env.UMA_WEB_BASE;
if (!base) throw new Error("Set UMA_WEB_BASE to the isolated test server");
const output = new URL("../../.cache/reports/audio-practice/", import.meta.url); await mkdir(output, { recursive: true });
const tone = await readFile(new URL("../../internal/content/testdata/audio-tone.mp3", import.meta.url));
const hash = data => createHash("sha256").update(data).digest("hex");
const clips = Array.from({ length: 8 }, (_, i) => ({ clipId: `tone-${i}`, characterId: `synthetic-${i}`, characterName: `合成角色${i + 1}`, transcript: "合成测试音，不是真实角色台词", audioPath: `/assets/audio/test-tone-${i}.mp3`, durationMillis: 340, byteLength: tone.length, sha256: hash(tone) }));
const contentJSON = JSON.stringify({ clips, mediaBytes: tone.length * clips.length });
const pack = { schemaVersion: 1, modeId: "audio", rulesVersion: "2.0.0-draft.1", datasetVersion: "synthetic-test", contentPackVersion: "synthetic-test", sourceUrls: ["https://example.invalid/"], reviewStatus: "source-snapshot", contentJSON, sha256: hash(contentJSON), byteLength: Buffer.byteLength(contentJSON) };
const checks = [], requests = [], errors = [];
function check(name, ok, detail) { checks.push({ name, ok: Boolean(ok), detail }); console.log(`${ok ? "PASS" : "FAIL"} ${name}`); if (!ok) throw new Error(name); }
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, acceptDownloads: true });
async function routes(ctx, corrupt = false) {
  await ctx.route("**/api/v1/practice-packs/audio", route => route.fulfill({ contentType: "application/json", body: JSON.stringify(pack) }));
  await ctx.route("**/assets/audio/test-tone-*.mp3", route => route.fulfill({ contentType: "audio/mpeg", body: corrupt ? Buffer.from("broken") : tone }));
}
await routes(context);
const page = await context.newPage(); page.on("pageerror", e => errors.push(e.message));
page.on("request", r => { const path = new URL(r.url()).pathname; if (path.startsWith("/api/") || path.startsWith("/assets/audio/")) requests.push(path); });
const button = name => page.getByRole("button", { name, exact: true });
const heading = name => page.getByRole("heading", { name, exact: true });
async function archive(target = page) { return target.evaluate(async () => { const db = await new Promise((res, rej) => { const r = indexedDB.open("umaguess-audio-practice"); r.onsuccess = () => res(r.result); r.onerror = () => rej(r.error); }); const read = (s, k) => new Promise(res => { const r = db.transaction(s).objectStore(s).get(k); r.onsuccess = () => res(r.result); }); const game = await read("games", location.pathname.split("/").at(-1)); const pack = (await read("packs", game.packKey)).pack; db.close(); return { game, pack }; }); }
async function waitGame(predicate, data) { await page.waitForFunction(async ({ predicate, data }) => { const db = await new Promise(res => { const r = indexedDB.open("umaguess-audio-practice"); r.onsuccess = () => res(r.result); }); const g = await new Promise(res => { const r = db.transaction("games").objectStore("games").get(location.pathname.split("/").at(-1)); r.onsuccess = () => res(r.result); }); db.close(); return new Function("g", "data", `return ${predicate}`)(g, data); }, { predicate, data }); }
async function select(i, characterId) { await page.getByRole("combobox", { name: `第${i + 1}段选择角色`, exact: true }).selectOption(characterId); await waitGame("g.draft.some(p=>p.clipId===g.clipIds[data.i]&&p.characterId===data.characterId)", { i, characterId }); }
async function save(count) { await button("保存这版配对").click(); await waitGame("g.submissions===data", count); }
async function finish() { page.once("dialog", d => d.accept()); await button("结束并揭晓").click(); }
async function again() { const previous = page.url(); await button("再听一局").click(); await page.waitForFunction(previous => location.href !== previous, previous); await heading("听声音，选角色").waitFor(); }
try {
  const actual = await fetch(`${base}/api/v1/practice-packs/audio`); check("Real unreviewed source remains unavailable", actual.status === 422, actual.status);
  await page.goto(`${base}/play/audio`); await button("查看并准备语音").waitFor(); check("Practice entry creates no identity or persistent connection", requests.length === 0, [...requests]);
  await page.getByLabel("这局听几段？", { exact: true }).selectOption("6");
  let consent = ""; page.once("dialog", async d => { consent = d.message(); await d.accept(); }); await button("查看并准备语音").click(); await button("开始听音配对").waitFor({ timeout: 30000 });
  check("Explicit consent includes curated audio count and size", /8 段/.test(consent) && /MB/.test(consent), consent);
  check("Only metadata and eight selected files downloaded", requests.length === 9 && requests.filter(p => p.startsWith("/assets/audio/")).length === 8, [...requests]);
  check("Preparation needs no identity cookies", (await context.cookies()).length === 0);
  await context.setOffline(true); await button("开始听音配对").click(); await page.waitForURL(/\/practice\/audio\/local-/); await heading("听声音，选角色").waitFor();
  await page.getByRole("button", { name: "播放第1段", exact: true }).waitFor(); await page.waitForFunction(() => ![...document.querySelectorAll("button")].find(b => b.getAttribute("aria-label") === "播放第1段").disabled);
  const initial = await archive(); check("Six distinct clips and matching speaker candidates, 90 seconds after preparation", initial.game.clipIds.length === 6 && new Set(initial.game.candidateIds).size === 6 && initial.game.deadlineAt - initial.game.createdAt === 90000);
  check("No audio autoplays before interaction", await page.getByLabel("正在播放", { exact: true }).count() === 0);
  await page.getByRole("button", { name: "播放第1段", exact: true }).click(); await waitGame("g.plays[g.clipIds[0]]===1"); await page.waitForTimeout(450);
  await page.getByRole("button", { name: "播放第1段", exact: true }).click(); await waitGame("g.plays[g.clipIds[0]]===2"); await page.waitForTimeout(450);
  check("Successful prepared playback consumes exactly two starts", await page.getByRole("button", { name: "播放第1段", exact: true }).isDisabled());
  await page.reload(); await heading("听声音，选角色").waitFor(); const restored = await archive(); check("Offline refresh preserves clock question and play budget", restored.game.deadlineAt === initial.game.deadlineAt && JSON.stringify(restored.game.clipIds) === JSON.stringify(initial.game.clipIds) && restored.game.plays[initial.game.clipIds[0]] === 2);
  await page.clock.install();
  for (const [width, height] of [[320, 740], [390, 844], [768, 1024], [1024, 768], [1440, 1000], [1920, 1080]]) {
    await page.setViewportSize({ width, height }); check(`${width}px has no horizontal overflow`, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    if (width === 320 || width === 1440) await page.screenshot({ path: new URL(`play-${width}.png`, output).pathname, fullPage: true, animations: "disabled" });
  }
  check("Wide layout uses audio pairing table", await page.getByRole("table").isVisible()); await page.setViewportSize({ width: 390, height: 844 }); check("Phone layout uses sequential cards", !await page.getByRole("table").isVisible() && await page.getByRole("list").count() > 0);
  for (let i = 0; i < initial.game.clipIds.length; i++) await select(i, clips.find(c => c.clipId === initial.game.clipIds[i]).characterId);
  check("Selections remain draft until explicit save", (await archive()).game.submitted === null); await save(1);
  const firstId = clips.find(c => c.clipId === initial.game.clipIds[0]).characterId, secondId = clips.find(c => c.clipId === initial.game.clipIds[1]).characterId;
  await page.getByRole("combobox", { name: "第1段选择角色", exact: true }).selectOption(""); await waitGame("g.draft.length===5"); await select(1, firstId); await select(0, secondId); check("Draft revision preserves confirmed answer", (await archive()).game.submitted[0].characterId === firstId);
  await save(2); await page.getByRole("combobox", { name: "第1段选择角色", exact: true }).selectOption(""); await waitGame("g.draft.length===5"); await select(1, secondId); await select(0, firstId); await save(3);
  await page.getByRole("combobox", { name: "第1段选择角色", exact: true }).selectOption(""); await waitGame("g.draft.length===5"); await page.clock.fastForward(91000); await heading("听声认人，全对啦！").waitFor();
  check("Deadline scores latest saved mapping, does not submit unsaved draft", await page.getByText("本局得分 100 · 答对 6 / 6 段", { exact: true }).isVisible() && (await archive()).game.submissions === 3);
  await page.evaluate(()=>scrollTo(0,0)); await page.screenshot({ path: new URL("result-390.png", output).pathname, fullPage: true, animations: "disabled" });
  const downloadPromise = page.waitForEvent("download"); await button("导出练习记录").click(); const file = await (await downloadPromise).path();
  await page.goto(`${base}/play/audio`); await page.locator('input[type="file"]').setInputFiles(file); await heading("听声认人，全对啦！").waitFor(); check("Import keeps original clock but creates separate record", (await archive()).game.gameId !== initial.game.gameId && (await archive()).game.deadlineAt === initial.game.deadlineAt);
  await again(); const other = await context.newPage(); await other.goto(page.url()); await other.getByRole("heading", { name: "听声音，选角色", exact: true }).waitFor(); const next = await archive(); await select(0, clips.find(c => c.clipId === next.game.clipIds[0]).characterId); await other.getByText("另一页已经更新了这局练习", { exact: false }).waitFor(); check("Other tab cannot overwrite a newer draft", await other.getByRole("button", { name: "保存这版配对", exact: true }).isDisabled()); await other.close(); await finish(); await page.getByText("本局得分 0 · 答对 0 / 6 段", { exact: true }).waitFor(); check("Early finish never submits draft", (await archive()).game.submitted === null);
  await again(); await button("播放有问题？").click(); for (let i = 0; i < 6; i++) { page.once("dialog", d => d.accept()); await page.getByRole("button", { name: "这段听不了", exact: true }).first().click(); await waitGame("g.failedIds.length===data", i + 1); } await finish(); await heading("这局音频没能播出来").waitFor(); check("All failed clips produce no score, not a wrong-answer zero", await page.getByText("本局不计成绩", { exact: true }).isVisible());
  check("Independent gameplay refresh import and new rounds add no business requests", requests.length === 9, [...requests]); check("Player copy remains location-free", findExecutionLocationWords(await readPlayerSurface(page)).length === 0);
  const corrupt = await browser.newContext(); await routes(corrupt, true); const broken = await corrupt.newPage(); await broken.goto(`${base}/play/audio`); broken.once("dialog", d => d.accept()); await broken.getByRole("button", { name: "查看并准备语音", exact: true }).click(); await broken.getByText("这段音频不完整，请重新准备后再试", { exact: true }).waitFor(); check("Corrupt bytes never enable start", await broken.getByRole("button", { name: "开始听音配对", exact: true }).count() === 0); await corrupt.close();
  const denied = await browser.newContext({ acceptDownloads: true }); await routes(denied); await denied.addInitScript(() => Object.defineProperty(globalThis, "indexedDB", { get() { throw new Error("private server storage"); } })); const limited = await denied.newPage(); limited.on("pageerror", e => errors.push(e.message)); await limited.goto(`${base}/play/audio`); limited.once("dialog", d => d.accept()); await limited.getByRole("button", { name: "查看并准备语音", exact: true }).click(); await limited.getByRole("button", { name: "开始听音配对", exact: true }).waitFor(); await limited.getByRole("button", { name: "开始听音配对", exact: true }).click(); await limited.getByText("本局仍可继续，离开或刷新前请导出练习记录。", { exact: true }).waitFor(); const saving = limited.waitForEvent("download"); await limited.getByRole("button", { name: "导出练习记录", exact: true }).click(); check("Storage failure permits play and complete record export", (await saving).suggestedFilename().startsWith("umaguess-audio-") && !/private server storage/.test(await readPlayerSurface(limited))); await denied.close();
  check("No uncaught browser errors", errors.length === 0, errors);
} catch (error) { checks.push({ name: "execution", ok: false, detail: String(error) }); await page.screenshot({ path: new URL("failure.png", output).pathname, fullPage: true }).catch(() => {}); await writeFile(new URL("failure.txt", output), await page.locator("body").innerText().catch(() => "")); }
finally { await writeFile(new URL("report.json", output), JSON.stringify({ checks, requests, errors, limits: ["Synthetic audio validates mechanics only. Real character listening/attribution/transcript/name-spoiler review remains pending."] }, null, 2)); await browser.close(); }
if (checks.some(c => !c.ok)) process.exitCode = 1;
