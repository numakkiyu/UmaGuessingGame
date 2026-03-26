import crypto from "node:crypto";
import { and, asc, eq } from "drizzle-orm";
import { getServerConfig } from "@/config/server";
import { getDb } from "@/lib/db/client";
import { gameGuesses, games } from "@/lib/db/schema";
import { buildGuessRow } from "@/lib/game/compare";
import { loadQuestionBankReady } from "@/lib/question-bank";
import { gameStateSchema, type GuessRow } from "@/lib/validation/schemas";

function randomItem<T>(items: T[]) {
  return items[Math.floor(Math.random() * items.length)];
}

async function getQuestionBankMap() {
  const entries = await loadQuestionBankReady();
  return new Map(entries.map((entry) => [entry.id, entry]));
}

async function getGameRecord(gameId: string) {
  const db = getDb();
  const [game] = await db.select().from(games).where(eq(games.id, gameId)).limit(1);
  if (!game) return null;
  const guessesRows = await db
    .select()
    .from(gameGuesses)
    .where(eq(gameGuesses.gameId, gameId))
    .orderBy(asc(gameGuesses.guessIndex));

  return { game, guessesRows };
}

export function resolveGameProgress(remainingGuesses: number, isCorrect: boolean) {
  if (isCorrect) {
    return {
      remainingGuesses,
      status: "won" as const,
    };
  }

  const nextRemaining = Math.max(remainingGuesses - 1, 0);
  return {
    remainingGuesses: nextRemaining,
    status: nextRemaining <= 0 ? ("lost" as const) : ("playing" as const),
  };
}

function shouldRevealAnswer(status: "playing" | "won" | "lost", includeAnswer: boolean) {
  return includeAnswer || status !== "playing";
}

export async function createNewGame() {
  const db = getDb();
  const bank = await loadQuestionBankReady();
  if (bank.length === 0) {
    throw new Error("question_bank_ready.json 为空，当前无法创建新局。");
  }

  const answer = randomItem(bank);
  const gameId = `game_${crypto.randomUUID()}`;
  await db.insert(games).values({
    id: gameId,
    answerCharacterId: answer.id,
    status: "playing",
    remainingGuesses: getServerConfig().maxGuesses,
    maxGuesses: getServerConfig().maxGuesses,
  });

  return {
    gameId,
    status: "playing" as const,
    remainingGuesses: getServerConfig().maxGuesses,
    guessRows: [],
  };
}

export async function getGameState(gameId: string, includeAnswer = false) {
  const record = await getGameRecord(gameId);
  if (!record) {
    throw new Error("游戏不存在。");
  }

  const bank = await getQuestionBankMap();
  const answer = bank.get(record.game.answerCharacterId);
  const guessRows = record.guessesRows.map((entry) => entry.guessRow as GuessRow);
  const revealAnswer = shouldRevealAnswer(record.game.status as "playing" | "won" | "lost", includeAnswer);
  return gameStateSchema.parse({
    gameId: record.game.id,
    status: record.game.status,
    remainingGuesses: record.game.remainingGuesses,
    guessRows,
    answerCharacterId: revealAnswer ? record.game.answerCharacterId : null,
    answerDisplayName: revealAnswer ? answer?.name_zh ?? null : null,
  });
}

export async function submitGuess(gameId: string, characterId: string) {
  const db = getDb();
  const record = await getGameRecord(gameId);
  if (!record) {
    throw new Error("游戏不存在。");
  }
  if (record.game.status !== "playing") {
    return getGameState(gameId, true);
  }

  const bank = await getQuestionBankMap();
  const guess = bank.get(characterId);
  const answer = bank.get(record.game.answerCharacterId);
  if (!guess || !answer) {
    throw new Error("当前题库中找不到对应角色。");
  }

  const guessRow = buildGuessRow(guess, answer);
  const isCorrect = characterId === record.game.answerCharacterId;
  const nextProgress = resolveGameProgress(record.game.remainingGuesses, isCorrect);

  await db.insert(gameGuesses).values({
    gameId,
    guessIndex: record.guessesRows.length + 1,
    characterId,
    guessRow,
  });

  await db
    .update(games)
    .set({
      remainingGuesses: nextProgress.remainingGuesses,
      status: nextProgress.status,
      updatedAt: new Date(),
    })
    .where(and(eq(games.id, gameId), eq(games.status, "playing")));

  return getGameState(gameId, true);
}
