import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { SortPracticeGamePage } from "./sort-practice-game-page";

const state = vi.hoisted(() => ({save: vi.fn(), load: vi.fn(), volatile: new Map()}));
vi.mock("@/lib/local/sort-practice-store", () => ({
  saveSortGame: state.save, loadSortGame: state.load, volatileSortGames: state.volatile,
  createSortGame: vi.fn(), readSortPack: () => ({profiles: Array.from({length:6},(_,i)=>({characterId:`c${i}`,displayName:`角色${i}`,imagePath:"",values:{height:150+i}}))}),
}));
vi.mock("@/components/uma/game-stage", () => ({GameStage: ({children}: {children: React.ReactNode}) => <div>{children}</div>}));
afterEach(() => {cleanup(); vi.clearAllMocks(); state.volatile.clear();});

it("排序保存失败后恢复时使用最后成功的版本，不误报跨页冲突", async () => {
  const now = Date.now();
  state.load.mockResolvedValue({schemaVersion:1,pack:{},game:{schemaVersion:1,modeId:"sort",gameId:"sort-check",packKey:"pack",rulesVersion:"2.0.0-draft.1",key:"height",openBook:false,cardIds:["c0","c1","c2","c3","c4","c5"],draft:[["c0"],["c1"],["c2"],["c3"],["c4"],["c5"]],submitted:null,createdAt:now,updatedAt:now,deadlineAt:now+180000,ended:false,revision:0}});
  state.save.mockRejectedValueOnce(new Error("temporary storage error")).mockResolvedValue(undefined);
  render(<MemoryRouter initialEntries={["/practice/sort/sort-check"]}><Routes><Route path="/practice/sort/:gameId" element={<SortPracticeGamePage/>}/></Routes></MemoryRouter>);
  const down = await screen.findAllByRole("button",{name:/下移/});
  fireEvent.click(down[0]!);
  await waitFor(() => expect(state.volatile.get("sort-check")?.savedRevision).toBe(0));
  fireEvent.click(await screen.findByRole("button",{name:"保存作答"}));
  await waitFor(() => expect(state.save).toHaveBeenCalledTimes(2));
  expect(state.save.mock.calls[0]![1]).toBe(0);
  expect(state.save.mock.calls[1]![1]).toBe(0);
  await waitFor(() => expect(state.volatile.has("sort-check")).toBe(false));
  expect(screen.queryByText("另一页已经更新了这局挑战")).toBeNull();
});
