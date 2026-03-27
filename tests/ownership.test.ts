import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { buildOwnerCookieValue, canEditRoom } from "../src/lib/game/ownership";

describe("room ownership", () => {
  it("accepts the matching owner cookie", () => {
    const roomCode = "ABCD1234";
    const cookieValue = buildOwnerCookieValue(roomCode, "test-signing-secret");

    assert.equal(canEditRoom(roomCode, cookieValue, "test-signing-secret"), true);
  });

  it("rejects a missing or mismatched owner cookie", () => {
    assert.equal(canEditRoom("ABCD1234", null, "test-signing-secret"), false);
    assert.equal(
      canEditRoom("ABCD1234", buildOwnerCookieValue("WXYZ5678", "test-signing-secret"), "test-signing-secret"),
      false,
    );
  });
});
