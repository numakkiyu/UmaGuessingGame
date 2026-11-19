import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useGameCommand } from "./use-game-command";
import { validMemoryFlip } from "@/lib/api/game-commands";
import { ApiError, NetworkError, TimeoutError } from "@/lib/api/client";
const body = { phaseVersion: 1, submissionRevision: 7, controlGeneration: 2, modeAction: { kind: "memory.flip" as const, cardId: "card-3" } };
beforeEach(() => sessionStorage.clear());
afterEach(() => cleanup());

it("lost response survives refresh and confirms the exact original action once", async () => {
  const execute = vi.fn().mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce({ accepted: true });
  const first = renderHook(() => useGameCommand("same-person:same-round", validMemoryFlip, execute));
  await act(async () => { await first.result.current.run(body); });
  const original = execute.mock.calls[0];
  expect(first.result.current.pending?.body).toEqual(body);
  first.unmount();
  const next = renderHook(() => useGameCommand("same-person:same-round", validMemoryFlip, execute));
  expect(execute).toHaveBeenCalledTimes(1); // No background retry on restore.
  await act(async () => { await next.result.current.run(); });
  expect(execute.mock.calls[1]).toEqual(original);
  expect(next.result.current.pending).toBeNull();
  expect(sessionStorage.length).toBe(0);
});

it("new round or identity never inherits a previous uncertain request", async () => {
  const execute = vi.fn().mockRejectedValue(new NetworkError("offline", new Error("offline")));
  const hook = renderHook(({ scope }) => useGameCommand(scope, validMemoryFlip, execute), { initialProps: { scope: "person-a:round-a" } });
  await act(async () => { await hook.result.current.run(body); });
  hook.rerender({ scope: "person-b:round-a" });
  expect(hook.result.current.pending).toBeNull();
  hook.rerender({ scope: "person-a:round-b" });
  expect(hook.result.current.pending).toBeNull();
  expect(execute).toHaveBeenCalledTimes(1);
});

it("concurrent clicks cannot submit twice, including while refreshing the new state", async () => {
  let finish!: () => void;
  const refreshed = new Promise<void>(resolve => { finish = resolve; });
  const execute = vi.fn().mockResolvedValue({ accepted: true });
  const hook = renderHook(() => useGameCommand("s:r", validMemoryFlip, execute, () => refreshed));
  let pending!: Promise<unknown>;
  act(() => { pending = hook.result.current.run(body); });
  await act(async () => { await hook.result.current.run(body); });
  expect(execute).toHaveBeenCalledTimes(1);
  expect(hook.result.current.busy).toBe(true);
  await act(async () => { finish(); await pending; });
  expect(hook.result.current.busy).toBe(false);
});

it("overload retains the uncertain command, a definitive rejection releases it", async () => {
  const execute = vi.fn().mockRejectedValueOnce(new TimeoutError(10))
    .mockRejectedValueOnce(new ApiError({ status: 429, code: "rate-limited", message: "busy", requestId: null }))
    .mockRejectedValueOnce(new ApiError({ status: 409, code: "phase-conflict", message: "changed", requestId: null }));
  const hook = renderHook(() => useGameCommand("s:r", validMemoryFlip, execute));
  await act(async () => { await hook.result.current.run(body); });
  await act(async () => { await hook.result.current.run(); });
  expect(hook.result.current.pending).not.toBeNull();
  await act(async () => { await hook.result.current.run(); });
  expect(hook.result.current.pending).toBeNull();
  expect(execute.mock.calls[0]).toEqual(execute.mock.calls[2]);
});

it("expired or malformed saved commands do not execute", async () => {
  const execute = vi.fn();
  sessionStorage.setItem("uma:game-command:s:r", JSON.stringify({ key: "a".repeat(48), body, createdAt: Date.now() - 24 * 60 * 60 * 1000 }));
  const expired = renderHook(() => useGameCommand("s:r", validMemoryFlip, execute));
  expect(expired.result.current.expired).toBe(true);
  await act(async () => { await expired.result.current.run(); });
  expired.unmount();
  sessionStorage.setItem("uma:game-command:s:r", JSON.stringify({ key: "a".repeat(48), body: { modeAction: { kind: "read-answer" } }, createdAt: Date.now() }));
  const invalid = renderHook(() => useGameCommand("s:r", validMemoryFlip, execute));
  expect(invalid.result.current.expired).toBe(true);
  await act(async () => { await invalid.result.current.run(); });
  expect(execute).not.toHaveBeenCalled();
});
