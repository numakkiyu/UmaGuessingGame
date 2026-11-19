import { describe, expect, it } from "vitest";
import { canAnimateRoute } from "./route-curtain-policy";

describe("route curtain boundaries", () => {
  it.each([
    ["/", "/help"], ["/friends", "/rooms/new"], ["/", "/events/new"],
    ["/play/classic", "/practice/local-123"], ["/practice/local-123", "/"],
    ["/rooms/rom_123", "/friends"], ["/practice/sort/local-123", "/play/sort"],
  ])("animates page navigation %s → %s", (from, to) => {
    expect(canAnimateRoute(from, to)).toBe(true);
  });
  it.each([
    ["/", "/"], ["/matchmaking", "/rooms/rom_123"], ["/join", "/rooms/rom_123"],
    ["/play/sort", "/practice/sort/local-123"], ["/play/memory", "/practice/memory/local-123"],
    ["/events/new", "/events/evt_123/host"], ["/", "/challenges/chl_123"],
  ])("does not hide an active game %s → %s", (from, to) => {
    expect(canAnimateRoute(from, to)).toBe(false);
  });
});
