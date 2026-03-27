import assert from "node:assert/strict";
import { describe, it } from "node:test";

process.env.APP_SIGNING_SECRET ??= "test-signing-secret";
process.env.DATABASE_URL ??= "postgresql://postgres:postgres@localhost:5432/umaguessinggame";
process.env.REDIS_URL ??= "redis://localhost:6379";

describe("game auth", () => {
  it("accepts a viewer token for the matching room and session", async () => {
    const { buildSessionCookieValue } = await import("../src/lib/auth/session");
    const { buildGameViewerToken, canEditGameFromHeaders } = await import("../src/lib/game/auth");

    const sessionId = "sess_testviewer";
    const request = new Request("http://localhost:3000/api/game/new", {
      headers: {
        "user-agent": "test-agent",
        "accept-language": "zh-CN",
      },
    });
    const viewerToken = buildGameViewerToken("ROOM1234", request, sessionId);
    const sessionCookie = buildSessionCookieValue(sessionId);

    const viewer = canEditGameFromHeaders({
      roomCode: "ROOM1234",
      headers: request.headers,
      sessionCookie: sessionId,
      viewerCookie: viewerToken,
    });

    assert.ok(viewer);
    assert.equal(viewer?.subjectId, "ROOM1234");
    assert.equal(sessionCookie.startsWith(`${sessionId}.`), true);
  });

  it("rejects a viewer token when the room does not match", async () => {
    const { buildGameViewerToken, canEditGameFromHeaders } = await import("../src/lib/game/auth");

    const request = new Request("http://localhost:3000/api/game/new", {
      headers: {
        "user-agent": "test-agent",
        "accept-language": "zh-CN",
      },
    });
    const viewerToken = buildGameViewerToken("ROOM1234", request, "sess_testviewer");

    const viewer = canEditGameFromHeaders({
      roomCode: "ROOM5678",
      headers: request.headers,
      sessionCookie: "sess_testviewer",
      viewerCookie: viewerToken,
    });

    assert.equal(viewer, null);
  });
});
