import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import i18n, { changeLanguage, LANGUAGES } from ".";
import { pathCopy, PathNotice } from "./path";
import { errorPlayerText, resolvePlayerText } from "./player-text";
import { cardMatches, cardName } from "./entities";
import { playerMessage } from "@/lib/player-notice";
import { PathPracticeStartPage } from "@/features/practice/path-practice-pages";
import { api } from "@/lib/api";
import { changePath, checkRoute, pathResult, shortestRoute, type PracticeGraph } from "@/lib/domain/path-practice";
import { createPathGame, pathStore, readPathPack, validatePathArchive } from "@/lib/local/path-practice-store";
import { sha256 } from "@/lib/local/checksum";
const language = (lang: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); vi.restoreAllMocks(); await changeLanguage("zh-Hans"); });
const graph: PracticeGraph = { nodes: ["a", "b", "c", "d"].map(id => ({ characterId: id, name: `原名${id}`, localizedNames: { ja: `ウマ娘${id}`, "zh-Hant": `角色${id}` }, imagePath: `/assets/avatars/${id}.webp` })), edges: [["a", "b"], ["b", "d"], ["a", "c"], ["c", "b"]].map(([from, to], i) => ({ from: from!, to: to!, raceId: `r${i}`, raceName: "有马纪念", date: "1989-12-24" })) };
function pack(g: unknown = graph) {
  const contentJSON = JSON.stringify(g);
  return { schemaVersion: 1 as const, modeId: "co-race-path" as const, rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", reviewStatus: "source-snapshot" as const, sourceUrls: ["https://wiki.biligame.com/umamusume/"], contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length };
}
function leaves(value: unknown, prefix = ""): Record<string, string> {
  if (typeof value === "string") return { [prefix]: value };
  return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${prefix}.${key}`)));
}
describe("co-appearance practice localization", () => {
  it("keeps all rule steps keys and interpolation values aligned", () => {
    const source = leaves(pathCopy["zh-Hans"]);
    for (const lang of LANGUAGES) {
      const target = leaves(pathCopy[lang]);
      expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());
      for (const [key, text] of Object.entries(source)) {
        expect(target[key]?.trim()).toBeTruthy();
        const params = (s: string) => [...s.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort();
        expect(params(target[key]!)).toEqual(params(text));
      }
    }
  });
  it("changes challenge rules and a visible error without resetting pending preparation", async () => {
    vi.spyOn(pathStore, "latestPack").mockResolvedValue(null); vi.spyOn(pathStore, "list").mockResolvedValue([]);
    let reject!: (error: unknown) => void, signal!: AbortSignal;
    const fetch = vi.spyOn(api, "getPathPracticePack").mockImplementation(s => { signal = s!; return new Promise((_resolve, fail) => { reject = fail; }); });
    render(<MemoryRouter><PathPracticeStartPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("radio", { name: "闭卷 · 凭记忆连线" }));
    fireEvent.click(await screen.findByRole("button", { name: "准备同场资料" }));
    await language("ja");
    expect((screen.getByRole("radio", { name: "記憶を頼りにつなぐ" }) as HTMLInputElement).checked).toBe(true);
    expect(screen.getByText(pathCopy.ja.rules[4])).toBeTruthy();
    expect(signal.aborted).toBe(false); expect(fetch).toHaveBeenCalledTimes(1); expect(pathStore.latestPack).toHaveBeenCalledTimes(1);
    await act(async () => { reject(new PathNotice("unavailable")); });
    await screen.findByText(pathCopy.ja.notices.unavailable); await language("zh-Hant");
    expect(screen.getByText(pathCopy["zh-Hant"].notices.unavailable)).toBeTruthy();
  });
  it("retranslates an existing missing-edge error with its original step without leaking the route", async () => {
    const issue = checkRoute(graph, ["a", "b", "c", "d"], "a")!;
    expect(issue.step).toBe(3);
    const message = errorPlayerText(issue.notice, "path", "notices.stopped");
    for (const lang of LANGUAGES) {
      await language(lang);
      const expected = i18n.t("notices.missing", { ns: "path", step: 3 });
      expect(resolvePlayerText(message)).toBe(expected); expect(playerMessage(issue.notice, "fallback")).toBe(expected);
      expect(resolvePlayerText(message)).not.toMatch(/原名|ウマ娘[a-d]|a.*b.*d|\{\{/);
      expect(() => changePath({ ...createPathGame(pack(), "closed", 1000), start: "a", goal: "d" }, graph, ["a", "b", "c", "d"], true, 1001)).toThrow(expected);
    }
  });
  it("keeps endpoints graph scores draft and last saved route stable while searching source-backed names", async () => {
    const p = pack(), loaded = readPathPack(p).graph;
    const g = { ...createPathGame(p, "closed", 1000), start: "a", goal: "d", draft: ["a"] };
    const saved = changePath(g, loaded, ["a", "b", "d"], true, 1001), current = changePath(saved, loaded, ["a", "c", "b", "d"], false, 1002), before = JSON.stringify(current);
    for (const lang of LANGUAGES) {
      await language(lang);
      expect(cardName(loaded.nodes[0]!)).toBe(lang === "ja" ? "ウマ娘a" : lang === "zh-Hant" ? "角色a" : "原名a");
      expect(cardMatches(loaded.nodes[0]!, "ウマ娘ａ")).toBe(true);
      expect(shortestRoute(loaded, "a", "d")).toEqual(["a", "b", "d"]);
      expect(pathResult(current, loaded).scoreUnits).toBe(10000);
      expect(validatePathArchive({ schemaVersion: 1, pack: p, game: current }).game).toEqual(current);
      expect(JSON.stringify(current)).toBe(before);
    }
    expect(pathResult(changePath(current, loaded, current.draft, true, 1003), loaded).scoreUnits).toBe(9000);
    const old = { ...graph, nodes: graph.nodes.map(({ localizedNames: _names, ...n }) => n) };
    expect(readPathPack(pack(old)).graph.nodes[0]?.localizedNames).toBeUndefined();
    expect(() => readPathPack(pack({ ...graph, nodes: [{ ...graph.nodes[0], localizedNames: { en: "unknown" } }, ...graph.nodes.slice(1)] }))).toThrow();
  });
});
