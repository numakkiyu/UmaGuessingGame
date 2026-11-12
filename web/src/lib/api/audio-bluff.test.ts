
import { describe, expect, it } from "vitest";
import { DEFAULT_MAX_PLAYS, normalizeAudioRound, validateAudioDraft } from "./audio";
import {
  OPTIONS_GROUP_THRESHOLD,
  groupOptions,
  isRevealPhase,
  normalizeBluffRound,
  validateBluffDraft,
} from "./bluff";

describe("normalizeAudioRound", () => {
  it("读实测字段名（audioPath / durationMillis / maxPlays）", () => {
    const round = normalizeAudioRound({
      publicState: {
        clips: [{ clipId: "c1", audioPath: "/assets/audio/a.mp3", durationMillis: 12000, maxPlays: 2 }],
        candidates: [{ candidateId: "1001", name: "特别周" }],
        preloadRequired: true,
        scoring: "每对 25 分",
      },
    });
    expect(round.clips).toHaveLength(1);
    expect(round.clips[0]?.audioPath).toBe("/assets/audio/a.mp3");
    expect(round.clips[0]?.durationMillis).toBe(12000);
    expect(round.clips[0]?.maxPlays).toBe(2);
    expect(round.candidates[0]?.name).toBe("特别周");
    expect(round.preloadRequired).toBe(true);
  });

  it("兼容契约字段名（assetUrl / durationMs）", () => {
    const round = normalizeAudioRound({
      publicState: {
        clips: [{ clipId: "c1", assetUrl: "/assets/audio/b.mp3", durationMs: 5000 }],
        candidates: [],
      },
    });
    expect(round.clips[0]?.audioPath).toBe("/assets/audio/b.mp3");
    expect(round.clips[0]?.durationMillis).toBe(5000);
  });

  it("maxPlays 缺失时用默认 2 次", () => {
    const round = normalizeAudioRound({
      publicState: { clips: [{ clipId: "c1", audioPath: "/a.mp3" }], candidates: [] },
    });
    expect(round.clips[0]?.maxPlays).toBe(DEFAULT_MAX_PLAYS);
    expect(DEFAULT_MAX_PLAYS).toBe(2);
  });

  it("clipId 缺失时用 audioPath 末段作稳定标识", () => {
    const round = normalizeAudioRound({
      publicState: { clips: [{ audioPath: "/assets/audio/xyz123.mp3" }], candidates: [] },
    });
    expect(round.clips[0]?.clipId).toBe("xyz123.mp3");
  });

  it("没有 audioPath 的条目被跳过（无法播放）", () => {
    const round = normalizeAudioRound({
      publicState: { clips: [{ clipId: "c1" }, { clipId: "c2", audioPath: "/a.mp3" }], candidates: [] },
    });
    expect(round.clips).toHaveLength(1);
    expect(round.clips[0]?.clipId).toBe("c2");
  });

  it("preloadRequired 缺省为 true（安全默认：宁可多等不误判）", () => {
    const round = normalizeAudioRound({ publicState: { clips: [], candidates: [] } });
    expect(round.preloadRequired).toBe(true);
  });

  it("空投影不崩", () => {
    const round = normalizeAudioRound(null);
    expect(round.clips).toEqual([]);
    expect(round.candidates).toEqual([]);
    expect(round.matchedCount).toBe(0);
  });

  it("validateAudioDraft 要求配对所有能听的段", () => {
    expect(validateAudioDraft(new Map(), ["c1"])).toContain("至少");
    // 只配了一段，但有两段能听 → 不完整
    expect(validateAudioDraft(new Map([["c1", "1001"]]), ["c1", "c2"])).toContain("还有 1 段");
    // 全部配完 → 通过
    expect(validateAudioDraft(new Map([["c1", "1001"], ["c2", "1002"]]), ["c1", "c2"])).toBeNull();
    // 听不了的段不算在「能听」里
    expect(validateAudioDraft(new Map([["c1", "1001"]]), ["c1"])).toBeNull();
  });
});

describe("normalizeBluffRound", () => {
  const base = {
    publicState: {
      prompt: "她最喜欢的食物是____",
      promptSource: "特别周",
      options: [
        { optionId: "o1", text: "胡萝卜", authorIds: [] },
        { optionId: "o2", text: "苹果", authorIds: ["mem_1"] },
      ],
      decoyCount: 1,
      voteCount: 0,
      optionsGrouped: false,
      scoring: "投对得分",
    },
    round: { phase: "collecting" },
  };

  it("读实测字段", () => {
    const round = normalizeBluffRound(base);
    expect(round.prompt).toContain("最喜欢的食物");
    expect(round.options).toHaveLength(2);
    expect(round.decoyCount).toBe(1);
    expect(round.optionsGrouped).toBe(false);
  });

  it("作者信息在揭晓前丢弃，不能用空作者推断真相", () => {
    const round = normalizeBluffRound(base);
    const truth = round.options.find((o) => o.authorIds.length === 0);
    expect(truth?.optionId).toBe("o1");
    expect(round.options.every(o=>o.authorIds.length===0)).toBe(true);
    // 投票阶段不能据此判断
    expect(isRevealPhase(round)).toBe(false);
  });

  it("选项超过 9 个时自动标记为需要分组", () => {
    const many = {
      publicState: {
        prompt: "x",
        options: Array.from({ length: 12 }, (_, i) => ({
          optionId: `o${i}`,
          text: `t${i}`,
          authorIds: [`mem_${i}`],
        })),
        decoyCount: 12,
        voteCount: 0,
      },
    };
    const round = normalizeBluffRound(many);
    expect(round.optionsGrouped).toBe(true);
    expect(round.options).toHaveLength(12);
  });

  it("reveal 子阶段和答案字段不能冒充真正揭晓阶段", () => {
    const round = normalizeBluffRound({
      publicState: { ...base.publicState, subphase: "reveal", revealedOptionId: "o1" },
    });
    expect(isRevealPhase(round)).toBe(false);
    expect(round.revealedOptionId).toBeNull();
  });

  it("isRevealPhase 在 voting 子阶段为 false（不能显示来源）", () => {
    const round = normalizeBluffRound({
      publicState: { ...base.publicState, subphase: "voting" },
    });
    expect(isRevealPhase(round)).toBe(false);
  });

  it("groupOptions 在未超限时返回单组", () => {
    const round = normalizeBluffRound(base);
    expect(groupOptions(round.options)).toHaveLength(1);
  });

  it("groupOptions 在超限时按阈值切分", () => {
    const options = Array.from({ length: 20 }, (_, i) => ({
      optionId: `o${i}`,
      text: `t${i}`,
      authorIds: [],
      selfAuthored: false,
    }));
    const groups = groupOptions(options);
    expect(groups).toHaveLength(3);
    expect(groups[0]).toHaveLength(OPTIONS_GROUP_THRESHOLD);
    expect(groups[2]).toHaveLength(2);
  });

  it("空投影不崩", () => {
    const round = normalizeBluffRound(undefined);
    expect(round.options).toEqual([]);
    expect(round.prompt).toBe("");
    expect(round.promptSource).toBeNull();
  });

  it("validateBluffDraft 对假答案按 NFC 计 2–40 个字", () => {
    expect(validateBluffDraft("decoy", "", false)).toBe("textLength");
    expect(validateBluffDraft("decoy", "a".repeat(81), false)).toBe("textLength");
    expect(validateBluffDraft("decoy", "正常长度", false)).toBeNull();
    expect(validateBluffDraft("vote", "", false)).toBe("chooseFirst");
    expect(validateBluffDraft("vote", "o1", false)).toBeNull();
  });
});
