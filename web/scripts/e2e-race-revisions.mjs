#!/usr/bin/env node
import { setPlayerName } from "./player-profile.mjs";
import { chromium } from "playwright";
import { writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const base = process.env.UMA_WEB_BASE ?? "http://127.0.0.1:8081";
const output = resolve(new URL("../../.cache/reports/", import.meta.url).pathname);
const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_CHROMIUM ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM } : {}) });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const checks = [], errors = [];
const check = (name, ok, detail) => { checks.push({ name, ok, detail }); if (!ok) throw new Error(name); };
page.on("pageerror", e => errors.push(e.message));
const board = () => page.getByRole("list", { name: "当前排序，从上到下", exact: true });
async function submit() {
  const pending = page.waitForResponse(r => /\/rounds\/[^/]+\/submissions$/.test(r.url()) && r.request().method() === "POST");
  await page.getByRole("button", { name: "保存本次作答", exact: true }).click();
  const response = await pending, receipt = await response.json();
  check("revision accepted", response.ok() && receipt.outcome === "accepted", { status: response.status() });
  await page.getByText("本次作答已保存，截止前可以调整后再次保存。真实名次在揭晓后显示。", { exact: true }).waitFor();
  const roundId = new URL(response.url()).pathname.split("/")[4];
  const snapshot = await (await page.request.get(`${base}/api/v1/rounds/${roundId}`)).json();
  const state = snapshot.publicState ?? snapshot.round?.publicState;
  check("accepted revision exposes neither answer nor correctness", !/"(?:correctPairs|finishRank|scoreUnits|answer)"/.test(JSON.stringify({ state, feedback: receipt.feedback })));
  return { roundId, state };
}
try {
  await page.goto(base + "/play/race-rebuild/online");
  await setPlayerName(page, "赛况验证");
  await page.getByRole("button", { name: "开始对局", exact: true }).click();
  await board().waitFor({ timeout: 30000 });
  await board().getByRole("button", { name: /合并为并列/ }).first().click();
  const first = await submit();
  check("first submission retains explicit tied group", first.state.self.groups[0].length === 2 && first.state.self.revisions === 1);
  await board().getByRole("button", { name: /下移/ }).first().click();
  const second = await submit();
  check("second revision replaces saved grouping", second.state.self.revisions === 2 && second.state.self.groups[1].length === 2 && JSON.stringify(first.state.self.groups) !== JSON.stringify(second.state.self.groups));
  await page.reload(); await board().waitFor();
  check("refresh restores saved ties without locking further revisions", await board().locator(":scope > li").nth(1).getByRole("button", { name: /拆成单独名次/ }).isEnabled());
  await page.setViewportSize({ width: 320, height: 740 });
  check("narrow revision controls fit viewport", await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  await page.screenshot({ path: resolve(output, "race-revisions-mobile.png"), fullPage: true, animations: "disabled" });
  check("no execution location or premature correctness in player copy", !/(本机|本地|服务器|云端|服务端|客户端|对关系正确|正确率\s*[:：\d])/.test(await page.locator("body").innerText()));
  check("no uncaught page errors", errors.length === 0, errors);
} catch (error) {
  checks.push({ name: "execution", ok: false, detail: String(error) });
  await page.screenshot({ path: resolve(output, "race-revisions-failure.png"), fullPage: true }).catch(() => {});
  await writeFile(resolve(output, "race-revisions-failure.txt"), await page.locator("body").innerText());
} finally {
  await browser.close();
  await writeFile(resolve(output, "race-revisions-e2e.json"), JSON.stringify({ checks, errors }, null, 2));
}
console.log(JSON.stringify(checks, null, 2));
if (checks.some(c => !c.ok)) process.exitCode = 1;
