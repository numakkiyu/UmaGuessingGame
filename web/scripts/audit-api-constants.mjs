/** Prints action kinds, transition names and API constants directly from their source declarations. */
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

const root = new URL("../../", import.meta.url).pathname;
const sections = [];

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    const st = statSync(p);
    if (st.isDirectory()) walk(p, out);
    else if (name.endsWith(".go") && !name.endsWith("_test.go")) out.push(p);
  }
  return out;
}

// 1) 每个玩法的 modeAction.kind
const kinds = new Set();
for (const file of walk(join(root, "internal/modes"))) {
  const src = readFileSync(file, "utf8");
  // 不要用 [a-z]+\.[a-z]+ 这种「只匹配我预期的形状」的正则——
  // 变异测试发现，把 classic.guess 改成含大写的值后脚本会静默漏掉它，
  // 于是「没有输出」被误读成「没有这个 kind」。这里捕获任何非空字符串。
  for (const m of src.matchAll(/Action\w*\s*=\s*"([^"]+)"/g)) kinds.add(m[1]);
}
const outer = [...kinds].filter((k) => k.includes(".")).sort();
const inner = [...kinds].filter((k) => !k.includes(".")).sort();
sections.push(["modeAction.kind（对外，带 <mode>. 前缀）", outer]);
if (inner.length) sections.push(["玩法内部子动作（不是对外 kind，别直接当 modeAction.kind 用）", inner]);

// 2) tiePolicy 合法值
const events = readFileSync(join(root, "internal/room/events.go"), "utf8");
const tie = [...events.matchAll(/in\.TiePolicy\s*!=\s*"([a-z-]+)"/g)].map((m) => m[1]);
sections.push(["tiePolicy（建活动时）", [...new Set(tie)].sort()]);

// 3) 活动状态机
const statuses = [...events.matchAll(/Event\w+\s+EventStatus\s*=\s*"([a-z]+)"/g)].map((m) => m[1]);
sections.push(["EventStatus（活动生命周期）", statuses]);

// 4) 房间阶段
const transitions = readFileSync(join(root, "internal/room/transitions.go"), "utf8");
const phases = [...transitions.matchAll(/"(prepared|briefing|collecting|locked|revealed|settled|cancelled)"/g)]
  .map((m) => m[1]);
sections.push(["房间阶段（transitions target）", [...new Set(phases)]]);

// 5) 三个版本字段名（最容易混）
sections.push([
  "版本字段（三个都不一样，别混）",
  ["房间转移 → expectedPhaseVersion", "活动转移 → expectedEventVersion", "作答提交 → phaseVersion + submissionRevision"],
]);

for (const [title, items] of sections) {
  console.log(`\n${title}`);
  for (const it of items) console.log(`  ${it}`);
}
console.log("");

/*
 * 断言：常量形状必须可预期。这条让脚本从「打印清单」变成「检查」——
 * 若有人把 kind 写成驼峰、带空格、或漏了点号前缀，CI 会红，
 * 而不是静默输出一个奇怪的字符串等人踩坑。
 */
const problems = [];
/* 只校验带 <mode>. 前缀的对外 kind，排除 strategy.action 内部的 recover/follow 子动作 */
const outerKinds = [...kinds].filter((k) => k.includes("."));
for (const kind of outerKinds) {
  if (!/^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$/.test(kind)) {
    problems.push(`modeAction.kind 形状异常: "${kind}"（应为 小写.小写）`);
  }
}
if (kinds.size === 0) problems.push("没有找到任何 modeAction.kind——正则可能失效了");
if (tie.length !== 2) problems.push(`tiePolicy 应恰好 2 个合法值，实际 ${tie.length} 个`);
if (statuses.length < 4) problems.push(`EventStatus 应至少 4 个，实际 ${statuses.length} 个`);
if (problems.length > 0) {
  console.error("\n契约常量检查失败：");
  for (const p of problems) console.error(`  - ${p}`);
  process.exit(1);
}
console.log(`契约常量检查通过：${outerKinds.length} 个对外 kind、${tie.length} 个 tiePolicy、${statuses.length} 个 EventStatus、${[...new Set(phases)].length} 个阶段`);
