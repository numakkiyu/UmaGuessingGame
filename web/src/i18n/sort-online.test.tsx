import {act,cleanup,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {MemoryRouter,Route,Routes} from "react-router";
import {SortResultPanel,SortRoundPage} from "@/features/modes/sort-round-page";
import {normalizeSortRound,sortDraftIssues} from "@/lib/api/sort";
import {validSortOrder} from "@/lib/api/game-commands";
import {TimeoutError} from "@/lib/api/client";
import {changeLanguage,LANGUAGES} from ".";
import {onlineSortValue,sortCriterion,sortOnlineCopy} from "./sort-online";

const f=vi.hoisted(()=>({api:{getRound:vi.fn(),createSubmission:vi.fn()},room:{roomId:"room-original",name:"原房名",currentRound:{roundId:"round-original"},members:[]},characters:[{characterId:"a",displayName:"原名甲",localizedNames:{ja:"確認済みの甲","zh-Hant":"確認名甲"}},{characterId:"b",displayName:"原名乙"},{characterId:"c",displayName:"原名丙"}]}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:f.api,session:{sessionId:"session-original"}})}));
vi.mock("@/lib/api/queries",()=>({useCharacters:()=>({data:{items:f.characters}})}));
vi.mock("@/lib/api/room-queries",()=>({useRoom:()=>({data:{room:f.room},isLoading:false,isError:false,refetch:vi.fn()})}));
vi.mock("@/lib/realtime/use-realtime",()=>({useRealtime:()=>({status:"online"})}));
vi.mock("@/features/rooms/team-round-control",()=>({TeamRoundControl:()=>null}));
function view(overrides:Record<string,unknown>={}) {return {round:{roundId:"round-original",phase:"collecting",phaseVersion:3,paused:false,deadlineAt:"2026-11-11T22:00:00Z"},publicState:{config:{key:"height"},cards:[{characterId:"a",name:"原名甲"},{characterId:"b",name:"原名乙"},{characterId:"c",name:"原名丙"}],totalPairs:3,self:{submitted:false,revisions:0}},submissionRevision:4,controlGeneration:2,canSubmit:true,viewerRole:"player",...overrides};}
function show(){return render(<MemoryRouter initialEntries={["/rooms/room-original"]}><Routes><Route path="/rooms/:roomId" element={<SortRoundPage/>}/></Routes></MemoryRouter>);}
const lang=(value:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(value);});
const board=()=>screen.getByRole("table",{name:/当前排序|目前排序|現在の並び/});
function leaves(v:unknown,p=""):Record<string,string>{if(typeof v==="string")return {[p]:v};return Object.assign({},...Object.entries(v as object).map(([k,x])=>leaves(x,`${p}.${k}`)));}
beforeEach(()=>{vi.clearAllMocks();localStorage.clear();sessionStorage.clear();f.api.getRound.mockResolvedValue({data:view()});});
afterEach(async()=>{cleanup();vi.restoreAllMocks();localStorage.clear();sessionStorage.clear();await changeLanguage("zh-Hans");});

