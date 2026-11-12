import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { PublicRoundStage } from "@/features/display/public-round-stage";
import { RaceTrack } from "@/features/modes/race-track";

const { state, refresh } = vi.hoisted(() => ({ state: { value: {} as Record<string, unknown> }, refresh: vi.fn() }));
vi.mock(import("@/lib/api/round-snapshot"), async (original) => ({
  ...await original(),
  useRoundSnapshot: () => ({
    raw: null, meta: null, canSubmit: true, submissionRevision: 0, submissionRevisionsBySlot: {}, controlGeneration: 0,
    viewerRole: "display", serverTime: null, isLoading: false, isError: false, error: null,
    ...state.value, refetch: refresh, refreshAfterSubmit: refresh,
  }),
}));
afterEach(() => { cleanup(); vi.useRealTimers(); refresh.mockReset(); });

describe("现场公开进度", () => {
  it("不把私有线索、答案或内部标识渲染到舞台", () => {
    state.value = {
      raw: { privateState: { answer: "不该公开的答案" }, publicState: {}, round: { roundId: "rnd_secret" } },
      meta: { roundId: "rnd_secret", phase: "collecting", paused: false, deadlineAt: null },
      isLoading: false, isError: false,
    };
    const { container } = render(<PublicRoundStage roundId="rnd_secret" modeId="clue-puzzle" />);
    expect(screen.getByRole("heading", { name: "团队情报拼图" })).toBeTruthy();
    expect(container.textContent).not.toMatch(/不该公开的答案|rnd_secret|privateState/);
    expect(container.querySelector("button, input, textarea, [contenteditable]" )).toBeNull();
  });
  it("刷新在卸载后停止，暂停状态不伪装成正在倒计时", () => {
    vi.useFakeTimers();
    state.value = { raw: { publicState: {} }, meta: { roundId: "r", paused: true, phase: "collecting", deadlineAt: new Date(Date.now()+10000).toISOString() } };
    const { unmount } = render(<PublicRoundStage roundId="r" modeId="classic" />);
    expect(screen.getByRole("timer").textContent).toBe("已暂停");
    vi.advanceTimersByTime(2000);
    expect(refresh).toHaveBeenCalledTimes(1);
    unmount();
    vi.advanceTimersByTime(4000);
    expect(refresh).toHaveBeenCalledTimes(1);
  });
  it("队伍位置改变后仍留在原赛道，方便观众追踪", () => {
    const a = { subjectId:"a", label:"胡萝卜队", position:1, stamina:6, isSelf:false, avatarSrc:null };
    const b = { ...a, subjectId:"b", label:"蜂蜜队", position:5 };
    const { rerender } = render(<RaceTrack teams={[a,b]} totalTurns={8} staminaCap={8} maxGainPerTurn={5} />);
    const laneNames = () => screen.getAllByRole("listitem").map((item) => item.querySelector(".uma-race-track__name")?.textContent);
    const before = laneNames();
    rerender(<RaceTrack teams={[{...b,position:6},{...a,position:10}]} totalTurns={8} staminaCap={8} maxGainPerTurn={5} />);
    expect(laneNames()).toEqual(before);
    expect(screen.getByLabelText(/赛道/).textContent).toContain("胡萝卜队");
  });
});

describe("公开阶段说明",()=>{
 it("娱乐项目不声称计入正式总榜，创意审核和投票使用各自状态",()=>{
  state.value={raw:{publicState:{subphase:"review"}},meta:{roundId:"r",phase:"locked",paused:false,deadlineAt:null}};
  const view=render(<PublicRoundStage roundId="r" modeId="creative" competitive={false}/>);
  expect(screen.getByText(/主持正在检查投稿/)).toBeTruthy();expect(screen.getByText(/作娱乐展示/)).toBeTruthy();
  state.value={...state.value,raw:{publicState:{subphase:"voting"}}};view.rerender(<PublicRoundStage roundId="r" modeId="creative" competitive={false}/>);expect(screen.getByText(/正在投票/)).toBeTruthy();
 });
 it("取消或读取失败不继续显示运行倒计时，未知进度不编造已交比例",()=>{
  state.value={raw:{publicState:{}},meta:{roundId:"r",phase:"cancelled",paused:false,deadlineAt:new Date(Date.now()+10000).toISOString()}};
  const view=render(<PublicRoundStage roundId="r" modeId="classic" competitive/>);expect(screen.getByRole("timer").textContent).toBe("已取消");expect(document.body.textContent).not.toMatch(/\d+%/);
  state.value={...state.value,meta:{roundId:"r",phase:"collecting",paused:false,deadlineAt:new Date(Date.now()+10000).toISOString()},isError:true};view.rerender(<PublicRoundStage roundId="r" modeId="classic" competitive/>);expect(screen.getByRole("timer").textContent).not.toMatch(/\d+:\d+/);expect(screen.getByRole("alert")).toBeTruthy();
 });
});
