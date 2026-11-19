import { dismissFault } from "@/test/dismiss-fault";
import { saveTrainerProfile } from "@/lib/trainer-profile";
import { act,cleanup,fireEvent,render,screen,waitFor } from "@testing-library/react";
import { afterEach,beforeEach,expect,it,vi } from "vitest";
import { MemoryRouter,Route,Routes } from "react-router";
import { changeLanguage,LANGUAGES } from ".";
import { matchmakingCopy } from "./matchmaking";
import { MatchmakingPage } from "@/features/rooms/matchmaking-page";
import { ApiError,TimeoutError } from "@/lib/api/client";
const state=vi.hoisted(()=>({api:{getMatchmakingLobby:vi.fn(),getActiveMatchmakingTicket:vi.fn(),createMatchmakingProof:vi.fn(),heartbeatMatchmakingTicket:vi.fn(),createMatchmakingTicket:vi.fn(),getMatchmakingTicket:vi.fn(),respondToMatch:vi.fn(),cancelMatchmakingTicket:vi.fn()},ensure:vi.fn(),restore:vi.fn(),work:vi.fn()}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:state.api,session:null,ensureSession:state.ensure,restoreSession:state.restore})}));
vi.mock("@/features/rooms/matchmaking-work",()=>({prepareMatchWork:state.work}));
const language=(lang:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(lang);});
const ticket=(status="queueing",roomId:string|null=null)=>({data:{ticketId:"original-ticket",poolId:"classic-duel",players:2,accepted:0,status,roomId,expiresAt:new Date(Date.now()+20000).toISOString(),serverTime:new Date().toISOString()}});
function show(){return render(<MemoryRouter><Routes><Route path="/" element={<MatchmakingPage/>}/><Route path="/rooms/:id" element={<p>original room</p>}/></Routes></MemoryRouter>);}
function leaves(value:unknown,prefix=""):Record<string,string>{if(typeof value==="string")return {[prefix]:value};return Object.assign({},...Object.entries(value as object).map(([k,v])=>leaves(v,`${prefix}.${k}`)));}
async function start(){const button=await screen.findByRole("button",{name:"开始匹配"});await waitFor(()=>expect(button.hasAttribute("disabled")).toBe(false));fireEvent.click(button);}
beforeEach(()=>{
 vi.resetAllMocks();state.ensure.mockResolvedValue({sessionId:"session"});state.restore.mockResolvedValue(null);state.work.mockResolvedValue("42");
 state.api.getActiveMatchmakingTicket.mockResolvedValue({data:{ticket:null}});
 state.api.createMatchmakingProof.mockResolvedValue({data:{challengeId:"challenge",bits:16,expiresAt:new Date(Date.now()+90000).toISOString()}});
 state.api.getMatchmakingTicket.mockResolvedValue(ticket());state.api.heartbeatMatchmakingTicket.mockResolvedValue(ticket());state.api.createMatchmakingTicket.mockResolvedValue(ticket());
 state.api.getMatchmakingLobby.mockResolvedValue({data:{pools:["classic-duel","classic-four","sort-four","memory-four"].map(poolId=>({poolId,queued:0,confirming:0,playing:0,rooms:0,players:poolId==="classic-duel"?2:4})),queued:3,confirming:2,playing:4,rooms:1}});
});
afterEach(async()=>{cleanup();vi.restoreAllMocks();await changeLanguage("zh-Hans");});
it("三语键名和插值齐全，大厅展示真实汇总且不混入分享码输入",async()=>{
 const source=leaves(matchmakingCopy["zh-Hans"]);
 for(const lang of LANGUAGES){const target=leaves(matchmakingCopy[lang]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[k,v]of Object.entries(source)){expect(target[k]?.trim()).toBeTruthy();const params=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(params(target[k]!)).toEqual(params(v));}}
 show();await screen.findByText("3 人排队");expect(screen.getByText("4 人对局中")).toBeTruthy();expect(screen.queryByRole("spinbutton")).toBeNull();
 for(const lang of LANGUAGES){await language(lang);expect(screen.getByRole("heading",{name:matchmakingCopy[lang].title})).toBeTruthy();expect(document.body.textContent).not.toMatch(/ticketId|CSRF|幂等键|哈希|SHA-256/);}
});
it("入场先完成挑战，创建中切语不重发且不改变赛制",async()=>{
 let resolve!:(value:unknown)=>void;state.api.createMatchmakingTicket.mockImplementationOnce(()=>new Promise(done=>{resolve=done;}));
 saveTrainerProfile({displayName:"特别周",avatarId:"1001"});show();expect(screen.queryByLabelText("昵称")).toBeNull();await start();
 await waitFor(()=>expect(state.api.createMatchmakingTicket).toHaveBeenCalledOnce());await language("ja");
 expect(state.ensure).toHaveBeenCalledWith("特别周");expect(state.work).toHaveBeenCalledOnce();expect(state.api.createMatchmakingTicket.mock.calls[0]?.[0]).toEqual({poolId:"classic-duel",proof:{challengeId:"challenge",nonce:"42"}});
 await act(async()=>resolve(ticket()));expect(state.api.createMatchmakingTicket).toHaveBeenCalledOnce();
});
it("丢失创建响应时原样重试，不重算挑战或重新获取凭证",async()=>{
 state.api.createMatchmakingTicket.mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce(ticket());show();await start();await dismissFault();await screen.findByRole("button",{name:"再试一次"});
 const first=state.api.createMatchmakingTicket.mock.calls[0];await language("ja");fireEvent.click(screen.getByRole("button",{name:"もう一度試す"}));
 await waitFor(()=>expect(state.api.createMatchmakingTicket).toHaveBeenCalledTimes(2));
 // The AbortSignal may differ; body and command identity must remain identical.
 expect(state.api.createMatchmakingTicket.mock.calls[1]?.slice(0,2)).toEqual(first?.slice(0,2));expect(state.work).toHaveBeenCalledOnce();expect(state.api.createMatchmakingProof).toHaveBeenCalledOnce();
});
it("已确认仍轮询直到房间可用，切语不重建心跳定时器",async()=>{
 state.api.createMatchmakingTicket.mockResolvedValue(ticket("matched-pending-accept"));state.api.respondToMatch.mockResolvedValue(ticket("ready"));
 const timer=vi.spyOn(window,"setInterval");show();await start();fireEvent.click(await screen.findByRole("button",{name:"确认进入对局"}));await screen.findByRole("heading",{name:matchmakingCopy["zh-Hans"].acceptedLabel});
 const before=timer.mock.calls.filter(call=>call[1]===3000).length;await language("ja");expect(timer.mock.calls.filter(call=>call[1]===3000)).toHaveLength(before);
 state.api.heartbeatMatchmakingTicket.mockResolvedValue(ticket("playing","confirmed-room"));const poll=timer.mock.calls.filter(call=>call[1]===3000).at(-1)?.[0];await act(async()=>{await (poll as ()=>Promise<void>)();});await screen.findByText("original room");
});
it("取消响应不确定时保留票据，并使用同一取消命令重试",async()=>{
 state.api.cancelMatchmakingTicket.mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce({});show();await start();fireEvent.click(await screen.findByRole("button",{name:"取消排队"}));
 await dismissFault();await screen.findByRole("button",{name:"再试一次"});expect(screen.getByRole("heading",{name:"正在寻找对手"})).toBeTruthy();const first=state.api.cancelMatchmakingTicket.mock.calls[0];
 fireEvent.click(screen.getByRole("button",{name:"再试一次"}));await screen.findByRole("heading",{name:"已取消排队"});expect(state.api.cancelMatchmakingTicket.mock.calls[1]).toEqual(first);
});
it("刷新恢复现有对局，不创建第二张票据或挑战",async()=>{
 state.restore.mockResolvedValue({sessionId:"existing",displayName:"小特"});state.api.getActiveMatchmakingTicket.mockResolvedValue({data:{ticket:ticket("playing","restored-room").data}});show();await screen.findByText("original room");expect(state.work).not.toHaveBeenCalled();expect(state.api.createMatchmakingTicket).not.toHaveBeenCalled();
});
it("大厅关闭仍能去单人或好友页，不显示无效匹配按钮",async()=>{
 state.api.getMatchmakingLobby.mockRejectedValue(new ApiError({status:403,code:"matchmaking-lobby-disabled",message:"private detail",requestId:null}));show();await screen.findByText("匹配大厅暂未开放");expect(screen.queryByRole("button",{name:"开始匹配"})).toBeNull();expect(screen.getByRole("link",{name:"找朋友玩"}).getAttribute("href")).toBe("/friends");expect(document.body.textContent).not.toContain("private detail");
});
