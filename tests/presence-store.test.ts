import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { heartbeatRoomPresence } from "../src/lib/realtime/presence-store";

describe("room presence store", () => {
  it("counts active spectators and tracks average latency", async () => {
    const roomCode = `ROOM1234-${Date.now()}`;

    await heartbeatRoomPresence({
      roomCode,
      sessionId: "host-1",
      role: "host",
      latencyMs: 40,
    });

    await heartbeatRoomPresence({
      roomCode,
      sessionId: "viewer-1",
      role: "spectator",
      latencyMs: 180,
    });

    const summary = await heartbeatRoomPresence({
      roomCode,
      sessionId: "viewer-2",
      role: "spectator",
      latencyMs: 420,
    });

    assert.equal(summary.hostOnline, true);
    assert.equal(summary.spectatorCount, 2);
    assert.equal(summary.averageLatencyMs, 300);
  });
});
