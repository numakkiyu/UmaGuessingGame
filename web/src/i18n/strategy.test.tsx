import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage, LANGUAGES } from ".";
import { strategyCopy, strategyAction, StrategyNotice } from "./strategy";
import { errorPlayerText, resolvePlayerText } from "./player-text";
import { StrategyPracticeStartPage } from "@/features/practice/strategy-practice-pages";
import { RaceTrack } from "@/features/modes/race-track";
import { api } from "@/lib/api";
import baseline from "../../../data/manual/rules-track.json";
import { confirmStrategy, selectStrategy, strategyPoint, strategyResult, strategyTargets, strategyTurnDeadline, syncStrategyTimeouts } from "@/lib/domain/strategy-practice";
import { createStrategyGame, strategyStore, readStrategyPack, validateStrategyArchive } from "@/lib/local/strategy-practice-store";
import { sha256 } from "@/lib/local/checksum";
const language = (lang: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); vi.restoreAllMocks(); await changeLanguage("zh-Hans"); });
const contentJSON = JSON.stringify({ track: baseline.track, simulation: true, notOfficialData: true });
const pack = { schemaVersion: 1 as const, modeId: "strategy-race" as const, rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", reviewStatus: "source-snapshot" as const, sourceUrls: ["https://wiki.biligame.com/umamusume/"], contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length };
function leaves(value: unknown, prefix = ""): Record<string, string> { if (typeof value === "string") return { [prefix]: value }; return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${prefix}.${key}`))); }
describe("strategy race localization", () => {
  it("keeps all action rules keys and placeholders aligned", () => {
    const source = leaves(strategyCopy["zh-Hans"]);
    for (const lang of LANGUAGES) {
      const target = leaves(strategyCopy[lang]); expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());
      for (const [key, text] of Object.entries(source)) { expect(target[key]?.trim()).toBeTruthy(); const params = (s: string) => [...s.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort(); expect(params(target[key]!)).toEqual(params(text)); }
    }
  });
  it("rephrases pending preparation and existing errors without retrying or cancelling", async () => {
    vi.spyOn(strategyStore, "latestPack").mockResolvedValue(null); vi.spyOn(strategyStore, "list").mockResolvedValue([]);
    let reject!: (error: unknown) => void, signal!: AbortSignal;
    const fetch = vi.spyOn(api, "getStrategyPracticePack").mockImplementation(s => { signal = s!; return new Promise((_resolve, fail) => { reject = fail; }); });
    render(<MemoryRouter><StrategyPracticeStartPage /></MemoryRouter>);
    fireEvent.click(await screen.findByRole("button", { name: "准备赛道规则" })); await language("ja");
    expect(screen.getByText(strategyCopy.ja.rules[0])).toBeTruthy(); expect(signal.aborted).toBe(false); expect(fetch).toHaveBeenCalledTimes(1); expect(strategyStore.latestPack).toHaveBeenCalledTimes(1);
    await act(async () => { reject(new StrategyNotice("stamina", { minimum: 2 })); }); await screen.findByText("スパートには体力が 2 以上必要です。別の行動を選んでください"); await language("zh-Hant"); expect(screen.getByText("衝刺至少需要 2 點體力，請換個行動")).toBeTruthy();
  });
  it("preserves selected action original turn deadlines timeout defaults and achievable targets", async () => {
    const { track } = readStrategyPack(pack), selected = selectStrategy(createStrategyGame(pack, 1000), track, "sprint", 2000), before = JSON.stringify(selected), target = strategyTargets(track)[0]!;
    for (const lang of LANGUAGES) {
      await language(lang); expect(strategyAction("sprint")).toBe(strategyCopy[lang].actions.sprint); expect(strategyAction("constructor")).toBe(strategyCopy[lang].action);
      expect(validateStrategyArchive({ schemaVersion: 1, pack, game: selected }).game).toEqual(selected);
      expect(JSON.stringify(selected)).toBe(before); expect(strategyTurnDeadline(selected, track)).toBe(21000); expect(strategyPoint(track, selected.actions)).toEqual({ position: 0, stamina: 6 });
      const elapsed = syncStrategyTimeouts(selected, track, 61000); expect(elapsed.actions).toEqual([21000, 41000, 61000].map(at => ({ action: "steady", at, automatic: true }))); expect(elapsed.selected).toBeNull();
      const completed = target.actions.reduce((g, action, i) => confirmStrategy(selectStrategy(g, track, action, 1001 + i), track, 1001 + i), { ...createStrategyGame(pack, 1000), targetPosition: target.position, targetStamina: target.stamina }); expect(strategyResult(completed, track).achieved).toBe(true);
    }
    const error = errorPlayerText(new StrategyNotice("select"), "strategy", "notices.stopped"); await language("ja"); expect(resolvePlayerText(error)).toBe(strategyCopy.ja.notices.select);
  });
  it("translates track descriptions without moving lanes or rewriting a player's team name", async () => {
    const teams = [{ subjectId: "b", label: "玩家のチーム甲", position: 9, stamina: 4, isSelf: true, avatarSrc: null }, { subjectId: "a", label: "同名のチーム", position: 12, stamina: 6, isSelf: false, avatarSrc: null }];
    const { container } = render(<RaceTrack teams={teams} totalTurns={8} staminaCap={8} maxGainPerTurn={5} />);
    const positions = () => [...container.querySelectorAll("[data-position]")].map(e => e.getAttribute("data-position"));
    expect(positions()).toEqual(["12", "9"]); await language("ja");
    expect(screen.getByRole("region", { name: "コース：全 8 ターン、目盛りの上限 40 マス" })).toBeTruthy();
    expect(screen.getByText("同名のチーム 位置 12、体力 6；玩家のチーム甲（あなた）位置 9、体力 4", { exact: true })).toBeTruthy();
    expect(positions()).toEqual(["12", "9"]); expect(teams[0]?.label).toBe("玩家のチーム甲");
  });
});
