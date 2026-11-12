/**
 * 经典玩法归一化测试。
 *
 * 这一层的存在理由就是「契约与实测不一致」，所以测试必须同时覆盖两种形态：
 * - 契约形态：cells 是对象、历史在 privateState.guesses
 * - 实测形态：cells 是数组、历史在 publicState.self.rows、privateState 为空
 * 任何一侧漏掉，页面都会在某次后端调整后白屏。
 */
import { describe, expect, it } from "vitest";
import {
  CORE_FIELDS,
  FIELD_ORDER,
  FIELD_LABELS,
  SECONDARY_FIELDS,
  guessedCharacterIds,
  normalizeClassicRound,
  normalizeRowFromFeedback,
} from "@/lib/api/classic";
import type { components } from "@/lib/api/types";

type RoundView = components["schemas"]["RoundView"];

const NINE_FIELDS = [...FIELD_ORDER];

function makeCellsArray(overrides: Partial<Record<string, string>> = {}) {
  return NINE_FIELDS.map((field) => ({
    field,
    value: overrides[field] ?? `${field}-value`,
    status: field === "star" ? "correct" : field === "surface" ? "wrong" : "near",
  }));
}

/** 实测形态：publicState.self 承载历史与额度 */
function makeLiveRound(overrides: { attempts?: number; remainingWrong?: number; rows?: unknown[] } = {}): RoundView {
  return {
    viewerRole: "player",
    round: {
      roundId: "rnd_1",
      modeId: "classic",
      phase: "collecting",
      phaseVersion: 3,
      deadlineAt: null,
      paused: false,
      rulesVersion: "2.0.0-draft.1",
      datasetVersion: "ds",
      contentPackVersion: "pack",
    },
    publicState: {
      modeId: "classic",
      rulesVersion: "2.0.0-draft.1",
      phase: "collecting",
      config: { wrongBudget: 8, variant: "single", timeLimitSeconds: 0, practice: false },
      fieldOrder: NINE_FIELDS,
      poolSize: 108,
      subjects: [],
      self: {
        subjectId: "mem_1",
        attempts: overrides.attempts ?? 0,
        remainingWrong: overrides.remainingWrong ?? 0,
        solved: false,
        failed: false,
        rows: overrides.rows ?? null,
      },
    },
    serverTime: "2026-11-08T00:00:00Z",
    roomVersion: 4,
    privateState: {},
    controlGeneration: 0,
    submissionRevision: 0,
  } as unknown as RoundView;
}

describe("字段定义", () => {
  it("九字段顺序固定", () => {
    expect(NINE_FIELDS).toEqual([
      "star",
      "surface",
      "distance",
      "style",
      "sex",
      "g1",
      "g23",
      "grade",
      "dormitory",
    ]);
  });

  it("核心四项 + 其余五项正好覆盖九字段，无重复无遗漏", () => {
    const combined = [...CORE_FIELDS, ...SECONDARY_FIELDS];
    expect(combined).toHaveLength(9);
    expect(new Set(combined).size).toBe(9);
    expect([...combined].sort()).toEqual([...FIELD_ORDER].sort());
  });

  it("每个字段都有中文显示名", () => {
    for (const field of FIELD_ORDER) {
      expect(FIELD_LABELS[field]).toBeTruthy();
    }
  });
});

describe("实测形态（cells 为数组、历史在 publicState.self）", () => {
  it("解析出九字段，顺序稳定", () => {
    const view = makeLiveRound({
      attempts: 1,
      remainingWrong: 7,
      rows: [
        {
          characterId: "1001",
          displayName: "特别周",
          avatarPath: "/assets/avatars/1001.png",
          attempt: 1,
          correct: false,
          cells: makeCellsArray(),
        },
      ],
    });

    const round = normalizeClassicRound(view);
    expect(round).not.toBeNull();
    expect(round!.rows).toHaveLength(1);
    expect(round!.rows[0]!.cells.map((cell) => cell.field)).toEqual(NINE_FIELDS);
    expect(round!.remainingWrong).toBe(7);
    expect(round!.attempts).toBe(1);
  });

  it("按 attempt 升序排列历史，即使服务端乱序", () => {
    const rows = [
      { characterId: "1002", displayName: "B", avatarPath: null, attempt: 2, correct: false, cells: makeCellsArray() },
      { characterId: "1001", displayName: "A", avatarPath: null, attempt: 1, correct: false, cells: makeCellsArray() },
    ];
    const round = normalizeClassicRound(makeLiveRound({ attempts: 2, rows }));
    expect(round!.rows.map((row) => row.attempt)).toEqual([1, 2]);
  });
});

