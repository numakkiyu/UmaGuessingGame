import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { buildGuessRow, compareDistance, compareStar } from "../src/lib/game/compare";
import type { QuestionBankEntry } from "../src/lib/validation/schemas";

const baseEntry: QuestionBankEntry = {
  id: "special_week",
  name_zh: "特别周",
  name_tw: "特別週",
  name_jp: "スペシャルウィーク",
  name_en: "Special Week",
  aliases: ["特别周", "特別週"],
  star: 3,
  surface_group: ["草地"],
  distance_group: ["中", "长"],
  running_style_group: ["先", "差"],
  sex_type: "牝",
  g1_bracket: "3-4",
  g23_bracket: "4-6",
  g1_has_jpn: false,
  g23_has_jpn: false,
  school_grade: "中等部",
  dormitory: "栗东宿舍",
  image_url: "https://patchwiki.biligame.com/example.png",
  image_local_path: "/api/assets/special_week-thumb",
  asset_id: "special_week-thumb",
  source_priority: ["biliwiki"],
};

describe("compare functions", () => {
  it("marks same star as correct", () => {
    assert.equal(compareStar(baseEntry, baseEntry), "correct");
  });

  it("marks overlapping distance as near", () => {
    assert.equal(
      compareDistance(baseEntry, {
        ...baseEntry,
        distance_group: ["英", "中"],
      }),
      "near",
    );
  });

  it("builds guess row with values and statuses", () => {
    const row = buildGuessRow(baseEntry, baseEntry);
    assert.equal(row.cells.star.value, "3星");
    assert.equal(row.cells.star.status, "correct");
    assert.equal(row.cells.surface.value, "草地");
  });

  it("shows J only when the bracket is correct and the answer is a Jpn grade family", () => {
    const answer = {
      ...baseEntry,
      g1_has_jpn: true,
      g23_has_jpn: true,
    };
    const correctRow = buildGuessRow(baseEntry, answer);
    assert.equal(correctRow.cells.g1.value, "3-4J");
    assert.equal(correctRow.cells.g23.value, "4-6J");

    const nearRow = buildGuessRow(
      {
        ...baseEntry,
        g1_bracket: "1-2",
        g23_bracket: "7+",
      },
      answer,
    );
    assert.equal(nearRow.cells.g1.value, "1-2");
    assert.equal(nearRow.cells.g23.value, "7+");
  });
});
