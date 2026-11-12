import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage } from "@/i18n";
import { NetworkError } from "@/lib/api/client";
import { useStartRound } from "./use-start-round";

const mock = vi.hoisted(() => ({ createRoom: vi.fn(), createRound: vi.fn(), createTransition: vi.fn(), ensureSession: vi.fn() }));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: mock, ensureSession: mock.ensureSession }) }));
vi.mock("@/lib/api/queries", () => ({ useModes: () => ({ data: [{ modeId: "race-rebuild", informationStyle: "open" }] }) }));
const wrapper = ({ children }: { children: React.ReactNode }) => <MemoryRouter>{children}</MemoryRouter>;

beforeEach(() => {
  vi.resetAllMocks();
  mock.ensureSession.mockResolvedValue({});
  mock.createRoom.mockResolvedValue({ data: { room: { roomId: "same-room" } } });
  mock.createRound.mockResolvedValue({ data: { roundId: "same-round", phaseVersion: 7 } });
  mock.createTransition.mockImplementation(async (_id, body) => ({ data: { phaseVersion: body.expectedPhaseVersion + 1 } }));
});
afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });

describe("start retry keeps an immutable operation across language changes", () => {
  it.each(["room", "round", "briefing", "collecting"] as const)("recovers an unknown %s response without duplicating earlier successful steps", async failedStep => {
    const lost = new NetworkError("PRIVATE_RAW", null);
    if (failedStep === "room") mock.createRoom.mockRejectedValueOnce(lost);
    else if (failedStep === "round") mock.createRound.mockRejectedValueOnce(lost);
    else {
      let rejected = false;
      mock.createTransition.mockImplementation(async (_id, body) => {
        if (!rejected && body.target === failedStep) { rejected = true; throw lost; }
        return { data: { phaseVersion: body.expectedPhaseVersion + 1 } };
      });
    }
    const { result, rerender } = renderHook(({ modeName }) => useStartRound({ modeId: "race-rebuild", modeName, displayName: "小草🍀", modeConfig: { cooperative: true } }), { wrapper, initialProps: { modeName: "历史赛况复原" } });
    await act(async () => { await result.current.start(); });
    expect(result.current.failure?.kind).toBe("offline");
    const body = structuredClone(mock.createRoom.mock.calls[0]![0]);
    const firstKey = mock.createRoom.mock.calls[0]![1];
    await changeLanguage("ja");
    rerender({ modeName: "史実の着順クイズ" });
    await act(async () => { await result.current.start(); });
    expect(result.current.failure).toBeNull();
    expect(result.current.steps).toEqual({ session: "done", room: "done", round: "done", start: "done" });
    expect(body.name).toBe("小草🍀的历史赛况复原");
    expect(mock.createRoom).toHaveBeenCalledTimes(failedStep === "room" ? 2 : 1);
    for (const call of mock.createRoom.mock.calls) { expect(call[0]).toEqual(body); expect(call[1]).toBe(firstKey); }
    expect(mock.createRound).toHaveBeenCalledTimes(failedStep === "round" ? 2 : 1);
    const firstRound = mock.createRound.mock.calls[0]!;
    expect(firstRound[0]).toBe("same-room");
    expect(firstRound[1].modeConfig).toEqual(body.modeConfig);
    expect(firstRound[3]).toEqual(expect.any(String));
    for (const call of mock.createRound.mock.calls) expect(call).toEqual(firstRound);
    for (const target of ["briefing", "collecting"]) {
      const calls = mock.createTransition.mock.calls.filter(call => call[1].target === target);
      expect(calls).toHaveLength(failedStep === target ? 2 : 1);
      expect(calls[0]![0]).toBe("same-round");
      expect(calls[0]![1].expectedPhaseVersion).toBe(target === "briefing" ? 7 : 8);
      expect(calls[0]![3]).toEqual(expect.any(String));
      for (const call of calls) expect(call).toEqual(calls[0]);
    }
    expect(new Set([firstKey, firstRound[3], ...mock.createTransition.mock.calls.map(call => call[3])]).size).toBe(4);
  });

  it("ignores duplicate clicks before React has committed the running state", async () => {
    const { result } = renderHook(() => useStartRound({ modeId: "race-rebuild", modeName: "历史赛况复原", displayName: "小草" }), { wrapper });
    await act(async () => { await Promise.all([result.current.start(), result.current.start()]); });
    expect(mock.createRoom).toHaveBeenCalledTimes(1);
    expect(mock.createRound).toHaveBeenCalledTimes(1);
    expect(mock.createTransition).toHaveBeenCalledTimes(2);
  });
});
