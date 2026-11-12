import { describe, expect, it } from "vitest";
import fixtures from "@/lib/domain/testdata/path-routes.json";
import { changePath, checkRoute, pathIndex, pathResult, pathScore, shortestRoute, type PracticeGraph } from "@/lib/domain/path-practice";
import { createPathGame, readPathPack, validatePathArchive, type PathPack } from "./path-practice-store";
import { sha256 } from "./checksum";
const graph = (): PracticeGraph => ({ nodes: fixtures.nodes.map(characterId => ({ characterId, name: `角色${characterId}`, imagePath: `/assets/avatars/${characterId}.webp` })), edges: fixtures.edges.map(([from, to], i) => ({ from: from!, to: to!, raceId: `r${i}`, raceName: "锦标", date: "2026-01-01" })) });
function pack(g = graph()): PathPack { const contentJSON = JSON.stringify(g); return { schemaVersion: 1, modeId: "co-race-path", rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", sourceUrls: ["https://wiki.biligame.com/umamusume/"], reviewStatus: "source-snapshot", contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length }; }
describe("shared Go/browser co-appearance rules", () => {
  for (const f of fixtures.cases) it(f.name, () => {
    const p = pack(), g = { ...createPathGame(p, "closed", 1000), start: "a", goal: f.goal, draft: ["a"] };
    if (f.accepted) expect(pathResult(changePath(g, graph(), f.path, true, 1001), graph()).scoreUnits).toBe(f.scoreUnits);
    else expect(() => changePath(g, graph(), f.path, true, 1001)).toThrow();
  });
});
describe("co-appearance practice recovery", () => {
  it("draws distinct connected endpoints within six edges and prefers multi-step questions", () => {
    const p = pack(); for (let i = 1; i <= 20; i++) { const g = createPathGame(p, "open", 1000 + i), route = shortestRoute(graph(), g.start, g.goal)!; expect(route.length).toBeGreaterThanOrEqual(3); expect(route.length).toBeLessThanOrEqual(7); expect(g.start).not.toBe("z"); expect(g.goal).not.toBe("z"); expect(g.deadlineAt).toBe(181000 + i); }
    expect(shortestRoute(graph(), "a", "z")).toBeNull();
    const adjacent = graph(); adjacent.edges = [adjacent.edges[0]!]; expect(createPathGame(pack(adjacent), "open", 1000).draft).toHaveLength(1);
    const disconnected = graph(); disconnected.edges = []; expect(() => createPathGame(pack(disconnected), "open", 1000)).toThrow();
  });
  it("preserves last confirmed path, permits revisions and does not auto-submit at cutoff", () => {
    const p = pack(); let g = { ...createPathGame(p, "closed", 1000), start: "a", goal: "d", draft: ["a"] };
    g = changePath(g, graph(), ["a", "b", "d"], false, 1001); expect(pathResult(g, graph()).scoreUnits).toBe(0);
    g = changePath(g, graph(), g.draft, true, 1002); g = changePath(g, graph(), ["a", "c", "e", "d"], false, 1003); expect(pathResult(g, graph()).scoreUnits).toBe(10000);
    g = changePath(g, graph(), g.draft, true, 1004); expect(pathResult(g, graph()).scoreUnits).toBe(9000); expect(g.submissions).toBe(2);
    expect(validatePathArchive({ schemaVersion: 1, pack: p, game: g }).game).toEqual(g);
    expect(() => changePath(g, graph(), ["a"], true, g.deadlineAt)).toThrow(); expect(() => changePath({ ...g, ended: true }, graph(), ["a"], false, 1005)).toThrow();
  });
  it("reports the actual missing step without a reference solution and keeps draft unsubmitted", () => {
    const p = pack(), g = { ...createPathGame(p, "closed", 1000), start: "a", goal: "d", draft: ["a"] };
    const next = changePath(g, graph(), ["a", "e", "d"], false, 1001); expect(next.submitted).toBeNull(); expect(checkRoute(graph(), next.draft, "a")?.step).toBe(1); expect(() => changePath(next, graph(), next.draft, true, 1002)).toThrow(/第 1 步/);
    expect(pathScore(20, 2)).toBe(2000);
  });
  it("deduplicates adjacency and retains distinct race evidence", () => {
    const g = graph(); g.edges.push({ ...g.edges[0]!, raceId: "another" }); const index = pathIndex(g); expect(index.adjacency.get("a")?.size).toBe(2); expect(index.evidence.get("a|b")).toHaveLength(2);
  });
  it("rejects impossible imports and discards supplied score and foreign media", () => {
    const p = pack(), g = changePath({ ...createPathGame(p, "closed", 1000), start: "a", goal: "d", draft: ["a"] }, graph(), ["a", "b", "d"], true, 1001);
    for (const patch of [{ goal: "a" }, { goal: "z" }, { draft: ["a", "a"] }, { submitted: ["a", "e", "d"] }, { deadlineAt: g.deadlineAt + 1 }, { submissions: 0 }, { submittedAt: g.deadlineAt }, { style: "secret" }]) expect(() => validatePathArchive({ schemaVersion: 1, pack: p, game: { ...g, ...patch } })).toThrow();
    expect(validatePathArchive({ schemaVersion: 1, pack: p, game: { ...g, scoreUnits: 10000, secret: "discard" } }).game).not.toHaveProperty("scoreUnits");
    const raw = graph(); raw.nodes[0]!.imagePath = "https://untrusted.example/x"; expect(readPathPack(pack(raw)).graph.nodes[0]!.imagePath).toBe("/assets/avatars/a.webp");
    const dup = graph(); dup.edges.push({ ...dup.edges[0]!, from: "b", to: "a" }); expect(() => readPathPack(pack(dup))).toThrow();
    const invalid = graph(); invalid.edges[0]!.date = "2026-02-30"; expect(() => readPathPack(pack(invalid))).toThrow(); expect(() => readPathPack({ ...p, byteLength: 1 })).toThrow();
  });
});
