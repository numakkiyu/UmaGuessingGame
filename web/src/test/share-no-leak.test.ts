// Sharing answers depends on reveal permission, not on whether the current player solved the question
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const SOURCE = readFileSync(
  resolve(process.cwd(), "src/features/classic/classic-room-page.tsx"),
  "utf8",
);

describe("经典对局分享文本", () => {
  it("分享入口存在", () => {
    expect(SOURCE).toContain("ShareResultButton");
  });

  it("是否写答案只由服务端揭晓状态决定，不由 solved 决定", () => {
    // 抓出拼装分享文本的那一段。
    const start = SOURCE.indexOf("const shareLines");
    expect(start, "找不到 shareLines，分享文本构造被改动了").toBeGreaterThan(-1);
    const block = SOURCE.slice(start, start + 900);

    expect(block).toContain("revealedAnswerCharacterId");
    // solved/failed 只能出现在「猜了几次」的措辞里，不能出现在写答案的条件里。
    const answerLine = block
      .split("\n")
      .filter((line) => line.includes("答案是") || line.includes("shareLines.push"))
      .join("\n");
    expect(answerLine).not.toMatch(/round\.solved|round\.failed/);
  });

  it("答案行只由揭晓标识决定，不额外要求自己猜过", () => {
    const start = SOURCE.indexOf("const shareLines");
    const block = SOURCE.slice(start, start + 900);
    // 揭晓后的分享答案独立于本人的猜测记录；零次作答也可分享答案
    expect(block).toMatch(/if \(round\.revealedAnswerCharacterId\)/);
    expect(block).not.toMatch(/if \(round\.revealedAnswerCharacterId && answerRow\)/);
  });
});
