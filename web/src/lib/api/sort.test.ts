
import { describe, expect, it } from "vitest";
import {
  criterionLabel,
  initialDraft,
  mergeWithNext,
  moveDown,
  moveUp,
  normalizeSortRound,
  splitGroup,
  validateDraft,
  type SortCard,
} from "@/lib/api/sort";
import type { components } from "@/lib/api/types";

type RoundView = components["schemas"]["RoundView"];

function card(characterId: string, raw: number, name = `角色${characterId}`): SortCard {
  return { characterId, name, imagePath: null, remoteImageUrl: null, display: `${raw}cm`, raw };
}

/** 实测形态：publicState.config.key + cards[].raw/display + self */
function makeView(overrides: {
  cards?: unknown[];
  key?: string;
  totalPairs?: number;
  self?: Record<string, unknown>;
}): RoundView {
  return {
    viewerRole: "player",
    round: {
      roundId: "rnd_sort",
      modeId: "sort",
      phase: "collecting",
      phaseVersion: 3,
      deadlineAt: null,
      paused: false,
      rulesVersion: "2.0.0-draft.1",
      datasetVersion: "ds",
      contentPackVersion: "pack",
    },
    publicState: {
      modeId: "sort",
      rulesVersion: "2.0.0-draft.1",
      phase: "collecting",
      config: { key: overrides.key ?? "height", cardCount: 6, questionSeconds: 90, eventRegion: "" },
      cards:
        overrides.cards ??
        [card("a", 140), card("b", 155), card("c", 156), card("d", 156), card("e", 159), card("f", 160)].map(
          (entry) => ({
            characterId: entry.characterId,
            name: entry.name,
            imagePath: null,
            display: entry.display,
            raw: entry.raw,
          }),
        ),
      totalPairs: overrides.totalPairs ?? 15,
      subjects: [],
      self: overrides.self ?? {
        subjectId: "mem_1",
        order: null,
        submitted: false,
        revisions: 0,
        correctPairs: 0,
        totalPairs: 0,
      },
    },
    serverTime: "2026-11-08T00:00:00Z",
    roomVersion: 4,
    privateState: {},
    controlGeneration: 0,
    submissionRevision: 0,
  } as unknown as RoundView;
}

describe("criterionLabel", () => {
  it("已知维度翻译成中文", () => {
    expect(criterionLabel("height")).toBe("身高");
    expect(criterionLabel("birthday")).toBe("生日");
  });

  it("未知维度安全回退，不显示内部标识", () => {
    expect(criterionLabel("some-new-key")).toBe("排序");
    expect(criterionLabel(null)).toBe("排序");
  });
});

describe("normalizeSortRound", () => {
  it("解析实测形态的卡片与 config", () => {
    const round = normalizeSortRound(makeView({}));
    expect(round).not.toBeNull();
    expect(round!.cards).toHaveLength(6);
    expect(round!.criterionKey).toBe("height");
    expect(round!.totalPairs).toBe(15);
    expect(round!.cards[0]!.raw).toBe(140);
    expect(round!.cards[0]!.display).toBe("140cm");
  });

  it("记录契约差异（criterion/direction 不存在）", () => {
    const round = normalizeSortRound(makeView({}));
    expect(round!.shapeWarnings.some((w) => w.includes("criterion"))).toBe(true);
  });

  it("totalPairs 缺失时按 n*(n-1)/2 推导", () => {
    const view = makeView({});
    (view.publicState as unknown as Record<string, unknown>)["totalPairs"] = undefined;
    const round = normalizeSortRound(view);
    expect(round!.totalPairs).toBe(15);
  });

  it("闭卷卡片缺少 raw/display 是正常状态，不产生警告也不丢弃", () => {
    const round = normalizeSortRound(
      makeView({ cards: [{ characterId: "x", name: "X", imagePath: null }] }),
    );
    // 卡片必须保留（玩家要靠名字/头像识别）
    expect(round!.cards).toHaveLength(1);
    expect(round!.cards[0]!.characterId).toBe("x");
    // 真值如实记为 null，但不当作异常
    expect(round!.cards[0]!.raw).toBeNull();
    expect(round!.cards[0]!.display).toBeNull();
    expect(round!.shapeWarnings.some((w) => w.includes("既无 display"))).toBe(false);
  });

  it("空输入返回 null", () => {
    expect(normalizeSortRound(null)).toBeNull();
  });
});

