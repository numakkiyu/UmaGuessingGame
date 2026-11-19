/** Checks that joining a full room renders the capacity message through the ordinary form flow. */
import { chromium } from "playwright";

const base = process.env.UMA_WEB_BASE;
const invite = process.env.UMA_INVITE;

const browser = await chromium.launch({
  executablePath: process.env.PLAYWRIGHT_CHROMIUM,
  headless: true,
});
const ctx = await browser.newContext();
const page = await ctx.newPage();

await page.goto(`${base}/join?code=${invite}`, { waitUntil: "networkidle" });
await page.waitForTimeout(800);

// 填昵称（邀请码已由 ?code= 预填）
await page.getByPlaceholder("其他玩家看到的名字").fill("玩家E3");
await page.getByRole("button", { name: /加入房间/ }).click();
await page.waitForTimeout(2000);

// page.evaluate 的回调在浏览器里执行，但 ESLint 按 Node 环境解析这个文件，
// 所以显式从回调参数拿 document，避免 no-undef。
const body = await page.evaluate(() => globalThis.document.body.innerText);
const hasFull = /满员/.test(body);
const hasGeneric = /你填的内容有一处不合规则/.test(body);

console.log("显示「满员」新文案:", hasFull);
console.log("显示旧的误导文案:", hasGeneric);
console.log("--- 相关片段 ---");
for (const line of body.split("\n")) {
  if (/满员|不合规则|席位|大厅另找/.test(line)) console.log("  ", line.trim());
}

await page.screenshot({ path: ".cache/reports/full-room-copy.png", fullPage: true });
await browser.close();
process.exit(hasFull && !hasGeneric ? 0 : 1);
