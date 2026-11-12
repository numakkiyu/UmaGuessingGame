/** Checks keyboard reachability, visible focus and image alternatives in a real browser. */
import { chromium } from "playwright";

const base = process.env.UMA_WEB_BASE;
const browser = await chromium.launch({
  executablePath: process.env.PLAYWRIGHT_CHROMIUM,
  headless: true,
});
const page = await browser.newPage();
await page.goto(base, { waitUntil: "networkidle" });

const report = {};

// 1) 图片是否都有 alt（装饰性图可用 alt="" 但必须有该属性）
report.images = await page.evaluate(() =>
  Array.from(globalThis.document.images).map((img) => ({
    src: img.currentSrc.slice(-40) || "(inline)",
    alt: img.getAttribute("alt"),
  })),
);

// 2) 表单控件是否都有可访问名称
report.controls = await page.evaluate(() =>
  Array.from(globalThis.document.querySelectorAll("input,select,textarea,button")).map((el) => {
    const id = el.getAttribute("id");
    const labelledBy = el.getAttribute("aria-labelledby");
    const ariaLabel = el.getAttribute("aria-label");
    const label = id ? globalThis.document.querySelector(`label[for="${id}"]`) : null;
    const text = (el.textContent || "").trim().slice(0, 20);
    return {
      tag: el.tagName.toLowerCase(),
      name: ariaLabel || labelledBy || label?.textContent?.trim() || text || null,
      placeholder: el.getAttribute("placeholder"),
    };
  }),
);

// 3) Tab 键能走到的元素数与前几个的可见焦点
await page.keyboard.press("Tab");
const firstFocus = await page.evaluate(() => {
  const el = globalThis.document.activeElement;
  if (!el || el === globalThis.document.body) return null;
  const cs = globalThis.getComputedStyle(el);
  return {
    tag: el.tagName.toLowerCase(),
    text: (el.textContent || "").trim().slice(0, 24),
    outline: cs.outlineStyle !== "none" && cs.outlineWidth !== "0px",
    boxShadow: cs.boxShadow !== "none",
  };
});
report.firstTabTarget = firstFocus;

// 4) 连续 Tab 5 次，记录经过的元素
const trail = [];
for (let i = 0; i < 5; i += 1) {
  await page.keyboard.press("Tab");
  trail.push(
    await page.evaluate(() => {
      const el = globalThis.document.activeElement;
      return el && el !== globalThis.document.body
        ? `${el.tagName.toLowerCase()}:${(el.textContent || "").trim().slice(0, 16)}`
        : "body";
    }),
  );
}
report.tabTrail = trail;

// 5) 跳转链接（skip link）是否存在——键盘用户需要绕过导航
// 注意：本项目的中文文案是「跳到主要内容」——不要只匹配「跳转/跳过」，
// 否则会把已经存在的跳转链接判成缺失（我第一次就漏了「跳到」）。
report.skipLink = await page.evaluate(() =>
  Array.from(globalThis.globalThis.document.querySelectorAll("a")).some((a) =>
    /跳(到|转|过)|skip/i.test(a.textContent || ""),
  ),
);

console.log(JSON.stringify(report, null, 2));
await page.screenshot({ path: ".cache/reports/a11y-home.png", fullPage: false });
await browser.close();
