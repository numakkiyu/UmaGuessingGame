import { dismissFault } from "@/test/dismiss-fault";
import { chooseSelect } from "@/test/select";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { changeLanguage } from "@/i18n";
import { TeamRoundControl } from "./team-round-control";
import { TimeoutError } from "@/lib/api/client";
import type { components } from "@/lib/api/types";
const state = vi.hoisted(() => ({ issue: vi.fn() }));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: { issueControlLease: state.issue } }) }));
const room = { roomId: "rom_test", viewer: { membershipId: "a1", capabilities: ["play", "manage-room"] }, teams: [{ teamId: "a", label: "胡萝卜队" }, { teamId: "b", label: "冲刺队" }], members: [
  { membershipId: "a1", displayName: "特别周 · 001", role: "team-captain", teamId: "a" }, { membershipId: "a2", displayName: "特别周 · 002", role: "player", teamId: "a" },
  { membershipId: "b1", displayName: "小栗帽", role: "team-captain", teamId: "b" }, { membershipId: "b2", displayName: "玉藻十字", role: "player", teamId: "b" },
] } as unknown as components["schemas"]["RoomSnapshot"];
const raw = (holder = "a1", generation = 0, roundId = "rnd_one") => ({ round: { roundId, modeId: "memory", phase: "collecting" }, canSubmit: holder === "a1", teamControllers: [
  { teamId: "a", membershipId: holder, displayName: holder === "a1" ? "特别周 · 001" : "特别周 · 002", generation },
  { teamId: "b", membershipId: "b1", displayName: "小栗帽", generation: 0 },
] });
beforeEach(() => state.issue.mockReset().mockResolvedValue({ data: {} }));
afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });
it("队友只看到自己的接棒提示，观众不显示操作安排", () => {
  const player: components["schemas"]["RoomSnapshot"] = { ...room, viewer: { ...room.viewer, membershipId: "a2", capabilities: ["play"] } };
  const view = render(<TeamRoundControl room={player} raw={{ ...raw(), canSubmit: false }} onChanged={vi.fn()} />);
  expect(screen.getByText("轮到特别周 · 001确认作答")).toBeTruthy(); expect(screen.getByText("翻完两张牌后，下一位队友接棒")).toBeTruthy();
  expect(screen.queryByText("安排队伍代表")).toBeNull(); expect(screen.queryByText("小栗帽")).toBeNull();
  view.rerender(<TeamRoundControl room={{ ...player, viewer: { ...player.viewer, membershipId: "spectator" } }} raw={raw()} onChanged={vi.fn()} />);
  expect(screen.queryByRole("region")).toBeNull(); expect(screen.queryByText(/接棒/)).toBeNull();
});
it("未知换人结果只重试原队伍原请求，其他队不能触发它", async () => {
  state.issue.mockRejectedValueOnce(new TimeoutError(15000)); const onChanged = vi.fn().mockResolvedValue(undefined);
  render(<TeamRoundControl room={room} raw={raw()} onChanged={onChanged} />);
  fireEvent.click(screen.getByText("安排队伍代表"));
  await chooseSelect(screen.getByLabelText("本次代表（胡萝卜队）"),"a2");
  fireEvent.click(screen.getByRole("button", { name: "交给胡萝卜队选定的队员" }));
  const retry = await screen.findByRole("button", { name: "确认胡萝卜队刚才的换人" });
  const other = screen.getByRole("button", { name: "交给冲刺队选定的队员" }) as HTMLButtonElement;
  expect(other.disabled).toBe(true); fireEvent.click(other); expect(state.issue).toHaveBeenCalledTimes(1);
  fireEvent.click(retry); await waitFor(() => expect(onChanged).toHaveBeenCalledOnce());
  expect(state.issue.mock.calls[1]).toEqual(state.issue.mock.calls[0]);
  expect(state.issue.mock.calls[0]?.[1]).toEqual({ roundId: "rnd_one", membershipId: "a2", expectedGeneration: 0 });
});
it("自动轮换或换局后不沿用过时的手动选择", async () => {
  const view = render(<TeamRoundControl room={room} raw={raw()} onChanged={vi.fn()} />);
  fireEvent.click(screen.getByText("安排队伍代表"));
  const select = screen.getByLabelText("本次代表（胡萝卜队）") as HTMLSelectElement;
  await chooseSelect(select,"a2");
  view.rerender(<TeamRoundControl room={room} raw={raw("a1", 2)} onChanged={vi.fn()} />);
  expect(select.value).toBe("a1");
  await chooseSelect(select,"a2");
  view.rerender(<TeamRoundControl room={room} raw={raw("a1", 2, "rnd_two")} onChanged={vi.fn()} />);
  expect(select.value).toBe("a1");
});

it("切语不重发换人，未确认时锁定其他队，重试保留原回合代次和成员", async () => {
 state.issue.mockRejectedValueOnce(new TimeoutError(15000));
 const onChanged=vi.fn().mockResolvedValue(undefined);
 const numbered={...room,members:room.members.map((m,i)=>({...m,publicCode:String(i+1).padStart(6,"0")}))};
 const before=JSON.stringify(numbered), snapshot=raw();
 render(<TeamRoundControl room={numbered} raw={snapshot} onChanged={onChanged}/>);
 fireEvent.click(screen.getByText("安排队伍代表"));
 await chooseSelect(screen.getByLabelText("本次代表（胡萝卜队）"),"a2");
 await act(async()=>{await changeLanguage("ja");});
 expect((screen.getByLabelText("今回の回答担当（胡萝卜队）") as HTMLSelectElement).value).toBe("a2");
 expect(screen.getByLabelText("今回の回答担当（胡萝卜队）").textContent).toContain("特别周 · 002（ルーム内 ID 000002）");
 expect(state.issue).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"胡萝卜队で選んだメンバーに交代"}));
 await screen.findByText("交代の結果がまだ確認できていません。直前の操作を先に確認してください");
 await act(async()=>{await changeLanguage("zh-Hant");});
 await dismissFault();expect(screen.getByRole("button",{name:"交給冲刺队選定的隊員"}).hasAttribute("disabled")).toBe(true);
 expect((screen.getByLabelText("本次代表（胡萝卜队）") as HTMLSelectElement).value).toBe("a2");
 fireEvent.click(screen.getByRole("button",{name:"確認胡萝卜队剛才的換人"}));
 await waitFor(()=>expect(state.issue).toHaveBeenCalledTimes(2));
 expect(state.issue.mock.calls[1]).toEqual(state.issue.mock.calls[0]);
 expect(state.issue.mock.calls[0]?.[1]).toEqual({roundId:"rnd_one",membershipId:"a2",expectedGeneration:0});
 expect(JSON.stringify(numbered)).toBe(before);expect(snapshot.round.roundId).toBe("rnd_one");
});