describe("契约形态（cells 为对象、历史在 privateState.guesses）", () => {
  it("从 privateState.guesses 读取历史", () => {
    const view = {
      viewerRole: "player",
      round: {
        roundId: "rnd_1",
        modeId: "classic",
        phase: "collecting",
        phaseVersion: 3,
        deadlineAt: null,
        paused: false,
        rulesVersion: "r",
        datasetVersion: "d",
        contentPackVersion: "p",
      },
      publicState: {
        modeId: "classic",
        rulesVersion: "r",
        phase: "collecting",
        config: { wrongBudget: 8 },
        fieldOrder: NINE_FIELDS,
        poolSize: 10,
        subjects: [],
        self: { subjectId: "m", attempts: 1, remainingWrong: 5, solved: false, failed: false, rows: null },
      },
      privateState: {
        modeId: "classic",
        remainingWrongGuesses: 5,
        guesses: [
          {
            characterId: "1001",
            displayName: "特别周",
            attempt: 1,
            cells: Object.fromEntries(
              NINE_FIELDS.map((field) => [field, { value: `${field}v`, status: "correct" }]),
            ),
          },
        ],
      },
      serverTime: "2026-11-08T00:00:00Z",
      roomVersion: 1,
      controlGeneration: 0,
      submissionRevision: 1,
    } as unknown as RoundView;

    const round = normalizeClassicRound(view);
    expect(round!.rows).toHaveLength(1);
    expect(round!.rows[0]!.cells).toHaveLength(9);
    expect(round!.rows[0]!.cells.every((cell) => cell.status === "correct")).toBe(true);
  });
});

describe("额度归一化", () => {
  it("首次提交前服务端报 0 时用 wrongBudget 显示，避免误以为额度用完", () => {
    const round = normalizeClassicRound(makeLiveRound({ attempts: 0, remainingWrong: 0 }));
    expect(round!.remainingWrong).toBe(8);
  });

  it("已有提交记录时以服务端值为准，不做推断", () => {
    const round = normalizeClassicRound(
      makeLiveRound({
        attempts: 3,
        remainingWrong: 0,
        rows: [
          { characterId: "1001", displayName: "A", avatarPath: null, attempt: 1, correct: false, cells: makeCellsArray() },
        ],
      }),
    );
    // 有提交记录且服务端说 0，就是真的用完，不能改写成 8
    expect(round!.remainingWrong).toBe(0);
  });

  it("额度缺失时是 null，界面显示「—」而不是 0", () => {
    const view = makeLiveRound();
    (view.publicState as unknown as Record<string, unknown>)["self"] = {
      subjectId: "m",
      attempts: 1,
      solved: false,
      failed: false,
      rows: [],
    };
    (view.publicState as unknown as Record<string, unknown>)["config"] = {};
    const round = normalizeClassicRound(view);
    expect(round!.remainingWrong).toBeNull();
  });
});

describe("健壮性", () => {
  it("未知字段被丢弃并记录，不会当成已知字段渲染", () => {
    const view = makeLiveRound({
      attempts: 1,
      rows: [
        {
          characterId: "1001",
          displayName: "A",
          avatarPath: null,
          attempt: 1,
          correct: false,
          cells: [...makeCellsArray(), { field: "brandNewField", value: "x", status: "correct" }],
        },
      ],
    });
    const round = normalizeClassicRound(view);
    expect(round!.rows[0]!.cells).toHaveLength(9);
    expect(round!.shapeWarnings.some((w) => w.includes("brandNewField"))).toBe(true);
  });

  it("缺失字段补 unknown，保证九格永远完整", () => {
    const view = makeLiveRound({
      attempts: 1,
      rows: [
        {
          characterId: "1001",
          displayName: "A",
          avatarPath: null,
          attempt: 1,
          correct: false,
          cells: [{ field: "star", value: "3星", status: "correct" }],
        },
      ],
    });
    const round = normalizeClassicRound(view);
    expect(round!.rows[0]!.cells).toHaveLength(9);
    expect(round!.rows[0]!.cells.filter((cell) => cell.status === "unknown")).toHaveLength(8);
  });

  it("非法状态值退化为 unknown，不猜测语义", () => {
    const view = makeLiveRound({
      attempts: 1,
      rows: [
        {
          characterId: "1001",
          displayName: "A",
          avatarPath: null,
          attempt: 1,
          correct: false,
          cells: NINE_FIELDS.map((field) => ({ field, value: "v", status: "maybe" })),
        },
      ],
    });
    const round = normalizeClassicRound(view);
    expect(round!.rows[0]!.cells.every((cell) => cell.status === "unknown")).toBe(true);
  });

  it("空输入返回 null 而不是空对象", () => {
    expect(normalizeClassicRound(null)).toBeNull();
    expect(normalizeClassicRound(undefined)).toBeNull();
  });

  it("回执归一化与快照归一化得到同一形状", () => {
    const feedback = {
      characterId: "1001",
      displayName: "特别周",
      attempt: 1,
      correct: false,
      cells: makeCellsArray(),
    };
    const row = normalizeRowFromFeedback(feedback);
    expect(row).not.toBeNull();
    expect(row!.cells.map((cell) => cell.field)).toEqual(NINE_FIELDS);
  });

  it("已猜角色集合用于搜索排除", () => {
    const view = makeLiveRound({
      attempts: 2,
      rows: [
        { characterId: "1001", displayName: "A", avatarPath: null, attempt: 1, correct: false, cells: makeCellsArray() },
        { characterId: "1002", displayName: "B", avatarPath: null, attempt: 2, correct: false, cells: makeCellsArray() },
      ],
    });
    const round = normalizeClassicRound(view);
    const ids = guessedCharacterIds(round);
    expect(ids.has("1001")).toBe(true);
    expect(ids.has("1002")).toBe(true);
    expect(ids.has("1003")).toBe(false);
  });
});
