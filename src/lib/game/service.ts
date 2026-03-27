import crypto from "node:crypto";
import { and, asc, eq } from "drizzle-orm";
import { getServerConfig } from "@/config/server";
import { getDb } from "@/lib/db/client";
import { gameGuesses, games } from "@/lib/db/schema";
import { buildGuessRow } from "@/lib/game/compare";
import { isInfrastructureConnectionError } from "@/lib/game/errors";
import {
  appendLocalGuess,
  createLocalGame,
  getLocalGameRecord,
  updateLocalGameProgress,
} from "@/lib/game/local-store";
import { loadQuestionBankReady } from "@/lib/question-bank";
import { gameStateSchema, type GuessRow } from "@/lib/validation/schemas";

function randomItem<T>(items: T[]) {
  return items[Math.floor(Math.random() * items.length)];
}

const ROOM_CODE_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";
const ROOM_CODE_LENGTH = 8;

let didWarnAutoFallback = false;

function canAutoFallbackToLocalStore() {
  const config = getServerConfig();
  return config.localGameStoreEnabled || config.appEnv !== "production";
}

function warnAutoFallback(reason: unknown) {
  if (didWarnAutoFallback) {
    return;
  }

  didWarnAutoFallback = true;
  const message = reason instanceof Error ? reason.message : String(reason);
  console.warn(`[game] Falling back to local store: ${message}`);
}

async function getQuestionBankMap() {
  const entries = await loadQuestionBankReady();
  return new Map(entries.map((entry) => [entry.id, entry]));
}

function createRoomCode() {
  let roomCode = "";
  for (let index = 0; index < ROOM_CODE_LENGTH; index += 1) {
    const nextIndex = crypto.randomInt(0, ROOM_CODE_ALPHABET.length);
    roomCode += ROOM_CODE_ALPHABET[nextIndex];
  }
  return roomCode;
}

async function createUniqueRoomCode() {
  for (let attempt = 0; attempt < 12; attempt += 1) {
    const roomCode = createRoomCode();
    const existing = await getGameRecord(roomCode);
    if (!existing) {
      return roomCode;
    }
  }

  throw new Error("这会儿还没法安排新房间，稍后再试一次吧。");
}

async function getGameRecord(gameId: string) {
  if (getServerConfig().localGameStoreEnabled) {
    return getLocalGameRecord(gameId);
  }

  try {
    const db = getDb();
    const [game] = await db.select().from(games).where(eq(games.id, gameId)).limit(1);
    if (!game) return null;
    const guessesRows = await db
      .select()
      .from(gameGuesses)
      .where(eq(gameGuesses.gameId, gameId))
      .orderBy(asc(gameGuesses.guessIndex));

    return { game, guessesRows };
  } catch (error) {
    if (!canAutoFallbackToLocalStore() || !isInfrastructureConnectionError(error)) {
      throw error;
    }

    warnAutoFallback(error);
    return getLocalGameRecord(gameId);
  }
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

function shouldRevealAnswer(status: "playing" | "won" | "lost" | "ended", includeAnswer: boolean) {
  if (includeAnswer) {
    return true;
  }

  return status === "won" || status === "lost";
}

export async function createNewGame() {
  const bank = await loadQuestionBankReady();
  if (bank.length === 0) {
    throw new Error("question_bank_ready.json 为空，当前无法创建新局。");
  }

  const answer = randomItem(bank);
  const gameId = await createUniqueRoomCode();
  const now = new Date();
  if (getServerConfig().localGameStoreEnabled) {
    await createLocalGame({
      id: gameId,
      answerCharacterId: answer.id,
      remainingGuesses: getServerConfig().maxGuesses,
      maxGuesses: getServerConfig().maxGuesses,
    });
  } else {
    try {
      const db = getDb();
      await db.insert(games).values({
        id: gameId,
        answerCharacterId: answer.id,
        status: "playing",
        remainingGuesses: getServerConfig().maxGuesses,
        maxGuesses: getServerConfig().maxGuesses,
        createdAt: now,
        updatedAt: now,
      });
    } catch (error) {
      if (!canAutoFallbackToLocalStore() || !isInfrastructureConnectionError(error)) {
        throw error;
      }

      warnAutoFallback(error);
      await createLocalGame({
        id: gameId,
        answerCharacterId: answer.id,
        remainingGuesses: getServerConfig().maxGuesses,
        maxGuesses: getServerConfig().maxGuesses,
      });
    }
  }

  return {
    gameId,
    roomCode: gameId,
    status: "playing" as const,
    remainingGuesses: getServerConfig().maxGuesses,
    guessRows: [],
    startedAt: now.toISOString(),
    finishedAt: null,
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
  const revealAnswer = shouldRevealAnswer(
    record.game.status as "playing" | "won" | "lost" | "ended",
    includeAnswer,
  );
  return gameStateSchema.parse({
    gameId: record.game.id,
    roomCode: record.game.id,
    status: record.game.status,
    remainingGuesses: record.game.remainingGuesses,
    guessRows,
    startedAt: new Date(record.game.createdAt).toISOString(),
    finishedAt:
      record.game.status === "playing"
        ? null
        : new Date(record.game.updatedAt).toISOString(),
    updatedAt: new Date(record.game.updatedAt).toISOString(),
    answerCharacterId: revealAnswer ? record.game.answerCharacterId : null,
    answerDisplayName: revealAnswer ? answer?.name_zh ?? null : null,
  });
}

export async function submitGuess(gameId: string, characterId: string) {
  const record = await getGameRecord(gameId);
  if (!record) {
    throw new Error("游戏不存在。");
  }
  if (record.game.status !== "playing") {
    return getGameState(gameId, record.game.status !== "ended");
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

  if (getServerConfig().localGameStoreEnabled) {
    await appendLocalGuess({
      gameId,
      guessIndex: record.guessesRows.length + 1,
      characterId,
      guessRow,
    });
    await updateLocalGameProgress({
      gameId,
      remainingGuesses: nextProgress.remainingGuesses,
      status: nextProgress.status,
    });
  } else {
    try {
      const db = getDb();
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
    } catch (error) {
      if (!canAutoFallbackToLocalStore() || !isInfrastructureConnectionError(error)) {
        throw error;
      }

      warnAutoFallback(error);
      await appendLocalGuess({
        gameId,
        guessIndex: record.guessesRows.length + 1,
        characterId,
        guessRow,
      });
      await updateLocalGameProgress({
        gameId,
        remainingGuesses: nextProgress.remainingGuesses,
        status: nextProgress.status,
      });
    }
  }

  return getGameState(gameId, nextProgress.status !== "playing");
}

export async function endGame(gameId: string) {
  const record = await getGameRecord(gameId);
  if (!record) {
    throw new Error("游戏不存在。");
  }

  if (record.game.status !== "playing") {
    return getGameState(gameId, record.game.status !== "ended");
  }

  if (getServerConfig().localGameStoreEnabled) {
    await updateLocalGameProgress({
      gameId,
      remainingGuesses: record.game.remainingGuesses,
      status: "ended",
    });
  } else {
    try {
      const db = getDb();
      await db
        .update(games)
        .set({
          status: "ended",
          updatedAt: new Date(),
        })
        .where(and(eq(games.id, gameId), eq(games.status, "playing")));
    } catch (error) {
      if (!canAutoFallbackToLocalStore() || !isInfrastructureConnectionError(error)) {
        throw error;
      }

      warnAutoFallback(error);
      await updateLocalGameProgress({
        gameId,
        remainingGuesses: record.game.remainingGuesses,
        status: "ended",
      });
    }
  }

  return getGameState(gameId);
}
