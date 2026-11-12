#!/usr/bin/env node
/** Checks player-visible strings without treating identifiers, styling values or comments as interface copy. */
import { readdir, readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { findExecutionLocationWords } from "./player-copy-policy.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const SRC = resolve(HERE, "../src");
// Use the declared, isolated parser, as with the lint/contract toolchain.
const toolchainRequire = createRequire(new URL("../tools/toolchain/package.json", import.meta.url));
const ts = toolchainRequire("typescript");

/* 玩家系统文案的禁用词 */
const BANNED = [
  // 游玩不展示执行位置；必要保存/连接提示只说明影响和下一步。
  "单人联网",
  // 请求与重试实现
  "幂等键", "幂等", "Idempotency-Key", "相同请求", "重放", "payload",
  // 身份内部机制
  "CSRF", "cookie", "sessionId", "membership", "控制租约", "匿名会话", "会话过期",
  "session", "membership", "generation",
  // 接口和数据结构
  "OpenAPI", "REST", "Schema", "DTO", "契约差异", "序列化",
  // 同步和版本内部状态
  "WebSocket", "轮询降级", "outbox", "roomVersion", "phaseVersion", "epoch",
  // 存储和部署
  "SQLite", "WAL", "Docker",
  // 开发进度和验收
  "骨架已就绪", "待联调", "占位实现", "全绿", "兜底策略",
  // 明确的「还没做」措辞：未完成状态不作为可用玩法展示
  "还在实现中", "尚未实现", "开发中", "敬请期待", "ComingSoon", "Coming Soon",
  // 原始错误
  "HTTP 403", "HTTP 500", "异常堆栈",
  // 助手自述
  "作为 AI", "根据你的需求", "我已为你", "希望能帮到你",
  // 空泛宣传
  "赋能", "全方位", "全面提升", "丝滑闭环", "极致体验", "沉浸式盛宴",
  // 编号措辞：界面使用「问题编号」，「诊断编号」是开发用语
  "诊断编号",
];

/* 必要故障诊断仅按具体文案豁免，主提示仍需独立可理解，不得包含凭证、路径、堆栈或答案 */
const ALLOWED_DIAGNOSTIC_CONTEXT = [
  "问题编号", // 参考故障写法明确允许的措辞
];

// Only skip the actual attribute value, never its entire source line.
const NON_VISIBLE_ATTRS = /^(?:className|class|key|id|href|src|role|type|name|htmlFor|autoComplete|inputMode|data-.*|aria-hidden)$/;

async function collectFiles(dir) {
  const out = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...(await collectFiles(full)));
    else if (/\.tsx?$/.test(entry.name) && !/\.test\./.test(entry.name)) out.push(full);
  }
  return out;
}

/**
 * Parse syntax rather than stripping comments/whole lines with regular expressions.
 * This covers compact JSX, multiline children, accessible labels and template
 * fragments. Internal identifiers/paths/types remain outside the copy surface.
 * This is a static guard, not proof of every dynamic/API-rendered string.
 */
export function extractVisibleStrings(source, filename = "copy.tsx") {
  const file = ts.createSourceFile(filename, source, ts.ScriptTarget.Latest, true,
    filename.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const results = [];
  function visit(node) {
    const isJsxText = ts.isJsxText(node);
    const isString = ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)
      || ts.isTemplateHead(node) || ts.isTemplateMiddle(node) || ts.isTemplateTail(node);
    if (isJsxText || isString) {
      let attribute = null;
      for (let parent = node.parent; parent; parent = parent.parent) {
        if (ts.isJsxAttribute(parent)) { attribute = parent; break; }
        if (ts.isJsxElement(parent) || ts.isJsxSelfClosingElement(parent)) break;
      }
      const parent = node.parent;
      const internal = attribute && NON_VISIBLE_ATTRS.test(attribute.name.getText(file));
      const typeOrKey = ts.isLiteralTypeNode(parent)
        || ((ts.isPropertyAssignment(parent) || ts.isPropertySignature(parent)) && parent.name === node)
        || ts.isImportDeclaration(parent) || ts.isExportDeclaration(parent);
      const text = node.text.trim();
      const directChild = isJsxText || (ts.isJsxExpression(parent)
        && (ts.isJsxElement(parent.parent) || ts.isJsxFragment(parent.parent)));
      const explicitVisible = directChild || attribute;
      // Bare or namespaced code identifiers are not prose unless rendered directly.
      const codeLike = !explicitVisible && /^[\w$.-]+(?::[\w$.-]+)*$/.test(text);
      const pathLike = /^(?:[/.#]|https?:|@\/)/.test(text) || text.includes("var(--");
      if (text && !internal && !typeOrKey && !codeLike && !pathLike) {
        const { line } = file.getLineAndCharacterOfPosition(node.getStart(file));
        results.push({ text, line: line + 1 });
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(file);
  return results;
}

export function findViolations(source, filename = "copy.tsx") {
  const strings = extractVisibleStrings(source, filename);
  const violations = [];
  for (const { text, line } of strings) {
    for (const word of findExecutionLocationWords(text)) {
      violations.push({ line, word, text: text.slice(0, 100) });
    }
    for (const word of BANNED) {
      if (ALLOWED_DIAGNOSTIC_CONTEXT.some(allowed => text.includes(allowed) && word === allowed)) continue;
      if (text.includes(word)) violations.push({ line, word, text: text.slice(0, 100) });
    }
  }
  return { scannedStrings: strings.length, violations };
}

async function main() {
const files = await collectFiles(SRC);
const violations = [];
let scannedStrings = 0;

for (const file of files) {
  const source = await readFile(file, "utf8");
  const result = findViolations(source, file);
  scannedStrings += result.scannedStrings;
  for (const violation of result.violations) {
    violations.push({ file: file.replace(`${SRC}/`, ""), ...violation });
  }
}

const asJson = process.argv.includes("--json");
if (asJson) {
  console.log(JSON.stringify({ scannedFiles: files.length, scannedStrings, violations }, null, 2));
} else {
  console.log(`[copy] 扫描 ${files.length} 个文件、${scannedStrings} 条可见字符串`);
  if (violations.length === 0) {
    console.log("[copy] 未发现玩家可见禁词");
  } else {
    console.error(`[copy] 发现 ${violations.length} 处玩家可见禁词：`);
    for (const v of violations) {
      console.error(`  ${v.file}:${v.line}  [${v.word}]  ${v.text}`);
    }
  }
}
process.exitCode = violations.length > 0 ? 1 : 0;

}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) await main();
