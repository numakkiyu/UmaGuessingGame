import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { EventRoundControls } from "./event-round-controls";
import { changeLanguage } from "@/i18n";
import { TimeoutError } from "@/lib/api/client";
import type { RoundSnapshot } from "@/lib/api/round-snapshot";
const f=vi.hoisted(()=>({api:{createTransition:vi.fn()}}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:f.api,session:{sessionId:"host"}})}));
const lang=(value:"zh-Hans"|"zh-Hant"|"ja")=>act(async()=>{await changeLanguage(value);});
const snapshot=(modeId="classic",phase="collecting")=>({meta:{roundId:"r",modeId,phase,phaseVersion:7,deadlineAt:"2099-01-01T00:00:00Z",paused:false},raw:null} as unknown as RoundSnapshot);
const show=(s=snapshot(),closed=false,onChanged=async()=>true)=>render(<MemoryRouter><EventRoundControls snapshot={s} closed={closed} roundId="r" roomId="room" onChanged={onChanged}/></MemoryRouter>);
beforeEach(async()=>{sessionStorage.clear();f.api.createTransition.mockReset().mockResolvedValue({});await lang("zh-Hans");});
afterEach(cleanup);
it("sequential windows and review/vote modes do not expose forbidden early collection controls",()=>{for(const id of["trade","rapport","strategy-race","creative","bluff"]){const p=show(snapshot(id));expect(screen.queryByRole("button",{name:"收卷"})).toBeNull();if(id==="creative"||id==="bluff"){expect(screen.queryByRole("button",{name:"加时30秒"})).toBeNull();expect(screen.getByRole("link",{name:"打开本局游玩画面"}).getAttribute("href")).toBe("/rooms/room");}p.unmount();}show(snapshot("memory","briefing"));expect(screen.queryByRole("button",{name:"开始作答"})).toBeNull();expect(screen.getByRole("button",{name:"暂停比赛"})).toBeTruthy();});
it("cancel wording accurately voids the whole item and preserves Unicode reason when switching language",async()=>{show();fireEvent.click(screen.getByRole("button",{name:"作废本分项"}));expect(screen.getByText(/这个分项的全部回合不再计入总榜/)).toBeTruthy();fireEvent.change(screen.getByLabelText("作废原因（5至240个字符）"),{target:{value:"🏇".repeat(240)}});await lang("ja");expect((screen.getByLabelText("無効にする理由（5～240文字）") as HTMLInputElement).value).toBe("🏇".repeat(240));fireEvent.click(screen.getByRole("button",{name:/^無効にする$/}));await waitFor(()=>expect(f.api.createTransition).toHaveBeenCalledOnce());expect(f.api.createTransition.mock.calls[0]?.[1]).toEqual({target:"cancel",expectedPhaseVersion:7,reason:"🏇".repeat(240)});});
it("an unknown transition survives closure, language, and missing metadata without another write",async()=>{f.api.createTransition.mockRejectedValueOnce(new TimeoutError(10));let p=show();fireEvent.click(screen.getByRole("button",{name:"暂停比赛"}));await screen.findByRole("button",{name:"确认刚才的操作"});const original=f.api.createTransition.mock.calls[0];p.unmount();await lang("zh-Hant");p=show({meta:null} as RoundSnapshot,true);expect(f.api.createTransition).toHaveBeenCalledOnce();fireEvent.click(screen.getByRole("button",{name:"確認剛才的操作"}));await waitFor(()=>expect(f.api.createTransition).toHaveBeenCalledTimes(2));expect(f.api.createTransition.mock.calls[1]).toEqual(original);p.unmount();});
it("an expired transition is held until all required data is successfully read",async()=>{sessionStorage.setItem("uma:game-command:host:r:host-transition",JSON.stringify({key:"a".repeat(48),body:{target:"pause",expectedPhaseVersion:7},createdAt:Date.now()-86400000}));const refresh=vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);show(snapshot(),false,refresh);fireEvent.click(screen.getByRole("button",{name:"查看最新进度"}));await screen.findByText("暂时没能读到最新进度，请保留页面并稍后刷新");expect(sessionStorage.length).toBe(1);fireEvent.click(screen.getByRole("button",{name:"查看最新进度"}));await waitFor(()=>expect(sessionStorage.length).toBe(0));expect(f.api.createTransition).not.toHaveBeenCalled();});

it("locked review cannot skip moderation and voting by revealing early",()=>{for(const id of["creative","bluff"]){let p=show({...snapshot(id,"locked"),raw:{publicState:{subphase:"review"}}});expect(screen.queryByRole("button",{name:"揭晓答案"})).toBeNull();expect(screen.getByRole("link",{name:"打开本局游玩画面"})).toBeTruthy();p.unmount();p=show({...snapshot(id,"locked"),raw:{publicState:{subphase:"closed"}}});expect(screen.getByRole("button",{name:"揭晓答案"})).toBeTruthy();p.unmount();}});

it("a stale snapshot from the previous round cannot expose commands for the new round",()=>{show({...snapshot(),meta:{...snapshot().meta!,roundId:"previous"}});expect(screen.queryByRole("button",{name:"暂停比赛"})).toBeNull();expect(f.api.createTransition).not.toHaveBeenCalled();});

it("an activity paused during restart can resume before reveal or settlement",async()=>{
 for(const phase of["locked","revealed"]){
  const s=snapshot("classic",phase);s.meta!.paused=true;s.meta!.deadlineAt=null;
  const page=show(s);
  expect(screen.queryByRole("button",{name:phase==="locked"?"揭晓答案":"结算本局"})).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"继续比赛"}));
  await waitFor(()=>expect(f.api.createTransition).toHaveBeenCalled());
  expect(f.api.createTransition.mock.calls.at(-1)?.[1]).toMatchObject({target:"resume",expectedPhaseVersion:7});
  page.unmount();sessionStorage.clear();f.api.createTransition.mockClear();
 }
});