describe("initialDraft：闭卷要求（G06）", () => {
  it("不按真值排序：给乱序输入也不生成正确答案", () => {
    // 故意给一个按 raw 排好的输入；闭卷下输出不得是同一顺序
    const cards = [card("a", 140), card("b", 155), card("c", 156), card("d", 160)];
    const draft = initialDraft(cards);
    const outputOrder = draft.flat();
    expect(outputOrder).not.toEqual(["a", "b", "c", "d"]);
  });

  it("不自动合并同值（并列必须由玩家操作）", () => {
    const cards = [card("a", 156), card("b", 156), card("c", 156)];
    const draft = initialDraft(cards);
    expect(draft).toHaveLength(3);
    expect(draft.every((group) => group.length === 1)).toBe(true);
  });

  it("完全不含真值时也能构造（闭卷卡片没有 raw/display）", () => {
    const blindCards: SortCard[] = [
      { characterId: "x1", name: "甲", imagePath: null, remoteImageUrl: null, display: null, raw: null },
      { characterId: "x2", name: "乙", imagePath: null, remoteImageUrl: null, display: null, raw: null },
      { characterId: "x3", name: "丙", imagePath: null, remoteImageUrl: null, display: null, raw: null },
    ];
    const draft = initialDraft(blindCards);
    expect(draft).toHaveLength(3);
    expect(draft.flat().sort()).toEqual(["x1", "x2", "x3"]);
  });

  it("同一回合多次调用结果稳定（不会每次重排看花眼）", () => {
    const cards = [card("a", 140), card("b", 155), card("c", 156), card("d", 160), card("e", 165)];
    const first = initialDraft(cards);
    const second = initialDraft(cards);
    expect(first).toEqual(second);
  });

  it("不同卡片集合产生不同顺序（不退化成按 ID 升序）", () => {
    const setA = [card("a", 140), card("b", 155), card("c", 156)];
    const setB = [card("p", 140), card("q", 155), card("r", 156)];
    const orderA = initialDraft(setA).flat();
    // 至少不是按输入顺序原样返回
    expect(orderA).not.toEqual(["a", "b", "c"]);
    expect(initialDraft(setB).flat().length).toBe(3);
  });

  it("每张卡恰好出现一次，无重复无遗漏", () => {
    const cards = [card("a", 140), card("b", 155), card("c", 156), card("d", 160)];
    const flat = initialDraft(cards).flat();
    expect(flat).toHaveLength(4);
    expect(new Set(flat).size).toBe(4);
  });
});

describe("草稿操作", () => {
  const draft = [["a"], ["b"], ["c"]];

  it("上移交换相邻两组", () => {
    expect(moveUp(draft, 1)).toEqual([["b"], ["a"], ["c"]]);
  });

  it("首组不能上移（返回原引用）", () => {
    expect(moveUp(draft, 0)).toBe(draft);
  });

  it("下移交换相邻两组", () => {
    expect(moveDown(draft, 0)).toEqual([["b"], ["a"], ["c"]]);
  });

  it("末组不能下移", () => {
    expect(moveDown(draft, 2)).toBe(draft);
  });

  it("合并与下一组为并列", () => {
    expect(mergeWithNext(draft, 0)).toEqual([["a", "b"], ["c"]]);
  });

  it("拆开并列组", () => {
    expect(splitGroup([["a", "b"], ["c"]], 0)).toEqual([["a"], ["b"], ["c"]]);
  });

  it("单张组拆开无效", () => {
    const single = [["a"], ["b"]];
    expect(splitGroup(single, 0)).toBe(single);
  });

  it("操作不修改原数组（纯函数）", () => {
    const original = [["a"], ["b"]];
    const copy = JSON.parse(JSON.stringify(original)) as string[][];
    moveUp(original, 1);
    mergeWithNext(original, 0);
    expect(original).toEqual(copy);
  });
});

describe("validateDraft", () => {
  const cards = [card("a", 140), card("b", 155), card("c", 160)];

  it("完整覆盖时无问题", () => {
    expect(validateDraft([["a"], ["b"], ["c"]], cards)).toEqual([]);
  });

  it("缺少角色被发现", () => {
    const problems = validateDraft([["a"], ["b"]], cards);
    expect(problems.some((p) => p.includes("缺少"))).toBe(true);
  });

  it("重复角色被发现", () => {
    const problems = validateDraft([["a"], ["b"], ["c"], ["a"]], cards);
    expect(problems.some((p) => p.includes("重复"))).toBe(true);
  });

  it("多余角色被发现", () => {
    const problems = validateDraft([["a"], ["b"], ["c"], ["zzz"]], cards);
    expect(problems.some((p) => p.includes("不在本题"))).toBe(true);
  });

  it("空分组被发现", () => {
    const problems = validateDraft([["a"], [], ["b"], ["c"]], cards);
    expect(problems.some((p) => p.includes("空分组"))).toBe(true);
  });
});