it("三语键和参数完整，未知维度安全回退，赛事日期不按生日显示",async()=>{
 const source=leaves(sortOnlineCopy["zh-Hans"]);for(const l of LANGUAGES){const target=leaves(sortOnlineCopy[l]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[k,v]of Object.entries(source)){expect(target[k]?.trim()).toBeTruthy();const params=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(params(target[k]!)).toEqual(params(v));}}
 await lang("ja");expect(sortCriterion("private-key")).toBe("カードの情報");expect(onlineSortValue("eventDate",20261111)).toBe("2026年11月11日");expect(onlineSortValue("birthday",1111)).toBe("11月11日");expect(onlineSortValue("private-key",123)).toBe("—");
});
it("切语保留草稿与并列，重复缺卡旧草稿不接受，不从公开人物索引补答案",async()=>{
 localStorage.setItem("umaguess:sort-draft:round-original",JSON.stringify([["a"],["a"],["b"]]));show();await screen.findByRole("heading",{name:"你的排序"});expect(board().querySelector("tbody")!.children).toHaveLength(3);expect(board().textContent).toContain("原名丙");
 fireEvent.click(within(board()).getAllByRole("button",{name:/合并/})[0]!);const draft=localStorage.getItem("umaguess:sort-draft:round-original");expect(JSON.parse(draft!)).toHaveLength(2);await lang("ja");expect(localStorage.getItem("umaguess:sort-draft:round-original")).toBe(draft);expect(board().textContent).toContain("確認済みの甲");expect(board().textContent).not.toContain("cm");expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("保存失败不声称保存成功，校验文案不透原ID",async()=>{
 show();await screen.findByRole("heading",{name:"你的排序"});const original=localStorage.setItem;vi.spyOn(localStorage,"setItem").mockImplementation(function(this:Storage,key,value){if(key.startsWith("umaguess:sort-draft:"))throw Error("private storage detail");return original.call(this,key,value);});
 fireEvent.click(screen.getByRole("button",{name:"保存草稿（不计分）"}));await screen.findByText("这次没能保留草稿。请保持此页打开，检查排序后直接提交");expect(document.body.textContent).not.toContain("草稿已保存");await lang("ja");expect(document.body.textContent).toContain("下書きを保存できませんでした");expect(document.body.textContent).not.toContain("private storage detail");expect(sortDraftIssues([["secret-id"],["secret-id"]],[])).toEqual(["unknown","duplicate","unknown"]);
});
it("丢响应后切语刷新，使用完整原始并列组及版本确认，不自动重交",async()=>{
 f.api.createSubmission.mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce({data:{outcome:"accepted"}});const rendered=show();await screen.findByRole("heading",{name:"你的排序"});fireEvent.click(within(board()).getAllByRole("button",{name:/合并/})[0]!);fireEvent.click(screen.getByRole("button",{name:"提交作答"}));await screen.findByRole("button",{name:"确认上次作答"});const original=f.api.createSubmission.mock.calls[0];expect(validSortOrder(original?.[1])).toBe(true);
 await lang("ja");expect(f.api.createSubmission).toHaveBeenCalledOnce();rendered.unmount();f.api.getRound.mockResolvedValue({data:view({round:{...view().round,phase:"locked",phaseVersion:9},submissionRevision:8,controlGeneration:7,canSubmit:false})});show();await screen.findByRole("button",{name:"前回の回答を確認"});await screen.findByRole("heading",{name:"自分の並び"});fireEvent.click(screen.getByRole("button",{name:"前回の回答を確認"}));await waitFor(()=>expect(f.api.createSubmission).toHaveBeenCalledTimes(2));expect(f.api.createSubmission.mock.calls[1]).toEqual(original);await waitFor(()=>expect(sessionStorage.length).toBe(0));
});
it("正式提交后仍可修订，揭晓前无分数，快照按本人实际并列组恢复",async()=>{
 const data=view();data.publicState.self={submitted:true,revisions:1,...{groups:[["a","c"],["b"]],order:["a","c","b"],correctPairs:3,totalPairs:3}};f.api.getRound.mockResolvedValue({data});show();await screen.findByRole("heading",{name:"你的排序"});expect(board().querySelector("tbody")!.children).toHaveLength(2);expect(screen.getByRole("button",{name:"更新作答"}).hasAttribute("disabled")).toBe(false);expect(screen.queryByText("正确率")).toBeNull();expect(screen.queryByText("漂亮，顺序全对！")).toBeNull();await lang("zh-Hant");expect(board().querySelector("tbody")!.children).toHaveLength(2);expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("失效的恢复记录不能执行，读取最新快照失败不解除保护",async()=>{
 sessionStorage.setItem("uma:game-command:session-original:round-original:sort",JSON.stringify({key:"a".repeat(48),createdAt:Date.now(),body:{phaseVersion:3,submissionRevision:0,controlGeneration:0,modeAction:{kind:"sort.order",groups:[["a","a"]]}}}));show();await screen.findByRole("button",{name:"查看最新记录"});f.api.getRound.mockRejectedValueOnce(new Error("private-read-error"));fireEvent.click(screen.getByRole("button",{name:"查看最新记录"}));await waitFor(()=>expect(f.api.getRound).toHaveBeenCalledTimes(2));expect(screen.getByRole("button",{name:"查看最新记录"})).toBeTruthy();expect(f.api.createSubmission).not.toHaveBeenCalled();expect(document.body.textContent).not.toContain("private-read-error");
});
it("揭晓保持玩家错误并列，答案按真实同值分组，取消不显示分数或答案",async()=>{
 const data=view({round:{...view().round,phase:"revealed"}});data.publicState.self={submitted:true,revisions:1,...{groups:[["a","c"],["b"]],order:["a","c","b"],correctPairs:1,totalPairs:3}};Object.assign(data.publicState,{answer:{order:["a","b","c"],values:[{characterId:"a",value:150},{characterId:"b",value:150},{characterId:"c",value:160}]}});const round=normalizeSortRound(data as never)!;expect(round.myOrder).toEqual([["a","c"],["b"]]);const rendered=render(<MemoryRouter><SortResultPanel round={round}/></MemoryRouter>);expect(screen.getByText("33%")).toBeTruthy();expect(screen.getByText("原名甲 = 原名乙")).toBeTruthy();expect(screen.getByText("原名甲 = 原名丙")).toBeTruthy();expect(screen.getAllByText("第 3 名")).toHaveLength(2);await lang("ja");expect(screen.getAllByText("同率 1 位")).toHaveLength(2);rendered.unmount();render(<MemoryRouter><SortResultPanel round={{...round,phase:"cancelled"}}/></MemoryRouter>);expect(screen.queryByText("得点")).toBeNull();expect(screen.queryByText("正しい並び")).toBeNull();
});
it("旧扁平记录不靠答案自动合并，非法请求不恢复执行",()=>{
 const data=view();Object.assign(data.publicState.cards[0]!,{raw:150});Object.assign(data.publicState.cards[1]!,{raw:150});Object.assign(data.publicState.self,{order:["a","b","c"],submitted:true});const legacy=normalizeSortRound(data as never)!;expect(legacy.myOrder).toEqual([["a"],["b"],["c"]]);expect(legacy.myOrderGroupingKnown).toBe(false);const result=render(<MemoryRouter><SortResultPanel round={{...legacy,phase:"revealed"}}/></MemoryRouter>);expect(screen.getByText(sortOnlineCopy["zh-Hans"].legacyOrder)).toBeTruthy();result.unmount();
 for(const groups of [[[],["a","b"]],[["a","a"]],[["a"]],[["a","b","c","d","e","f","g"]]])expect(validSortOrder({phaseVersion:1,submissionRevision:0,controlGeneration:0,modeAction:{kind:"sort.order",groups}})).toBe(false);
});
