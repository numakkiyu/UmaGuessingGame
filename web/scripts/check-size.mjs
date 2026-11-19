#!/usr/bin/env node
/** Measures gzip size for entry dependencies, initial stylesheets and individual chunks. Exits nonzero when a budget is exceeded. */
import { readdir, readFile } from "node:fs/promises";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import { staticImports } from "./static-imports.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "..");

/** 预算（字节）。gzip 后的实测值，不是未压缩大小。 */
const BUDGETS = {
  // 首屏关键 JS gzip ≤220 KiB
  entryJS: 220 * 1024,
  // 初始关键 CSS gzip ≤45 KiB
  entryCSS: 45 * 1024,
  // 本项目防回归线：任一异步 chunk gzip 不超过 120 KiB
  singleChunk: 120 * 1024,
};

const args = process.argv.slice(2);
const asJSON = args.includes("--json");
const distIdx = args.indexOf("--dist");
const DIST = distIdx >= 0 ? resolve(args[distIdx + 1]) : resolve(WEB_ROOT, "dist");

const kib = (n) => `${(n / 1024).toFixed(1)} KiB`;
const pct = (used, budget) => `${((used / budget) * 100).toFixed(1)}%`;

/** 递归列出目录下的文件（相对 DIST 的 posix 路径）。 */
async function walk(dir, base = "") {
  const out = [];
  let entries;
  try {
    entries = await readdir(join(dir, base), { withFileTypes: true });
  } catch {
    return out;
  }
  for (const e of entries) {
    const rel = base ? `${base}/${e.name}` : e.name;
    if (e.isDirectory()) out.push(...(await walk(dir, rel)));
    else out.push(rel);
  }
  return out;
}

/** gzip 一个产物文件并返回字节数。 */
async function gzippedSize(rel) {
  const buf = await readFile(join(DIST, rel));
  return gzipSync(buf, { level: 9 }).length;
}

/**
 * 从 index.html 里解析首屏入口。
 *
 * 只认真正的加载标签：<script src>、<link rel=stylesheet href>。刻意不把
 * <link rel=modulepreload> 之外的预取算进来——预取不是首屏阻塞资源，把它算进预算
 * 会让预算随预取策略变化而抖动。
 */
function parseEntryHTML(html) {
  const scripts = [];
  const styles = [];
  const scriptRe = /<script\b[^>]*\bsrc="([^"]+)"[^>]*>/gi;
  const styleRe = /<link\b[^>]*\brel="stylesheet"[^>]*\bhref="([^"]+)"[^>]*>/gi;
  // 属性顺序不固定，所以再跑一遍宽松匹配。
  const looseScript = /<script\b[^>]*src="([^"]+)"/gi;
  const looseStyle = /<link\b[^>]*href="([^"]+\.css)"/gi;
  let m;
  while ((m = scriptRe.exec(html)) || (m = looseScript.exec(html))) {
    if (m[1]) scripts.push(m[1]);
  }
  while ((m = styleRe.exec(html)) || (m = looseStyle.exec(html))) {
    if (m[1]) styles.push(m[1]);
  }
  return { scripts: [...new Set(scripts)], styles: [...new Set(styles)] };
}

/**
 * 跟随入口的静态 import，收集同步依赖。
 *
 * 读取静态 import/export 声明；动态 import() 属于异步 chunk，不计入首屏。
 * 复用检查工具链已有的语法解析器，覆盖压缩输出，不从字符串示例中误取依赖。
 */
