#!/usr/bin/env node
/** Regenerates types from api/openapi.json and checks type and operation drift. */
import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "..");
// Use the public contract so checks work in a clean checkout
const CONTRACT = resolve(WEB_ROOT, "../api/openapi.json");
const GENERATED = resolve(WEB_ROOT, "src/lib/api/schema.d.ts");
const TOOLCHAIN = resolve(WEB_ROOT, "tools/toolchain");
const OPENAPI_TS = resolve(TOOLCHAIN, "node_modules/.bin/openapi-typescript");

const problems = [];

function sha256(text) {
  return createHash("sha256").update(text).digest("hex");
}

async function regenerate(target) {
  if (!existsSync(OPENAPI_TS)) {
    problems.push(
      `缺少隔离工具链的 openapi-typescript：${OPENAPI_TS}\n  先运行 pnpm --dir tools/toolchain install`,
    );
    return null;
  }
  await execFileAsync(OPENAPI_TS, [CONTRACT, "-o", target], { cwd: TOOLCHAIN });
  return readFile(target, "utf8");
}

/** 从生成类型里抽出 operations 接口的键名 */
function extractOperationIds(source) {
  const start = source.indexOf("export interface operations {");
  if (start < 0) return [];
  const body = source.slice(start);
  return [...body.matchAll(/\n {4}([A-Za-z][A-Za-z0-9]*): \{/g)].map((match) => match[1]);
}

/** 从契约里抽出声明的 operationId */
function extractDeclaredOperations(contract) {
  const declared = [];
  for (const [, pathItem] of Object.entries(contract.paths ?? {})) {
    for (const [, operation] of Object.entries(pathItem)) {
      if (operation && typeof operation === "object" && "operationId" in operation) {
        declared.push(operation.operationId);
      }
    }
  }
  return declared;
}

async function main() {
  if (!existsSync(CONTRACT)) {
    console.error(`[api-drift] 契约文件不存在：${CONTRACT}`);
    process.exitCode = 1;
    return;
  }
  if (!existsSync(GENERATED)) {
    console.error(`[api-drift] 生成类型不存在：${GENERATED}\n  先运行 pnpm run api:types`);
    process.exitCode = 1;
    return;
  }

  const contract = JSON.parse(await readFile(CONTRACT, "utf8"));
  const committed = await readFile(GENERATED, "utf8");

  const tempDir = await mkdtemp(join(tmpdir(), "uma-api-drift-"));
  try {
    const fresh = await regenerate(join(tempDir, "schema.d.ts"));

    if (fresh !== null) {
      if (sha256(fresh) !== sha256(committed)) {
        problems.push(
          "生成类型与契约不一致（已漂移）。\n  运行 pnpm run api:types 重新生成并提交。",
        );
      } else {
        console.log(`[api-drift] schema.d.ts 与契约一致（sha256 ${sha256(committed).slice(0, 16)}…）`);
      }
    }
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }

  // operationId 一一对应
  const declared = extractDeclaredOperations(contract);
  const generated = extractOperationIds(committed);

  const missingInTypes = declared.filter((id) => !generated.includes(id));
  const extraInTypes = generated.filter((id) => !declared.includes(id));

  if (missingInTypes.length > 0) {
    problems.push(`契约里有但生成类型缺失的 operationId：${missingInTypes.join(", ")}`);
  }
  if (extraInTypes.length > 0) {
    problems.push(`生成类型里有但契约未声明的 operationId：${extraInTypes.join(", ")}`);
  }
  if (missingInTypes.length === 0 && extraInTypes.length === 0) {
    console.log(`[api-drift] operationId 一一对应：契约 ${declared.length} 个，生成 ${generated.length} 个`);
  }

  // 前端引用的 operationId 必须都在契约中
  const CONTRACT_API = resolve(WEB_ROOT, "src/lib/api/contract.ts");
  if (existsSync(CONTRACT_API)) {
    const source = await readFile(CONTRACT_API, "utf8");
    const referenced = [...source.matchAll(/call\(\s*"([A-Za-z][A-Za-z0-9]*)"/g)].map((m) => m[1]);
    const unknown = [...new Set(referenced)].filter((id) => !declared.includes(id));
    if (unknown.length > 0) {
      problems.push(`前端引用了契约中不存在的 operationId：${unknown.join(", ")}`);
    } else {
      console.log(`[api-drift] 前端引用的 ${new Set(referenced).size} 个 operationId 均存在于契约`);
    }
  }

  if (problems.length > 0) {
    console.error("\n[api-drift] 发现 %d 个问题：", problems.length);
    for (const problem of problems) console.error(`  - ${problem}`);
    process.exitCode = 1;
    return;
  }

  console.log("[api-drift] 契约类型检查通过");
}

await main();
