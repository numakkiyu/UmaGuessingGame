import {act,cleanup,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {MemoryRouter,Route,Routes} from "react-router";
import {DraftResultPanel,DraftRoundPage,draftScores} from "@/features/modes/draft-round-page";
import {normalizeDraftRound,loadLineupDraft,saveLineupDraft} from "@/lib/api/draft";
import {validDraftLineup} from "@/lib/api/game-commands";
import {TimeoutError} from "@/lib/api/client";
import {changeLanguage,LANGUAGES} from ".";
import {draftOnlineCopy} from "./draft-online";
const f=vi.hoisted(()=>({api:{getRound:vi.fn(),createSubmission:vi.fn()},room:{roomId:"room-draft",currentRound:{roundId:"round-draft"},members:[{membershipId:"p1",nickname:"同名",publicCode:"000123"},{membershipId:"p2",nickname:"同名",publicCode:"456789"}]},characters:[{characterId:"a",displayName:"来源甲",localizedNames:{ja:"確認した名前甲","zh-Hant":"來源甲"},aliases:["甲外号"]}]}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:f.api,session:{sessionId:"session-draft"}})}));
vi.mock("@/lib/api/queries",()=>({useCharacters:()=>({data:{items:f.characters}})}));
vi.mock("@/lib/api/room-queries",()=>({useRoom:()=>({data:{room:f.room},isLoading:false,isError:false,refetch:vi.fn()})}));
vi.mock("@/lib/realtime/use-realtime",()=>({useRealtime:()=>({status:"online"})}));
vi.mock("@/features/rooms/team-round-control",()=>({TeamRoundControl:()=>null}));
const names=["来源甲","来源乙","来源丙","来源丁","来源戊"];
function view(){return {round:{roundId:"round-draft",modeId:"draft",phase:"collecting",phaseVersion:3,deadlineAt:new Date(Date.now()+240000).toISOString(),paused:false},publicState:{config:{budget:24,planningSeconds:240},phase:"collecting",cards:names.map((name,index)=>({id:"abcde"[index],name,price:6,aptitudes:{surface:{草地:"A"},distance:{中:"A"},style:{先:"A"}}})),tasks:Array.from({length:4},(_,i)=>({taskId:`t${i+1}`,surface:"草地",distance:"中",style:"先"})),subjects:[{subjectId:"p1",revisions:0,locked:false},{subjectId:"p2",revisions:0,locked:false}],self:{subjectId:"p1",assignments:{} as Record<string,string>,revisions:0,locked:false,totalPoints:0,spent:0,taskScores:[] as {taskId:string;cardId:string;total:number}[]}},canSubmit:true,submissionRevision:0,controlGeneration:0};}
const show=()=>render(<MemoryRouter initialEntries={["/rooms/room-draft"]}><Routes><Route path="/rooms/:roomId" element={<DraftRoundPage/>}/></Routes></MemoryRouter>);
const lang=(value:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(value);});
async function choose(index:number,name:string){const area=await screen.findByRole("region",{name:"四项任务"});const item=within(area).getAllByRole("listitem")[index]!;fireEvent.click(within(item).getByRole("button",{name:/选角色|换一位/}));const dialog=await screen.findByRole("dialog");fireEvent.click(within(dialog).getByRole("button",{name:new RegExp(name)}));await waitFor(()=>expect(screen.queryByRole("dialog")).toBeNull());}
async function complete(){for(let i=0;i<4;i++)await choose(i,names[i]!);}
beforeEach(()=>{vi.clearAllMocks();localStorage.clear();sessionStorage.clear();f.api.getRound.mockResolvedValue({data:view()});});
afterEach(async()=>{cleanup();vi.restoreAllMocks();localStorage.clear();sessionStorage.clear();await changeLanguage("zh-Hans");});
function leaves(value:unknown,path=""):Record<string,string>{if(typeof value==="string")return {[path]:value};return Object.assign({},...Object.entries(value as object).map(([key,item])=>leaves(item,`${path}.${key}`)));}

