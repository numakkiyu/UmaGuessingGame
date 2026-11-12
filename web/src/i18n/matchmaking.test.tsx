import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { changeLanguage, LANGUAGES } from ".";
import { matchmakingCopy } from "./matchmaking";
import { MatchmakingPage } from "@/features/rooms/matchmaking-page";
import { ApiError, TimeoutError } from "@/lib/api/client";
const state=vi.hoisted(()=>({api:{createMatchmakingTicket:vi.fn(),getMatchmakingTicket:vi.fn(),respondToMatch:vi.fn(),cancelMatchmakingTicket:vi.fn()},ensure:vi.fn()}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:state.api,session:null,ensureSession:state.ensure})}));
const language=(lang:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(lang);});
const ticket=(status="queueing",roomId:string|null=null)=>({data:{ticketId:"original-ticket",status,roomId,expiresAt:"2026-11-11T18:00:00Z",serverTime:"2026-11-11T17:00:00Z"}});
function show(){return render(<MemoryRouter><Routes><Route path="/" element={<MatchmakingPage/>}/><Route path="/rooms/:id" element={<p>original room</p>}/></Routes></MemoryRouter>);}
function leaves(value:unknown,prefix=""):Record<string,string>{if(typeof value==="string")return {[prefix]:value};return Object.assign({},...Object.entries(value as object).map(([k,v])=>leaves(v,`${prefix}.${k}`)));}
beforeEach(()=>{vi.clearAllMocks();state.ensure.mockResolvedValue(undefined);state.api.getMatchmakingTicket.mockResolvedValue(ticket());});
afterEach(async()=>{cleanup();vi.restoreAllMocks();await changeLanguage("zh-Hans");});
it("三语状态和规则插值齐全，未匹配不声称已提交，真实撮合不误称未实现",async()=>{
 const source=leaves(matchmakingCopy["zh-Hans"]);for(const lang of LANGUAGES){const target=leaves(matchmakingCopy[lang]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[k,v]of Object.entries(source)){expect(target[k]?.trim()).toBeTruthy();const params=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(params(target[k]!)).toEqual(params(v));}}
 show();expect(document.body.textContent).not.toContain("答案记下了");for(const lang of LANGUAGES){await language(lang);expect(screen.getByRole("heading",{name:matchmakingCopy[lang].title})).toBeTruthy();expect(document.body.textContent).not.toMatch(/自动配对暂时还不可用|ticketId|CSRF|幂等键/);}
});
it("匹配创建中切语保留名字和错误额度且不重发，排队切语不启动额外读取或重置定时器",async()=>{
 let resolve!:(value:unknown)=>void;state.api.createMatchmakingTicket.mockImplementationOnce(()=>new Promise(done=>{resolve=done;}));
 const timers=vi.spyOn(window,"setInterval"),clears=vi.spyOn(window,"clearInterval");show();fireEvent.change(screen.getByLabelText("显示名"),{target:{value:"原名／トレーナー"}});fireEvent.change(screen.getByLabelText("错误额度"),{target:{value:"10"}});
 await language("ja");expect((screen.getByLabelText("ニックネーム") as HTMLInputElement).value).toBe("原名／トレーナー");expect((screen.getByLabelText("間違えられる回数") as HTMLInputElement).value).toBe("10");fireEvent.click(screen.getByRole("button",{name:"相手を探す"}));await waitFor(()=>expect(state.api.createMatchmakingTicket).toHaveBeenCalledOnce());
 await language("zh-Hant");expect(screen.getByText(matchmakingCopy["zh-Hant"].enteringDetail)).toBeTruthy();expect(state.ensure).toHaveBeenCalledWith("原名／トレーナー");expect(state.api.createMatchmakingTicket.mock.calls[0]?.[0]).toEqual({modeConfig:{modeId:"classic",maxWrongGuesses:10,winPolicy:"first-correct",durationSeconds:0,shadowEnabled:false}});
 await act(async()=>{resolve(ticket());});await screen.findByRole("heading",{name:matchmakingCopy["zh-Hant"].statuses.queueing.label});const count=timers.mock.calls.filter(c=>c[1]===3000).length,cleared=clears.mock.calls.length;
 await language("ja");await language("zh-Hans");expect(timers.mock.calls.filter(c=>c[1]===3000)).toHaveLength(count);expect(clears.mock.calls).toHaveLength(cleared);expect(state.api.getMatchmakingTicket).not.toHaveBeenCalled();expect(state.api.createMatchmakingTicket).toHaveBeenCalledOnce();
});
it("同一匹配确认中切语不重复确认，原ticket和决定保持到相同房间",async()=>{
 state.api.createMatchmakingTicket.mockResolvedValue(ticket("matched-pending-accept"));let resolve!:(value:unknown)=>void;state.api.respondToMatch.mockImplementationOnce(()=>new Promise(done=>{resolve=done;}));show();fireEvent.click(screen.getByRole("button",{name:"开始匹配"}));await screen.findByRole("button",{name:"确认进入对局"});await language("ja");fireEvent.click(screen.getByRole("button",{name:"対戦に参加する"}));await language("zh-Hant");expect(state.api.respondToMatch).toHaveBeenCalledOnce();expect(state.api.respondToMatch.mock.calls[0]).toEqual(["original-ticket",{accept:true}]);expect(screen.getByRole("button",{name:"確認加入遊戲"}).hasAttribute("disabled")).toBe(true);await act(async()=>{resolve(ticket("ready","original-room"));});await screen.findByText("original room");
});
it("已显示匹配错误可重译，丢失创建响应同规则重试复用原key",async()=>{
 state.api.createMatchmakingTicket.mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce(ticket());show();fireEvent.click(screen.getByRole("button",{name:"开始匹配"}));await screen.findByRole("button",{name:"再试一次"});const first=state.api.createMatchmakingTicket.mock.calls[0];const text=screen.getByRole("alert").textContent;await language("ja");expect(screen.getByRole("alert").textContent).not.toBe(text);fireEvent.click(screen.getByRole("button",{name:"もう一度試す"}));await waitFor(()=>expect(state.api.createMatchmakingTicket).toHaveBeenCalledTimes(2));expect(state.api.createMatchmakingTicket.mock.calls[1]).toEqual(first);
});
it("取消中切语不重发，明确拒绝不显示原始异常",async()=>{
 state.api.createMatchmakingTicket.mockResolvedValue(ticket());let reject!:(error:unknown)=>void;state.api.cancelMatchmakingTicket.mockImplementationOnce(()=>new Promise((_resolve,fail)=>{reject=fail;}));show();fireEvent.click(screen.getByRole("button",{name:"开始匹配"}));await screen.findByRole("button",{name:"取消排队"});fireEvent.click(screen.getByRole("button",{name:"取消排队"}));await language("ja");expect(screen.getByRole("button",{name:"キャンセルしています…"}).hasAttribute("disabled")).toBe(true);expect(state.api.cancelMatchmakingTicket).toHaveBeenCalledExactlyOnceWith("original-ticket");await act(async()=>{reject(new ApiError({status:403,code:"forbidden",message:"private error",requestId:null}));});await screen.findByRole("button",{name:"もう一度試す"});expect(document.body.textContent).not.toContain("private error");
});
it("本人确认但房间尚未建立时继续查询，切语不伪称双方确认，直到真实房间可用才跳转",async()=>{
 state.api.createMatchmakingTicket.mockResolvedValue(ticket("matched-pending-accept"));
 state.api.respondToMatch.mockResolvedValue(ticket("ready"));
 state.api.getMatchmakingTicket.mockResolvedValue(ticket("ready","confirmed-room"));
 const timer=vi.spyOn(window,"setInterval");show();
 fireEvent.click(screen.getByRole("button",{name:"开始匹配"}));
 fireEvent.click(await screen.findByRole("button",{name:"确认进入对局"}));
 await screen.findByRole("heading",{name:matchmakingCopy["zh-Hans"].acceptedLabel});
 expect(document.body.textContent).not.toContain(matchmakingCopy["zh-Hans"].joining);
 await language("ja");expect(screen.getByRole("heading",{name:matchmakingCopy.ja.acceptedLabel})).toBeTruthy();
 expect(state.api.respondToMatch).toHaveBeenCalledOnce();
 const poll=timer.mock.calls.filter(call=>call[1]===3000).at(-1)?.[0];expect(typeof poll).toBe("function");
 await act(async()=>{(poll as ()=>void)();});await screen.findByText("original room");
 expect(state.api.getMatchmakingTicket).toHaveBeenCalledExactlyOnceWith("original-ticket");
});
