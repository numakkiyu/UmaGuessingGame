import {readFileSync} from "node:fs";
import {act,cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {MemoryRouter,Route,Routes} from "react-router";
import {changeLanguage,LANGUAGES} from ".";
import {clueOnlineCopy} from "./clue-online";
import {clueCards,clueCardLabel,clueText} from "./clue-content";
import {CluePuzzleRoundPage} from "@/features/modes/clue-puzzle-round-page";
import {normalizeCluePuzzleRound} from "@/lib/api/modes-g09-g12";
import {loadClueDraft,saveClueDraft} from "@/lib/local/clue-online-draft";
import {NetworkError,TimeoutError} from "@/lib/api/client";
import type * as RoundSnapshotModule from "@/lib/api/round-snapshot";
const fixture=vi.hoisted(()=>({api:{createSubmission:vi.fn()},refresh:vi.fn(),view:{} as Record<string,unknown>,session:"person",room:{name:"原名ルーム",currentRound:{roundId:"round"}}}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:fixture.api,session:{sessionId:fixture.session}})}));
vi.mock("@/lib/api/room-queries",()=>({useRoom:()=>({data:{room:fixture.room}})}));
vi.mock("@/lib/api/round-snapshot",async original=>({...await original<typeof RoundSnapshotModule>(),useRoundSnapshot:()=>({...fixture.view,refetch:fixture.refresh})}));
vi.mock("@/lib/realtime/use-realtime",()=>({useRealtime:()=>({status:"online"})}));
vi.mock("@/features/rooms/team-round-control",()=>({TeamRoundControl:()=>null}));
const cards=clueCards.slice(0,4).map(c=>({cardId:c.id,label:c.source}));
function state(phase="collecting",paused=false,generation=1,revision=0){const meta={roundId:"round",phase,paused,phaseVersion:3,deadlineAt:"2099-01-01T00:00:00Z"};return {meta,canSubmit:true,controlGeneration:generation,submissionRevision:revision,raw:{round:meta,publicState:{config:{answerSeconds:240},cards,slots:[1,2,3,4].map(slotIndex=>({slotIndex,label:`第 ${slotIndex} 位`})),shared:[],privateClues:[{constraintId:"one",text:`${cards[0]!.label} 在第 2 位`,kind:"position",cards:[cards[0]!.cardId],position:2}],constraintCount:8,selfIsController:true,self:{subjectId:"team",assignment:{"1":cards[0]!.cardId,"2":cards[1]!.cardId,"3":cards[2]!.cardId,"4":cards[3]!.cardId},submissions:revision}}}};}
function show(){return render(<MemoryRouter initialEntries={["/rooms/room"]}><Routes><Route path="/rooms/:roomId" element={<CluePuzzleRoundPage/>}/></Routes></MemoryRouter>);}
const language=(l:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(l);});
beforeEach(async()=>{localStorage.clear();sessionStorage.clear();vi.clearAllMocks();fixture.session="person";fixture.view=state();fixture.refresh.mockResolvedValue(true);await language("zh-Hans");});afterEach(cleanup);

