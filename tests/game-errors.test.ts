import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  isInfrastructureConnectionError,
  toPlayerFacingGameError,
} from "../src/lib/game/errors";

describe("game error mapping", () => {
  it("treats connection timeout as infrastructure failure", () => {
    assert.equal(
      isInfrastructureConnectionError(new Error("Connection timeout")),
      true,
    );
  });

  it("returns player-friendly text for infrastructure failures", () => {
    assert.equal(
      toPlayerFacingGameError(new Error("connect ETIMEDOUT 127.0.0.1:6379"), "fallback"),
      "这会儿还没法开始，稍后再试一次吧。",
    );
  });
});
