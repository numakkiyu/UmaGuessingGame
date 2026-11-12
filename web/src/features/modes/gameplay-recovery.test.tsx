import { chooseSelect } from "@/test/select";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { MemoryRoundPage } from "./memory-round-page";
import { TradeRoundPage } from "./trade-round-page";
import type * as RoundSnapshotModule from "@/lib/api/round-snapshot";
import { TimeoutError } from "@/lib/api/client";
import { changeLanguage } from "@/i18n";
const fixture = vi.hoisted(() => ({ api: { createSubmission: vi.fn(), createTrade: vi.fn(), respondToTrade: vi.fn() }, view: {} as Record<string, unknown>, refresh: vi.fn(), room: { roomId: "room", name: "特雷森游玩会", viewer: { capabilities: ["play"] }, currentRound: { roundId: "round" }, members: [{ membershipId: "me", nickname: "特别周", publicCode: "000123" }, { membershipId: "other", nickname: "特别周", publicCode: "123456" }] } }));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: fixture.api, session: { sessionId: "same-person" } }) }));
vi.mock("@/lib/api/room-queries", () => ({ useRoom: () => ({ data: { room: fixture.room }, refetch: vi.fn() }) }));
vi.mock("@/lib/api/round-snapshot", async original => ({ ...await original<typeof RoundSnapshotModule>(), useRoundSnapshot: () => ({ ...fixture.view, refetch: fixture.refresh, refreshAfterSubmit: fixture.refresh }) }));
vi.mock("@/lib/realtime/use-realtime", () => ({ useRealtime: () => ({ status: "online", transport: "sse" }) }));
vi.mock("@/features/rooms/team-round-control", () => ({ TeamRoundControl: () => null }));
beforeEach(async () => { sessionStorage.clear(); localStorage.clear(); await changeLanguage("zh-Hans"); vi.clearAllMocks(); fixture.refresh.mockResolvedValue(true); });
afterEach(() => cleanup());
function show(component: React.ReactNode) { return render(<MemoryRouter initialEntries={["/rooms/room"]}><Routes><Route path="/rooms/:roomId" element={component}/></Routes></MemoryRouter>); }
function memory() {
  fixture.view = { meta: { phase: "collecting", phaseVersion: 3, deadlineAt: null }, submissionRevision: 6, submissionRevisionsBySlot: { flip: 6 }, controlGeneration: 2, canSubmit: true, raw: { publicState: { cards: [{ cardId: "card-a", position: 0, state: "hidden" }, { cardId: "card-b", position: 1, state: "hidden" }], self: { attempts: 0, flipped: [], matched: [], finished: false } } } };
}
function trade(currentRound = 1) {
  fixture.view = { meta: { roundId: "round", phase: "collecting", phaseVersion: currentRound, deadlineAt: null }, canSubmit: true, raw: { publicState: { currentRound, instances: [{ instanceId: "a", cardId: "speed", label: "速度" }, { instanceId: "b", cardId: "stamina", label: "耐力" }], self: { subjectId: "me", hand: ["a"] }, hands: { me: ["a"], other: ["b"] }, trades: [] } } };
}

it("memory result button confirms the failed flip even after state and controller changed", async () => {
  memory();
  fixture.api.createSubmission.mockRejectedValueOnce(new TimeoutError(10)).mockResolvedValueOnce({ data: { outcome: "accepted", feedback: { resolved: true, matched: true } } });
  const page = show(<MemoryRoundPage />);
  fireEvent.click(screen.getAllByRole("button", { name: /未翻开/ })[0]!);
  await screen.findByRole("button", { name: "查看提交结果" });
  const original = fixture.api.createSubmission.mock.calls[0];
  page.unmount();
  fixture.view = { ...fixture.view, canSubmit: false, submissionRevision: 7, controlGeneration: 3, meta: { phase: "locked", phaseVersion: 4, deadlineAt: null } };
  show(<MemoryRoundPage />);
  fireEvent.click(screen.getByRole("button", { name: "查看提交结果" }));
  await waitFor(() => expect(fixture.api.createSubmission).toHaveBeenCalledTimes(2));
  expect(fixture.api.createSubmission.mock.calls[1]).toEqual(original);
  await waitFor(() => expect(screen.queryByRole("button", { name: "查看提交结果" })).toBeNull());
});

it("trade draft belongs to its current turn; same-nickname recipients show room IDs", async () => {
  trade(); const page = show(<TradeRoundPage />);
  fireEvent.click(screen.getByRole("button", { name: /速度/ }));
  await chooseSelect(screen.getByLabelText("交易对象"),"other");
  fireEvent.click(screen.getByRole("button", { name: /耐力/ }));
  expect(screen.getByRole("button", { name: "发出提案" }).hasAttribute("disabled")).toBe(false);
  trade(2); page.rerender(<MemoryRouter initialEntries={["/rooms/room"]}><Routes><Route path="/rooms/:roomId" element={<TradeRoundPage />}/></Routes></MemoryRouter>);
  expect(screen.getByRole("button", { name: "发出提案" }).hasAttribute("disabled")).toBe(true);
  expect(fixture.api.createTrade).not.toHaveBeenCalled();
});

it("lost trade proposal confirms its original body after refresh instead of using a new turn", async () => {
  trade(); fixture.api.createTrade.mockRejectedValueOnce(new TimeoutError(10)).mockResolvedValueOnce({ data: { tradeId: "trade" } });
  const page = show(<TradeRoundPage />);
  fireEvent.click(screen.getByRole("button", { name: /速度/ }));
  await chooseSelect(screen.getByLabelText("交易对象"),"other");
  fireEvent.click(screen.getByRole("button", { name: /耐力/ }));
  fireEvent.click(screen.getByRole("button", { name: "发出提案" }));
  await screen.findByRole("button", { name: "确认刚才的结果" });
  const original = fixture.api.createTrade.mock.calls[0]; page.unmount();
  trade(2); show(<TradeRoundPage />);
  fireEvent.click(screen.getByRole("button", { name: "确认刚才的结果" }));
  await waitFor(() => expect(fixture.api.createTrade).toHaveBeenCalledTimes(2));
  expect(fixture.api.createTrade.mock.calls[1]).toEqual(original);
});
