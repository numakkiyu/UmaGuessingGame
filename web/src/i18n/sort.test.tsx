import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { changeLanguage, LANGUAGES } from ".";
import { sortCopy, SortNotice, sortDisplay } from "./sort";
import { characterName } from "./entities";
import { errorPlayerText, resolvePlayerText } from "./player-text";
import { SortBoard } from "@/features/modes/sort-board";
import { changeSort, sortCards, sortResult, type SortProfile } from "@/lib/domain/sort-practice";
import { createSortGame, readSortPack, validateSortArchive } from "@/lib/local/sort-practice-store";
import { sha256 } from "@/lib/local/checksum";

const language = (lang: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });
const profiles: SortProfile[] = Array.from({ length: 6 }, (_, i) => ({ characterId: `c${i}`, displayName: `原名${i}`, localizedNames: { ja: `ウマ娘${i}`, "zh-Hant": `角色${i}` }, imagePath: "", values: { height: 150 + Math.floor(i / 2), birthday: 101 + i } }));
function pack(cards: unknown = profiles) {
  const contentJSON = JSON.stringify({ cards });
  return { schemaVersion: 1 as const, modeId: "sort" as const, rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", reviewStatus: "source-snapshot" as const, sourceUrls: ["https://wiki.biligame.com/umamusume/"], contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length };
}
function leaves(value: unknown, prefix = ""): Record<string, string> {
  if (typeof value === "string") return { [prefix]: value };
  return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${prefix}.${key}`)));
}
describe("sort localization and stable ordering", () => {
  it("aligns all three rule sets and placeholders", () => {
    const source = leaves(sortCopy["zh-Hans"]);
    for (const lang of LANGUAGES) {
      const target = leaves(sortCopy[lang]);
      expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());
      for (const [key, text] of Object.entries(source)) {
        expect(target[key]?.trim()).toBeTruthy();
        const params = (s: string) => [...s.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort();
        expect(params(target[key]!)).toEqual(params(text));
      }
    }
  });
  it("keeps skipped ranks and the same controls while localizing card names and labels", async () => {
    const up = vi.fn(), split = vi.fn();
    const game = createSortGame(pack(), "height", false, 1000);
    render(<SortBoard draft={[["c0", "c1"], ["c2"], ["c3"], ["c4"], ["c5"]]} cards={sortCards(game, profiles)} locked={false} onChange={() => {}} onMoveUp={up} onMoveDown={() => {}} onMerge={() => {}} onSplit={split} />);
    await language("ja");
    expect(screen.getByText("ウマ娘0")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "「3 位」を上へ" }));
    fireEvent.click(screen.getByRole("button", { name: "「同率 1 位」を別々の順位に分ける" }));
    expect(up).toHaveBeenCalledWith(1);
    expect(split).toHaveBeenCalledWith(0);
    expect(screen.getByText("同率 1 位：2 枚を同順位にしています")).toBeTruthy();
    await language("zh-Hant");
    expect(screen.getByRole("button", { name: "將「第 3 名」上移" })).toBeTruthy();
    expect(screen.queryByText(/排序數值/)).toBeNull();
  });
  it("localizes birthdays without changing numeric ordering, ties or the last submitted answer", async () => {
    const p = pack(), game = createSortGame(p, "birthday", true, 1000);
    const saved = changeSort(game, sortResult(game, profiles).groups, "submit", 2000);
    const current = changeSort(saved, [...saved.draft].reverse(), "draft", 2001);
    const before = JSON.stringify(current);
    for (const lang of LANGUAGES) {
      await language(lang);
      const cards = sortCards(current, profiles);
      expect(cards.find(c => c.characterId === "c0")?.display).toBe(lang === "ja" ? "1月1日" : "1 月 1 日");
      expect(cards.find(c => c.characterId === "c0")?.raw).toBe(101);
      expect(sortCards({ ...current, openBook: false }, profiles).every(c => c.display === null && c.raw === null)).toBe(true);
      expect(sortResult(current, profiles).scoreUnits).toBe(10000);
      expect(JSON.stringify(validateSortArchive({ schemaVersion: 1, pack: p, game: current }).game)).toBe(before);
      expect(sortDisplay("height", 165)).toBe("165 cm");
    }
  });
  it("supports old archives and safely re-localizes feature failures", async () => {
    const old = profiles.map(({ localizedNames: _names, ...p }) => p);
    expect(characterName(readSortPack(pack(old)).profiles[0]!, "ja")).toBe("原名0");
    expect(readSortPack(pack()).profiles[0]?.localizedNames?.ja).toBe("ウマ娘0");
    expect(() => readSortPack(pack([{ ...profiles[0], localizedNames: { en: "name" } }, ...profiles.slice(1)]))).toThrow();
    const notice = errorPlayerText(new SortNotice("groups"), "sort", "flow.actionFailed");
    await language("ja");
    expect(resolvePlayerText(notice)).toBe(sortCopy.ja.notices.groups);
    await language("zh-Hant");
    expect(resolvePlayerText(notice)).toBe(sortCopy["zh-Hant"].notices.groups);
  });
});
