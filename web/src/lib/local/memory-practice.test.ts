import { describe, expect, it } from "vitest";
import scoring from "@/lib/domain/testdata/memory-scoring.json";
import { ANSWER_MS, flipMemory, memoryScore, memoryState, MISS_MS, replayMemory, VIEW_MS, type MemoryGame } from "@/lib/domain/memory-practice";
import { createMemoryGame, readMemoryPack, validateMemoryArchive, type MemoryPack } from "./memory-practice-store";
import { sha256 } from "./checksum";
function pack(): MemoryPack {
  const contentJSON = JSON.stringify({ faces: Array.from({ length: 12 }, (_, i) => ({ pairId: `p${i}`, imagePath: `/assets/faces/p${i}.webp` })) });
  return { schemaVersion: 1, modeId: "memory", rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", sourceUrls: ["https://wiki.biligame.com/umamusume/"], reviewStatus: "source-snapshot", contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length };
}
function started(): MemoryGame { const game = createMemoryGame(pack(), 1000); return { ...game, startedAt: 2000, updatedAt: 2000, revision: 1 }; }
describe("shared memory scoring", () => { for (const f of scoring) it(f.name, () => expect(memoryScore(f.matched, f.wrong)).toBe(f.scoreUnits)); });
describe("memory practice progression and recovery", () => {
  it("uses eight unique pairs, waits for start and enforces fixed viewing/answer windows", () => {
    const g = started(); expect(new Set(g.board).size).toBe(8); for (const id of g.board) expect(g.board.filter(v => v === id)).toHaveLength(2);
    expect(() => flipMemory(g, 0, 2000 + VIEW_MS - 1)).toThrow();
    expect(memoryState(g, 2000).visible.size).toBe(16);
    expect(memoryState(g, 2000 + VIEW_MS).visible.size).toBe(0);
    expect(memoryState(g, 2000 + VIEW_MS + ANSWER_MS).finished).toBe(true);
    expect(() => flipMemory(g, 0, 2000 + VIEW_MS + ANSWER_MS)).toThrow();
    expect(() => flipMemory({ ...g, startedAt: null }, 0, 10000)).toThrow();
  });
  it("a miss costs one attempt and stays visible across recovery until the same deadline", () => {
    const g = started(), other = g.board.findIndex(id => id !== g.board[0]);
    const first = flipMemory(g, 0, 10000), miss = flipMemory(first, other, 10001);
    expect(replayMemory(first).attempts).toBe(0);
    const recovered = validateMemoryArchive({ schemaVersion: 1, game: miss, pack: pack() }).game;
    expect(memoryState(recovered, 10002).visible.size).toBe(2);
    expect(replayMemory(recovered).wrong).toBe(1);
    expect(() => flipMemory(recovered, 3, 10002)).toThrow();
    expect(memoryState(recovered, 10001 + MISS_MS).visible.size).toBe(0);
    expect(() => flipMemory(first, 0, 10001)).toThrow();
  });
  it("finds each pair once and never accepts actions after complete", () => {
    let g = started(), at = 10000;
    for (const id of new Set(g.board)) for (const index of g.board.flatMap((v, i) => v === id ? [i] : [])) g = flipMemory(g, index, at++);
    expect(memoryState(g, at).scoreUnits).toBe(10000);
    expect(memoryState(g, at).finished).toBe(true);
    expect(() => flipMemory(g, 0, at)).toThrow();
  });
  it("stops at twenty wrong attempts and floors the score", () => {
    let g = started(), at = 10000; const second = g.board.findIndex(id => id !== g.board[0]);
    for (let i = 0; i < 20; i++) { g = flipMemory(g, 0, at); g = flipMemory(g, second, at + 1); at += MISS_MS + 1; }
    expect(memoryState(g, at).finished).toBe(true); expect(memoryState(g, at).attempts).toBe(20); expect(memoryState(g, at).scoreUnits).toBe(0);
    expect(() => flipMemory(g, 0, at)).toThrow();
  });
  it("rejects altered public data, external image paths, malformed pairs and impossible imports", () => {
    const p = pack(), g = started();
    expect(() => readMemoryPack({ ...p, contentJSON: p.contentJSON + " " })).toThrow();
    const bad = JSON.stringify({ faces: readMemoryPack(p).faces.map(f => ({ ...f, imagePath: "https://evil.example/face.webp" })) });
    expect(() => readMemoryPack({ ...p, contentJSON: bad, sha256: sha256(bad), byteLength: bad.length })).toThrow();
    expect(() => validateMemoryArchive({ schemaVersion: 1, pack: p, game: { ...g, board: Array(16).fill(g.board[0]) } })).toThrow();
    expect(() => validateMemoryArchive({ schemaVersion: 1, pack: p, game: { ...g, moves: [{ index: 0, at: 2001 }] } })).toThrow();
    const checked = validateMemoryArchive({ schemaVersion: 1, pack: p, game: { ...g, scoreUnits: 10000, secret: "discard" } });
    expect(checked.game).not.toHaveProperty("scoreUnits"); expect(checked.game).not.toHaveProperty("secret");
  });
});
