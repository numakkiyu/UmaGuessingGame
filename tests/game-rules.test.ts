import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { readPublicGameState } from "../src/app/api/game/[id]/route";
import { resolveGameProgress } from "../src/lib/game/service";
import type { getGameState } from "../src/lib/game/service";

describe("game rules", () => {
  it("does not force answer reveal while recovering an active game", async () => {
    const calls: Array<[string, boolean?]> = [];
    const reader: typeof getGameState = (async (gameId: string, includeAnswer?: boolean) => {
      calls.push([gameId, includeAnswer]);
      return {
        gameId,
        roomCode: gameId,
        status: "playing",
        remainingGuesses: 6,
        guessRows: [],
        answerCharacterId: null,
        answerDisplayName: null,
      };
    }) as typeof getGameState;

    await readPublicGameState("game_active", reader);

    assert.deepEqual(calls, [["game_active", undefined]]);
  });

  it("shows an ended state instead of exposing the finished answer in public recovery", async () => {
    const reader: typeof getGameState = (async (gameId: string) => ({
      gameId,
      roomCode: gameId,
      status: "won",
      remainingGuesses: 4,
      guessRows: [],
      answerCharacterId: "answer",
      answerDisplayName: "特别周",
    })) as typeof getGameState;

    const state = await readPublicGameState("game_done", reader);

    assert.equal(state.status, "ended");
    assert.equal(state.answerCharacterId, null);
    assert.equal(state.answerDisplayName, null);
  });

  it("stops decreasing remaining guesses after a correct guess", () => {
    assert.deepEqual(resolveGameProgress(5, true), {
      remainingGuesses: 5,
      status: "won",
    });
  });

  it("marks the game as lost only after the last wrong guess", () => {
    assert.deepEqual(resolveGameProgress(1, false), {
      remainingGuesses: 0,
      status: "lost",
    });
  });
});
