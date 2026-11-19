import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { changeLanguage } from "@/i18n";
import type * as Snapshot from "@/lib/api/round-snapshot";
import { DraftRoundPage } from "./draft-round-page";
import { AptitudeGridRoundPage } from "./aptitude-grid-round-page";
import { AudioRoundPage } from "./audio-round-page";
import { PedigreeRoundPage } from "./pedigree-round-page";
import { BluffRoundPage } from "./bluff-round-page";
import { RaceRebuildRoundPage } from "./race-rebuild-round-page";
import { SortRoundPage } from "./sort-round-page";
import { StrategyRaceRoundPage } from "./strategy-race-round-page";
import { CoRacePathRoundPage } from "./co-race-path-round-page";
import { gridOnlineCopy } from "@/i18n/grid-online";
import { sortOnlineCopy } from "@/i18n/sort-online";
import { draftOnlineCopy } from "@/i18n/draft-online";
import { audioOnlineCopy } from "@/i18n/audio-online";
import { pedigreeOnlineCopy } from "@/i18n/pedigree-online";
import { bluffOnlineCopy } from "@/i18n/bluff-online";
import { raceOnlineCopy } from "@/i18n/race-online";
import { strategyOnlineCopy } from "@/i18n/strategy-online";
import { pathOnlineCopy } from "@/i18n/path-online";

const f = vi.hoisted(() => ({ api: { createSubmission: vi.fn(), getRound: vi.fn() }, refresh: vi.fn() }));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: f.api, session: { sessionId: "session" } }) }));
vi.mock("@/lib/api/queries", () => ({ useCharacters: () => ({ data: { items: [] } }) }));
vi.mock("@/lib/api/room-queries", () => ({ useRoom: () => ({ data: { room: { roomId: "room", currentRound: { roundId: "round" } } }, refetch: f.refresh }) }));
vi.mock("@/lib/api/round-snapshot", async original => ({ ...await original<typeof Snapshot>(), useRoundSnapshot: () => ({ raw: null, meta: null, submissionRevision: 0, controlGeneration: 0, canSubmit: false, isLoading: true, refetch: f.refresh, refreshAfterSubmit: f.refresh }) }));
vi.mock("@/lib/realtime/use-realtime", () => ({ useRealtime: () => ({ status: "online" }) }));
vi.mock("@/features/rooms/team-round-control", () => ({ TeamRoundControl: () => null }));
beforeEach(async () => {
  vi.clearAllMocks(); sessionStorage.clear(); localStorage.clear(); await changeLanguage("zh-Hans");
  f.api.getRound.mockImplementation(() => new Promise(() => {}));
  f.api.createSubmission.mockResolvedValue({ data: { outcome: "accepted" } });
  f.refresh.mockResolvedValue(true);
});
afterEach(cleanup);

const cases = [
  { scope: "draft", Component: DraftRoundPage, action: { kind: "draft.lineup", assignments: [1,2,3,4].map(i => ({slotId:`t${i}`,characterId:`c${i}`})) }, copy: draftOnlineCopy },
  { scope: "grid", Component: AptitudeGridRoundPage, action: { kind: "grid.claim", cellId: "cell", characterId: "c" }, copy: gridOnlineCopy },
  { scope: "audio", Component: AudioRoundPage, action: { kind: "audio.ready", readyClipIds: ["a","b"], playbackFailures: [] }, copy: audioOnlineCopy },
  { scope: "pedigree", Component: PedigreeRoundPage, action: { kind: "pedigree.assignment", assignments: [{slotId:"sire",horseId:"horse"}] }, copy: pedigreeOnlineCopy },
  { scope: "bluff", Component: BluffRoundPage, action: { kind: "bluff.decoy", text: "测试答案" }, copy: bluffOnlineCopy },
  { scope: "race", Component: RaceRebuildRoundPage, action: { kind: "race.order", groups: [["a"],["b"],["c"],["d"]] }, copy: raceOnlineCopy },
  { scope: "sort", Component: SortRoundPage, action: { kind: "sort.order", groups: [["a"],["b"]] }, copy: sortOnlineCopy },
  { scope: "strategy", Component: StrategyRaceRoundPage, action: { kind: "strategy.action", turn: 1, action: "steady" }, copy: strategyOnlineCopy },
  { scope: "path", Component: CoRacePathRoundPage, action: { kind: "path.route", characterIds: ["a","b"] }, copy: pathOnlineCopy },
];
for (const {scope, Component, action, copy} of cases) {
  it(`${scope}: confirms the saved command before the question finishes loading`, async () => {
    const key = "a".repeat(48);
    const body = { phaseVersion: 3, submissionRevision: 0, controlGeneration: 0, modeAction: action };
    sessionStorage.setItem(`uma:game-command:session:round:${scope}`, JSON.stringify({key,body,createdAt:Date.now()}));
    render(<MemoryRouter initialEntries={["/rooms/room"]}><Routes><Route path="/rooms/:roomId" element={<Component/>}/></Routes></MemoryRouter>);
    fireEvent.click(await screen.findByRole("button", { name: copy["zh-Hans"].confirmAction }));
    await waitFor(() => expect(f.api.createSubmission).toHaveBeenCalledExactlyOnceWith("round", body, key));
  });
}
