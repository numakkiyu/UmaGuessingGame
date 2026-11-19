import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import { setTimeout } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const docs = fileURLToPath(new URL("..", import.meta.url));
const socket = createServer();
await new Promise((resolve, reject) => socket.once("error", reject).listen(0, "127.0.0.1", resolve));
const port = socket.address().port;
await new Promise(resolve => socket.close(resolve));
const base = `http://127.0.0.1:${port}${process.env.DOCS_BASE || "/UmaGuessingGame/"}`;
const server = spawn("pnpm", ["exec", "vitepress", "preview", ".", "--host", "127.0.0.1", "--port", String(port)], { cwd: docs, stdio: ["ignore", "pipe", "pipe"] });
let output = "", launchError;
server.stdout.on("data", chunk => { output += chunk; });
server.stderr.on("data", chunk => { output += chunk; });
server.on("error", error => { launchError = error; });
let browser, checks = 0;

try {
  let ready = false;
  for (let attempt = 0; attempt < 100; attempt++) {
    if (launchError) throw launchError;
    if (server.exitCode !== null) throw new Error(`Preview stopped: ${output}`);
    try { ready = (await fetch(base)).ok; } catch { /* The preview may still be starting. */ }
    if (ready) break;
    await setTimeout(100);
  }
  assert(ready, `Preview did not start: ${output}`);
  browser = await chromium.launch({ headless: true, ...(process.env.DOCS_BROWSER_CHANNEL ? { channel: process.env.DOCS_BROWSER_CHANNEL } : {}) });
  for (const colorScheme of ["light", "dark"]) {
    for (const locale of ["", "zh-Hant/", "ja/"]) {
      const context = await browser.newContext({ colorScheme });
      const page = await context.newPage();
      const errors = [];
      page.on("pageerror", error => errors.push(error.message));
      try {
        for (const width of [360, 390, 768, 959, 1024, 1199, 1280, 1440, 1920]) {
          await page.setViewportSize({ width, height: 1000 });
          for (const name of ["技术架构说明", "文档索引", "配置参考"]) {
            const label = `${colorScheme} ${locale}${name} ${width}px`;
            const response = await page.goto(base + locale + encodeURIComponent(name));
            assert.equal(response.status(), 200, label);
            await page.evaluate(() => document.fonts.ready);
            await page.waitForFunction(() => [...document.images].every(image => {
              const box = image.getBoundingClientRect();
              return !box.width || box.bottom <= 0 || box.top >= innerHeight || image.complete;
            }));
            const layout = await page.evaluate(() => {
              const title = document.querySelector(".VPNavBarTitle .title").getBoundingClientRect();
              const search = document.querySelector(".VPNavBarSearch").getBoundingClientRect();
              return {
                overflow: document.documentElement.scrollWidth > innerWidth,
                missingImages: [...document.images].filter(image => {
                  const box = image.getBoundingClientRect();
                  return box.width && box.bottom > 0 && box.top < innerHeight && !image.naturalWidth;
                }).map(image => image.src),
                overlap: title.right > search.left + 1,
                tables: [...document.querySelectorAll(".vp-doc table")].map(table => ({
                  width: table.getBoundingClientRect().width,
                  container: table.parentElement.clientWidth,
                })),
                fallbackVisible: [...document.querySelectorAll(".architecture-static")].some(element => getComputedStyle(element).display !== "none"),
              };
            });
            assert(!layout.overflow, `${label}: page overflow`);
            assert(!layout.overlap, `${label}: logo overlaps search`);
            assert.deepEqual(layout.missingImages, [], `${label}: missing image`);
            assert(!layout.fallbackVisible, `${label}: duplicate diagram`);
            for (const table of layout.tables) assert(table.width >= table.container - 2, `${label}: table does not fill its container`);
            assert.deepEqual(errors, [], label);
            checks++;
          }
          if ([390, 1024, 1280, 1440, 1920].includes(width)) {
            if (width < 1200) {
              await page.locator(".VPNavBarHamburger").click();
              await page.locator("#VPNavScreen").waitFor({ state: "visible" });
              await page.locator(".VPNavBarHamburger").click();
              await page.locator("#VPNavScreen").waitFor({ state: "hidden" });
            }
            if (width < 960) {
              await page.locator(".VPLocalNav .menu").click();
              await page.locator(".VPSidebar.open").waitFor({ state: "visible" });
              await page.keyboard.press("Escape");
              await page.locator(".VPSidebar.open").waitFor({ state: "hidden" });
            }
            if (width < 1440) {
              await page.locator(".VPLocalNavOutlineDropdown > button").click();
              await page.locator(".VPLocalNavOutlineDropdown .items").waitFor({ state: "visible" });
              await page.keyboard.press("Escape");
              await page.locator(".VPLocalNavOutlineDropdown .items").waitFor({ state: "hidden" });
            }
            await page.goto(base + locale);
            await page.evaluate(() => document.fonts.ready);
            assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${colorScheme} ${locale}home ${width}px: overflow`);
          }
        }
      } finally { await context.close(); }
    }
  }
  console.log(`${checks} documentation layout checks passed; navigation passed in all three languages and both themes`);
} finally {
  await browser?.close();
  server.kill("SIGTERM");
}
