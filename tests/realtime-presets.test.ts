import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  defaultRoomSyncPreset,
  resolveRoomSyncPollInterval,
} from "../src/lib/realtime/presets";

describe("room sync presets", () => {
  it("uses balanced as the default viewing rhythm", () => {
    assert.equal(defaultRoomSyncPreset, "balanced");
  });

  it("speeds up the live preset", () => {
    assert.equal(resolveRoomSyncPollInterval(2500, "live"), 1200);
    assert.equal(resolveRoomSyncPollInterval(900, "live"), 900);
  });

  it("slows down the save-data preset", () => {
    assert.equal(resolveRoomSyncPollInterval(2500, "save-data"), 6000);
    assert.equal(resolveRoomSyncPollInterval(7000, "save-data"), 7000);
  });

  it("keeps the balanced preset unchanged", () => {
    assert.equal(resolveRoomSyncPollInterval(2500, "balanced"), 2500);
  });
});

