import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage, LANGUAGES } from ".";
import { aptitude } from "./aptitude";
import { aptitudeValues, aptitudeValue, taskConditions } from "./aptitude-values";
import { AptitudeNotice, aptitudeNotices } from "./aptitude-notices";
import { cardMatches, cardName } from "./entities";
import { errorPlayerText, resolvePlayerText } from "./player-text";
import { ActivityPracticeStart } from "@/features/practice/activity-practice-kit";
import { gridAdapter } from "@/features/practice/aptitude-adapters";
import { claimGrid, changeLineup, publishedPracticePrice, lineupScore, type AptitudeCard } from "@/lib/domain/aptitude-practice";
import { createGridGame, createDraftGame, readGridPack, readDraftPack, validateGridArchive, validateDraftArchive } from "@/lib/local/aptitude-practice-store";
import { sha256 } from "@/lib/local/checksum";
import { playerMessage } from "@/lib/player-notice";

const language = (lang: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); vi.restoreAllMocks(); await changeLanguage("zh-Hans"); });
const cards: AptitudeCard[] = ["短", "英", "中"].flatMap((distance, i) => ["逃", "先", "差"].map((style, j) => ({
  id: `c${i}${j}`, name: `原名${i}${j}`, localizedNames: { "zh-Hans": `简中${i}${j}`, "zh-Hant": `繁中${i}${j}`, ja: `ウマ娘${i}${j}` }, imagePath: "",
  aptitudes: { surface: { "草地": "A" }, distance: { [distance]: "A" }, style: { [style]: "A" } },
})));
function envelope<M extends "aptitude-grid" | "draft">(modeId: M, content: unknown) {
  const contentJSON = JSON.stringify(content);
  return { schemaVersion: 1 as const, modeId, rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "p", sourceUrls: ["https://wiki.biligame.com/umamusume/"], reviewStatus: "source-snapshot" as const, contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length };
}
function leaves(value: unknown, prefix = ""): Record<string, string> {
  if (typeof value === "string") return { [prefix]: value };
  return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${prefix}.${key}`)));
}

describe("aptitude practice localization boundaries", () => {
  it("provides matching keys, rule steps and placeholders in every locale", () => {
    for (const catalog of [aptitude, aptitudeValues, aptitudeNotices]) {
      const source = leaves(catalog["zh-Hans"]);
      for (const lang of LANGUAGES) {
        const target = leaves(catalog[lang]);
        expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());
        for (const [key, text] of Object.entries(source)) {
          expect(target[key]?.trim(), `${lang}${key}`).toBeTruthy();
          const params = (s: string) => [...s.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort();
          expect(params(target[key]!)).toEqual(params(text));
        }
      }
    }
  });

  it("changes rules during preparation without reloading, cancelling or replacing the adapter", async () => {
    const pack = envelope("aptitude-grid", { cards });
    vi.spyOn(gridAdapter.store, "latestPack").mockResolvedValue(null);
    vi.spyOn(gridAdapter.store, "list").mockResolvedValue([]);
    vi.spyOn(gridAdapter.store, "install").mockResolvedValue();
    let release!: (value: { data: typeof pack }) => void;
    let signal!: AbortSignal;
    const fetch = vi.spyOn(gridAdapter, "fetch").mockImplementation(s => { signal = s; return new Promise(resolve => { release = resolve; }); });
    render(<MemoryRouter><ActivityPracticeStart adapter={gridAdapter} /></MemoryRouter>);
    fireEvent.click(await screen.findByRole("button", { name: aptitude["zh-Hans"]["aptitude-grid"].prepareLabel }));
    const identity = gridAdapter;
    await language("ja");
    expect(gridAdapter).toBe(identity);
    expect(gridAdapter.rules).toEqual(aptitude.ja["aptitude-grid"].rules);
    expect(screen.getByRole("heading", { name: aptitude.ja["aptitude-grid"].title })).toBeTruthy();
    expect(signal.aborted).toBe(false);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(gridAdapter.store.latestPack).toHaveBeenCalledTimes(1);
    await act(async () => { release({ data: pack }); });
    await screen.findByRole("button", { name: aptitude.ja["aptitude-grid"].startLabel });
  });

  it("retranslates saved authored errors while hiding unexpected exception details", async () => {
    const mismatch = new AptitudeNotice("mismatch"), budget = new AptitudeNotice("budget");
    const saved = errorPlayerText(mismatch, "aptitude", "ui.empty");
    await language("ja");
    expect(resolvePlayerText(saved)).toBe(aptitudeNotices.ja.mismatch);
    expect(playerMessage(budget, "fallback")).toBe(aptitudeNotices.ja.budget);
    await language("zh-Hant");
    expect(resolvePlayerText(saved)).toBe(aptitudeNotices["zh-Hant"].mismatch);
    expect(playerMessage(new Error("private token secret"), "安全說明")).toBe("安全說明");
  });

  it("accepts optional source names and legacy cards but rejects unsupported metadata", () => {
    const result = readGridPack(envelope("aptitude-grid", { cards })).cards;
    expect(result[0]?.localizedNames).toEqual(cards[0]?.localizedNames);
    expect(cardName(result[0]!, "ja")).toBe("ウマ娘00");
    expect(result.filter(c => cardMatches(c, "　ウマ娘００ ")).map(c => c.id)).toEqual(["c00"]);
    const legacy = cards.map(({ localizedNames: _names, ...c }) => c);
    expect(cardName(readGridPack(envelope("aptitude-grid", { cards: legacy })).cards[0]!, "ja")).toBe("原名00");
    for (const invalid of [{ en: "name" }, { ja: " " }, { ja: 42 }]) {
      expect(() => readGridPack(envelope("aptitude-grid", { cards: [{ ...cards[0], localizedNames: invalid }, ...cards.slice(1)] }))).toThrow();
    }
  });

  it("leaves raw conditions, claims, drafts, prices, deadlines and scoring unchanged", async () => {
    const gridPack = envelope("aptitude-grid", { cards });
    const grid = claimGrid({ ...createGridGame(gridPack, 1000), rowAxis: "distance" }, cards, 0, "c00", 1001);
    const market = [...cards, ...cards.slice(0, 3).map(c => ({ ...c, id: `${c.id}extra` }))].map(c => ({ ...c, price: publishedPracticePrice(c.aptitudes) }));
    const tasks = Array.from({ length: 4 }, (_, i) => ({ taskId: `t${i}`, surface: "草地", distance: "短", style: "逃" }));
    const draftPack = envelope("draft", { cards: market, tasks });
    const draft = tasks.reduce((g, t, i) => changeLineup(g, tasks, t.taskId, market[i]!.id, 1001), createDraftGame(draftPack, 1000));
    const before = JSON.stringify({ grid, draft, cards, market, tasks });
    const score = lineupScore(draft.draft, market, tasks);
    for (const lang of LANGUAGES) {
      await language(lang);
      expect(taskConditions(tasks[0]!)).toBe(lang === "ja" ? "芝 · 短距離 · 逃げ" : lang === "zh-Hant" ? "草地 · 短 · 逃" : "草地 · 短 · 逃");
      expect(validateGridArchive({ schemaVersion: 1, game: grid, pack: gridPack }).game).toEqual(grid);
      expect(validateDraftArchive({ schemaVersion: 1, game: draft, pack: draftPack }).game).toEqual(draft);
      expect(readDraftPack(draftPack).cards).toEqual(market);
      expect(lineupScore(draft.draft, market, tasks)).toEqual(score);
      expect(JSON.stringify({ grid, draft, cards, market, tasks })).toBe(before);
      expect(aptitudeValue("style", "constructor")).toBe("—");
    }
  });
});
