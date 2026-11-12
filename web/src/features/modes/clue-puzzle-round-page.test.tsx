import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { CluePuzzleRoundPage } from "./clue-puzzle-round-page";
import type * as Snapshot from "@/lib/api/round-snapshot";
import { TimeoutError } from "@/lib/api/client";
import { normalizeCluePuzzleRound } from "@/lib/api/modes-g09-g12";

const fixture = vi.hoisted(() => ({ api: { createSubmission: vi.fn() }, view: {} as Record<string, unknown>, refresh: vi.fn(), room: { roomId: "room", name: "情报游玩会", currentRound: { roundId: "round" } } }));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: fixture.api, session: { sessionId: "person" } }) }));
vi.mock("@/lib/api/room-queries", () => ({ useRoom: () => ({ data: { room: fixture.room } }) }));
vi.mock("@/lib/api/round-snapshot", async original => ({ ...await original<typeof Snapshot>(), useRoundSnapshot: () => ({ ...fixture.view, refetch: fixture.refresh, refreshAfterSubmit: fixture.refresh }) }));
vi.mock("@/lib/realtime/use-realtime", () => ({ useRealtime: () => ({ status: "online", transport: "sse" }) }));
vi.mock("@/features/rooms/team-round-control", () => ({ TeamRoundControl: () => null }));
const assignment = { 1: "a", 2: "b", 3: "c", 4: "d" };
function setup() {
  fixture.view = { meta: { roundId: "round", phase: "collecting", phaseVersion: 3, paused: false }, canSubmit: true, submissionRevision: 0, submissionRevisionsBySlot: {}, controlGeneration: 0, raw: { publicState: { selfIsController: true, cards: ["a", "b", "c", "d"].map((cardId, i) => ({ cardId, label: `档案${i + 1}` })), slots: [1, 2, 3, 4].map(slotIndex => ({ slotIndex, label: `第${slotIndex}位` })), shared: [{ constraintId: "shared", text: "大家的线索" }], privateClues: [{ constraintId: "mine", text: "我的线索" }], self: { subjectId: "team", assignment, submissions: 0 }, constraintCount: 4 } } };
}
function show() { return render(<MemoryRouter initialEntries={["/rooms/room"]}><Routes><Route path="/rooms/:roomId" element={<CluePuzzleRoundPage />} /></Routes></MemoryRouter>); }
beforeEach(() => { localStorage.clear(); sessionStorage.clear(); vi.clearAllMocks(); fixture.refresh.mockResolvedValue(undefined); setup(); });
afterEach(cleanup);

it("reads actual shared clue arrays and clearing the whole draft stays cleared", () => {
  expect(normalizeCluePuzzleRound(fixture.view.raw).sharedClues[0]?.text).toBe("大家的线索");
  show();
  expect(screen.getByText("大家的线索")).toBeTruthy();
  for (const i of [1, 2, 3, 4]) {
    fireEvent.click(screen.getByRole("button", { name: `选择位置：第 ${i} 位` }));
    fireEvent.click(screen.getByRole("button", { name: "清空这个位置" }));
  }
  expect(screen.getAllByText("还没放卡")).toHaveLength(4);
  expect(screen.getByRole("button", { name: "确认这份分配" }).hasAttribute("disabled")).toBe(true);
});

it("moving an assigned card never duplicates it and a new team head invalidates drafts", () => {
  const page = show();
  fireEvent.click(screen.getByRole("button", { name: "选择位置：第 2 位" }));
  fireEvent.click(screen.getAllByRole("button", { name: "选择档案1" })[0]!);
  expect(screen.getByRole("button", { name: "选择位置：第 1 位" }).textContent).toContain("还没放卡");
  expect(screen.getByRole("button", { name: "确认这份分配" }).hasAttribute("disabled")).toBe(true);
  fixture.view = { ...fixture.view, submissionRevision: 1 };
  page.rerender(<MemoryRouter><CluePuzzleRoundPage /></MemoryRouter>);
  expect(screen.getByRole("button", { name: "选择位置：第 1 位" }).textContent).toContain("档案1");
  expect(screen.getByRole("button", { name: "确认这份分配" }).hasAttribute("disabled")).toBe(false);
});

it("confirms the original assignment after refresh and representative/phase changes", async () => {
  fixture.api.createSubmission.mockRejectedValueOnce(new TimeoutError(10)).mockResolvedValueOnce({ data: { outcome: "accepted" } });
  const page = show(); fireEvent.click(screen.getByRole("button", { name: "确认这份分配" }));
  await screen.findByRole("button", { name: "确认刚才的分配" });
  const original = fixture.api.createSubmission.mock.calls[0];
  expect(screen.getByRole("button", { name: "选择位置：第 1 位" }).hasAttribute("disabled")).toBe(true);
  page.unmount();
  fixture.view = { ...fixture.view, meta: { roundId: "round", phase: "locked", phaseVersion: 4 }, canSubmit: false, controlGeneration: 1, submissionRevision: 1 };
  show(); fireEvent.click(screen.getByRole("button", { name: "确认刚才的分配" }));
  await waitFor(() => expect(fixture.api.createSubmission).toHaveBeenCalledTimes(2));
  expect(fixture.api.createSubmission.mock.calls[1]).toEqual(original);
  await waitFor(() => expect(screen.queryByRole("button", { name: "确认刚才的分配" })).toBeNull());
});

it("paused and read-only views cannot edit or submit", () => {
  fixture.view = { ...fixture.view, meta: { roundId: "round", phase: "collecting", phaseVersion: 3, paused: true } };
  show();expect(screen.getByRole("button", { name: "选择位置：第 1 位" }).hasAttribute("disabled")).toBe(true);
  expect(screen.getByRole("button", { name: "确认这份分配" }).hasAttribute("disabled")).toBe(true);
});

it("renders only announced team scores and never marks an alternative solution wrong", () => {
  fixture.view = { ...fixture.view, meta: { roundId: "round", phase: "settled", phaseVersion: 6 }, canSubmit: false, raw: { round: {phase:"settled"}, publicState: { ...(fixture.view.raw as { publicState: object }).publicState, answer: { solutionCount: 2, slots: [{ slotIndex: 1, cardId: "b", label: "档案2" }], constraints: [] } }, subjectNames: { red: "胡萝卜队", blue: "冲刺队" }, results: [{ subjectId: "red", scoreUnits: 10000, rank: 1 }, { subjectId: "blue", scoreUnits: 5000, rank: 2 }] } };
  show();expect(screen.getByRole("heading", { name: "本局成绩" })).toBeTruthy();
  expect(screen.getAllByText("100 分")).toHaveLength(2);
  expect(screen.getByRole("heading", { name: "一种可行的排列" })).toBeTruthy();
  expect(screen.queryByText("你猜对了")).toBeNull();
  expect(screen.queryByRole("button", { name: "确认这份分配" })).toBeNull();
});
