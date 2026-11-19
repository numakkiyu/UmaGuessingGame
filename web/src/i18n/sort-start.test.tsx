import { readTrainerProfile, saveTrainerProfile } from "@/lib/trainer-profile";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { SortPlayPage } from "@/features/modes/sort-play-page";
import { NetworkError } from "@/lib/api/client";
import { changeLanguage, LANGUAGES } from ".";
import { entry } from "./entry";
import { sortStart } from "./sort-start";

const fixture = vi.hoisted(() => ({
  ensureSession: vi.fn(), api: { createRoom: vi.fn(), createRound: vi.fn(), createTransition: vi.fn() },
}));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: fixture.api, session: null, ensureSession: fixture.ensureSession }) }));
vi.mock("@/lib/api/queries", () => ({
  useModes: () => ({ data: [{ modeId: "sort", availability: "available", rulesVersion: "test-rules" }] }),
  usePublicConfig: () => ({ data: { modeCapabilities: { sort: { solo: true } } }, isPending: false, isError: false }),
}));
const language = (lng: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lng); });
function show() {
  return render(<MemoryRouter initialEntries={["/start"]}><Routes>
    <Route path="/start" element={<SortPlayPage />} />
    <Route path="/rooms/:id" element={<p>game-opened</p>} />
  </Routes></MemoryRouter>);
}
beforeEach(async () => {
  vi.resetAllMocks(); await changeLanguage("zh-Hans");
  fixture.ensureSession.mockResolvedValue({});
  fixture.api.createRoom.mockResolvedValue({ data: { roomId: "same-room" } });
  fixture.api.createRound.mockResolvedValue({ data: { roundId: "same-round", phaseVersion: 1 } });
  fixture.api.createTransition.mockResolvedValue({ data: { phaseVersion: 2 } });
});
afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });

it("switches the complete start form and rules without discarding the nickname or starting a game", async () => {
  show(); act(() => saveTrainerProfile({ displayName: "周末训练员", avatarId: "1001" }));
  fireEvent.click(screen.getByText(sortStart["zh-Hans"].scoring));
  for (const lng of LANGUAGES) {
    await language(lng);
    expect(readTrainerProfile().displayName).toBe("周末训练员");
    expect(screen.queryByLabelText(entry[lng].start.name)).toBeNull();
    expect(screen.getByRole("heading", { name: sortStart[lng].title })).toBeTruthy();
    expect(screen.getByText(sortStart[lng].rules[0])).toBeTruthy();
    expect(screen.getByRole("button", { name: entry[lng].start.play })).toBeTruthy();
  }
  expect(fixture.ensureSession).not.toHaveBeenCalled(); expect(fixture.api.createRoom).not.toHaveBeenCalled();
});

it("switching language during start keeps the same in-flight game", async () => {
  let finish!: () => void;
  fixture.ensureSession.mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve; }));
  show(); fireEvent.click(screen.getByRole("button", { name: entry["zh-Hans"].start.play }));
  await language("ja"); expect(screen.getByText(entry.ja.start.steps.session)).toBeTruthy();
  expect(fixture.ensureSession).toHaveBeenCalledOnce(); expect(fixture.api.createRoom).not.toHaveBeenCalled();
  await act(async () => { finish(); }); await screen.findByText("game-opened");
  expect(fixture.api.createRoom).toHaveBeenCalledOnce(); expect(fixture.api.createRound).toHaveBeenCalledOnce();
  expect(fixture.api.createTransition).toHaveBeenCalledTimes(2);
});

it("retranslates a failure without retrying or exposing diagnostics", async () => {
  fixture.ensureSession.mockRejectedValueOnce(new NetworkError("private diagnostic", undefined));
  show(); fireEvent.click(screen.getByRole("button", { name: entry["zh-Hans"].start.play }));
  await waitFor(() => expect(screen.getByText("连接中断")).toBeTruthy());
  await language("ja"); expect(screen.getByText("接続が切れました")).toBeTruthy();
  expect(document.body.textContent).not.toContain("private diagnostic");
  expect(fixture.ensureSession).toHaveBeenCalledOnce(); expect(fixture.api.createRoom).not.toHaveBeenCalled();
});