async function collectSyncDeps(entryRel, seen = new Set()) {
  const rel = entryRel.replace(/^\//, "");
  if (seen.has(rel)) return seen;
  seen.add(rel);
  let code;
  try {
    code = await readFile(join(DIST, rel), "utf8");
  } catch {
    return seen;
  }
  const dir = dirname(rel);
  for (const imported of staticImports(code)) {
    const dep = join(dir, imported).replace(/\\/g, "/");
    await collectSyncDeps(dep, seen);
  }
  return seen;
}

async function main() {
  const results = [];
  const problems = [];

  const indexPath = join(DIST, "index.html");
  let html;
  try {
    html = await readFile(indexPath, "utf8");
  } catch {
    const msg = `产物目录里没有 index.html：${DIST}（先跑 pnpm run build）`;
    if (asJSON) {
      console.log(JSON.stringify({ ok: false, error: msg, dist: DIST }, null, 2));
    } else {
      console.error(`size: ${msg}`);
    }
    process.exit(1);
  }

  const { scripts, styles } = parseEntryHTML(html);
  // The compatibility gate starts this entry only on supported browsers.
  // Its game dependencies still count toward the normal first screen.
  for (const match of html.matchAll(/data-game-entry="([^"]+)"/g)) scripts.push(match[1]);
  for (const match of html.matchAll(/data-game-style="([^"]+)"/g)) styles.push(match[1]);

  // ---- 首屏 JS ----
  const entryFiles = new Set();
  for (const s of scripts) {
    for (const dep of await collectSyncDeps(s)) entryFiles.add(dep);
  }
  let entryJSTotal = 0;
  for (const f of [...entryFiles].sort()) {
    let size;
    try {
      size = await gzippedSize(f);
    } catch {
      continue;
    }
    entryJSTotal += size;
    results.push({ group: "entryJS", file: f, gzip: size });
  }
  const jsOver = entryJSTotal > BUDGETS.entryJS;
  if (jsOver) {
    problems.push(
      `首屏关键 JS gzip ${kib(entryJSTotal)} 超过预算 ${kib(BUDGETS.entryJS)}` +
        `（超出 ${kib(entryJSTotal - BUDGETS.entryJS)}，占预算 ${pct(entryJSTotal, BUDGETS.entryJS)}）`,
    );
  }

  // ---- 首屏 CSS ----
  let entryCSSTotal = 0;
  for (const s of styles) {
    const rel = s.replace(/^\//, "");
    let size;
    try {
      size = await gzippedSize(rel);
    } catch {
      continue;
    }
    entryCSSTotal += size;
    results.push({ group: "entryCSS", file: rel, gzip: size });
  }
  const cssOver = entryCSSTotal > BUDGETS.entryCSS;
  if (cssOver) {
    problems.push(
      `首屏关键 CSS gzip ${kib(entryCSSTotal)} 超过预算 ${kib(BUDGETS.entryCSS)}` +
        `（超出 ${kib(entryCSSTotal - BUDGETS.entryCSS)}）`,
    );
  }

  // ---- 单个异步 chunk ----
  const all = await walk(DIST);
  const jsChunks = all.filter((f) => extname(f) === ".js");
  const chunkSizes = [];
  for (const f of jsChunks) {
    const size = await gzippedSize(f);
    chunkSizes.push({ file: f, gzip: size });
  }
  chunkSizes.sort((a, b) => b.gzip - a.gzip);
  for (const c of chunkSizes) {
    if (c.gzip > BUDGETS.singleChunk) {
      problems.push(
        `单个 chunk ${c.file} gzip ${kib(c.gzip)} 超过单块上限 ${kib(BUDGETS.singleChunk)}`,
      );
    }
  }

  const summary = {
    ok: problems.length === 0,
    dist: DIST,
    budgets: {
      entryJS: BUDGETS.entryJS,
      entryCSS: BUDGETS.entryCSS,
      singleChunk: BUDGETS.singleChunk,
    },
    measured: {
      entryJS: entryJSTotal,
      entryCSS: entryCSSTotal,
      entryJSFiles: [...entryFiles].length,
      jsChunks: jsChunks.length,
      largestChunk: chunkSizes[0] ?? null,
    },
    entryFiles: results,
    problems,
  };

  if (asJSON) {
    console.log(JSON.stringify(summary, null, 2));
  } else {
    console.log(`产物：${DIST}`);
    console.log(
      `首屏关键 JS   ${kib(entryJSTotal).padStart(10)} / ${kib(BUDGETS.entryJS).padStart(10)}` +
        `   ${pct(entryJSTotal, BUDGETS.entryJS)}（${entryFiles.size} 个文件）`,
    );
    console.log(
      `首屏关键 CSS  ${kib(entryCSSTotal).padStart(10)} / ${kib(BUDGETS.entryCSS).padStart(10)}` +
        `   ${pct(entryCSSTotal, BUDGETS.entryCSS)}`,
    );
    if (chunkSizes[0]) {
      console.log(
        `最大单块      ${kib(chunkSizes[0].gzip).padStart(10)} / ${kib(BUDGETS.singleChunk).padStart(10)}` +
          `   ${pct(chunkSizes[0].gzip, BUDGETS.singleChunk)}  ${chunkSizes[0].file}`,
      );
    }
    console.log(`JS chunk 数   ${jsChunks.length}`);
    if (problems.length) {
      console.error("");
      for (const p of problems) console.error(`size: ${p}`);
    } else {
      console.log("\nsize OK：全部在预算内。");
    }
  }

  process.exit(problems.length === 0 ? 0 : 1);
}

main().catch((err) => {
  console.error(`size: ${err?.stack || err}`);
  process.exit(1);
});
