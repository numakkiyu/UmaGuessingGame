#!/usr/bin/env node
/** Checks uma-*, u-* and sr-only references against local CSS definitions. Tailwind utilities are outside this check. */
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const WEB = new URL("..", import.meta.url).pathname.replace(/\/$/, "");
const SRC = join(WEB, "src");
const STYLES = join(SRC, "styles");

/** 递归收集目录下的指定后缀文件。 */
function collect(dir, ext, out = []) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      collect(full, ext, out);
    } else if (full.endsWith(ext)) {
      out.push(full);
    }
  }
  return out;
}

// 1) 已定义的类名：从 CSS 里抓 `.uma-xxx`。
// 也接受 `.uma-xxx--modifier` 这种修饰形式，因为修饰类通常和基类一起出现。
const defined = new Set();
for (const file of collect(STYLES, ".css")) {
  const css = readFileSync(file, "utf8");
  // 本项目有两套自定义前缀：uma-*（组件）与 u-*（工具类）。
  // sr-only 是跨项目通用的无障碍类，也在这里定义，一并收进来。
  for (const match of css.matchAll(/\.((?:uma|u|sr)-[a-z0-9-]+)/g)) {
    defined.add(match[1]);
  }
}

// 2) 被引用的类名：从 tsx/ts 里抓字符串中出现的 uma-xxx。
// 只抓完整的类名 token，避免把 uma-card__title 里的 uma-card 也当成引用。
const used = new Map(); // className -> [file:line]
for (const file of [...collect(SRC, ".tsx"), ...collect(SRC, ".ts")]) {
  // 跳过生成文件与检查脚本自身。
  if (file.includes("schema.d.ts")) continue;
  const text = readFileSync(file, "utf8");
  const lines = text.split("\n");
  lines.forEach((line, index) => {
    for (const match of line.matchAll(/(?<![a-zA-Z0-9_-])((?:uma|u|sr)-[a-z0-9-]+)/g)) {
      const name = match[1];
      if (!used.has(name)) used.set(name, []);
      used.get(name).push(`${relative(WEB, file)}:${index + 1}`);
    }
  });
}

// Reject empty scans so a broken path or matcher cannot pass silently
if (defined.size < 10) {
  console.error(`class names: only ${defined.size} definitions found in styles/*.css`);
  console.error("扫描 CSS 定义失败了（路径错、目录改名，或正则没匹配上）。");
  console.error("空集不能当作通过。");
  process.exit(1);
}
if (used.size < 10) {
  console.error(`class names: only ${used.size} references found in src/`);
  console.error("扫描源码引用失败了（路径错、后缀变了，或正则没匹配上）。");
  console.error("空集不能当作通过。");
  process.exit(1);
}

// 4) 差集：用了但没定义。
const missing = [];
for (const [name, where] of used) {
  if (!defined.has(name)) missing.push({ name, where });
}

if (missing.length === 0) {
  console.log(`class names: ${used.size} referenced, all defined`);
  process.exit(0);
}

console.error(`class names: ${missing.length} referenced but never defined`);
for (const item of missing.sort((a, b) => a.name.localeCompare(b.name))) {
  console.error(`  .${item.name}  ← ${item.where.join(", ")}`);
}
console.error("");
console.error("这些类名不会报错、不会警告，浏览器会静默忽略它们。");
console.error("改成项目里真实存在的类，或用 @/components/ui 下的组件。");
process.exit(1);
