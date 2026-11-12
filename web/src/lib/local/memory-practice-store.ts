import type { components } from "@/lib/api/schema";
import { MemoryNotice } from "@/i18n/memory";
import { replayMemory, type MemoryFace, type MemoryGame } from "@/lib/domain/memory-practice";
import { activityStore } from "./activity-store";
import { newPracticeId } from "./practice-store";
import { sha256 } from "./checksum";

export type MemoryPack = components["schemas"]["MemoryPracticePack"];
export type MemoryArchive = { schemaVersion: 1; game: MemoryGame; pack: MemoryPack };
const object = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const invalid = () => new MemoryNotice("incomplete");
export const memoryPackKey = (p: MemoryPack) => `memory:${p.contentPackVersion}:${p.sha256}`;
export function readMemoryPack(raw: unknown): { pack: MemoryPack; faces: MemoryFace[] } {
  if (!object(raw) || raw.schemaVersion !== 1 || raw.modeId !== "memory" || raw.rulesVersion !== "2.0.0-draft.1" || raw.reviewStatus !== "source-snapshot") throw invalid();
  for (const key of ["datasetVersion", "contentPackVersion"]) if (typeof raw[key] !== "string" || !raw[key] || raw[key].length > 160) throw invalid();
  if (!Array.isArray(raw.sourceUrls) || raw.sourceUrls.length > 10 || raw.sourceUrls.some(s => typeof s !== "string" || s.length > 500 || !s.startsWith("https://"))) throw invalid();
  if (typeof raw.contentJSON !== "string" || raw.contentJSON.length > 1_000_000 || !/^[a-f0-9]{64}$/.test(String(raw.sha256)) || new TextEncoder().encode(raw.contentJSON).length !== raw.byteLength || sha256(raw.contentJSON) !== raw.sha256) throw invalid();
  let content: unknown; try { content = JSON.parse(raw.contentJSON); } catch { throw invalid(); }
  if (!object(content) || !Array.isArray(content.faces) || content.faces.length < 8 || content.faces.length > 1000) throw invalid();
  const ids = new Set<string>(), images = new Set<string>(), faces: MemoryFace[] = [];
  for (const f of content.faces) {
    if (!object(f) || typeof f.pairId !== "string" || !/^[A-Za-z0-9_-]{1,100}$/.test(f.pairId) || ids.has(f.pairId) || typeof f.imagePath !== "string" || !/^\/assets\/faces\/[A-Za-z0-9_-]+\.(webp|png)$/.test(f.imagePath) || images.has(f.imagePath)) throw invalid();
    ids.add(f.pairId); images.add(f.imagePath); faces.push({ pairId: f.pairId, imagePath: f.imagePath });
  }
  const pack: MemoryPack = { schemaVersion: 1, modeId: "memory", rulesVersion: raw.rulesVersion, datasetVersion: raw.datasetVersion as string, contentPackVersion: raw.contentPackVersion as string, sourceUrls: raw.sourceUrls as string[], reviewStatus: "source-snapshot", contentJSON: raw.contentJSON, sha256: raw.sha256 as string, byteLength: raw.byteLength as number };
  return { pack, faces };
}
function shuffle<T>(items: T[], now: number): T[] {
  for (let i = items.length - 1; i > 0; i--) {
    const bound = Math.floor(0x100000000 / (i + 1)) * (i + 1); let n: number;
    do { n = (crypto.getRandomValues(new Uint32Array(1))[0]! ^ (now >>> 0)) >>> 0; } while (n >= bound);
    const j = n % (i + 1); [items[i], items[j]] = [items[j]!, items[i]!];
  }
  return items;
}
export function createMemoryGame(pack: MemoryPack, now = Date.now()): MemoryGame {
  const faces = shuffle(readMemoryPack(pack).faces, now).slice(0, 8);
  return { schemaVersion: 1, modeId: "memory", gameId: newPracticeId(), packKey: memoryPackKey(pack), rulesVersion: pack.rulesVersion, board: shuffle(faces.flatMap(f => [f.pairId, f.pairId]), now), moves: [], createdAt: now, updatedAt: now, startedAt: null, ended: false, revision: 0 };
}
export function validateMemoryArchive(raw: unknown): MemoryArchive {
  if (!object(raw) || raw.schemaVersion !== 1 || !object(raw.game)) throw invalid();
  const { pack, faces } = readMemoryPack(raw.pack), g = raw.game;
  if (g.schemaVersion !== 1 || g.modeId !== "memory" || typeof g.gameId !== "string" || !/^local-[a-f0-9]{32}$/.test(g.gameId) || g.packKey !== memoryPackKey(pack) || g.rulesVersion !== pack.rulesVersion || typeof g.ended !== "boolean") throw invalid();
  for (const key of ["createdAt", "updatedAt", "revision"]) if (!Number.isSafeInteger(g[key]) || Number(g[key]) < 0 || Number(g[key]) > 8_640_000_000_000_000) throw invalid();
  if (Number(g.createdAt) <= 0 || Number(g.updatedAt) < Number(g.createdAt) || Number(g.revision) > 1000 || g.startedAt !== null && (!Number.isSafeInteger(g.startedAt) || Number(g.startedAt) < Number(g.createdAt) || Number(g.startedAt) > Number(g.updatedAt))) throw invalid();
  if (!Array.isArray(g.board) || g.board.length !== 16 || g.board.some(id => typeof id !== "string" || !faces.some(f => f.pairId === id)) || new Set(g.board).size !== 8 || g.board.some(id => (g.board as unknown[]).filter(other => other === id).length !== 2) || !Array.isArray(g.moves) || g.moves.length > 40) throw invalid();
  const moves = g.moves.map(m => { if (!object(m) || !Number.isSafeInteger(m.index) || !Number.isSafeInteger(m.at) || Number(m.at) > Number(g.updatedAt)) throw invalid(); return { index: Number(m.index), at: Number(m.at) }; });
  const game: MemoryGame = { schemaVersion: 1, modeId: "memory", gameId: g.gameId, packKey: g.packKey as string, rulesVersion: g.rulesVersion as string, board: [...g.board] as string[], moves, createdAt: Number(g.createdAt), updatedAt: Number(g.updatedAt), startedAt: g.startedAt as number | null, ended: g.ended, revision: Number(g.revision) };
  replayMemory(game); return { schemaVersion: 1, game, pack };
}
export const memoryStore = activityStore<MemoryPack, MemoryGame>("umaguess-memory-practice", validateMemoryArchive, memoryPackKey);
export const volatileMemoryGames = new Map<string, MemoryArchive>();