it("has all three locale keys and interpolation variables and exact authored12card coverage",()=>{
 const source=clueOnlineCopy["zh-Hans"];for(const lang of LANGUAGES){const target=clueOnlineCopy[lang];expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const key of Object.keys(source) as (keyof typeof source)[]){const vars=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(target[key].trim()).toBeTruthy();expect(vars(target[key])).toEqual(vars(source[key]));}}
 const raw=JSON.parse(readFileSync("../data/manual/cluepuzzle-deck.json","utf8"));expect(clueCards).toHaveLength(raw.cards.length);for(const c of raw.cards)expect(clueCardLabel(c,"ja").original).toBe(false);expect(clueCardLabel({...cards[0]!,label:"新档案"},"ja")).toEqual({text:"新档案",original:true});
});
it("localises all five exact generated statements without reversing order or inventing a position",()=>{
 const a=cards[0]!,b=cards[1]!,c=cards[2]!;
 for(const item of [
  {kind:"position",cards:[a.cardId],position:2,text:`${a.label} 在第 2 位`},
  {kind:"before",cards:[a.cardId,b.cardId],text:`${a.label} 排在 ${b.label} 前面`},
  {kind:"adjacent",cards:[a.cardId,b.cardId],text:`${a.label} 与 ${b.label} 相邻`},
  {kind:"not_with",cards:[a.cardId,b.cardId],text:`${a.label} 与 ${b.label} 不相邻`},
  {kind:"group",cards:[a.cardId,b.cardId,c.cardId],text:`${a.label}、${b.label}、${c.label} 连续排在一起`}
 ]){const clue={...item,constraintId:"one"};expect(clueText(clue,cards,"ja").original).toBe(false);expect(clueText(clue,cards,"zh-Hant").original).toBe(false);expect(clueText({...clue,text:"同号另一条规则"},cards,"ja")).toEqual({text:"同号另一条规则",original:true});}
 const old={constraintId:"old",kind:"position",cards:[a.cardId],text:`${a.label} 在第 2 位`};expect(clueText(old,cards,"ja").text).toContain("2番目");expect(clueText({...old,position:3},cards,"ja").original).toBe(true);expect(clueText({...old,kind:"future"},cards,"ja").original).toBe(true);
 expect(clueText({constraintId:"not",kind:"not_with",cards:[a.cardId,b.cardId],text:`${a.label} 与 ${b.label} 不相邻`},cards,"ja").text).toContain("隣り合わない");
});
it("validates persistent empty drafts, age size allowed slots and duplicate card protections",()=>{
 const slots=[1,2,3,4],ids=cards.map(c=>c.cardId);expect(saveClueDraft("scope",{assignment:new Map(),activeSlot:null})).toBe(true);expect(loadClueDraft("scope",slots,ids)?.assignment.size).toBe(0);expect(loadClueDraft("other",slots,ids)).toBeNull();
 for(const v of [{assignment:[[1,ids[0]],[2,ids[0]]],activeSlot:1,savedAt:Date.now()},{assignment:[[7,ids[0]]],activeSlot:null,savedAt:Date.now()},{assignment:[],activeSlot:null,savedAt:Date.now()+120000},{assignment:[],activeSlot:null,savedAt:Date.now()-86400001}]){localStorage.setItem("umaguess:clue-draft:bad",JSON.stringify(v));expect(loadClueDraft("bad",slots,ids)).toBeNull();}
 localStorage.setItem("umaguess:clue-draft:bad","x".repeat(4097));expect(loadClueDraft("bad",slots,ids)).toBeNull();
});
it("preserves a cleared slot through Japanese, paused refresh without sending, but new controller revision and identity start authoritative",async()=>{
 let page=show();fireEvent.click(screen.getByRole("button",{name:"选择位置：第 1 位"}));fireEvent.click(screen.getByRole("button",{name:"清空这个位置"}));await language("ja");expect(screen.getAllByText("カードなし")).toHaveLength(1);page.unmount();fixture.view=state("collecting",true);page=show();expect(screen.getAllByText("カードなし")).toHaveLength(1);expect(screen.getByRole("button",{name:"この配置で確定"}).hasAttribute("disabled")).toBe(true);expect(fixture.api.createSubmission).not.toHaveBeenCalled();
 page.unmount();fixture.view=state("collecting",false,2,1);page=show();expect(screen.queryByText("カードなし")).toBeNull();page.unmount();fixture.session="other";fixture.view=state();show();expect(screen.queryByText("カードなし")).toBeNull();
});
it("replays original assignment after controller rotation paused Traditional refresh, retaining a new current draft",async()=>{
 fixture.api.createSubmission.mockRejectedValueOnce(new TimeoutError(10)).mockResolvedValueOnce({data:{outcome:"accepted"}});let page=show();fireEvent.click(screen.getByRole("button",{name:"确认这份卡片排列"}));await screen.findByRole("button",{name:"确认刚才的卡片排列"});const original=fixture.api.createSubmission.mock.calls[0];page.unmount();
 fixture.view=state("collecting",true,2,1);const scope="person:round:clue-puzzle:team:2:1";saveClueDraft(scope,{assignment:new Map(),activeSlot:null});await language("zh-Hant");page=show();expect(fixture.api.createSubmission).toHaveBeenCalledOnce();fireEvent.click(screen.getByRole("button",{name:"確認剛才的卡片排列"}));await waitFor(()=>expect(fixture.api.createSubmission).toHaveBeenCalledTimes(2));expect(fixture.api.createSubmission.mock.calls[1]).toEqual(original);await screen.findByText("卡片排列已確認，下一位隊友可以接著調整。答案於揭曉時公布");expect(screen.getAllByText("還沒放卡")).toHaveLength(4);expect(loadClueDraft(scope,[1,2,3,4],cards.map(c=>c.cardId))?.assignment.size).toBe(0);page.unmount();
});
it("keeps original-confirm action available during a failed read and never prints raw exception",async()=>{
 fixture.api.createSubmission.mockRejectedValueOnce(new TimeoutError(10)).mockResolvedValueOnce({data:{outcome:"accepted"}});const page=show();fireEvent.click(screen.getByRole("button",{name:"确认这份卡片排列"}));await screen.findByRole("button",{name:"确认刚才的卡片排列"});const original=fixture.api.createSubmission.mock.calls[0];page.unmount();fixture.view={...state(),raw:null,isError:true,error:new NetworkError("secret upstream traceback",null)};await language("ja");show();expect(document.body.textContent).not.toContain("secret upstream traceback");fireEvent.click(screen.getByRole("button",{name:"さきほどの配置を確認"}));await waitFor(()=>expect(fixture.api.createSubmission).toHaveBeenCalledTimes(2));expect(fixture.api.createSubmission.mock.calls[1]).toEqual(original);
});
it("deadline forbids edits and confirmation even while latest snapshot phase is collecting",async()=>{
 const v=state();v.meta.deadlineAt="2020-01-01T00:00:00Z";fixture.view=v;show();await waitFor(()=>expect(screen.getByRole("button",{name:"确认这份卡片排列"}).hasAttribute("disabled")).toBe(true));expect(screen.getByRole("button",{name:"选择位置：第 1 位"}).hasAttribute("disabled")).toBe(true);
});
it("cancelled snapshot strips stale answers and formal scores and spectator has no fake arrangement",async()=>{
 const v=state("cancelled");const raw={...v.raw,publicState:{...v.raw.publicState,answer:{slots:[{slotIndex:1,cardId:cards[0]!.cardId,label:cards[0]!.label}],constraints:[],solutionCount:1}},results:[{subjectId:"team",scoreUnits:10000,rank:1}]};fixture.view={...v,raw};expect(normalizeCluePuzzleRound(raw).answer).toBeUndefined();expect(normalizeCluePuzzleRound(raw).scores).toEqual([]);show();expect(screen.queryByRole("heading",{name:"参考答案"})).toBeNull();expect(screen.queryByRole("heading",{name:"本局成绩"})).toBeNull();expect(screen.queryByRole("heading",{name:"你的线索"})).toBeNull();cleanup();delete(raw.publicState as Record<string,unknown>).self;delete(raw.publicState as Record<string,unknown>).privateClues;fixture.view={...state(),raw:{...raw,round:{phase:"collecting"},publicState:{...raw.publicState,selfIsController:false}}};await language("ja");show();expect(screen.queryByRole("heading",{name:"チームの配置"})).toBeNull();expect(fixture.api.createSubmission).not.toHaveBeenCalled();
});
it("missing formal result shows dash, known source clues are Japanese and titles localise without identity changes",async()=>{
 const v=state("revealed");fixture.view=v;show();await language("ja");expect(screen.getByRole("heading",{name:"チーム推理パズル"})).toBeTruthy();expect(screen.getByText("— 成績を集計しています")).toBeTruthy();expect(screen.queryByText("100 点")).toBeNull();expect(screen.getByText("原名ルーム")).toBeTruthy();
});
it("storage denial is truthful and does not send or discard current clear choice",()=>{
 const spy=vi.spyOn(localStorage,"setItem").mockImplementation(()=>{throw Error("blocked");});show();fireEvent.click(screen.getByRole("button",{name:"选择位置：第 1 位"}));fireEvent.click(screen.getByRole("button",{name:"清空这个位置"}));expect(screen.getByText("没能保存选择，请保持页面打开，核对后继续")).toBeTruthy();expect(screen.getByText("还没放卡")).toBeTruthy();expect(fixture.api.createSubmission).not.toHaveBeenCalled();spy.mockRestore();
});
it("closing shows authoritative confirmed mapping instead of a newer unsent empty draft",async()=>{
 const scope="person:round:clue-puzzle:team:1:0";saveClueDraft(scope,{assignment:new Map(),activeSlot:1});fixture.view=state("settled");await language("ja");show();expect(screen.queryByText("カードなし")).toBeNull();expect(screen.queryByRole("button",{name:"この場所を空にする"})).toBeNull();expect(screen.queryByText("まず場所を選んでね")).toBeNull();expect(loadClueDraft(scope,[1,2,3,4],cards.map(c=>c.cardId))?.assignment.size).toBe(0);expect(fixture.api.createSubmission).not.toHaveBeenCalled();
});
