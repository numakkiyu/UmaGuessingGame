import { promises as fs } from "node:fs";
import path from "node:path";
import type { GuessRow } from "@/lib/validation/schemas";

type StoredGame = {
  id: string;
  answerCharacterId: string;
  status: "playing" | "won" | "lost" | "ended";
  remainingGuesses: number;
  maxGuesses: number;
  createdAt: string;
  updatedAt: string;
};

type StoredGuess = {
  gameId: string;
  guessIndex: number;
  characterId: string;
  guessRow: GuessRow;
  createdAt: string;
};

type LocalGameStore = {
  games: StoredGame[];
  guesses: StoredGuess[];
};

const storePath = path.join(
  process.cwd(),
  "data",
  "runtime",
  "local-game-store.json",
);

async function readStore(): Promise<LocalGameStore> {
  try {
    const raw = await fs.readFile(storePath, "utf8");
    return JSON.parse(raw) as LocalGameStore;
  } catch {
    return { games: [], guesses: [] };
  }
}

async function writeStore(store: LocalGameStore) {
  await fs.mkdir(path.dirname(storePath), { recursive: true });
  await fs.writeFile(storePath, `${JSON.stringify(store, null, 2)}\n`, "utf8");
}

export async function createLocalGame(input: {
  id: string;
  answerCharacterId: string;
  remainingGuesses: number;
  maxGuesses: number;
}) {
  const now = new Date().toISOString();
  const store = await readStore();
  store.games = store.games.filter((game) => game.id !== input.id);
  store.games.push({
    id: input.id,
    answerCharacterId: input.answerCharacterId,
    status: "playing",
    remainingGuesses: input.remainingGuesses,
    maxGuesses: input.maxGuesses,
    createdAt: now,
    updatedAt: now,
  });
  await writeStore(store);
}

export async function getLocalGameRecord(gameId: string) {
  const store = await readStore();
  const game = store.games.find((item) => item.id === gameId) ?? null;
  if (!game) {
    return null;
  }

  const guessesRows = store.guesses
    .filter((item) => item.gameId === gameId)
    .sort((left, right) => left.guessIndex - right.guessIndex);

  return { game, guessesRows };
}

export async function appendLocalGuess(input: {
  gameId: string;
  characterId: string;
  guessIndex: number;
  guessRow: GuessRow;
}) {
  const store = await readStore();
  store.guesses.push({
    gameId: input.gameId,
    characterId: input.characterId,
    guessIndex: input.guessIndex,
    guessRow: input.guessRow,
    createdAt: new Date().toISOString(),
  });
  await writeStore(store);
}

export async function updateLocalGameProgress(input: {
  gameId: string;
  remainingGuesses: number;
  status: "playing" | "won" | "lost" | "ended";
}) {
  const store = await readStore();
  store.games = store.games.map((game) =>
    game.id === input.gameId
      ? {
          ...game,
          remainingGuesses: input.remainingGuesses,
          status: input.status,
          updatedAt: new Date().toISOString(),
        }
      : game,
  );
  await writeStore(store);
}
