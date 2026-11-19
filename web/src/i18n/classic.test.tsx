import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import i18n, { changeLanguage, LANGUAGES } from ".";
import { classic } from "./classic";
import { localNotices } from "./local-notices";
import { characterName, formatDateTime, validLocalizedNames } from "./entities";
import { displayClassicValue } from "./classic-values";
import { errorPlayerText, resolvePlayerText, usePlayerText } from "./player-text";
import { LocalizedNotice } from "@/lib/player-notice";
import { SearchInput } from "@/components/uma/search-input";
import { GuessCard, GuessTable } from "@/features/classic/guess-card";
import { compareClassic, guessClassic, classicProgress, type PracticeCharacter, type ClassicGame, type PracticePack } from "@/lib/domain/classic-practice";
import { readPracticePack, packKey, CLASSIC_RULES } from "@/lib/local/practice-pack";
import { validatePracticeArchive } from "@/lib/local/practice-store";
import { sha256 } from "@/lib/local/checksum";
import vectors from "@/lib/domain/testdata/classic-comparison.json";

const characters: PracticeCharacter[] = [
  { ...vectors[0]!.answer, localizedNames: { "zh-Hans": "特别周", "zh-Hant": "特別週", ja: "スペシャルウィーク" } } as PracticeCharacter,
  { ...vectors[0]!.answer, characterId: "other", displayName: "另一个角色", localizedNames: { ja: "別のウマ娘" } } as PracticeCharacter,
];
function makePack(chars: unknown[]): PracticePack {
  const contentJSON = JSON.stringify({ characters: chars });
  return { schemaVersion: 1, modeId: "classic", rulesVersion: CLASSIC_RULES, datasetVersion: "test", contentPackVersion: "test", sourceUrls: ["https://wiki.biligame.com/umamusume/"], reviewStatus: "source-snapshot", contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length };
}
const pack = makePack(characters);
const game: ClassicGame = { schemaVersion: 1, modeId: "classic", gameId: "local-" + "a".repeat(32), packKey: packKey(pack), rulesVersion: CLASSIC_RULES, answerId: "a", wrongBudget: 4, guesses: [], ended: false, createdAt: 1000, updatedAt: 1000, revision: 0 };
const language = (lang: "zh-Hans" | "zh-Hant" | "ja") => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });
function entries(value: object, prefix = ""): Record<string, string> {
  return Object.fromEntries(Object.entries(value).flatMap(([key, item]) => typeof item === "string" ? [[`${prefix}${key}`, item]] : Object.entries(entries(item, `${prefix}${key}.`))));
}

describe("classic localization preserves the game", () => {
  it("provides the same nonempty keys and placeholders in all three locales", () => {
    for (const catalog of [classic, localNotices]) {
      const source = entries(catalog["zh-Hans"]);
      for (const lang of LANGUAGES) {
        const localized = entries(catalog[lang]);
        expect(Object.keys(localized).sort()).toEqual(Object.keys(source).sort());
        for (const [key, value] of Object.entries(localized)) {
          expect(value.trim()).not.toBe("");
          expect([...value.matchAll(/{{\s*(\w+)\s*}}/g)].map(m => m[1]).sort()).toEqual([...source[key]!.matchAll(/{{\s*(\w+)\s*}}/g)].map(m => m[1]).sort());
        }
      }
    }
  });

  it("localizes saved notice meaning and progress without restarting their operation", async () => {
    let operationCount = 0;
    function Notice() {
      const [message, setMessage, setError] = usePlayerText("classic");
      return <><button onClick={() => { operationCount++; setMessage("preparingAvatars", { done: 4, total: 10 }); }}>prepare</button><button onClick={() => setError(new LocalizedNotice("duplicate"), "guessFailed")}>duplicate</button><p>{message}</p></>;
    }
    render(<Notice />);
    fireEvent.click(screen.getByText("prepare"));
    await language("ja");
    expect(screen.getByText("アイコンを準備中 4 / 10")).toBeTruthy();
    expect(operationCount).toBe(1);
    fireEvent.click(screen.getByText("duplicate"));
    expect(screen.getByText(localNotices.ja.duplicate)).toBeTruthy();
    await language("zh-Hant");
    expect(screen.getByText(localNotices["zh-Hant"].duplicate)).toBeTruthy();
    const safe = errorPlayerText(new Error("SQL password ANSWER PRIVATE"), "classic", "guessFailed");
    expect(resolvePlayerText(safe)).toBe(classic["zh-Hant"].guessFailed);
  });

  it("changes names, enum presentation and date formatting without changing the question, grades or archive", async () => {
    const next = guessClassic(game, characters, "other", 1001);
    const archive = { schemaVersion: 1, game: next, pack };
    const snapshot = JSON.stringify(archive);
    const before = classicProgress(next, characters);
    for (const lang of LANGUAGES) {
      await language(lang);
      expect(classicProgress(next, characters)).toEqual(before);
      expect(validatePracticeArchive(archive).game).toEqual(next);
      expect(JSON.stringify(archive)).toBe(snapshot);
      expect(characterName(characters[0]!)).toBe(characters[0]!.localizedNames![lang]);
      const row = compareClassic(characters[0]!, characters[1]!, 1);
      expect(displayClassicValue({ field: "surface", value: "草地/泥地" })).toBe(`${classic[lang].values.turf} / ${classic[lang].values.dirt}`);
      expect(displayClassicValue({ field: "star", value: "3星" })).toBe(i18n.t("values.star", { ns: "classic", count: 3 }));
      expect(displayClassicValue({ field: "g1", value: "1-2J" })).toBe("1-2J");
      expect(row.cells).toEqual(before.rows[0]!.cells);
      expect(formatDateTime(1000)).not.toBe("—");
    }
    expect(formatDateTime("invalid")).toBe("—");
  });

  it("accepts legacy packs without regional names and rejects malformed optional metadata", () => {
    const legacy = characters.map(({ localizedNames: _names, ...c }) => c);
    expect(readPracticePack(makePack(legacy)).characters).toHaveLength(2);
    expect(characterName({ displayName: "旧资料" }, "ja")).toBe("旧资料");
    expect(validLocalizedNames(undefined)).toBe(true);
    for (const localizedNames of [null, [], { en: "English" }, { ja: "" }, { ja: 7 }, { ja: "a".repeat(101) }]) {
      expect(validLocalizedNames(localizedNames)).toBe(false);
      expect(() => readPracticePack(makePack([{ ...characters[0], localizedNames }, characters[1]]))).toThrow();
    }
  });

  it("keeps query and selected identity while candidate and feedback labels follow language", async () => {
    let selected: PracticeCharacter | null = null;
    render(<SearchInput items={characters} onSelect={c => { selected = c; }} />);
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "特別週" } });
    fireEvent.focus(input);
    expect(await screen.findByRole("option", { name: /特别周/ })).toBeTruthy();
    await language("ja");
    expect(input.value).toBe("特別週");
    const candidate = screen.getByRole("option", { name: /スペシャルウィーク/ });
    fireEvent.click(candidate);
    expect(selected).toBe(characters[0]);
    const row = compareClassic(characters[0]!, characters[0]!, 1);
    render(<><GuessCard row={row} latest /><GuessTable rows={[row]} /></>);
    expect(screen.getByRole("region", { name: classic.ja.feedback.table })).toBeTruthy();
    expect(screen.getAllByText("芝").length).toBe(2);
    await language("zh-Hant");
    expect(screen.getAllByText("特別週").length).toBe(2);
    expect(screen.getByRole("region", { name: classic["zh-Hant"].feedback.table })).toBeTruthy();
  });
});
