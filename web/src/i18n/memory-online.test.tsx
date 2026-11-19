import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { changeLanguage, LANGUAGES } from ".";
import { memoryOnlineCopy } from "./memory-online";
import { roundUICopy } from "./round-ui";
import { MemoryRoundPage } from "@/features/modes/memory-round-page";
import { phaseLabel, isSubmittablePhase, isTerminalPhase } from "@/lib/api/round-snapshot";
import type * as RoundSnapshotModule from "@/lib/api/round-snapshot";
import { ApiError, TimeoutError } from "@/lib/api/client";
const fixture=vi.hoisted(()=>({api:{createSubmission:vi.fn()},view:{} as Record<string,unknown>,refresh:vi.fn(),roomRefresh:vi.fn(),characters:[] as {displayName:string;localizedNames?:Record<string,string>}[],room:{name:"同乐／にんじん",currentRound:{roundId:"rnd_original"}}}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:fixture.api,session:{sessionId:"original"}})}));
vi.mock("@/lib/api/queries",()=>({useCharacters:()=>({data:{items:fixture.characters}})}));
vi.mock("@/lib/api/room-queries",()=>({useRoom:()=>({data:{room:fixture.room},refetch:fixture.roomRefresh})}));
vi.mock("@/lib/api/round-snapshot",async original=>({...await original<typeof RoundSnapshotModule>(),useRoundSnapshot:()=>({...fixture.view,refetch:fixture.refresh,refreshAfterSubmit:fixture.refresh})}));
vi.mock("@/lib/realtime/use-realtime",()=>({useRealtime:()=>({status:"online",transport:"sse"})}));
vi.mock("@/features/rooms/team-round-control",()=>({TeamRoundControl:()=>null}));
const language=(lang:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(lang);});
beforeEach(()=>{sessionStorage.clear();vi.clearAllMocks();fixture.characters=[];fixture.refresh.mockResolvedValue(undefined);fixture.view={meta:{phase:"collecting",phaseVersion:3,deadlineAt:new Date(Date.now()+90000).toISOString()},submissionRevision:4,controlGeneration:2,canSubmit:true,raw:{publicState:{cards:[{cardId:"opaque-a",position:0,state:"hidden"},{cardId:"opaque-b",position:1,state:"hidden"}],self:{attempts:0,flipped:[],matched:[],finished:false}}}};});
afterEach(async()=>{cleanup();await changeLanguage("zh-Hans");});
function show(){return render(<MemoryRouter initialEntries={["/rooms/original"]}><Routes><Route path="/rooms/:roomId" element={<MemoryRoundPage/>}/></Routes></MemoryRouter>);}
function leaves(value:unknown,prefix=""):Record<string,string>{if(typeof value==="string")return {[prefix]:value};return Object.assign({},...Object.entries(value as object).map(([k,v])=>leaves(v,`${prefix}.${k}`)));}
it("联机翻牌与共享阶段资源齐全，未知阶段不泄原枚举，规则判定不随语言变化",async()=>{
 for(const catalog of [memoryOnlineCopy,roundUICopy]){const source=leaves(catalog["zh-Hans"]);for(const lang of LANGUAGES){const target=leaves(catalog[lang]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const [k,v] of Object.entries(source)){expect(target[k]?.trim()).toBeTruthy();const params=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(params(target[k]!)).toEqual(params(v));}}}
 for(const lang of LANGUAGES){await changeLanguage(lang);expect(phaseLabel("collecting")).toBe(roundUICopy[lang].phases.collecting);expect(phaseLabel("private-unrecognized-phase")).toBe(roundUICopy[lang].unknown);expect(phaseLabel(null)).toBe(roundUICopy[lang].notStarted);expect(isSubmittablePhase("collecting")).toBe(true);expect(isTerminalPhase("locked")).toBe(false);expect(isTerminalPhase("settled")).toBe(true);}
});
it("发送中切语不重发，未知结果跨语重试保留牌ID和原版本，反馈原地重译",async()=>{
 let reject!:(error:unknown)=>void;
 fixture.api.createSubmission.mockImplementationOnce(()=>new Promise((_done,fail)=>{reject=fail;})).mockResolvedValueOnce({data:{outcome:"accepted",feedback:{resolved:true,matched:false}}});
 const before=JSON.stringify(fixture.view);show();fireEvent.click(screen.getByRole("button",{name:"第 1 行第 1 列，未翻开"}));
 await language("ja");expect(screen.getByText(memoryOnlineCopy.ja.confirming)).toBeTruthy();expect(fixture.api.createSubmission).toHaveBeenCalledOnce();
 expect(screen.getByRole("button",{name:"1 行 1 列、裏向き"}).hasAttribute("disabled")).toBe(true);
 await act(async()=>{reject(new TimeoutError(10));});await screen.findByRole("button",{name:memoryOnlineCopy.ja.confirmAction});
 await language("zh-Hant");expect(fixture.api.createSubmission).toHaveBeenCalledOnce();
 fireEvent.click(screen.getByRole("button",{name:memoryOnlineCopy["zh-Hant"].confirmAction}));
 await waitFor(()=>expect(fixture.api.createSubmission).toHaveBeenCalledTimes(2));
 expect(fixture.api.createSubmission.mock.calls[1]).toEqual(fixture.api.createSubmission.mock.calls[0]);
 expect(fixture.api.createSubmission.mock.calls[0]?.[1]).toEqual({phaseVersion:3,submissionRevision:4,controlGeneration:2,modeAction:{kind:"memory.flip",cardId:"opaque-a"}});
 await screen.findByText(memoryOnlineCopy["zh-Hant"].mismatchNotice);await language("ja");expect(screen.getByText(memoryOnlineCopy.ja.mismatchNotice)).toBeTruthy();
 expect(JSON.stringify(fixture.view)).toBe(before);expect(fixture.refresh).toHaveBeenCalledOnce();expect(fixture.roomRefresh).not.toHaveBeenCalled();expect(document.querySelectorAll("img")).toHaveLength(0);
});
it("明确拒绝错误跟随语言，题目状态和身份不会变化或泄原异常",async()=>{
 fixture.api.createSubmission.mockRejectedValueOnce(new ApiError({status:409,code:"submission-revision-conflict",message:"private diagnostic",requestId:"safe-123"}));
 show();fireEvent.click(screen.getByRole("button",{name:"第 1 行第 1 列，未翻开"}));await waitFor(()=>expect(fixture.refresh).toHaveBeenCalledOnce());
 const before=screen.getByRole("alert").textContent;await language("ja");expect(screen.getByRole("alert").textContent).not.toBe(before);expect(document.body.textContent).not.toContain("private diagnostic");expect(screen.getByText("safe-123")).toBeTruthy();expect(fixture.api.createSubmission).toHaveBeenCalledOnce();
});
it("已结束面板切语不重算得分，不渲染pairId或重开局",async()=>{
 fixture.view={...fixture.view,meta:{phase:"revealed",phaseVersion:5,deadlineAt:null},raw:{publicState:{cards:[{cardId:"x",position:0,state:"matched",imagePath:"/assets/public.webp"},{cardId:"y",position:1,state:"matched",imagePath:"/assets/public.webp"}],self:{attempts:2,matchedPairs:1,wrongAttempts:1,scoreUnits:9800,finished:true},answer:{pairs:[{pairId:"secret-pair",label:"来源原名",cards:[{cardId:"x",imagePath:"/assets/public.webp"},{cardId:"y",imagePath:"/assets/public.webp"}]}]}}}};
 fixture.characters=[{displayName:"来源原名",localizedNames:{ja:"確認した名前","zh-Hant":"來源名稱"}}];
 const before=JSON.stringify(fixture.view);show();for(const lang of LANGUAGES){await language(lang);expect(screen.getByText(memoryOnlineCopy[lang].complete)).toBeTruthy();expect(screen.getByText("98")).toBeTruthy();expect(screen.getByText(lang==="ja"?"確認した名前":lang==="zh-Hant"?"來源名稱":"来源原名")).toBeTruthy();expect(document.body.innerHTML).not.toContain("secret-pair");expect(JSON.stringify(fixture.view)).toBe(before);}expect(fixture.api.createSubmission).not.toHaveBeenCalled();expect(fixture.refresh).not.toHaveBeenCalled();
});
it("取消回合不展示正式成绩、答案或继续翻牌提示，三语均说明不计分",async()=>{
 fixture.view={...fixture.view,meta:{phase:"cancelled",phaseVersion:5,deadlineAt:null},raw:{publicState:{phase:"cancelled",cards:[{cardId:"x",position:0,state:"matched",imagePath:"/assets/public.webp"}],self:{attempts:2,matchedPairs:1,scoreUnits:9800,finished:true},answer:{pairs:[{pairId:"hidden",label:"不应揭晓",cards:[{cardId:"x",imagePath:"/assets/public.webp"}]}]}}}};
 show();
 for(const lang of LANGUAGES){
  await language(lang);
  expect(screen.getByText(memoryOnlineCopy[lang].cancelledDetail)).toBeTruthy();
  expect(screen.queryByText(memoryOnlineCopy[lang].answerPairs)).toBeNull();
  expect(screen.queryByText(memoryOnlineCopy[lang].currentScore.replace("{{score}}","98"))).toBeNull();
  expect(screen.queryByText("不应揭晓")).toBeNull();
  expect(document.querySelectorAll("img")).toHaveLength(0);
 }
 expect(fixture.api.createSubmission).not.toHaveBeenCalled();
});
it("正式零分不会被认输前的配对成绩覆盖",async()=>{
 fixture.view={...fixture.view,meta:{phase:"settled",phaseVersion:5,deadlineAt:null},raw:{results:[{subjectId:"me",scoreUnits:0,outcome:"surrendered"}],publicState:{cards:[{cardId:"x",position:0,state:"matched"},{cardId:"y",position:1,state:"matched"}],self:{subjectId:"me",attempts:2,matchedPairs:1,scoreUnits:9800,finished:true}}}};
 show();
 expect(screen.getByText(memoryOnlineCopy["zh-Hans"].currentScore.replace("{{score}}","0"))).toBeTruthy();
 expect(screen.queryByText("98")).toBeNull();
 expect(screen.queryByText(memoryOnlineCopy["zh-Hans"].answerRemaining)).toBeNull();
 expect(screen.queryByText(memoryOnlineCopy["zh-Hans"].turnOver)).toBeNull();
 expect(screen.getByText(memoryOnlineCopy["zh-Hans"].surrenderedDetail)).toBeTruthy();
 expect(screen.queryByText(memoryOnlineCopy["zh-Hans"].complete)).toBeNull();
});
