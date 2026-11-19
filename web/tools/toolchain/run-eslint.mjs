#!/usr/bin/env node
/* Run ESLint from web/ with the isolated toolchain dependencies. The configuration base stays at web/ while TypeScript 6 remains separate from the application compiler. */
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "../..");

// 以 toolchain 目录为基准解析 eslint 本身。
const toolchainRequire = createRequire(resolve(HERE, "package.json"));
const eslintEntry = toolchainRequire.resolve("eslint");
const { ESLint } = await import(`file://${eslintEntry}`);

process.chdir(WEB_ROOT);

const eslint = new ESLint({
  cwd: WEB_ROOT,
  overrideConfigFile: resolve(WEB_ROOT, "eslint.config.js"),
});

const results = await eslint.lintFiles([
  "src",
  "vite.config.ts",
  "vitest.config.ts",
  "scripts",
  "public/browser-support.js",
  "public/browser-notice.js",
]);

const formatter = await eslint.loadFormatter("stylish");
const output = formatter.format(results);
if (output) process.stdout.write(output);

const errorCount = results.reduce((sum, r) => sum + r.errorCount, 0);
const warningCount = results.reduce((sum, r) => sum + r.warningCount, 0);

console.log(
  `\nESLint: ${results.length} 个文件，${errorCount} 个错误，${warningCount} 个警告`,
);
process.exitCode = errorCount > 0 ? 1 : 0;
