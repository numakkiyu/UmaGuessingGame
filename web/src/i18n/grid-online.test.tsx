import {act,cleanup,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {MemoryRouter,Route,Routes} from "react-router";
import {AptitudeGridRoundPage,GridResult,gridSubjectName,parseGridScores} from "@/features/modes/aptitude-grid-round-page";
import {normalizeGridRound} from "@/lib/api/aptitude-grid";
import {ApiError,TimeoutError} from "@/lib/api/client";
import {validGridClaim} from "@/lib/api/game-commands";
import {changeLanguage,LANGUAGES} from ".";
import {gridOnlineCopy} from "./grid-online";
import type {RoomSnapshot} from "@/lib/api/types";

const f=vi.hoisted(()=>({api:{getRound:vi.fn(),createSubmission:vi.fn()},room:{roomId:"room-original",name:"原房名",currentRound:{roundId:"round-original"},members:[{membershipId:"p1",nickname:"同名",displayName:"同名",publicCode:"000123",capabilities:["play"]},{membershipId:"p2",nickname:"同名",displayName:"同名",publicCode:"456789",capabilities:["play"]}],viewer:{membershipId:"p1"}},characters:[{characterId:"a",displayName:"来源原名",localizedNames:{ja:"確認済みの名前","zh-Hant":"確認的名稱"},aliases:["角色外号"]},{characterId:"b",displayName:"来源乙",aliases:[]}]}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:f.api,session:{sessionId:"session-original"}})}));
vi.mock("@/lib/api/queries",()=>({useCharacters:()=>({data:{items:f.characters}})}));
vi.mock("@/lib/api/room-queries",()=>({useRoom:()=>({data:{room:f.room},isLoading:false,isError:false,refetch:vi.fn()})}));
vi.mock("@/lib/realtime/use-realtime",()=>({useRealtime:()=>({status:"online"})}));
vi.mock("@/features/rooms/team-round-control",()=>({TeamRoundControl:()=>null}));
function view(overrides:Record<string,unknown>={}) {return {round:{roundId:"round-original",modeId:"aptitude-grid",phase:"collecting",phaseVersion:3,paused:false,deadlineAt:new Date(Date.now()+180000).toISOString()},publicState:{config:{variant:"challenge",rowAxis:"distance"},phase:"collecting",cells:Array.from({length:9},(_,index)=>({cellId:`cell-${Math.floor(index/3)}-${index%3}`,index,row:Math.floor(index/3),col:index%3,rowCondition:{axis:"distance",value:["短","英","中"][Math.floor(index/3)]},colCondition:{axis:"style",value:["逃","先","差"][index%3]}})),pool:[{id:"a",name:"来源原名"},{id:"b",name:"来源乙"}],solvable:true,closed:false,subjects:[{subjectId:"p1",claims:0,timedOut:false},{subjectId:"p2",claims:0,timedOut:false}],self:{subjectId:"p1",claims:[],attempts:0,consecutiveTimeouts:0,timedOut:false}},submissionRevision:4,controlGeneration:2,canSubmit:true,viewerRole:"player",...overrides};}
function show(){return render(<MemoryRouter initialEntries={["/rooms/room-original"]}><Routes><Route path="/rooms/:roomId" element={<AptitudeGridRoundPage/>}/></Routes></MemoryRouter>);}
const lang=(l:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(l);});
function leaves(v:unknown,p=""):Record<string,string>{if(typeof v==="string")return {[p]:v};return Object.assign({},...Object.entries(v as object).map(([k,x])=>leaves(x,`${p}.${k}`)));}
beforeEach(()=>{vi.clearAllMocks();sessionStorage.clear();f.api.getRound.mockResolvedValue({data:view()});});
afterEach(async()=>{cleanup();vi.restoreAllMocks();sessionStorage.clear();await changeLanguage("zh-Hans");});
async function choose(){fireEvent.click(await screen.findByRole("button",{name:/第 1 行第 1 列/}));fireEvent.click(screen.getByRole("button",{name:"来源原名"}));}

