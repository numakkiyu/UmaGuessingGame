import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { resolveSecureAuthCookie } from "../src/lib/auth/session";

process.env.DATABASE_URL ??= "postgresql://postgres:postgres@localhost:5432/umaguessinggame";
process.env.REDIS_URL ??= "redis://localhost:6379";

describe("battle request", () => {
  it("does not force secure cookies for localhost production URLs", () => {
    assert.equal(
      resolveSecureAuthCookie("production", "http://127.0.0.1:3000"),
      false,
    );
    assert.equal(
      resolveSecureAuthCookie("production", "http://localhost:3000"),
      false,
    );
  });

  it("keeps secure cookies for https production traffic", () => {
    assert.equal(
      resolveSecureAuthCookie("production", "https://uma.example.com"),
      true,
    );
    assert.equal(
      resolveSecureAuthCookie("production", "http://internal-service", "https"),
      true,
    );
  });

  it("keeps cookies non-secure outside production", () => {
    assert.equal(resolveSecureAuthCookie("development", "https://uma.example.com"), false);
  });

  it("accepts a battle viewer token from the query string before cookies settle", async () => {
    const { buildBattleViewerToken } = await import("../src/lib/battle/identity");
    const { getBattlePlayerFromRequest } = await import("../src/lib/battle/request");

    const seedRequest = new Request("http://localhost:3000/api/rooms/create", {
      headers: {
        "user-agent": "test-agent",
        "accept-language": "zh-CN",
      },
    });
    const viewerToken = buildBattleViewerToken("player_query", seedRequest, "sess_query");

    const roomRequest = new Request(
      `http://localhost:3000/api/rooms/ROOM1234?viewerToken=${encodeURIComponent(viewerToken)}`,
      {
        headers: {
          "user-agent": "test-agent",
          "accept-language": "zh-CN",
        },
      },
    );

    assert.equal(getBattlePlayerFromRequest(roomRequest), "player_query");
  });
});
