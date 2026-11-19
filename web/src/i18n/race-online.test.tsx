import {act,cleanup,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {MemoryRouter,Route,Routes} from "react-router";
import {RaceRebuildRoundPage,RaceRevealPanel,loadRaceDraft,saveRaceDraft} from "@/features/modes/race-rebuild-round-page";
import {normalizeRaceRound} from "@/lib/api/race-rebuild";
import {validRaceOrder} from "@/lib/api/game-commands";
import {TimeoutError} from "@/lib/api/client";
import {changeLanguage,LANGUAGES} from ".";
import {raceOnlineCopy} from "./race-online";
const f=vi.hoisted(()=>({api:{getRound:vi.fn(),createSubmission:vi.fn()},room:{roomId:"room-race",currentRound:{roundId:"round-race"}},characters:[{characterId:"a",displayName:"来源甲",localizedNames:{ja:"確認した名前甲","zh-Hant":"來源甲"}}]}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:f.api,session:{sessionId:"session-race"}})}));
vi.mock("@/lib/api/queries",()=>({useCharacters:()=>({data:{items:f.characters}})}));
vi.mock("@/lib/api/room-queries",()=>({useRoom:()=>({data:{room:f.room},isLoading:false,isError:false,refetch:vi.fn()})}));
vi.mock("@/lib/realtime/use-realtime",()=>({useRealtime:()=>({status:"online"})}));
vi.mock("@/features/rooms/team-round-control",()=>({TeamRoundControl:()=>null}));
const entries=["a","b","c","d"].map((id,i)=>({characterId:id,characterName:["来源甲","来源乙","来源丙","来源丁"][i]}));
function view(){return {round:{roundId:"round-race",modeId:"race-rebuild",phase:"collecting",phaseVersion:3,deadlineAt:new Date(Date.now()+180000).toISOString(),paused:false},publicState:{config:{cooperative:true,questionSeconds:180},raceId:"source-race",raceName:"日本杯",raceDate:"1989-11-26",entries,totalPairs:6,clues:["来源甲 领先 来源乙"],clueRelations:[{firstId:"a",secondId:"b",kind:"before"}],self:{subjectId:"p1",submitted:false,revisions:0,groups:[] as string[][],order:[] as string[],totalPairs:6}},canSubmit:true,submissionRevision:0,controlGeneration:0};}
const show=()=>render(<MemoryRouter initialEntries={["/rooms/room-race"]}><Routes><Route path="/rooms/:roomId" element={<RaceRebuildRoundPage/>}/></Routes></MemoryRouter>);
const lang=(value:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(value);});
function leaves(value:unknown,path=""):Record<string,string>{if(typeof value==="string")return {[path]:value};return Object.assign({},...Object.entries(value as object).map(([key,item])=>leaves(item,`${path}.${key}`)));}
beforeEach(()=>{vi.clearAllMocks();localStorage.clear();sessionStorage.clear();f.api.getRound.mockResolvedValue({data:view()});});
afterEach(async()=>{cleanup();vi.restoreAllMocks();localStorage.clear();sessionStorage.clear();await changeLanguage("zh-Hans");});

