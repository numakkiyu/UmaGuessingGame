// Synthetic results cover missing ranks and ties that a sampled live race may not contain
import { describe, expect, it } from "vitest";
import { normalizeRaceRound, rankGapsOf, type RaceAnswer } from "@/lib/api/race-rebuild";

/** 构造一个最小可用的 RoundView（只填 normalizeRaceRound 会读的字段） */
function makeView(publicState: Record<string, unknown>) {
  return {
    viewerRole: "player",
    round: {
      roundId: "rnd_test",
      modeId: "race-rebuild",
      phase: "revealed",
      phaseVersion: 4,
      deadlineAt: null,
      paused: false,
      rulesVersion: "2.0.0-draft.1",
      datasetVersion: "ds-test",
      contentPackVersion: "pack-test",
    },
    publicState,
    serverTime: "2026-11-08T00:00:00Z",
    roomVersion: 1,
    privateState: {},
    controlGeneration: 0,
    submissionRevision: 0,
  } as unknown as Parameters<typeof normalizeRaceRound>[0];
}

describe("rankGapsOf", () => {
  it("从 rankGaps 推导存在与空缺的名次", () => {
    const answer: RaceAnswer = {
      ranked: [
        { characterId: "a", name: "A", finishRank: 1, tieGroup: null },
        { characterId: "b", name: "B", finishRank: 2, tieGroup: null },
        { characterId: "c", name: "C", finishRank: 3, tieGroup: null },
        { characterId: "d", name: "D", finishRank: 5, tieGroup: null },
        { characterId: "e", name: "E", finishRank: 7, tieGroup: null },
      ],
      rankGaps: [1, 2, 3, 5, 7],
      excluded: [],
    };
    const { present, missing } = rankGapsOf(answer);
    expect(present).toEqual([1, 2, 3, 5, 7]);
    // 关键：4 与 6 必须被识别为空缺，而不是被压缩掉
    expect(missing).toEqual([4, 6]);
  });

  it("rankGaps 缺失时从 ranked 推导（不能假设连续）", () => {
    const answer: RaceAnswer = {
      ranked: [
        { characterId: "a", name: "A", finishRank: 1, tieGroup: null },
        { characterId: "b", name: "B", finishRank: 3, tieGroup: null },
      ],
      rankGaps: [],
      excluded: [],
    };
    const { present, missing } = rankGapsOf(answer);
    expect(present).toEqual([1, 3]);
    expect(missing).toEqual([2]);
  });

  it("没有空缺时 missing 为空", () => {
    const answer: RaceAnswer = {
      ranked: [
        { characterId: "a", name: "A", finishRank: 1, tieGroup: null },
        { characterId: "b", name: "B", finishRank: 2, tieGroup: null },
      ],
      rankGaps: [1, 2],
      excluded: [],
    };
    expect(rankGapsOf(answer).missing).toEqual([]);
  });
});

