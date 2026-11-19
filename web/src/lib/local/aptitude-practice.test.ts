import { describe, expect, it } from "vitest";
import fixtures from "@/lib/domain/testdata/aptitude-rules.json";
import { bestLineup, changeLineup, claimGrid, DRAFT_BUDGET, draftFeasibleSets, gridConditions, gridFinished, gridSolution, lineupScore, publishedPracticePrice, satisfiesGrid, submitLineup, type AptitudeCard, type PricedCard } from "@/lib/domain/aptitude-practice";
import { taskPointsFor, type DraftTask, type DraftAptitudes } from "@/lib/api/draft";
import { createDraftGame, createGridGame, readDraftPack, readGridPack, validateDraftArchive, validateGridArchive, type DraftPack, type GridPack } from "./aptitude-practice-store";
import { sha256 } from "./checksum";
const axes = { distance: ["短", "英", "中"], style: ["逃", "先", "差"] };
function card(id: string, distance = "短", style = "逃"): AptitudeCard { return { id, name: `角色${id}`, imagePath: `/assets/avatars/${id}.webp`, aptitudes: { surface: { "草地": "A" }, distance: { [distance]: "A" }, style: { [style]: "A" } } }; }
const gridCards = axes.distance.flatMap((d, i) => axes.style.map((s, j) => card(`c${i}${j}`, d, s)));
const draftCards: PricedCard[] = Array.from({ length: 12 }, (_, i) => { const c = card(`d${i}`, axes.distance[i % 3], axes.style[Math.floor(i / 3) % 3]); return { ...c, price: publishedPracticePrice(c.aptitudes) }; });
const tasks: DraftTask[] = Array.from({ length: 4 }, (_, i) => ({ taskId: `t${i}`, surface: "草地", distance: axes.distance[i % 3]!, style: axes.style[i % 3]! }));
function envelope<M extends "draft" | "aptitude-grid">(modeId: M, data: unknown) { const contentJSON = JSON.stringify(data); return { schemaVersion: 1 as const, modeId, rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "p", sourceUrls: ["https://wiki.biligame.com/umamusume/"], reviewStatus: "source-snapshot" as const, contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length }; }
const gridPack = (): GridPack => envelope("aptitude-grid", { cards: gridCards });
const draftPack = (): DraftPack => envelope("draft", { cards: draftCards, tasks });
describe("shared Go and browser aptitude rules", () => {
  for (const f of fixtures.grid) it(f.name, () => expect(satisfiesGrid({ ...card("f"), aptitudes: { surface: {}, distance: { "短": f.distance }, style: { "逃": f.style } } }, "短", "逃")).toBe(f.fits));
  for (const f of fixtures.draft as { name: string; aptitudes: DraftAptitudes; price: number; points: number }[]) it(f.name, () => { expect(publishedPracticePrice(f.aptitudes)).toBe(f.price); expect(taskPointsFor({ ...card("f"), aptitudes: f.aptitudes, price: f.price }, tasks[0]!).total).toBe(f.points); });
});
describe("independent grid progression", () => {
  it("matches nine distinct characters in both orientations and detects Hall deficiency", () => {
    for (const axis of ["distance", "style"] as const) { const solution = gridSolution(gridCards, axis)!; expect(new Set(solution).size).toBe(9); solution.forEach((id, i) => { const cond = gridConditions(axis, i); expect(satisfiesGrid(gridCards.find(c => c.id === id)!, cond.distance, cond.style)).toBe(true); }); }
    const universal = { ...card("all"), aptitudes: { surface: {}, distance: Object.fromEntries(axes.distance.map(d => [d, "A"])), style: Object.fromEntries(axes.style.map(s => [s, "A"])) } };
    expect(gridSolution([universal, ...Array.from({ length: 8 }, (_, i) => card(`n${i}`))], "distance")).toBeNull();
    expect(() => readGridPack(envelope("aptitude-grid", { cards: [universal, ...Array.from({ length: 8 }, (_, i) => card(`n${i}`))] }))).toThrow();
  });
  it("rejects illegal attempts without consuming a claim, and locks claimed cells and characters", () => {
    const g = { ...createGridGame(gridPack(), 1000), rowAxis: "distance" as const };
    expect(() => claimGrid(g, gridCards, 0, "c11", 1001)).toThrow(/不扣机会/); expect(g.claims).toHaveLength(0); expect(g.revision).toBe(0);
    const next = claimGrid(g, gridCards, 0, "c00", 1001); expect(next.claims).toHaveLength(1);
    expect(() => claimGrid(next, gridCards, 0, "c01", 1002)).toThrow(/已经有人/);
    expect(() => claimGrid(next, gridCards, 1, "c00", 1002)).toThrow(/只能占一格/);
    expect(() => claimGrid(g, gridCards, 0, "c00", g.deadlineAt)).toThrow();
    expect(() => claimGrid({ ...g, ended: true }, gridCards, 0, "c00", 1001)).toThrow();
  });
  it("finishes on nine claims, preserving the original deadline across archive recovery", () => {
    let g = createGridGame(gridPack(), 1000); const solution = gridSolution(gridCards, g.rowAxis)!;
    solution.forEach((id, i) => { g = claimGrid(g, gridCards, i, id, 1001 + i); });
    expect(gridFinished(g, 1100)).toBe(true); expect(() => claimGrid(g, gridCards, 0, solution[0]!, 1100)).toThrow();
    expect(validateGridArchive({ schemaVersion: 1, pack: gridPack(), game: g }).game).toEqual(g);
  });
  it("rejects duplicated characters, impossible times and wrong claims in imports; ignores supplied scores", () => {
    const g = claimGrid({ ...createGridGame(gridPack(), 1000), rowAxis: "distance" }, gridCards, 0, "c00", 1001);
    const archive = { schemaVersion: 1, pack: gridPack(), game: g };
    for (const bad of [{ ...g, claims: [...g.claims, { cellIndex: 1, characterId: "c00", at: 1001 }] }, { ...g, claims: [{ cellIndex: 0, characterId: "c11", at: 1001 }] }, { ...g, claims: [{ cellIndex: 0, characterId: "c00", at: g.deadlineAt }], updatedAt: g.deadlineAt }]) expect(() => validateGridArchive({ ...archive, game: bad })).toThrow();
    expect(validateGridArchive({ ...archive, game: { ...g, scoreUnits: 10000, secret: "discard" } }).game).not.toHaveProperty("scoreUnits");
  });
});
describe("independent public draft", () => {
  it("draws twelve cards and four tasks, with at least two feasible sets", () => { const g = createDraftGame(draftPack(), 1000); expect(new Set(g.marketIds).size).toBe(12); expect(new Set(g.taskIds).size).toBe(4); expect(draftFeasibleSets(draftCards, 2)).toBe(2); expect(draftFeasibleSets(draftCards.slice(0, 4), 2)).toBe(1); });
  it("allows incomplete, duplicate and overbudget drafts but refuses final submission", () => {
    const g = createDraftGame(draftPack(), 1000); expect(() => submitLineup(g, draftCards, tasks, 1001)).toThrow();
    let duplicate = g; for (const t of tasks) duplicate = changeLineup(duplicate, tasks, t.taskId, draftCards[0]!.id, 1001);
    expect(Object.keys(duplicate.draft)).toHaveLength(4); expect(() => submitLineup(duplicate, draftCards, tasks, 1002)).toThrow(/不能参加两个/);
    const allA = { surface: { "草地": "A", "泥地": "A" }, distance: { "短": "A", "英": "A", "中": "A", "长": "A" }, style: { "逃": "A", "先": "A", "差": "A", "追": "A" } };
    const expensive = draftCards.map(c => ({ ...c, aptitudes: allA, price: 8 })); const assignments = Object.fromEntries(tasks.map((t, i) => [t.taskId, expensive[i]!.id]));
    expect(() => lineupScore(assignments, expensive, tasks)).toThrow(/超出预算/); expect(DRAFT_BUDGET).toBe(24);
  });
  it("locks a complete legal lineup at final confirmation and never auto-submits on timeout", () => {
    let g = createDraftGame(draftPack(), 1000); for (let i = 0; i < 4; i++) g = changeLineup(g, tasks, tasks[i]!.taskId, draftCards[i]!.id, 1001);
    expect(() => submitLineup(g, draftCards, tasks, g.deadlineAt)).toThrow(); expect(g.submitted).toBeNull();
    const submitted = submitLineup(g, draftCards, tasks, 1002); expect(submitted.ended).toBe(true); expect(lineupScore(submitted.submitted!, draftCards, tasks).spent).toBe(20);
    expect(() => changeLineup(submitted, tasks, tasks[0]!.taskId, "", 1003)).toThrow(); expect(() => submitLineup(submitted, draftCards, tasks, 1003)).toThrow();
    expect(validateDraftArchive({ schemaVersion: 1, pack: draftPack(), game: submitted }).game).toEqual(submitted);
    expect(validateDraftArchive({ schemaVersion: 1, pack: draftPack(), game: { ...g, ended: true, updatedAt: g.deadlineAt } }).game.submitted).toBeNull();
  });
  it("finds the global optimum with uniqueness and budget instead of reusing a best card", () => {
    const optimum = bestLineup(draftCards, tasks)!; expect(new Set(Object.values(optimum.assignments)).size).toBe(4); expect(optimum.spent).toBeLessThanOrEqual(24);
    expect(lineupScore(optimum.assignments, draftCards, tasks).totalPoints).toBe(optimum.totalPoints);
    for (let i = 0; i < 4; i++) for (const c of draftCards) { const changed = { ...optimum.assignments, [tasks[i]!.taskId]: c.id }; if (new Set(Object.values(changed)).size !== 4) continue; expect(lineupScore(changed, draftCards, tasks).totalPoints).toBeLessThanOrEqual(optimum.totalPoints); }
  });
  it("rejects imported final lineups with altered slots, time or score; public facts are whitelisted", () => {
    let g = createDraftGame(draftPack(), 1000); for (let i = 0; i < 4; i++) g = changeLineup(g, tasks, tasks[i]!.taskId, draftCards[i]!.id, 1001); g = submitLineup(g, draftCards, tasks, 1002);
    const archive = { schemaVersion: 1, pack: draftPack(), game: g };
    for (const bad of [{ ...g, submittedAt: g.deadlineAt }, { ...g, ended: false }, { ...g, draft: {} }, { ...g, taskIds: ["t0", "t0", "t2", "t3"] }]) expect(() => validateDraftArchive({ ...archive, game: bad })).toThrow();
    expect(validateDraftArchive({ ...archive, game: { ...g, scoreUnits: 10000 } }).game).not.toHaveProperty("scoreUnits");
    const content = { cards: draftCards.map(c => ({ ...c, hidden: "discard" })), tasks };
    expect(readDraftPack(envelope("draft", content)).cards[0]).not.toHaveProperty("hidden");
    expect(() => readDraftPack(envelope("draft", { ...content, cards: draftCards.map(c => ({ ...c, price: 1 })) }))).toThrow();
    expect(() => readDraftPack(envelope("draft", { ...content, cards: draftCards.map(c => ({ ...c, imagePath: "https://evil.example/c.webp" })) }))).toThrow();
  });
  it("accepts published Chinese task identifiers without exposing or rewriting them", () => {
    const localizedTasks = tasks.map(t => ({ ...t, taskId: `${t.surface}-${t.distance}-${t.style}-${t.taskId}` }));
    const p = envelope("draft", { cards: draftCards, tasks: localizedTasks });
    const g = createDraftGame(p, 1000); expect(g.taskIds).toHaveLength(4);
    expect(validateDraftArchive({ schemaVersion: 1, pack: p, game: g }).game.taskIds).toEqual(g.taskIds);
  });
  it("checks exact UTF-8 length and hash, grade keys, duplicate tasks and identifiers", () => {
    const p = draftPack(); expect(() => readDraftPack({ ...p, byteLength: p.contentJSON.length })).toThrow(); expect(() => readDraftPack({ ...p, contentJSON: p.contentJSON + " " })).toThrow();
    expect(() => readDraftPack(envelope("draft", { cards: draftCards, tasks: Array(4).fill(tasks[0]) }))).toThrow();
    expect(() => readDraftPack(envelope("draft", { cards: draftCards.map(c => ({ ...c, aptitudes: { ...c.aptitudes, distance: { internal: "A" } } })), tasks }))).toThrow();
  });
});