it("简繁日键和插值一致，条件安全映射且同名用六位编号区分",async()=>{
 const source=leaves(gridOnlineCopy["zh-Hans"]);for(const language of LANGUAGES){const target=leaves(gridOnlineCopy[language]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[key,value]of Object.entries(source)){expect(target[key]?.trim()).toBeTruthy();const params=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(params(target[key]!)).toEqual(params(value));}}
 expect(gridSubjectName(f.room as unknown as RoomSnapshot,"p1","参赛方")).toBe("同名 · 000123");expect(gridSubjectName(f.room as unknown as RoomSnapshot,"p2","参赛方")).toBe("同名 · 456789");expect(gridSubjectName(f.room as unknown as RoomSnapshot,"secret-team","参赛方")).toBe("参赛方");
 const data=view();data.publicState.cells[0]!.rowCondition={axis:"secret-axis",value:"secret-value"};f.api.getRound.mockResolvedValue({data});show();await screen.findByRole("heading",{name:"适性交叉占格"});await lang("ja");expect(document.body.textContent).not.toMatch(/secret-axis|secret-value/);
});
it("选择和搜索切语及重新挂载保持，公开索引不补隐藏适性",async()=>{
 const rendered=show();await choose();fireEvent.change(screen.getByRole("searchbox",{name:"搜索名字"}),{target:{value:"角色外号"}});await lang("ja");expect(screen.getByRole("searchbox",{name:"ウマ娘の名前で探す"}).getAttribute("value")).toBe("角色外号");expect(screen.getByRole("button",{name:"確認済みの名前"}).getAttribute("aria-pressed")).toBe("true");expect(screen.getByText("選択中：確認済みの名前")).toBeTruthy();expect(f.api.createSubmission).not.toHaveBeenCalled();rendered.unmount();show();await screen.findByText("選択中：確認済みの名前");expect(screen.getByRole("button",{name:/このマスを確定/}).hasAttribute("disabled")).toBe(false);
});
it("已接受丢响应、切语、刷新、轮次改变后只能确认原占格，不声称先成功",async()=>{
 f.api.createSubmission.mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce({data:{outcome:"accepted"}});const rendered=show();await choose();fireEvent.click(screen.getByRole("button",{name:"确认占格（不可撤销）"}));await screen.findByRole("button",{name:"确认上次占格"});const original=f.api.createSubmission.mock.calls[0];expect(validGridClaim(original?.[1])).toBe(true);expect(document.body.textContent).not.toContain("已占据第");await lang("ja");expect(f.api.createSubmission).toHaveBeenCalledOnce();rendered.unmount();
 const next=view();next.round.phaseVersion=9;next.submissionRevision=8;next.controlGeneration=7;next.publicState.config.variant="duel";Object.assign(next.publicState,{turnSubjectId:"p2"});f.api.getRound.mockResolvedValue({data:next});show();await screen.findByRole("button",{name:"前回の確定結果を確認"});await screen.findByText("同名 · 456789 の手番です");fireEvent.click(screen.getByRole("button",{name:"前回の確定結果を確認"}));await waitFor(()=>expect(f.api.createSubmission).toHaveBeenCalledTimes(2));expect(f.api.createSubmission.mock.calls[1]).toEqual(original);await screen.findByText("1 行 1 列を確定しました。取り消しはできません");
});
it("真实规则拒绝不占格、不清选择，安全错误切语重译后可改人",async()=>{
 f.api.createSubmission.mockRejectedValueOnce(new ApiError({status:422,code:"condition_not_satisfied",message:"private-character condition",requestId:null}));show();await choose();fireEvent.click(screen.getByRole("button",{name:"确认占格（不可撤销）"}));await screen.findByText(gridOnlineCopy["zh-Hans"].errors.condition_not_satisfied);expect(screen.getByRole("button",{name:"来源原名"}).getAttribute("aria-pressed")).toBe("true");await lang("zh-Hant");expect(screen.getByText(gridOnlineCopy["zh-Hant"].errors.condition_not_satisfied)).toBeTruthy();expect(document.body.textContent).not.toContain("private-character");fireEvent.click(screen.getByRole("button",{name:"来源乙"}));expect(screen.getByRole("button",{name:"確定占格（無法復原）"}).hasAttribute("disabled")).toBe(false);
});
it("双人轮流非我方、掉线出局、满盘、只读与讲解阶段全部锁定新操作",async()=>{
 const cases=[{turnSubjectId:"p2",config:{variant:"duel"}},{self:{subjectId:"p1",claims:[],attempts:0,timedOut:true}},{closed:true},{self:undefined}];
 for(const partial of cases){const data=view();Object.assign(data.publicState,partial);f.api.getRound.mockResolvedValue({data});const rendered=show();await screen.findByRole("heading",{name:"适性交叉占格"});await screen.findByRole("heading",{name:"棋盘"});const region=screen.getByRole("region",{name:"棋盘"});expect(within(region).getAllByRole("button").every(button=>button.hasAttribute("disabled"))).toBe(true);rendered.unmount();}
 const data=view();data.round.phase="briefing";f.api.getRound.mockResolvedValue({data});show();await screen.findByText("现在不能占格：观看/简报");expect(screen.getByRole("button",{name:"确认占格（不可撤销）"}).hasAttribute("disabled")).toBe(true);expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("失效待确认操作只在最新快照成功后解除，失败不能放行",async()=>{
 sessionStorage.setItem("uma:game-command:session-original:round-original:grid",JSON.stringify({key:"a".repeat(48),createdAt:Date.now(),body:{phaseVersion:3,submissionRevision:0,controlGeneration:0,modeAction:{kind:"grid.claim",cellId:"",characterId:"a"}}}));show();await screen.findByRole("button",{name:"查看最新棋盘"});f.api.getRound.mockRejectedValueOnce(new Error("private read"));fireEvent.click(screen.getByRole("button",{name:"查看最新棋盘"}));await waitFor(()=>expect(f.api.getRound).toHaveBeenCalledTimes(2));expect(screen.getByRole("button",{name:"查看最新棋盘"})).toBeTruthy();expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("轮流结果只按本人与对手占格和后端名次，不把共用盘算为个人挑战分",async()=>{
 const data=view();data.publicState.config.variant="duel";Object.assign(data.publicState,{aptitudes:[{characterId:"a",aptitudes:{}}]});Object.assign(data.publicState.cells[0]!,{claim:{subjectId:"p2",characterId:"a",name:"来源原名",turn:1,satisfied:true}});data.publicState.subjects[1]!.claims=1;const round=normalizeGridRound(data as never)!;
 const rendered=render(<MemoryRouter><GridResult round={round} labelFor={id=>id==="p1"?"同名 · 000123":"同名 · 456789"} scores={parseGridScores([{subjectId:"p2",scoreUnits:1100,rank:1},{subjectId:"p1",scoreUnits:0,rank:2}])}/></MemoryRouter>);expect(screen.queryByText("挑战得分")).toBeNull();expect(document.querySelectorAll('table')).toHaveLength(2);expect(screen.getAllByText("同名 · 456789").length).toBeGreaterThan(0);await lang("ja");expect(screen.queryByText("チャレンジ得点")).toBeNull();rendered.unmount();render(<MemoryRouter><GridResult round={round} cancelled/></MemoryRouter>);expect(screen.queryByRole("table")).toBeNull();expect(screen.getByText("このゲームの得点や答えは表示しません")).toBeTruthy();
});
it("已使用角色禁再选，不泄未知角色ID；结果没揭晓数据不造零分",async()=>{
 const data=view();Object.assign(data.publicState.cells[0]!,{claim:{subjectId:"p1",characterId:"a",name:"来源原名",turn:1}});Object.assign(data.publicState.self,{claims:[0],attempts:1});f.api.getRound.mockResolvedValue({data});const rendered=show();await screen.findByRole("heading",{name:"棋盘"});expect(screen.getByRole("button",{name:/来源原名.*已在棋盘上使用/}).hasAttribute("disabled")).toBe(true);rendered.unmount();const normalized=normalizeGridRound(data as never)!;render(<MemoryRouter><GridResult round={normalized}/></MemoryRouter>);expect(screen.getByRole("heading",{name:"等待结果揭晓"})).toBeTruthy();expect(screen.queryByText("挑战得分")).toBeNull();expect(validGridClaim({phaseVersion:1,submissionRevision:0,controlGeneration:0,modeAction:{kind:"grid.claim",cellId:"cell-0-0",characterId:"a"}})).toBe(true);
});
