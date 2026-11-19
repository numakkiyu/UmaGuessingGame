import { readTrainerProfile, saveTrainerProfile } from "@/lib/trainer-profile";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { ClassicOnlinePlayPage } from "@/features/classic/classic-online-play-page";
import { NetworkError } from "@/lib/api/client";
import { changeLanguage, LANGUAGES } from ".";
import { classicStart } from "./classic-start";

const fixture = vi.hoisted(() => ({
  ensureSession: vi.fn(), api: { createRoom: vi.fn(), createRound: vi.fn(), createTransition: vi.fn() },
}));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: fixture.api, session: null, ensureSession: fixture.ensureSession }) }));
const language = (lng: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lng); });
function show() {
  return render(<MemoryRouter initialEntries={["/start"]}><Routes>
    <Route path="/start" element={<ClassicOnlinePlayPage />} />
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

it("三语开局保留昵称和次数，切语不创建对局", async () => {
  show();
  act(() => saveTrainerProfile({ displayName: "周末训练员", avatarId: "1001" }));
  fireEvent.change(screen.getByLabelText("允许猜错的次数"), { target: { value: "6" } });
  for (const lng of LANGUAGES) {
    await language(lng);
    expect(readTrainerProfile().displayName).toBe("周末训练员");
    expect(screen.queryByLabelText(classicStart[lng].name)).toBeNull();
    expect((screen.getByLabelText(classicStart[lng].budget) as HTMLInputElement).value).toBe("6");
    expect(screen.getByRole("heading", { name: classicStart[lng].title })).toBeTruthy();
  }
  expect(fixture.ensureSession).not.toHaveBeenCalled(); expect(fixture.api.createRoom).not.toHaveBeenCalled();
});

it("开局途中切语不重开，收到结果后进入同一房间", async () => {
  let ready!: () => void;
  fixture.ensureSession.mockImplementationOnce(() => new Promise<void>(resolve => { ready = resolve; }));
  show(); fireEvent.click(screen.getByRole("button", { name: "开始对局" }));
  await language("ja");
  expect(screen.getByText("参加の準備")).toBeTruthy();
  expect(fixture.ensureSession).toHaveBeenCalledOnce(); expect(fixture.api.createRoom).not.toHaveBeenCalled();
  await act(async () => { ready(); });
  await screen.findByText("game-opened");
  expect(fixture.api.createRoom).toHaveBeenCalledOnce(); expect(fixture.api.createRound).toHaveBeenCalledOnce();
  expect(fixture.api.createTransition).toHaveBeenCalledTimes(2);
});

it("错误在切语时重新显示，保留输入且不自动重试", async () => {
  fixture.ensureSession.mockRejectedValueOnce(new NetworkError("private diagnostic", undefined));
  show(); act(() => saveTrainerProfile({ displayName: "原昵称", avatarId: "1001" }));
  fireEvent.click(screen.getByRole("button", { name: "开始对局" }));
  await waitFor(() => expect(screen.getByText("连接中断")).toBeTruthy());
  await language("ja"); expect(screen.getByText("接続が切れました")).toBeTruthy();
  expect(document.body.textContent).not.toContain("private diagnostic");
  expect(readTrainerProfile().displayName).toBe("原昵称");
  expect(fixture.ensureSession).toHaveBeenCalledWith("原昵称");
  expect(fixture.ensureSession).toHaveBeenCalledOnce(); expect(fixture.api.createRoom).not.toHaveBeenCalled();
});
