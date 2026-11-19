import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { RoomLobbyView } from "@/features/rooms/room-page";
import { StrategyRacePlayPage } from "@/features/modes/mode-play-pages";
import { changeLanguage, LANGUAGES } from ".";
import { modes } from "./modes";
import { modeRules } from "./mode-rules";
import type { RoomSnapshot } from "@/lib/api/types";

const fixture = vi.hoisted(() => ({
  modeId: "classic", api: { createRound: vi.fn(), createRoom: vi.fn() },
}));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: fixture.api, session: null, ensureSession: vi.fn() }) }));
vi.mock("@/lib/api/room-queries", () => ({ useRoom: () => ({
  data: { room: { roomId: "room-kept", modeId: fixture.modeId, kind: "friend", status: "lobby", name: "週末", viewer: { capabilities: ["host"], membershipId: "me" }, members: [], teams: [] } as unknown as RoomSnapshot },
  isLoading: false, isError: false,
}) }));
vi.mock("@/lib/api/queries", () => ({
  useModes: () => ({ data: [{ modeId: "strategy-race", availability: "available", minPlayers: 1 }] }),
  usePublicConfig: () => ({ data: { modeCapabilities: { "strategy-race": { solo: true, friend: true } } } }),
}));
vi.mock("@/lib/realtime/use-realtime", () => ({ useRealtime: () => ({ status: "online" }) }));
vi.mock("@/features/rooms/room-management", () => ({ RoomManagement: () => null }));
vi.mock("@/features/rooms/team-panel", () => ({ TeamPanel: () => null }));
beforeEach(() => { vi.clearAllMocks(); fixture.modeId = "classic"; });
afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });

it("room heading and start guidance use the same translated game name as the lobby", async () => {
  render(<MemoryRouter><RoomLobbyView roomId="room-kept" /></MemoryRouter>);
  for (const lng of LANGUAGES) {
    await act(async () => { await changeLanguage(lng); });
    expect(document.body.textContent!.split(modes[lng].classic!.name)).toHaveLength(3);
    if (lng !== "zh-Hans") expect(document.body.textContent).not.toContain("经典弗一把");
  }
  expect(fixture.api.createRound).not.toHaveBeenCalled();
});

it("an unknown mode never exposes its server identifier", async () => {
  fixture.modeId = "future-private-mode";
  render(<MemoryRouter><RoomLobbyView roomId="room-kept" /></MemoryRouter>);
  for (const lng of LANGUAGES) {
    await act(async () => { await changeLanguage(lng); });
    expect(document.body.textContent).not.toContain("future-private-mode");
  }
});

it("the strategy practice entry explains its scoring without starting a game on language change", async () => {
  render(<MemoryRouter><StrategyRacePlayPage /></MemoryRouter>);
  for (const lng of LANGUAGES) {
    await act(async () => { await changeLanguage(lng); });
    expect(screen.getByText(modeRules[lng]["strategy-race"].practiceSummary)).toBeTruthy();
  }
  expect(fixture.api.createRoom).not.toHaveBeenCalled();
});