it("三语键/插值一致，结构线索按公开地区名重译且不补私有名次",async()=>{
 const source=leaves(raceOnlineCopy["zh-Hans"]);for(const language of LANGUAGES){const target=leaves(raceOnlineCopy[language]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[key,value]of Object.entries(source)){const vars=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(vars(target[key]!)).toEqual(vars(value));}}
 show();await screen.findByText("来源甲的名次在来源乙之前");await lang("ja");expect(screen.getByText("確認した名前甲は来源乙より先にゴールしました")).toBeTruthy();expect(screen.getByRole("heading",{name:"ジャパンカップ"})).toBeTruthy();expect(document.body.textContent).not.toContain("来源甲 领先 来源乙");expect(screen.queryByRole("heading",{name:"レースの結果発表"})).toBeNull();expect(screen.getByRole("table",{name:"現在の並び、上から順"}).textContent).not.toContain("比較する値");
});
it("并列草稿切语和刷新保持，未保存移动不自动作答",async()=>{
 const rendered=show();const board=await screen.findByRole("table",{name:"当前排序，从上到下"});fireEvent.click(within(board).getAllByRole("button",{name:/合并/})[0]!);const raw=localStorage.getItem("umaguess:race-draft:session-race:round-race:race:p1");expect(JSON.parse(raw!)).toHaveLength(3);await lang("zh-Hant");expect(localStorage.getItem("umaguess:race-draft:session-race:round-race:race:p1")).toBe(raw);expect(f.api.createSubmission).not.toHaveBeenCalled();rendered.unmount();show();await screen.findByRole("table",{name:"目前排序，由上往下"});expect(screen.getByRole("table",{name:"目前排序，由上往下"}).querySelector("tbody")!.children.length).toBe(3);expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("真实原请求确认在刷新切语版本变化后不被idle守卫拦住，成功后仍可修订",async()=>{
 f.api.createSubmission.mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce({data:{outcome:"accepted"}});const rendered=show();await screen.findByRole("button",{name:"保存本次作答"});fireEvent.click(screen.getByRole("button",{name:"保存本次作答"}));await screen.findByRole("button",{name:"确认上次作答"});const original=f.api.createSubmission.mock.calls[0];expect(validRaceOrder(original?.[1])).toBe(true);await lang("ja");rendered.unmount();const next=view();next.round.phaseVersion=9;next.submissionRevision=1;next.publicState.self.submitted=true;next.publicState.self.groups=original?.[1].modeAction.groups;f.api.getRound.mockResolvedValue({data:next});show();await screen.findByRole("table",{name:"現在の並び、上から順"});fireEvent.click(screen.getByRole("button",{name:"前回の回答を確認"}));await waitFor(()=>expect(f.api.createSubmission).toHaveBeenCalledTimes(2));expect(f.api.createSubmission.mock.calls[1]).toEqual(original);await waitFor(()=>expect(screen.getByRole("button",{name:"今回の回答を保存"}).hasAttribute("disabled")).toBe(false));expect(screen.queryByRole("heading",{name:"レースの結果発表"})).toBeNull();
});
it("旧中文线索只识别两种既有公开句式，不从答案生成新线索，未知句式不直出",async()=>{
 const data=view();delete (data.publicState as Partial<typeof data.publicState>).clueRelations;data.publicState.clues=["来源甲 与 来源乙 并列","private-clue-unknown"];
 const normalized=normalizeRaceRound(data as never)!;expect(normalized.clueRelations).toEqual([{firstId:"a",secondId:"b",kind:"tied"}]);f.api.getRound.mockResolvedValue({data});show();await screen.findByText("来源甲与来源乙同名次完赛");expect(screen.getByText(raceOnlineCopy["zh-Hans"].clueUnavailable)).toBeTruthy();expect(document.body.textContent).not.toContain("private-clue-unknown");
});
it("存储失败不报草稿已保存；重复/空组恢复命令不执行",async()=>{
 const set=vi.spyOn(localStorage,"setItem").mockImplementation(()=>{throw Error("blocked");});expect(saveRaceDraft("x",[["a"]])).toBe(false);show();fireEvent.click(await screen.findByRole("button",{name:"保存草稿（不计分）"}));await screen.findByText(raceOnlineCopy["zh-Hans"].saveFailed);expect(screen.queryByText(raceOnlineCopy["zh-Hans"].saved)).toBeNull();set.mockRestore();localStorage.setItem("umaguess:race-draft:invalid",JSON.stringify([["a"],[]]));expect(loadRaceDraft("invalid")).toBeNull();expect(validRaceOrder({phaseVersion:1,submissionRevision:0,controlGeneration:0,modeAction:{kind:"race.order",groups:[["a"],["b"],["c"],[]]}})).toBe(false);
});
it("揭晓保留1/3/3/7和排除人数，用确认作答而非草稿，取消不展示答案",async()=>{
 const data=view();Object.assign(data.publicState,{answer:{ranked:entries.map((e,i)=>({characterId:e.characterId,name:e.characterName,finishRank:[1,3,3,7][i]})),rankGaps:[1,3,7],excluded:["private-dropped"]}});Object.assign(data.publicState.self,{submitted:true,groups:[["a"],["b","c"],["d"]],correctPairs:6});const round=normalizeRaceRound(data as never)!;
 const rendered=render(<MemoryRouter><RaceRevealPanel round={round} scoreUnits={10000}/></MemoryRouter>);expect(screen.getAllByText("第 7 名")).toHaveLength(1);expect(screen.getAllByText("第 3 名")).toHaveLength(2);expect(screen.getByText(/这局没出到第 2、4、5、6 名的参赛者/)).toBeTruthy();expect(document.body.textContent).not.toContain("private-dropped");expect(screen.getByText("100")).toBeTruthy();await lang("ja");expect(screen.getAllByText("3 着")).toHaveLength(2);rendered.unmount();render(<MemoryRouter><RaceRevealPanel round={round} cancelled scoreUnits={10000}/></MemoryRouter>);expect(screen.queryByRole("table")).toBeNull();expect(screen.queryByText("100")).toBeNull();
});
