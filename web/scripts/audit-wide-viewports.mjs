/** Checks page overflow at 2560, 3440 and 3840 pixels. */
import { chromium } from "playwright";

const base = process.env.UMA_WEB_BASE;
const ROUTES = (process.env.UMA_ROUTES || "/,/help,/play,/results").split(",");
const VIEWPORTS = [
  { name: "qhd-2560x1440", width: 2560, height: 1440 },
  { name: "uwqhd-3440x1440", width: 3440, height: 1440 },
  { name: "uhd-3840x2160", width: 3840, height: 2160 },
];

const browser = await chromium.launch({
  executablePath: process.env.PLAYWRIGHT_CHROMIUM,
  headless: true,
});
const issues = [];
let measurements = 0;

for (const vp of VIEWPORTS) {
  const ctx = await browser.newContext({ viewport: { width: vp.width, height: vp.height } });
  const page = await ctx.newPage();
  for (const route of ROUTES) {
    await page.goto(`${base}${route}`, { waitUntil: "networkidle" });
    await page.waitForTimeout(300);
    const m = await page.evaluate(() => {
      const doc = globalThis.document.documentElement;
      const body = globalThis.document.body;
      // 找出真正溢出的元素（排除显式局部滚动容器）
      const offenders = [];
      for (const el of globalThis.document.querySelectorAll("*")) {
        if (el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0) {
          const cs = globalThis.getComputedStyle(el);
          const scrollable = /auto|scroll/.test(cs.overflowX);
          /* .sr-only 使用裁剪和 1px 尺寸保留读屏文本，不计入视觉溢出 */
          const srOnly = el.classList.contains("sr-only");
          if (!scrollable && !srOnly) {
            offenders.push({
              tag: el.tagName.toLowerCase(),
              cls: (el.className || "").toString().slice(0, 60),
              over: el.scrollWidth - el.clientWidth,
            });
          }
        }
      }
      return {
        overflow: Math.max(doc.scrollWidth - doc.clientWidth, body.scrollWidth - body.clientWidth),
        offenders: offenders.slice(0, 3),
      };
    });
    measurements += 1;
    if (m.overflow > 0 || m.offenders.length > 0) {
      issues.push({ viewport: vp.name, route, ...m });
    }
  }
  await ctx.close();
}

await browser.close();
console.log(`超宽视口审计：${measurements} 次测量，${issues.length} 个问题`);
for (const i of issues) {
  console.log(`  ${i.viewport} ${i.route} 页面溢出=${i.overflow}px`);
  for (const o of i.offenders) console.log(`    <${o.tag} class="${o.cls}"> 超出 ${o.over}px`);
}
process.exit(issues.length === 0 ? 0 : 1);
