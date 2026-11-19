import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage, LANGUAGES } from ".";
import { raceCopy, RaceNotice } from "./race";
import { raceCourse, raceDate, raceName } from "./race-names";
import { errorPlayerText, resolvePlayerText } from "./player-text";
import { RacePracticeStartPage } from "@/features/practice/race-practice-pages";
import { api } from "@/lib/api";
import { changeRace, raceCards, raceClues, raceResult, type HistoricalRace } from "@/lib/domain/race-practice";
import { createRaceGame, raceStore, readRacePack, validateRaceArchive } from "@/lib/local/race-practice-store";
import { sha256 } from "@/lib/local/checksum";

const language = (lang: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); vi.restoreAllMocks(); await changeLanguage("zh-Hans"); });
const race: HistoricalRace = { raceId: "r1", name: "有马纪念", date: "1989-12-24", course: "中山", surface: "草地", distance: "2500m", excludedCount: 1, entries: [1, 3, 3, 7].map((finishRank, i) => ({ characterId: `c${i}`, name: `原名${i}`, localizedNames: { ja: `ウマ娘${i}`, "zh-Hant": `角色${i}` }, finishRank, imagePath: `/assets/avatars/c${i}.webp` })) };
function pack(races: unknown = [race]) {
  const contentJSON = JSON.stringify({ races });
  return { schemaVersion: 1 as const, modeId: "race-rebuild" as const, rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", reviewStatus: "source-snapshot" as const, sourceUrls: ["https://wiki.biligame.com/umamusume/"], contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length };
}
function leaves(value: unknown, prefix = ""): Record<string, string> {
  if (typeof value === "string") return { [prefix]: value };
  return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${prefix}.${key}`)));
}
describe("historical race localization", () => {
  it("keeps rule steps, keys and placeholders aligned", () => {
    const source = leaves(raceCopy["zh-Hans"]);
    for (const lang of LANGUAGES) {
      const target = leaves(raceCopy[lang]);
      expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());
      for (const [key, text] of Object.entries(source)) {
        expect(target[key]?.trim()).toBeTruthy();
        const params = (s: string) => [...s.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort();
        expect(params(target[key]!)).toEqual(params(text));
      }
    }
  });
  it("keeps the selected challenge and pending preparation while changing rules and errors", async () => {
    vi.spyOn(raceStore, "latestPack").mockResolvedValue(null);
    vi.spyOn(raceStore, "list").mockResolvedValue([]);
    let reject!: (error: unknown) => void, signal!: AbortSignal;
    const fetch = vi.spyOn(api, "getRacePracticePack").mockImplementation(s => { signal = s!; return new Promise((_resolve, fail) => { reject = fail; }); });
    render(<MemoryRouter><RacePracticeStartPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("radio", { name: "线索 · 找出先后" }));
    fireEvent.click(await screen.findByRole("button", { name: "准备赛事资料" }));
    await language("ja");
    expect((screen.getByRole("radio", { name: "ヒントから順番を考える" }) as HTMLInputElement).checked).toBe(true);
    expect(screen.getByRole("heading", { name: raceCopy.ja.title })).toBeTruthy();
    expect(screen.getByText(raceCopy.ja.rules[3])).toBeTruthy();
    expect(signal.aborted).toBe(false);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(raceStore.latestPack).toHaveBeenCalledTimes(1);
    await act(async () => { reject(new RaceNotice("stopped")); });
    await screen.findByText(raceCopy.ja.notices.stopped);
    await language("zh-Hant");
    expect(screen.getByText(raceCopy["zh-Hant"].notices.stopped)).toBeTruthy();
  });
  it("uses source names and date-only formatting without altering facts or missing ranks", async () => {
    const p = pack(), loaded = readRacePack(p).races[0]!, g = createRaceGame(p, "clues", 1000);
    const saved = changeRace(g, [["c0"], ["c1", "c2"], ["c3"]], true, 1001);
    const current = changeRace(saved, [...saved.draft].reverse(), false, 1002), before = JSON.stringify(current);
    for (const lang of LANGUAGES) {
      await language(lang);
      const clues = raceClues(loaded), result = raceResult(current, loaded);
      expect(result.scoreUnits).toBe(10000);
      expect(result.ranked.map(e => e.finishRank)).toEqual([1, 3, 3, 7]);
      expect(raceCards(current, loaded).every(c => c.display === null && c.raw === null)).toBe(true);
      expect(validateRaceArchive({ schemaVersion: 1, game: current, pack: p }).game).toEqual(current);
      expect(JSON.stringify(current)).toBe(before);
      expect(clues[1]).toBe(lang === "ja" ? "ウマ娘1とウマ娘2は同着です" : lang === "zh-Hant" ? "角色1和角色2同名次完賽" : "原名1与原名2同名次完赛");
    }
    expect(raceName("有马纪念", "ja")).toBe("有馬記念");
    expect(raceName("英里冠军杯", "zh-Hant")).toBe("一哩冠軍賽");
    expect(raceCourse("东京", "ja")).toBe("東京");
    expect(raceName("未知赛事", "ja")).toBe("未知赛事");
    expect(raceName("constructor", "ja")).toBe("constructor");
    expect(raceDate("1989-10-29", "ja")).toBe("1989年10月29日");
  });
  it("accepts legacy names and refuses malformed metadata without leaking exception text", async () => {
    const old = { ...race, entries: race.entries.map(({ localizedNames: _names, ...e }) => e) };
    expect(readRacePack(pack([old])).races[0]?.entries[0]?.localizedNames).toBeUndefined();
    expect(() => readRacePack(pack([{ ...race, entries: [{ ...race.entries[0], localizedNames: { ja: "" } }, ...race.entries.slice(1)] }]))).toThrow();
    const saved = errorPlayerText(new RaceNotice("groups"), "race", "result");
    await language("ja");
    expect(resolvePlayerText(saved)).toBe(raceCopy.ja.notices.groups);
    expect(resolvePlayerText(errorPlayerText(new Error("secret"), "race", "notices.stopped"))).toBe(raceCopy.ja.notices.stopped);
  });
});

it("distinguishes Japanese and Hong Kong queen races by reviewed identity and date", () => {
  const japan={raceId:"200708050411",date:"2007-11-11"};
  const hongKong={raceId:"2021H1042508",date:"2021-04-25"};
  expect(raceName("伊丽莎白二世女皇杯","ja",japan)).toBe("エリザベス女王杯");
  expect(raceName("伊丽莎白二世女王杯","ja",hongKong)).toBe("クイーンエリザベス2世カップ");
  expect(raceName("伊丽莎白二世女王杯","zh-Hant",hongKong)).toBe("富衛保險女皇盃");
  expect(raceName("伊丽莎白二世女皇杯","ja",hongKong)).toBe("伊丽莎白二世女皇杯");
  expect(raceName("伊丽莎白二世女王杯","ja",japan)).toBe("伊丽莎白二世女王杯");
  expect(raceName("伊丽莎白二世女皇杯","ja",{...japan,date:"2006-11-12"})).toBe("伊丽莎白二世女皇杯");
  expect(raceName("伊丽莎白二世女皇杯","ja")).toBe("伊丽莎白二世女皇杯");
});
it("retains historic names and age labels instead of applying present-day replacements", () => {
  expect(raceName("高松宫杯","ja",{raceId:"199607020211",date:"1996-05-19"})).toBe("高松宮杯");
  expect(raceName("日本泥地杯","ja",{raceId:"201209050211",date:"2012-12-02"})).toBe("ジャパンカップダート");
  expect(raceName("每日杯3岁锦标","ja",{raceId:"199608040511",date:"1996-10-19"})).toBe("デイリー杯3歳ステークス");
  expect(raceName("每日杯","ja")).toBe("毎日杯");
  expect(raceName("新潟3岁锦标","ja",{raceId:"199606040811",date:"1996-09-01"})).toBe("新潟3歳ステークス");
  expect(raceName("高松宫杯","ja",{raceId:"202607020211",date:"2026-05-19"})).toBe("高松宫杯");
  expect(raceName("constructor","ja",{raceId:"constructor",date:"2000-01-01"})).toBe("constructor");
});
it("keeps invalid date-only source values instead of silently normalizing another date",()=>{
  for(const invalid of ["2023-02-29","2024-02-31","2026-04-31","2024-2-9","2024-01-01T12:00:00Z","unknown"]){expect(raceDate(invalid,"ja")).toBe(invalid);}
  expect(raceDate("2024-02-29","zh-Hant")).toBe("2024年2月29日");
});
