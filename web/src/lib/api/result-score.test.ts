import { describe, expect, it } from "vitest";
import type { components } from "./types";
import { settledScore } from "./result-score";
import { normalizeMemoryRound } from "./memory";
import { normalizeSortRound } from "./sort";

type RoundView = components["schemas"]["RoundView"];
function view(phase = "settled", score: unknown = 0): RoundView {
  return {
    round: { roundId: "r", phase },
    publicState: { phase, self: { subjectId: "me", scoreUnits: 9900, correctPairs: 15, totalPairs: 15 }, cards: [] },
    results: [{ subjectId: "other", scoreUnits: 10000 }, { subjectId: "me", scoreUnits: score }],
  } as unknown as RoundView;
}

describe("authoritative result scores", () => {
  it("keeps the participant's settled zero instead of pre-surrender points", () => {
    for (const phase of ["revealed", "settled"]) {
      const snapshot = view(phase);
      expect(settledScore(snapshot, "me")).toBe(0);
      expect(normalizeSortRound(snapshot)?.scoreUnits).toBe(0);
      expect(normalizeMemoryRound(snapshot)?.self?.scoreUnits).toBe(0);
    }
  });
  it("never borrows another participant's score or treats live progress as final", () => {
    expect(settledScore(view(), "missing")).toBeNull();
    expect(settledScore(view(), undefined)).toBeNull();
    for (const phase of ["collecting", "locked", "cancelled"]) {
      expect(settledScore(view(phase), "me")).toBeNull();
    }
  });
  it("ignores malformed scores and preserves the provisional display", () => {
    for (const score of [-1, 1.2, NaN, Infinity, "10000", null]) {
      const snapshot = view("settled", score);
      expect(settledScore(snapshot, "me")).toBeNull();
      expect(normalizeSortRound(snapshot)?.scoreUnits).toBe(9900);
      expect(normalizeMemoryRound(snapshot)?.self?.scoreUnits).toBe(9900);
    }
  });
});
