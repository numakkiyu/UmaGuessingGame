import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage, LANGUAGES } from ".";
import { pedigreeCopy, pedigreeSlot, PedigreeNotice } from "./pedigree";
import { errorPlayerText, resolvePlayerText } from "./player-text";
import { characterName } from "./entities";
import { PedigreePracticeStartPage } from "@/features/practice/pedigree-practice-pages";
import { api } from "@/lib/api";
import { PEDIGREE_DISCLAIMER, PEDIGREE_SLOTS, changePedigree, pedigreeResult, type PedigreeSlotId } from "@/lib/domain/pedigree-practice";
import { createPedigreeGame, pedigreeStore, readPedigreePack, validatePedigreeArchive } from "@/lib/local/pedigree-practice-store";
import { sha256 } from "@/lib/local/checksum";
const language = (lang: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); vi.restoreAllMocks(); await changeLanguage("zh-Hans"); });
function content() { return { disclaimer: PEDIGREE_DISCLAIMER, slots: PEDIGREE_SLOTS, horses: Array.from({ length: 12 }, (_, i) => ({ horseId: `horse${i}`, name: `資料の馬${i}(USA)`, nkbId: null })), families: [{ subject: { characterId: "c1", characterName: "原名", localizedNames: { ja: "ウマ娘", "zh-Hant": "角色" }, prototypeName: "登録名", nkbId: "12345" }, relations: Object.fromEntries(PEDIGREE_SLOTS.map((s, i) => [s.slotId, `horse${i}`])) }] }; }
function pack(data: unknown = content()) {
  const contentJSON = JSON.stringify(data);
  return { schemaVersion: 1 as const, modeId: "pedigree" as const, rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", reviewStatus: "source-snapshot" as const, sourceUrls: ["https://wiki.biligame.com/umamusume/"], contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length };
}
function leaves(value: unknown, prefix = ""): Record<string, string> { if (typeof value === "string") return { [prefix]: value }; return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${prefix}.${key}`))); }
describe("historical pedigree localization", () => {
  it("keeps complete rules keys and placeholders aligned and distinguishes maternal from paternal ancestry", () => {
    const source = leaves(pedigreeCopy["zh-Hans"]);
    for (const lang of LANGUAGES) {
      const target = leaves(pedigreeCopy[lang]); expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());
      for (const [key, text] of Object.entries(source)) { expect(target[key]?.trim()).toBeTruthy(); const params = (s: string) => [...s.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort(); expect(params(target[key]!)).toEqual(params(text)); }
      expect(pedigreeSlot("sireDam", lang)).not.toEqual(pedigreeSlot("damSire", lang));
    }
    expect(pedigreeSlot("sireDam", "ja").means).toBe("父方の祖母"); expect(pedigreeSlot("damSire", "ja").means).toBe("母方の祖父");
  });
  it("keeps selected count and one pending preparation through language and error changes", async () => {
    vi.spyOn(pedigreeStore, "latestPack").mockResolvedValue(null); vi.spyOn(pedigreeStore, "list").mockResolvedValue([]);
    let reject!: (error: unknown) => void, signal!: AbortSignal;
    const fetch = vi.spyOn(api, "getPedigreePracticePack").mockImplementation(s => { signal = s!; return new Promise((_resolve, fail) => { reject = fail; }); });
    render(<MemoryRouter><PedigreePracticeStartPage /></MemoryRouter>);
    fireEvent.change(screen.getByLabelText("填写几处？"), { target: { value: "6" } }); fireEvent.click(await screen.findByRole("button", { name: "准备原型资料" }));
    await language("ja"); expect((screen.getByLabelText("何か所に挑戦する？") as HTMLSelectElement).value).toBe("6"); expect(screen.getByText(pedigreeCopy.ja.rules[0])).toBeTruthy();
    expect(signal.aborted).toBe(false); expect(fetch).toHaveBeenCalledTimes(1); expect(pedigreeStore.latestPack).toHaveBeenCalledTimes(1);
    await act(async () => { reject(new PedigreeNotice("unique")); }); await screen.findByText(pedigreeCopy.ja.notices.unique); await language("zh-Hant"); expect(screen.getByText(pedigreeCopy["zh-Hant"].notices.unique)).toBeTruthy();
  });
  it("keeps horse names canonical fact labels and last saved answers unchanged across locales", async () => {
    const p = pack(), { library } = readPedigreePack(p), family = library.families[0]!, drawn = createPedigreeGame(p, 6, 1000);
    const assignments = drawn.slotIds.map(slotId => ({ slotId, horseId: family.relations[slotId] }));
    const saved = changePedigree(drawn, assignments, true, 1001), current = changePedigree(saved, assignments.slice(1), false, 1002), before = JSON.stringify(current), factsBefore = JSON.stringify(library);
    for (const lang of LANGUAGES) {
      await language(lang);
      expect(characterName({ displayName: family.subject.characterName, localizedNames: family.subject.localizedNames })).toBe(lang === "ja" ? "ウマ娘" : lang === "zh-Hant" ? "角色" : "原名");
      expect(pedigreeResult(current, family).scoreUnits).toBe(10000);
      expect(validatePedigreeArchive({ schemaVersion: 1, pack: p, game: current }).game).toEqual(current);
      expect(JSON.stringify(current)).toBe(before); expect(JSON.stringify(readPedigreePack(p).library)).toBe(factsBefore);
      expect(JSON.parse(p.contentJSON).disclaimer).toBe(PEDIGREE_DISCLAIMER);
    }
    expect(pedigreeResult(changePedigree(current, current.draft, true, 1003), family).scoreUnits).toBe(8300);
    const duplicate = [{ slotId: "sire" as PedigreeSlotId, horseId: assignments[0]!.horseId }, { slotId: "dam" as PedigreeSlotId, horseId: assignments[0]!.horseId }]; expect(() => changePedigree(current, duplicate, true, 1004)).toThrow();
  });
  it("accepts old subject metadata rejects bad names and retranslates a saved error safely", async () => {
    const c = content(), { localizedNames: _names, ...subject } = c.families[0]!.subject;
    expect(readPedigreePack(pack({ ...c, families: [{ ...c.families[0], subject }] })).library.families[0]?.subject.localizedNames).toBeUndefined();
    expect(() => readPedigreePack(pack({ ...c, families: [{ ...c.families[0], subject: { ...subject, localizedNames: { ja: "" } } }] }))).toThrow();
    const error = errorPlayerText(new PedigreeNotice("empty"), "pedigree", "notices.stopped"); await language("ja"); expect(resolvePlayerText(error)).toBe(pedigreeCopy.ja.notices.empty);
    expect(resolvePlayerText(errorPlayerText(new Error("secret"), "pedigree", "notices.stopped"))).toBe(pedigreeCopy.ja.notices.stopped);
  });
});
