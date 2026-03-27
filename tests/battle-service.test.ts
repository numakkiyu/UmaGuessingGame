import assert from "node:assert/strict";
import { afterEach, beforeEach, describe, it } from "node:test";
import { loadQuestionBankReady } from "../src/lib/question-bank";

process.env.DATABASE_URL ??= "postgresql://postgres:postgres@localhost:5432/umaguessinggame";
process.env.REDIS_URL ??= "redis://localhost:6379";

let __peekBattleRoomForTests: typeof import("../src/lib/battle/service").__peekBattleRoomForTests;
let __resetBattleStoreForTests: typeof import("../src/lib/battle/service").__resetBattleStoreForTests;
let confirmMatchmaking: typeof import("../src/lib/battle/service").confirmMatchmaking;
let createFriendRoom: typeof import("../src/lib/battle/service").createFriendRoom;
let getMatchmakingStatus: typeof import("../src/lib/battle/service").getMatchmakingStatus;
let joinFriendRoom: typeof import("../src/lib/battle/service").joinFriendRoom;
let joinMatchmaking: typeof import("../src/lib/battle/service").joinMatchmaking;
let performRoomAction: typeof import("../src/lib/battle/service").performRoomAction;

describe("battle service", () => {
  beforeEach(async () => {
    if (!__resetBattleStoreForTests) {
      ({
        __peekBattleRoomForTests,
        __resetBattleStoreForTests,
        confirmMatchmaking,
        createFriendRoom,
        getMatchmakingStatus,
        joinFriendRoom,
        joinMatchmaking,
        performRoomAction,
      } = await import("../src/lib/battle/service"));
    }

    await __resetBattleStoreForTests();
  });

  afterEach(async () => {
    await __resetBattleStoreForTests();
  });

  it("starts a matchmaking room after both players confirm", async () => {
    const first = await joinMatchmaking("player_a", 60);
    const second = await joinMatchmaking("player_b", 75);

    const firstStatus = await getMatchmakingStatus("player_a", first.ticketId);
    const secondStatus = await getMatchmakingStatus("player_b", second.ticketId);

    assert.equal(firstStatus.status, "matched_pending_accept");
    assert.equal(secondStatus.status, "matched_pending_accept");
    assert.ok(firstStatus.roomCode);

    await confirmMatchmaking("player_a", first.ticketId);
    const confirmed = await confirmMatchmaking("player_b", second.ticketId);

    assert.equal(confirmed.status, "playing");
    assert.ok(confirmed.roomCode);
  });

  it("applies surrender immediately and marks the surrendering player as defeated", async () => {
    const hostState = await createFriendRoom("host_player");
    await joinFriendRoom("guest_player", hostState.roomCode);
    await performRoomAction(hostState.roomCode, "host_player", { type: "confirm-start" });
    await performRoomAction(hostState.roomCode, "guest_player", { type: "confirm-start" });

    const finished = await performRoomAction(hostState.roomCode, "guest_player", {
      type: "surrender",
    });

    assert.equal(finished.status, "finished");
    assert.equal(finished.winnerSeat, "A");
    assert.equal(finished.loserSeat, "B");
    assert.equal(finished.self.result, "surrendered");
  });

  it("allows only one successful pause in the same round", async () => {
    const hostState = await createFriendRoom("host_pause");
    await joinFriendRoom("guest_pause", hostState.roomCode);
    await performRoomAction(hostState.roomCode, "host_pause", { type: "confirm-start" });
    await performRoomAction(hostState.roomCode, "guest_pause", { type: "confirm-start" });

    await performRoomAction(hostState.roomCode, "host_pause", { type: "request-pause" });
    const paused = await performRoomAction(hostState.roomCode, "guest_pause", {
      type: "respond-pause",
      accept: true,
    });

    assert.equal(paused.status, "paused");
    assert.equal(paused.pauseUsed, true);

    await performRoomAction(hostState.roomCode, "host_pause", { type: "resume-now" });

    await assert.rejects(
      () => performRoomAction(hostState.roomCode, "host_pause", { type: "request-pause" }),
      /暂停已经用过了/,
    );
  });

  it("refreshes a new round inside the same room after both players request it", async () => {
    const questionBank = await loadQuestionBankReady();
    assert.ok(questionBank.length > 1);

    const hostState = await createFriendRoom("host_rematch");
    await joinFriendRoom("guest_rematch", hostState.roomCode);
    await performRoomAction(hostState.roomCode, "host_rematch", { type: "confirm-start" });
    await performRoomAction(hostState.roomCode, "guest_rematch", { type: "confirm-start" });

    const activeRoom = await __peekBattleRoomForTests(hostState.roomCode);
    assert.ok(activeRoom?.answerCharacterId);

    const answerId = activeRoom!.answerCharacterId!;
    const result = await performRoomAction(hostState.roomCode, "host_rematch", {
      type: "guess",
      characterId: answerId,
    });

    assert.equal(result.status, "finished");
    assert.equal(result.self.result, "won");

    await performRoomAction(hostState.roomCode, "host_rematch", { type: "request-rematch" });
    const nextRound = await performRoomAction(hostState.roomCode, "guest_rematch", {
      type: "request-rematch",
    });

    assert.equal(nextRound.status, "playing");
    assert.equal(nextRound.roundNumber, 2);
    assert.equal(nextRound.self.guessRows.length, 0);
    assert.equal(nextRound.self.remainingGuesses, 8);
    assert.equal(nextRound.answerCharacterId, null);
  });
});