it("三语覆盖相同语义和插值，不回显内部ID或原始任务值",async()=>{
 const source=leaves(draftOnlineCopy["zh-Hans"]);for(const language of LANGUAGES){const target=leaves(draftOnlineCopy[language]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[key,value]of Object.entries(source)){const vars=(text:string)=>[...text.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(target[key]?.trim()).toBeTruthy();expect(vars(target[key]!)).toEqual(vars(value));}}
 const data=view();data.publicState.tasks[0]!.surface="private-surface";data.publicState.cards[0]!.name="a";f.api.getRound.mockResolvedValue({data});show();await screen.findByRole("heading",{name:"四项任务"});await lang("ja");expect(document.body.textContent).not.toContain("private-surface");
});
it("选人草稿和已打开候选的搜索切语保持，刷新按身份/本局恢复而不提交",async()=>{
 const rendered=show();await choose(0,names[0]!);fireEvent.click(screen.getAllByRole("button",{name:"选角色"})[0]!);fireEvent.change(screen.getByRole("searchbox",{name:"搜索角色名字"}),{target:{value:"甲外号"}});await lang("ja");expect(screen.getByRole("searchbox",{name:"ウマ娘の名前で探す"}).getAttribute("value")).toBe("甲外号");expect(within(screen.getByRole("dialog")).getByRole("button",{name:/確認した名前甲/}).hasAttribute("disabled")).toBe(true);expect(f.api.createSubmission).not.toHaveBeenCalled();rendered.unmount();show();await screen.findByText("確認した名前甲");expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("未知结果经切语/刷新/版本改变后核对原完整编队和key，不被锁定状态拦住",async()=>{
 f.api.createSubmission.mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce({data:{outcome:"accepted"}});const rendered=show();await complete();fireEvent.click(screen.getByRole("button",{name:"最终提交（锁定编队）"}));await screen.findByRole("button",{name:"确认上次编队"});const original=f.api.createSubmission.mock.calls[0];expect(validDraftLineup(original?.[1])).toBe(true);await lang("ja");expect(f.api.createSubmission).toHaveBeenCalledOnce();rendered.unmount();
 const locked=view();locked.round.phaseVersion=9;locked.submissionRevision=1;locked.publicState.self.assignments={t1:"a",t2:"b",t3:"c",t4:"d"};locked.publicState.self.locked=true;locked.publicState.self.totalPoints=36;locked.publicState.self.spent=24;f.api.getRound.mockResolvedValue({data:locked});show();fireEvent.click(await screen.findByRole("button",{name:"前回の編成を確認"}));await waitFor(()=>expect(f.api.createSubmission).toHaveBeenCalledTimes(2));expect(f.api.createSubmission.mock.calls[1]).toEqual(original);await screen.findByText(draftOnlineCopy.ja.accepted);expect(screen.queryByRole("button",{name:draftOnlineCopy.ja.submit})).toBeNull();expect(screen.queryByRole("heading",{name:draftOnlineCopy.ja.result})).toBeNull();
});
it("存储失败不声称草稿已保存，非法/未来/跨题草稿不变成有效编队",async()=>{
 const set=vi.spyOn(localStorage,"setItem").mockImplementation(()=>{throw Error("blocked");});expect(saveLineupDraft("blocked",{assignments:{t1:"a"}})).toBe(false);show();fireEvent.click(await screen.findByRole("button",{name:"保存草稿（不计分）"}));await screen.findByText(draftOnlineCopy["zh-Hans"].saveFailed);expect(screen.queryByText(draftOnlineCopy["zh-Hans"].saved)).toBeNull();set.mockRestore();
 for(const value of [{assignments:{t1:44},savedAt:Date.now()},{assignments:[],savedAt:Date.now()},{assignments:{t1:"a"},savedAt:Date.now()+120000}]){localStorage.setItem("umaguess:draft-lineup:invalid",JSON.stringify(value));expect(loadLineupDraft("invalid")).toBeNull();}
 expect(validDraftLineup({phaseVersion:1,submissionRevision:0,controlGeneration:0,modeAction:{kind:"draft.lineup",assignments:[1,2,3,4].map(n=>({slotId:`t${n}`,characterId:"a"}))}})).toBe(false);
});
it("讲解/暂停/只读时不能改编队，已确定的角色不被新草稿覆盖",async()=>{
 for(const kind of ["briefing","paused","readonly"]){const data=view();if(kind==="briefing")data.round.phase="briefing";if(kind==="paused")data.round.paused=true;if(kind==="readonly")data.canSubmit=false;f.api.getRound.mockResolvedValue({data});const rendered=show();await screen.findByRole("heading",{name:"四项任务"});expect(screen.getAllByRole("button",{name:"选角色"}).every(button=>button.hasAttribute("disabled"))).toBe(true);rendered.unmount();}
 expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("最终成绩只读真实结算，未确认不计草稿、取消不展示答案成绩",async()=>{
 const data=view();data.round.phase="settled";const round=normalizeDraftRound(data as never)!;
 const rendered=render(<MemoryRouter><DraftResultPanel round={round} scores={draftScores([{subjectId:"p1",scoreUnits:0,rank:2},{subjectId:"p2",scoreUnits:7800,rank:1}])} labelFor={id=>id==="p1"?"同名 · 000123":"同名 · 456789"}/></MemoryRouter>);expect(screen.getByText(draftOnlineCopy["zh-Hans"].noFinal)).toBeTruthy();expect(screen.getAllByText("同名 · 456789").length).toBe(2);await lang("ja");expect(screen.getByText(draftOnlineCopy.ja.noFinal)).toBeTruthy();rendered.unmount();render(<MemoryRouter><DraftResultPanel round={round} cancelled/></MemoryRouter>);expect(screen.queryByRole("table")).toBeNull();expect(screen.getByText(draftOnlineCopy.ja.cancelledDetail)).toBeTruthy();
});
