import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage, LANGUAGES } from ".";
import { memoryCopy, MemoryNotice } from "./memory";
import { errorPlayerText, resolvePlayerText } from "./player-text";
import { MemoryCardButton } from "@/features/modes/memory-card";
import { MemoryPracticeStartPage } from "@/features/practice/memory-practice-start-page";
import { api } from "@/lib/api";
import { memoryStore, createMemoryGame, validateMemoryArchive, type MemoryPack } from "@/lib/local/memory-practice-store";
import { flipMemory, memoryState, VIEW_MS, ANSWER_MS, type MemoryGame } from "@/lib/domain/memory-practice";
import { sha256 } from "@/lib/local/checksum";

const language = (lang: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); vi.restoreAllMocks(); await changeLanguage("zh-Hans"); });
function leaves(value: unknown, prefix = ""): Record<string, string> {
  if (typeof value === "string") return { [prefix]: value };
  return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${prefix}.${key}`)));
}
function pack(): MemoryPack {
  const contentJSON = JSON.stringify({ faces: Array.from({ length: 8 }, (_, i) => ({ pairId: `f${i}`, imagePath: `/assets/faces/f${i}.webp` })) });
  return { schemaVersion: 1, modeId: "memory", rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", reviewStatus: "source-snapshot", sourceUrls: ["https://wiki.biligame.com/umamusume/"], contentJSON, sha256: sha256(contentJSON), byteLength: contentJSON.length };
}
describe("memory localization without changing the game", () => {
  it("aligns every rule, key and placeholder", () => {
    const source = leaves(memoryCopy["zh-Hans"]);
    for (const lang of LANGUAGES) {
      const target = leaves(memoryCopy[lang]);
      expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());
      for (const [key, text] of Object.entries(source)) {
        expect(target[key]?.trim()).toBeTruthy();
        const params = (s: string) => [...s.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort();
        expect(params(target[key]!)).toEqual(params(text));
      }
    }
  });
  it("updates card accessibility labels without exposing identity or hidden images", async () => {
    const flip = vi.fn();
    render(<MemoryCardButton index={5} card={{ cardId: "opaque-card", position: 5, state: "hidden", imagePath: null, label: "private answer" }} disabled={false} onFlip={flip} />);
    expect(screen.getByRole("button", { name: "第 2 行第 2 列，未翻开" })).toBeTruthy();
    await language("ja");
    fireEvent.click(screen.getByRole("button", { name: "2 行 2 列、裏向き" }));
    expect(flip).toHaveBeenCalledWith("opaque-card");
    expect(document.querySelectorAll("img")).toHaveLength(0);
    expect(document.body.textContent).not.toContain("private answer");
    await language("zh-Hant");
    expect(screen.getByRole("button", { name: "第 2 橫列、第 2 直欄，尚未翻開" })).toBeTruthy();
  });
  it("keeps ongoing preparation and an already-displayed failure through language changes", async () => {
    vi.spyOn(memoryStore, "latestPack").mockResolvedValue(null);
    vi.spyOn(memoryStore, "list").mockResolvedValue([]);
    let reject!: (error: unknown) => void, signal!: AbortSignal;
    const fetch = vi.spyOn(api, "getMemoryPracticePack").mockImplementation(s => { signal = s!; return new Promise((_resolve, fail) => { reject = fail; }); });
    render(<MemoryRouter><MemoryPracticeStartPage /></MemoryRouter>);
    fireEvent.click(await screen.findByRole("button", { name: memoryCopy["zh-Hans"].prepare }));
    await language("ja");
    expect(screen.getByText(memoryCopy.ja.preparing)).toBeTruthy();
    expect(signal.aborted).toBe(false);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(memoryStore.latestPack).toHaveBeenCalledTimes(1);
    await act(async () => { reject(new MemoryNotice("incomplete")); });
    await screen.findByText(memoryCopy.ja.notices.incomplete);
    await language("zh-Hant");
    expect(screen.getByText(memoryCopy["zh-Hant"].notices.incomplete)).toBeTruthy();
    const fallback = errorPlayerText(new Error("secret"), "memory", "prepareFailed");
    expect(resolvePlayerText(fallback)).toBe(memoryCopy["zh-Hant"].prepareFailed);
  });
  it("preserves a visible first card, matched pair, original windows and score on recovery", async () => {
    const p = pack();
    let game: MemoryGame = { ...createMemoryGame(p, 1000), startedAt: 2000, updatedAt: 2000, revision: 1 };
    game = flipMemory(game, 0, 2000 + VIEW_MS);
    const first = JSON.stringify(game), pair = game.board.findIndex((id, i) => i !== 0 && id === game.board[0]);
    for (const lang of LANGUAGES) {
      await language(lang);
      const restored = validateMemoryArchive({ schemaVersion: 1, game, pack: p }).game;
      expect(JSON.stringify(restored)).toBe(first);
      expect(memoryState(restored, 2000 + VIEW_MS + 1).visible).toEqual(new Set([0]));
      expect(memoryState(restored, 2000 + VIEW_MS + 1).deadlineAt).toBe(2000 + VIEW_MS + ANSWER_MS);
      const matched = memoryState(flipMemory(restored, pair, 2000 + VIEW_MS + 2), 2000 + VIEW_MS + 3);
      expect(matched.matched.size).toBe(2);
      expect(matched.attempts).toBe(1);
      expect(matched.scoreUnits).toBe(1200);
    }
  });
});
