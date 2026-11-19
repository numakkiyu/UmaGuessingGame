import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { JSDOM } from "jsdom";

const html = readFileSync(new URL("../../index.html", import.meta.url), "utf8");
const script = readFileSync(new URL("../../public/browser-support.js", import.meta.url), "utf8");
const notice = readFileSync(new URL("../../public/browser-notice.js", import.meta.url), "utf8");
function check(ua, { language = "zh-CN", saved, denied = false, missing = false, touch = 0 } = {}) {
  const dom = new JSDOM(html, { url: "https://game.example/play/classic", runScripts: "outside-only" });
  const { window } = dom;
  Object.defineProperty(window.navigator, "userAgent", { value: ua });
  Object.defineProperty(window.navigator, "languages", { value: [language] });
  Object.defineProperty(window.navigator, "maxTouchPoints", { value: touch });
  Object.assign(window, { fetch() {}, TextEncoder, structuredClone, CSS: { supports: () => !missing } });
  if (saved) window.localStorage.setItem("umaguess:language", saved);
  if (denied) Object.defineProperty(window, "localStorage", { get() { throw new Error("denied"); } });
  window.eval(script);
  if (!window.__UMAGUESS_BROWSER_SUPPORTED__) window.eval(notice);
  return dom;
}

const cases = [
  ["Chrome/110.0.0.0 Safari/537.36", false], ["Chrome/111.0.0.0 Safari/537.36", true],
  ["Chrome/157.0.0.0 Edg/110.0.0.0", false], ["Chrome/111.0.0.0 Edg/111.0.0.0", true],
  ["Firefox/127.0", false], ["Firefox/128.0", true], ["Firefox/140.0", true],
  ["Macintosh Version/17.3 Safari/605.1.15", false], ["Macintosh Version/17.4 Safari/605.1.15", true],
  ["Macintosh Version/18.0 Safari/605.1.15", true],
  ["iPhone; CPU iPhone OS 17_3 like Mac OS X Version/17.3 Safari/604.1", false],
  ["iPhone; CPU iPhone OS 17_4 like Mac OS X Version/17.4 Safari/604.1", true],
  ["iPhone; CPU iPhone OS 16_7 like Mac OS X CriOS/157.0 Safari/604.1", false],
  ["iPhone; CPU iPhone OS 18_0 like Mac OS X FxiOS/40.0 Safari/605.1.15", true],
  ["Mozilla/5.0 (Windows NT 6.1; Trident/7.0; rv:11.0) like Gecko", false],
  ["Mozilla/4.0 (compatible; MSIE 8.0; Windows NT 6.0)", false],
  ["Chrome/111.0.0.0 Edge/18.19041", false],
];
for (const [ua, expected] of cases) test(`browser policy: ${ua}`, () => {
  const dom = check(ua);
  assert.equal(dom.window.__UMAGUESS_BROWSER_SUPPORTED__, expected);
  assert.equal(dom.window.document.getElementById("browser-support").style.display, expected ? "none" : "block");
  dom.window.close();
});

test("uses iPadOS Safari's version when a tablet requests the desktop site", () => {
  for (const [version, expected] of [["17.3", false], ["17.4", true]]) {
    const dom = check(`Macintosh Version/${version} Safari/605.1.15`, { touch: 5 });
    assert.equal(dom.window.__UMAGUESS_BROWSER_SUPPORTED__, expected);
    dom.window.close();
  }
});

test("missing required features block even a recent or unrecognized user agent", () => {
  for (const ua of ["Chrome/157.0.0.0", "Other browser"]) {
    const dom = check(ua, { missing: true });
    assert.equal(dom.window.__UMAGUESS_BROWSER_SUPPORTED__, false);
    dom.window.close();
  }
});

test("starts the marked game module and styles only after a successful check", () => {
  const dom = check("Chrome/157.0.0.0");
  const entry = dom.window.document.createElement("script");
  entry.type = "application/json";
  entry.setAttribute("data-game-entry", "/assets/game.js");
  const style = dom.window.document.createElement("link");
  style.setAttribute("data-game-style", "/assets/game.css");
  dom.window.document.head.append(entry, style);
  dom.window.eval(script);
  assert(dom.window.document.querySelector('script[type="module"][src="/assets/game.js"]'));
  assert.equal(style.rel, "stylesheet");
  assert.equal(style.getAttribute("href"), "/assets/game.css");
  assert.equal(dom.window.document.querySelector('script[src="/browser-notice.js"]'), null);
  dom.window.close();
});

test("blocked preferences do not prevent the notice or block a capable browser", () => {
  const old = check("Firefox/127.0", { denied: true, language: "ja-JP" });
  assert.equal(old.window.document.documentElement.lang, "ja");
  assert.match(old.window.document.getElementById("browser-title").textContent, /ブラウザー/);
  old.window.document.querySelector('[data-browser-language="zh-Hant"]').click();
  assert.match(old.window.document.getElementById("browser-title").textContent, /瀏覽器/);
  old.window.close();
  const modern = check("Chrome/157.0.0.0", { denied: true });
  assert.equal(modern.window.__UMAGUESS_BROWSER_SUPPORTED__, true);
  modern.window.close();
});

test("follows the browser before a saved language and allows switching the notice", () => {
  const dom = check("Firefox/127.0", { saved: "zh-Hant", language: "ja-JP" });
  assert.equal(dom.window.document.documentElement.lang, "ja");
  dom.window.document.querySelector('[data-browser-language="zh-Hant"]').click();
  assert.equal(dom.window.localStorage.getItem("umaguess:language"), "zh-Hant");
  dom.window.dispatchEvent(new dom.window.Event("languagechange"));
  assert.equal(dom.window.document.documentElement.lang, "ja");
  dom.window.close();
});

for (const [language, expected] of [["zh-CN", "zh-Hans"], ["zh-SG", "zh-Hans"], ["zh-TW", "zh-Hant"], ["zh-HK", "zh-Hant"], ["zh-MO", "zh-Hant"], ["zh-Hans-HK", "zh-Hans"], ["ja-JP", "ja"]]) {
  test(`automatically localizes the notice for ${language}`, () => {
    const dom = check("Firefox/127.0", { saved:"ja", language });
    assert.equal(dom.window.document.documentElement.lang, expected);
    dom.window.close();
  });
}
