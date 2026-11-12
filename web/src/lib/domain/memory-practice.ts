import { MemoryNotice } from "@/i18n/memory";

export type MemoryFace = { pairId: string; imagePath: string };
export type MemoryMove = { index: number; at: number };
export type MemoryGame = {
  schemaVersion: 1; modeId: "memory"; gameId: string; packKey: string; rulesVersion: string;
  board: string[]; moves: MemoryMove[]; createdAt: number; updatedAt: number;
  startedAt: number | null; ended: boolean; revision: number;
};
export const VIEW_MS = 8000, ANSWER_MS = 90000, MISS_MS = 900, MAX_ATTEMPTS = 20;
export function memoryScore(matched: number, wrong: number): number {
  return Math.max(0, Math.floor(100 * matched / 8) - 2 * wrong) * 100;
}

// Replay recorded intentions. No imported/derived score or match flags are trusted.
export function replayMemory(game: MemoryGame) {
  const matched = new Set<number>();
  let first: number | null = null, wrong = 0, attempts = 0, lastAt = game.startedAt ?? game.createdAt;
  let miss: [number, number] | null = null, missUntil = 0;
  if (game.moves.length && game.startedAt === null) throw new MemoryNotice("notStarted");
  for (const move of game.moves) {
    if (!Number.isInteger(move.index) || move.index < 0 || move.index >= 16 || !Number.isSafeInteger(move.at)
      || move.at < lastAt || move.at < game.startedAt! + VIEW_MS || move.at >= game.startedAt! + VIEW_MS + ANSWER_MS
      || move.at < missUntil || matched.has(move.index) || first === move.index || attempts >= MAX_ATTEMPTS || matched.size === 16) {
      throw new MemoryNotice("badOrder");
    }
    lastAt = move.at; miss = null;
    if (first === null) { first = move.index; continue; }
    attempts++;
    if (game.board[first] === game.board[move.index]) { matched.add(first); matched.add(move.index); }
    else { wrong++; miss = [first, move.index]; missUntil = move.at + MISS_MS; }
    first = null;
  }
  return { matched, first, wrong, attempts, miss, missUntil, scoreUnits: memoryScore(matched.size / 2, wrong) };
}
export function memoryState(game: MemoryGame, now: number) {
  const state = replayMemory(game);
  const viewing = game.startedAt !== null && now < game.startedAt + VIEW_MS;
  const deadlineAt = game.startedAt === null ? null : game.startedAt + VIEW_MS + ANSWER_MS;
  const finished = game.ended || state.matched.size === 16 || state.attempts >= MAX_ATTEMPTS || deadlineAt !== null && now >= deadlineAt;
  const cooling = state.miss !== null && now < state.missUntil;
  return { ...state, viewing, deadlineAt, finished, cooling,
    visible: new Set(finished || viewing ? game.board.map((_, i) => i)
      : [...state.matched, ...(state.first === null ? [] : [state.first]), ...(cooling ? state.miss! : [])]) };
}
export function flipMemory(game: MemoryGame, index: number, now: number): MemoryGame {
  const state = memoryState(game, now);
  if (game.startedAt === null || state.viewing || state.finished || state.cooling) throw new MemoryNotice("cannotFlip");
  const next = { ...game, moves: [...game.moves, { index, at: now }], updatedAt: now, revision: game.revision + 1 };
  replayMemory(next);
  return next;
}