describe("normalizeRaceRound 名次解析", () => {
  it("保留服务端 finishRank 原值（7 就是 7，不是第 5 个）", () => {
    const view = makeView({
      modeId: "race-rebuild",
      rulesVersion: "2.0.0-draft.1",
      phase: "revealed",
      config: { questionSeconds: 180, cooperative: false, openBook: false },
      raceId: "198905040810",
      raceName: "测试赛",
      raceDate: "1989-05-04",
      entries: [
        { characterId: "a", characterName: "A" },
        { characterId: "b", characterName: "B" },
      ],
      totalPairs: 1,
      subjects: [],
      answer: {
        raceId: "198905040810",
        ranked: [
          { characterId: "a", name: "A", finishRank: 7 },
          { characterId: "b", name: "B", finishRank: 1 },
        ],
        rankGaps: [1, 7],
      },
    });

    const round = normalizeRaceRound(view);
    expect(round).not.toBeNull();
    expect(round?.answer).not.toBeNull();
    // 按真实名次排序：1 在前，7 在后
    expect(round?.answer?.ranked.map((e) => e.finishRank)).toEqual([1, 7]);
    expect(round?.answer?.ranked[1]?.finishRank).toBe(7);
    // 空缺 2–6 必须可推导
    expect(rankGapsOf(round!.answer!).missing).toEqual([2, 3, 4, 5, 6]);
  });

  it("服务端 answer.ranked 缺 name 时用公开参赛名单回填（不显示角色编号）", () => {
    const view = makeView({
      modeId: "race-rebuild",
      rulesVersion: "2.0.0-draft.1",
      phase: "revealed",
      config: { questionSeconds: 180, cooperative: false, openBook: false },
      raceId: "198905040810",
      raceName: "测试赛",
      raceDate: "1989-05-04",
      entries: [
        { characterId: "1072", characterName: "目白善信" },
        { characterId: "1071", characterName: "目白赖恩" },
      ],
      totalPairs: 1,
      subjects: [],
      // 实测形态：后端 ranked 只有 characterId/finishRank，没有 name
      answer: {
        ranked: [
          { characterId: "1072", finishRank: 1 },
          { characterId: "1071", finishRank: 2 },
        ],
        rankGaps: [1, 2],
      },
    });
    const round = normalizeRaceRound(view);
    expect(round?.answer?.ranked[0]?.name).toBe("目白善信");
    expect(round?.answer?.ranked[1]?.name).toBe("目白赖恩");
    // 不能退回编号
    expect(round?.answer?.ranked.map((e) => e.name)).not.toContain("1072");
  });

  it("服务端给了 name 时优先用服务端的", () => {
    const view = makeView({
      modeId: "race-rebuild",
      rulesVersion: "2.0.0-draft.1",
      phase: "revealed",
      config: { questionSeconds: 180, cooperative: false, openBook: false },
      raceId: "198905040810",
      raceName: "测试赛",
      raceDate: "1989-05-04",
      entries: [{ characterId: "1072", characterName: "旧名" }],
      totalPairs: 0,
      subjects: [],
      answer: {
        ranked: [{ characterId: "1072", name: "服务端名", finishRank: 1 }],
        rankGaps: [1],
      },
    });
    const round = normalizeRaceRound(view);
    expect(round?.answer?.ranked[0]?.name).toBe("服务端名");
  });

  it("未揭晓时 answer 为 null（不提前下发名次）", () => {
    const view = makeView({
      modeId: "race-rebuild",
      rulesVersion: "2.0.0-draft.1",
      phase: "collecting",
      config: { questionSeconds: 180, cooperative: false, openBook: false },
      raceId: "198905040810",
      raceName: "测试赛",
      raceDate: "1989-05-04",
      entries: [{ characterId: "a", characterName: "A" }],
      totalPairs: 0,
      subjects: [],
    });
    const round = normalizeRaceRound(view);
    expect(round?.answer).toBeNull();
  });

  it("并列名次共享同一 finishRank", () => {
    const view = makeView({
      modeId: "race-rebuild",
      rulesVersion: "2.0.0-draft.1",
      phase: "revealed",
      config: { questionSeconds: 180, cooperative: false, openBook: false },
      raceId: "198905040810",
      raceName: "测试赛",
      raceDate: "1989-05-04",
      entries: [
        { characterId: "a", characterName: "A" },
        { characterId: "b", characterName: "B" },
      ],
      totalPairs: 1,
      subjects: [],
      answer: {
        ranked: [
          { characterId: "a", name: "A", finishRank: 2, tieGroup: "g1" },
          { characterId: "b", name: "B", finishRank: 2, tieGroup: "g1" },
        ],
        rankGaps: [1, 2],
      },
    });
    const round = normalizeRaceRound(view);
    const ranks = round?.answer?.ranked.map((e) => e.finishRank) ?? [];
    expect(ranks).toEqual([2, 2]);
    expect(round?.answer?.ranked[0]?.tieGroup).toBe("g1");
  });
});
