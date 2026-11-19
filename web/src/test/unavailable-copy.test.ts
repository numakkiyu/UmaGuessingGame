
import { describe, expect, it } from "vitest";
import { describeUnavailable } from "@/lib/copy/unavailable";

const INTERNAL = [
  "no_local_audio_file",
  "unreviewed_fact_not_publishable",
  "no_reviewed_secret_fact",
  "no_usable_items",
  "minimum 8",
  "leading rejections",
  "本机", "本地", "服务器", "云端", "服务端", "客户端", "项目里",
];

const REAL_AUDIO = "audio: no usable items (minimum 8); leading rejections: no_local_audio_file=8";
const REAL_BLUFF =
  "bluff: no usable items (minimum 8); leading rejections: unreviewed_fact_not_publishable=254, no_reviewed_secret_fact=5";

function assertClean(modeId: string, reason: string) {
  const copy = describeUnavailable(modeId, reason);
  expect(copy).not.toBeNull();
  const visible = `${copy!.title}\n${copy!.detail}`;
  for (const token of INTERNAL) {
    expect(visible).not.toContain(token);
  }
  return copy!;
}

describe("describeUnavailable", () => {
  it("audio 的真实准入原因翻译成玩家说法", () => {
    const copy = assertClean("audio", REAL_AUDIO);
    expect(`${copy.title} ${copy.detail}`).toContain("语音题目");
    expect(copy.title).toContain("不能开局");
    expect(copy.detail).toContain("回大厅");
  });

  it("bluff 的真实准入原因翻译成玩家说法", () => {
    const copy = assertClean("bluff", REAL_BLUFF);
    expect(copy.title).toContain("核对");
  });

  it("未知拒绝码给通用说明，不回显原始字符串", () => {
    const copy = assertClean("some-mode", "some-mode: brand_new_rejection_code=42");
    expect(copy.title).toContain("暂时不能开局");
  });

  it("空原因表示可用，不渲染提示", () => {
    expect(describeUnavailable("audio", null)).toBeNull();
    expect(describeUnavailable("audio", undefined)).toBeNull();
    expect(describeUnavailable("audio", "   ")).toBeNull();
  });

  it("同一拒绝码对不同 modeId 也翻译一致", () => {
    const a = describeUnavailable("x", "x: no_local_audio_file=3");
    const b = describeUnavailable("audio", REAL_AUDIO);
    expect(a?.title).toBe(b?.title);
  });
});

/*
 * 回归防护：这个缺陷在 4 个地方重复出现过（mode-start-page、lobby、mode-info、
 * sort/memory play pages）。静态断言所有渲染点都必须经过 describeUnavailable，
 * 避免下次新增页面时又把内部拒绝码直出给玩家。
 */
describe("不可用原因的渲染点必须经过翻译层", () => {
  it("src 下没有任何地方把 unavailableReason 直接渲染", async () => {
    const { readdir, readFile } = await import("node:fs/promises");
    const { join, resolve } = await import("node:path");

    const root = resolve(process.cwd(), "src");
    const offenders: string[] = [];

    async function walk(dir: string): Promise<void> {
      for (const entry of await readdir(dir, { withFileTypes: true })) {
        const full = join(dir, entry.name);
        if (entry.isDirectory()) {
          await walk(full);
          continue;
        }
        if (!/\.tsx?$/.test(entry.name)) continue;
        if (entry.name === "unavailable.ts" || entry.name === "schema.d.ts") continue;
        const text = await readFile(full, "utf8");
        for (const line of text.split("\n")) {
          const trimmed = line.trim();
          // 跳过注释行与导入
          if (trimmed.startsWith("*") || trimmed.startsWith("//") || trimmed.startsWith("import")) continue;
          // 允许：经过 describeUnavailable 的取值
          if (trimmed.includes("describeUnavailable")) continue;
          // 禁止：形如 {unavailableReason} 或 {mode.unavailableReason} 的直接插值
          if (/\{\s*(mode\.)?unavailableReason\s*\}/.test(trimmed)) {
            offenders.push(`${full.replace(root, "src")}: ${trimmed.slice(0, 90)}`);
          }
        }
      }
    }

    await walk(root);
    expect(offenders).toEqual([]);
  });
});
