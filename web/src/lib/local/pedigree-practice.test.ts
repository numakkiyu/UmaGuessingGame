import { describe, expect, it } from "vitest";
import fixtures from "@/lib/domain/testdata/pedigree-assignments.json";
import { PEDIGREE_DISCLAIMER, PEDIGREE_SLOTS, changePedigree, normalizedHorseName, pedigreeResult, type PedigreeAssignment, type PedigreeGame } from "@/lib/domain/pedigree-practice";
import { createPedigreeGame, readPedigreePack, validatePedigreeArchive, type PedigreePack } from "./pedigree-practice-store";
import { sha256 } from "./checksum";
function content() { return { disclaimer: PEDIGREE_DISCLAIMER, slots: PEDIGREE_SLOTS, horses: "abcdefghijkl".split("").map(horseId => ({ horseId, name: horseId === "a" ? "メジロA(USA)" : horseId === "b" ? "メジロB(USA)" : `原型${horseId}`, nkbId: null })), families: ["1001", "1002"].map((characterId, i) => ({ subject: { characterId, characterName: `角色${characterId}`, prototypeName: `注册名${characterId}`, nkbId: i ? "000a0003bd" : "1995103211" }, relations: Object.fromEntries(PEDIGREE_SLOTS.map((s, index) => [s.slotId, "abcdefghijkl"[index + i * 6]])) })) }; }
function pack(c = content()): PedigreePack { const contentJSON = JSON.stringify(c); return { schemaVersion: 1, modeId: "pedigree", rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", sourceUrls: ["https://wiki.biligame.com/umamusume/"], reviewStatus: "source-snapshot", contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length }; }
function game(p = pack()): PedigreeGame { return { ...createPedigreeGame(p, 4, 1000), characterId: "1001", slotIds: fixtures.slotIds as PedigreeGame["slotIds"], candidateIds: fixtures.candidateIds }; }
describe("shared Go and browser pedigree assignment vectors", () => {
  for (const f of fixtures.cases) it(f.name, () => { const p = pack(), g = game(p), family = readPedigreePack(p).library.families[0]!; if (f.accepted) expect(pedigreeResult(changePedigree(g, f.assignments as PedigreeAssignment[], true, 1001), family).scoreUnits).toBe(f.scoreUnits); else expect(() => changePedigree(g, f.assignments as PedigreeAssignment[], true, 1001)).toThrow(); });
});
describe("pedigree practice draw and recovery", () => {
  it("draws four to six distinct directions with parents and ten shuffled choices containing every answer", () => {
    const p = pack(), { library } = readPedigreePack(p);
    for (const count of [4, 5, 6]) for (let at = 1; at <= 20; at++) { const g = createPedigreeGame(p, count, at), family = library.families.find(f => f.subject.characterId === g.characterId)!; expect(g.slotIds).toHaveLength(count); expect(g.slotIds.slice(0, 2)).toEqual(["sire", "dam"]); expect(new Set(g.candidateIds).size).toBe(10); for (const id of g.slotIds) expect(g.candidateIds).toContain(family.relations[id]); expect(g.deadlineAt).toBe(at + 180000); expect(validatePedigreeArchive({ schemaVersion: 1, pack: p, game: g }).game).toEqual(g); }
    expect(() => createPedigreeGame(p, 3)).toThrow(); expect(() => createPedigreeGame(p, 7)).toThrow();
  });
  it("keeps latest confirmed mapping distinct from drafts and permits corrections without revealing correctness", () => {
    const p = pack(), family = readPedigreePack(p).library.families[0]!, a: PedigreeAssignment[] = [{ slotId: "sire", horseId: "a" }], b: PedigreeAssignment[] = [{ slotId: "sire", horseId: "b" }];
    let g = changePedigree(game(p), a, false, 1001); expect(g.submitted).toBeNull(); expect(pedigreeResult(g, family).scoreUnits).toBe(0);
    g = changePedigree(g, a, true, 1002); expect(pedigreeResult(g, family).scoreUnits).toBe(2500);
    g = changePedigree(g, b, false, 1003); expect(g.submitted).toEqual(a); expect(pedigreeResult(g, family).scoreUnits).toBe(2500);
    g = changePedigree(g, b, true, 1004); expect(g.submissions).toBe(2); expect(pedigreeResult(g, family).scoreUnits).toBe(0); expect(g).not.toHaveProperty("correct");
    expect(validatePedigreeArchive({ schemaVersion: 1, pack: p, game: g }).game).toEqual(g);
    expect(() => changePedigree(g, a, true, g.deadlineAt)).toThrow(); expect(() => changePedigree({ ...g, ended: true }, a, false, 1005)).toThrow();
  });
  it("rounds percentage down for six directions and distinguishes historical name prefixes", () => {
    const p = pack(), { library } = readPedigreePack(p), g = { ...game(p), slotIds: PEDIGREE_SLOTS.map(s => s.slotId) };
    const next = changePedigree(g, [{ slotId: "sire", horseId: "a" }], true, 1001); expect(pedigreeResult(next, library.families[0]!).scoreUnits).toBe(1600);
    expect(normalizedHorseName(" Wishing  Well (USA) ")).toBe("Wishing Well"); expect(normalizedHorseName("メジロA(USA)")).not.toBe(normalizedHorseName("メジロB(USA)")); expect(normalizedHorseName("Horse(JPN)")).toBe("Horse"); expect(normalizedHorseName("horse")).not.toBe(normalizedHorseName("Horse"));
  });
  it("rejects malformed facts and identity ambiguity; never imports foreign media or claimed scores", () => {
    const p = pack(), g = changePedigree(game(p), [{ slotId: "sire", horseId: "a" }], true, 1001);
    for (const patch of [{ slotIds: ["sire", "dam", "dam", "damSire"] }, { slotIds: ["dam", "sire", "sireSire", "damSire"] }, { candidateIds: Array(10).fill("a") }, { characterId: "9999" }, { deadlineAt: g.deadlineAt + 1 }, { submittedAt: g.deadlineAt }, { submissions: 0 }, { draft: [{ slotId: "sire", horseId: "a" }, { slotId: "sire", horseId: "b" }] }]) expect(() => validatePedigreeArchive({ schemaVersion: 1, pack: p, game: { ...g, ...patch } })).toThrow();
    const restored = validatePedigreeArchive({ schemaVersion: 1, pack: p, game: { ...g, scoreUnits: 10000, rank: 1, imagePath: "https://evil.invalid/x" } }); expect(restored.game).not.toHaveProperty("scoreUnits"); expect(restored.game).not.toHaveProperty("rank"); expect(restored.game).not.toHaveProperty("imagePath");
    const mutations = [
      (c: ReturnType<typeof content>) => { c.disclaimer = "角色家庭"; },
      (c: ReturnType<typeof content>) => { c.families[1]!.subject.nkbId = c.families[0]!.subject.nkbId; },
      (c: ReturnType<typeof content>) => { c.families[1]!.subject.prototypeName = c.families[0]!.subject.prototypeName; },
      (c: ReturnType<typeof content>) => { c.families[0]!.relations.dam = "a"; },
      (c: ReturnType<typeof content>) => { delete c.families[0]!.relations.sire; },
      (c: ReturnType<typeof content>) => { c.horses[1]!.name = c.horses[0]!.name.replace("(USA)", ""); },
      (c: ReturnType<typeof content>) => { c.horses[0]!.name = c.families[0]!.subject.prototypeName; },
    ]; for (const mutate of mutations) { const c = content(); mutate(c); expect(() => readPedigreePack(pack(c))).toThrow(); }
    expect(() => readPedigreePack({ ...p, byteLength: 1 })).toThrow(); expect(() => readPedigreePack({ ...p, sha256: "0".repeat(64) })).toThrow();
  });
});
