import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { excludeGuessedSearchEntries } from "../src/lib/game/search";
import type { GuessRow, SearchIndexEntry } from "../src/lib/validation/schemas";

function createSearchEntry(id: string, name: string): SearchIndexEntry {
  return {
    id,
    name_zh: name,
    name_tw: null,
    name_jp: `${name}JP`,
    name_en: null,
    aliases: [name],
    image_local_path: `/assets/${id}.png`,
    image_url: null,
  };
}

function createGuessRow(characterId: string, displayName: string): GuessRow {
  return {
    characterId,
    displayName,
    avatarUrl: `/assets/${characterId}.png`,
    avatarFallbackUrl: null,
    cells: {
      star: { value: "3", status: "wrong" },
      surface: { value: "草地", status: "wrong" },
      distance: { value: "中距离", status: "wrong" },
      style: { value: "先行", status: "wrong" },
      sex: { value: "牝", status: "wrong" },
      g1: { value: "GI", status: "wrong" },
      g23: { value: "GII", status: "wrong" },
      grade: { value: "高等部", status: "wrong" },
      dormitory: { value: "栗东宿舍", status: "wrong" },
    },
  };
}

describe("search entry filtering", () => {
  it("keeps all entries when nothing has been guessed", () => {
    const entries = [createSearchEntry("special-week", "特别周")];

    assert.deepEqual(excludeGuessedSearchEntries(entries, []), entries);
  });

  it("removes already guessed characters from the candidate pool", () => {
    const entries = [
      createSearchEntry("special-week", "特别周"),
      createSearchEntry("silence-suzuka", "无声铃鹿"),
      createSearchEntry("tokai-teio", "东海帝皇"),
    ];
    const guessRows = [
      createGuessRow("special-week", "特别周"),
      createGuessRow("tokai-teio", "东海帝皇"),
    ];

    const filtered = excludeGuessedSearchEntries(entries, guessRows);

    assert.deepEqual(filtered, [entries[1]]);
  });
});
