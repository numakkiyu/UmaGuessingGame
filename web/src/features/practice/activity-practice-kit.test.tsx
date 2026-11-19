import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import { answerConfirmation, cancelConfirmation, confirmationStore } from "@/lib/confirmation";
import { PracticeConflict } from "@/lib/local/practice-store";
import { changeRace, type RaceGame } from "@/lib/domain/race-practice";
import { useActivityPractice, type PracticeAdapter } from "./activity-practice-kit";

const clock = 1_800_000_000_000;
function initialGame(): RaceGame {
  return { schemaVersion: 1, modeId: "race-rebuild", gameId: "race-check", packKey: "pack", rulesVersion: "2.0.0-draft.1", createdAt: clock, updatedAt: clock, deadlineAt: clock + 180_000, ended: false, revision: 0, raceId: "r", style: "closed", cardIds: ["a","b","c","d"], draft: [["a"],["b"],["c"],["d"]], submitted: null, submittedAt: null, submissions: 0 };
}
function setup() {
  const save = vi.fn().mockResolvedValue(undefined);
  const initial = {schemaVersion: 1 as const, pack: {}, game: initialGame()};
  const adapter = { modeId: "race-rebuild", title: "", summary: "", hint: "", prepareLabel: "", startLabel: "", filePrefix: "race", rules: [], fetch: vi.fn(), read: vi.fn(), validate: vi.fn(), create: initialGame, store: {save}, volatile: new Map() } as unknown as PracticeAdapter<object, RaceGame>;
  const hook = renderHook(() => useActivityPractice(adapter, initial), {wrapper: ({children}: {children: ReactNode}) => <MemoryRouter>{children}</MemoryRouter>});
  return {...hook, save, adapter, initial};
}
const submit = (g: RaceGame, at: number) => changeRace(g, g.draft, true, at);
async function decide(yes: boolean) {
  const current = confirmationStore.getSnapshot();
  expect(current).not.toBeNull();
  await act(async () => answerConfirmation(current!.id, yes));
}
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(clock); });
afterEach(() => { act(() => cancelConfirmation()); cleanup(); vi.useRealTimers(); });

it("交卷将当前排序和结束状态一次保存，无须先保存或等待倒计时", async () => {
  const h = setup();
  await act(async () => h.result.current.act((g,at) => changeRace(g, [["d"],["c"],["b"],["a"]], false, at)));
  h.save.mockClear();
  let pending!: Promise<void>;
  act(() => { pending = h.result.current.submitAndFinish(submit); });
  await decide(true); await act(async () => pending);
  expect(h.save).toHaveBeenCalledTimes(1);
  expect(h.result.current.game).toMatchObject({ended: true, submitted: [["d"],["c"],["b"],["a"]], submissions: 1});
  expect(h.result.current.game.updatedAt).toBeLessThan(h.result.current.game.deadlineAt);
  expect(h.save.mock.calls[0]![0]).toMatchObject({ended:true,submitted:h.result.current.game.draft});
});

it("取消交卷不保存、不结束，仍可继续修改", async () => {
  const h = setup(); let pending!: Promise<void>;
  act(() => {pending = h.result.current.submitAndFinish(submit);});
  await decide(false); await act(async () => pending);
  expect(h.save).not.toHaveBeenCalled(); expect(h.result.current.game.ended).toBe(false);
  await act(async () => h.result.current.act((g,at) => changeRace(g, [["b"],["a"],["c"],["d"]], false, at)));
  expect(h.result.current.game.revision).toBe(1);
});

it("确认框打开时到达截止时间，不能补交或覆盖已保存的答案", async () => {
  const h = setup(); let pending!: Promise<void>;
  act(() => {pending = h.result.current.submitAndFinish(submit);});
  await act(async () => vi.advanceTimersByTime(180_000));
  expect(confirmationStore.getSnapshot()).toBeNull();
  await act(async () => pending);
  expect(h.save).not.toHaveBeenCalled(); expect(h.result.current.game.submitted).toBeNull();
});

it("交卷遇到其他页面已更新时，不揭晓旧答案或覆盖新进度", async () => {
  const h = setup(); h.save.mockRejectedValue(new PracticeConflict()); let pending!: Promise<void>;
  act(() => {pending = h.result.current.submitAndFinish(submit);});
  await decide(true); await act(async () => pending);
  expect(h.result.current.stale).toBe(true); expect(h.result.current.game.ended).toBe(false);
});

it("存储短暂不可用后可恢复，保留上次成功保存的版本用于后续写入", async () => {
  const h = setup(); h.save.mockRejectedValueOnce(new Error("storage unavailable"));
  await act(async () => h.result.current.act((g,at) => changeRace(g, [["b"],["a"],["c"],["d"]], false, at)));
  expect(h.adapter.volatile.get("race-check")?.savedRevision).toBe(0);
  expect(h.result.current.saved).toBe(false);
  await act(async () => h.result.current.act(submit));
  expect(h.save.mock.calls[1]![2]).toBe(0);
  expect(h.result.current.saved).toBe(true); expect(h.result.current.stale).toBe(false);
  expect(h.adapter.volatile.has("race-check")).toBe(false);
});

it("交卷正在保存时重复点击，不生成第二次提交", async () => {
  const h = setup(); let release!: () => void;
  h.save.mockImplementation(() => new Promise<void>(resolve => {release = resolve;}));
  let pending!: Promise<void>;
  act(() => {pending = h.result.current.submitAndFinish(submit);});
  await decide(true);
  await act(async () => h.result.current.submitAndFinish(submit));
  expect(h.save).toHaveBeenCalledTimes(1); expect(confirmationStore.getSnapshot()).toBeNull();
  await act(async () => {release(); await pending;});
  expect(h.result.current.game.submissions).toBe(1);
});
