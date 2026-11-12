import { describe, expect, it } from "vitest";
import shared from "@/lib/domain/testdata/race-relations.json";
import { changeRace, raceCards, raceClues, raceResult, scoreRace, validRaceGroups, type HistoricalRace } from "@/lib/domain/race-practice";
import { createRaceGame, readRacePack, validateRaceArchive, type RacePack } from "./race-practice-store";
import { sha256 } from "./checksum";
const race = (): HistoricalRace => ({ raceId: "r1", name: "测试锦标", date: "2026-01-02", course: "阪神", surface: "草地", distance: "2200m", excludedCount: 1, entries: [1, 3, 3, 7, 9, 11, 13].map((rank, i) => ({ characterId: `c${i}`, name: `角色${i}`, imagePath: `/assets/avatars/c${i}.webp`, finishRank: rank })) });
function pack(races = [race()]): RacePack { const contentJSON = JSON.stringify({ races }); return { schemaVersion: 1, modeId: "race-rebuild", rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", sourceUrls: ["https://wiki.biligame.com/umamusume/"], reviewStatus: "source-snapshot", contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length }; }
describe("shared Go/browser historical race relations", () => {
  for (const f of shared) it(f.name, () => { const entries = Object.entries(f.truth).map(([characterId, finishRank]) => ({ characterId, finishRank: finishRank! })); expect(scoreRace(f.groups, entries)).toEqual({ correct: f.correct, total: f.total, scoreUnits: f.scoreUnits }); });
});
describe("historical practice progression and recovery", () => {
  it("draws exactly one race, keeps original identities/date and exposes no ranks on cards", () => {
    const g = createRaceGame(pack([race(), { ...race(), raceId: "r2", date: "2025-01-02" }]), "closed", 1000);
    expect(g.cardIds).toHaveLength(7); expect(new Set(g.cardIds).size).toBe(7); expect(g.deadlineAt).toBe(181000); expect(g.submitted).toBeNull();
    expect(raceCards(g, race()).every(c => c.raw === null && c.display === null)).toBe(true);
  });
  it("supports seven runners, true ties and source gaps without renumbering", () => {
    const g = createRaceGame(pack(), "clues", 1000), groups = [["c0"], ["c1", "c2"], ["c3"], ["c4"], ["c5"], ["c6"]];
    expect(validRaceGroups(groups, g.cardIds)).toBe(true); const next = changeRace(g, groups, true, 1001), result = raceResult(next, race());
    expect(result.scoreUnits).toBe(10000); expect(result.ranked.map(e => e.finishRank)).toEqual([1, 3, 3, 7, 9, 11, 13]);
    expect(validateRaceArchive({ schemaVersion: 1, pack: pack(), game: next }).game).toEqual(next);
  });
  it("drafts never auto-submit and last saved revision is scored at cutoff", () => {
    let g = createRaceGame(pack(), "closed", 1000); const answer = [["c0"], ["c1", "c2"], ["c3"], ["c4"], ["c5"], ["c6"]];
    g = changeRace(g, answer, false, 1001); expect(raceResult(g, race()).scoreUnits).toBe(0); expect(g.submissions).toBe(0);
    g = changeRace(g, answer, true, 1002); const saved = structuredClone(g.submitted);
    g = changeRace(g, [...answer].reverse(), false, 1003); expect(g.submitted).toEqual(saved); expect(raceResult(g, race()).scoreUnits).toBe(10000);
    g = changeRace(g, g.draft, true, 1004); expect(g.submissions).toBe(2); expect(raceResult(g, race()).scoreUnits).toBeLessThan(10000);
    expect(() => changeRace(g, answer, true, g.deadlineAt)).toThrow(); expect(() => changeRace({ ...g, ended: true }, answer, false, 1005)).toThrow();
  });
  it("rejects missing/duplicate/foreign entries and empty groups without a revision", () => {
    const g = createRaceGame(pack(), "closed", 1000);
    for (const groups of [[], [[]], [["c0", "c0", "c1", "c2", "c3", "c4", "c5"]], [["c0"]], [["c0", "c1", "c2", "c3", "c4", "c5", "foreign"]]]) expect(() => changeRace(g, groups, true, 1001)).toThrow();
    expect(g.revision).toBe(0); expect(g.submitted).toBeNull();
  });
  it("provides enough adjacent clues to determine all relations including ties", () => {
    const clues = raceClues(race()); expect(clues).toHaveLength(6); expect(clues[1]).toBe("角色1与角色2同名次完赛"); expect(clues[2]).toBe("角色2的名次在角色3之前"); expect(clues.join("")).not.toMatch(/第 [0-9]+ 名/);
  });
  it("rejects impossible archives and ignores imported scores/untrusted media URLs", () => {
    const p = pack(), g = changeRace(createRaceGame(p, "closed", 1000), race().entries.map(e => [e.characterId]), true, 1001);
    for (const patch of [{ raceId: "other" }, { cardIds: Array(7).fill("c0") }, { submittedAt: g.deadlineAt }, { deadlineAt: g.deadlineAt + 1000 }, { submitted: null }, { submissions: 0 }, { style: "open-secret" }, { updatedAt: 1000 }]) expect(() => validateRaceArchive({ schemaVersion: 1, pack: p, game: { ...g, ...patch } })).toThrow();
    const checked = validateRaceArchive({ schemaVersion: 1, pack: p, game: { ...g, scoreUnits: 10000, secret: "discard" } }); expect(checked.game).not.toHaveProperty("scoreUnits"); expect(checked.game).not.toHaveProperty("secret");
    const withExtra = race(); withExtra.entries[0]!.imagePath = "https://evil.example/a.webp"; expect(readRacePack(pack([withExtra])).races[0]!.entries[0]!.imagePath).toBe("/assets/avatars/c0.webp");
  });
  it("validates exact UTF-8, valid dates, explicit known ranks, count and race identity", () => {
    const p = pack(); expect(() => readRacePack({ ...p, byteLength: p.contentJSON.length })).toThrow(); expect(() => readRacePack({ ...p, contentJSON: p.contentJSON + " " })).toThrow();
    for (const r of [{ ...race(), date: "2026-02-30" }, { ...race(), entries: race().entries.slice(0, 3) }, { ...race(), entries: [...race().entries, race().entries[0]!] }, { ...race(), entries: race().entries.map(e => ({ ...e, finishRank: 0 })) }]) expect(() => readRacePack(pack([r]))).toThrow();
    expect(() => readRacePack(pack([race(), race()]))).toThrow();
  });
});
