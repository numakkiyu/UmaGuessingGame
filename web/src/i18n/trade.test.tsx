import { chooseSelect } from "@/test/select";
import {readFileSync} from "node:fs";
import {act,cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {MemoryRouter,Route,Routes} from "react-router";
import {changeLanguage,LANGUAGES} from ".";
import {tradeOnlineCopy} from "./trade-online";
import {tradeCards,tradeObjectives,tradeCardLabel,tradeObjectiveText} from "./trade-content";
import {TradeRoundPage} from "@/features/modes/trade-round-page";
import {normalizeTradeRound} from "@/lib/api/trade";
import {loadTradeDraft,saveTradeDraft,emptyTradeDraft} from "@/lib/local/trade-online-draft";
import {ApiError,NetworkError,TimeoutError} from "@/lib/api/client";
import type * as RoundSnapshotModule from "@/lib/api/round-snapshot";

const fixture=vi.hoisted(()=>({api:{createTrade:vi.fn(),respondToTrade:vi.fn()},refresh:vi.fn(),view:{} as Record<string,unknown>,session:"person",room:{name:"原名ルーム",currentRound:{roundId:"round"},members:[{membershipId:"me",nickname:"同名训练员",publicCode:"000123"},{membershipId:"other",nickname:"同名训练员",publicCode:"123456"}]}}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:fixture.api,session:{sessionId:fixture.session}})}));
vi.mock("@/lib/api/room-queries",()=>({useRoom:()=>({data:{room:fixture.room}})}));
vi.mock("@/lib/api/round-snapshot",async original=>({...await original<typeof RoundSnapshotModule>(),useRoundSnapshot:()=>({...fixture.view,refetch:fixture.refresh})}));
vi.mock("@/lib/realtime/use-realtime",()=>({useRealtime:()=>({status:"online"})}));
function state(turn=1,phase="collecting",paused=false) {
 const meta={roundId:"round",phase,paused,phaseVersion:turn*3,deadlineAt:"2099-01-01T00:00:00Z"};
 return {meta,canSubmit:true,raw:{round:meta,publicState:{currentRound:turn,config:{rounds:3,roundSeconds:60,competitive:false},instances:[{instanceId:"a",cardId:"speed",label:"速度档案"},{instanceId:"b",cardId:"stamina",label:"耐力档案"}],hands:{me:["a"],other:["b"]},self:{subjectId:"me",hand:["a"]},selfObjectives:[],objectives:[],trades:[]}}};
}
function show(){return render(<MemoryRouter initialEntries={["/rooms/room"]}><Routes><Route path="/rooms/:roomId" element={<TradeRoundPage/>}/></Routes></MemoryRouter>);}
const language=(l:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(l);});
async function choose(){fireEvent.click(screen.getByRole("button",{name:"选择速度档案"}));await chooseSelect(screen.getByLabelText("交易对象"),"other");fireEvent.click(screen.getByRole("button",{name:"选择耐力档案"}));}
beforeEach(async()=>{localStorage.clear();sessionStorage.clear();vi.clearAllMocks();fixture.session="person";fixture.view=state();fixture.refresh.mockResolvedValue(true);await language("zh-Hans");});
afterEach(()=>cleanup());
function leaves(v:unknown,path=""):Record<string,string>{if(typeof v==="string")return {[path]:v};return Object.assign({},...Object.entries(v as object).map(([k,v])=>leaves(v,`${path}.${k}`)));}

it("three locales have complete keys and variables; all authored cards and objectives match exact sources",()=>{
 const source=leaves(tradeOnlineCopy["zh-Hans"]);for(const l of LANGUAGES){const target=leaves(tradeOnlineCopy[l]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[k,v]of Object.entries(source)){const params=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(target[k]?.trim()).toBeTruthy();expect(params(target[k]!)).toEqual(params(v));}}
 const raw=JSON.parse(readFileSync("../data/manual/trade-content.json","utf8"));expect(tradeCards).toHaveLength(raw.cards.length);expect(tradeObjectives).toHaveLength(raw.objectives.length);
 for(const o of raw.objectives){const value={...o,satisfied:null};expect(tradeObjectiveText(value,"ja").original).toBe(false);expect(tradeObjectiveText({...value,text:"同号新目标"},"ja")).toEqual({text:"同号新目标",original:true});expect(tradeObjectiveText({...value,kinds:["changed"]},"ja").original).toBe(true);}
 for(const c of raw.cards){expect(tradeCardLabel(c.cardId,c.label,"zh-Hant").original).toBe(false);}expect(tradeCardLabel("speed","同号新牌","ja")).toEqual({text:"同号新牌",original:true});
});

it("drafts persist empty choices with expiry structure and identity isolation",()=>{
 expect(saveTradeDraft("one",emptyTradeDraft)).toBe(true);expect(loadTradeDraft("one")).toEqual(emptyTradeDraft);expect(loadTradeDraft("two")).toBeNull();
 for(const bad of ["{","x".repeat(2049),JSON.stringify({...emptyTradeDraft,savedAt:Date.now()-86400001}),JSON.stringify({...emptyTradeDraft,savedAt:Date.now()+120000}),JSON.stringify({offeredId:{secret:true},recipientId:null,requestedId:null,savedAt:Date.now()})]){localStorage.setItem("umaguess:trade-draft:bad",bad);expect(loadTradeDraft("bad")).toBeNull();}
});

it("selection and original names survive locale pause and refresh without submitting",async()=>{
 const page=show();await choose();expect(screen.getByRole("button",{name:"发出提案"}).hasAttribute("disabled")).toBe(false);
 await language("ja");expect((screen.getByLabelText("交換相手") as HTMLSelectElement).value).toBe("other");expect(screen.getAllByText(/同名训练员/).length).toBeGreaterThan(0);expect(fixture.api.createTrade).not.toHaveBeenCalled();page.unmount();fixture.view=state(1,"collecting",true);show();expect((screen.getByLabelText("交換相手") as HTMLSelectElement).value).toBe("other");expect(screen.getByRole("button",{name:"提案を送る"}).hasAttribute("disabled")).toBe(true);
});

it("empty choice saves and new turn or identity never inherits previous selection",async()=>{
 let page=show();await choose();fireEvent.click(screen.getByRole("button",{name:"清空选择"}));page.unmount();page=show();expect((screen.getByLabelText("交易对象") as HTMLSelectElement).value).toBe("");await choose();page.unmount();fixture.view=state(2);page=show();expect((screen.getByLabelText("交易对象") as HTMLSelectElement).value).toBe("");page.unmount();fixture.session="different-person";fixture.view=state();show();expect((screen.getByLabelText("交易对象") as HTMLSelectElement).value).toBe("");expect(fixture.api.createTrade).not.toHaveBeenCalled();
});

it("lost proposal confirms identical original body/key after new turn and reading failure",async()=>{
 fixture.api.createTrade.mockRejectedValueOnce(new TimeoutError(10)).mockResolvedValueOnce({data:{tradeId:"original"}});
 const page=show();await choose();fireEvent.click(screen.getByRole("button",{name:"发出提案"}));await screen.findByRole("button",{name:"确认刚才的结果"});const original=fixture.api.createTrade.mock.calls[0];page.unmount();
 fixture.view={...state(2),isError:true,error:new NetworkError("private network error",null),raw:null};await language("ja");show();expect(fixture.api.createTrade).toHaveBeenCalledOnce();fireEvent.click(screen.getByRole("button",{name:"さきほどの結果を確認"}));await waitFor(()=>expect(fixture.api.createTrade).toHaveBeenCalledTimes(2));expect(fixture.api.createTrade.mock.calls[1]).toEqual(original);expect(document.body.textContent).not.toContain("private network error");
});

it("lost acceptance retains accept and trade URL through pause and language changes",async()=>{
 const value=state();value.raw.publicState.trades=[{tradeId:"tr-original",round:1,senderId:"other",recipientId:"me",offeredInstanceId:"b",requestedInstanceId:"a",status:"pending"}] as never;fixture.view=value;
 fixture.api.respondToTrade.mockRejectedValueOnce(new TimeoutError(10)).mockResolvedValueOnce({data:{tradeId:"tr-original"}});
 const page=show();fireEvent.click(screen.getByRole("button",{name:"接受"}));await screen.findByRole("button",{name:"确认刚才的结果"});const original=fixture.api.respondToTrade.mock.calls[0];page.unmount();fixture.view=state(2,"collecting",true);await language("zh-Hant");show();fireEvent.click(screen.getByRole("button",{name:"確認剛才的結果"}));await waitFor(()=>expect(fixture.api.respondToTrade).toHaveBeenCalledTimes(2));expect(fixture.api.respondToTrade.mock.calls[1]).toEqual(original);expect(original?.[1]).toBe("tr-original");expect(original?.[2]).toEqual({accept:true});
});

it("normalizes published task outcome separately from official points and suppresses cancellation results",()=>{
 const raw={round:{phase:"settled"},publicState:{result:{players:[{subjectId:"me",completed:1,total:2,points:50,satisfiedIds:["one"],unsatisfiedIds:["two"]}]}},results:[{subjectId:"me",scoreUnits:0,rank:1},{subjectId:"invalid",scoreUnits:-1}]};
 expect(normalizeTradeRound(raw).scores).toEqual([{subjectId:"me",points:0,rank:1}]);expect(normalizeTradeRound(raw).result[0]?.points).toBe(50);
 for(const phase of ["collecting","locked","cancelled"]){expect(normalizeTradeRound({...raw,round:{phase}}).result).toEqual([]);expect(normalizeTradeRound({...raw,round:{phase}}).scores).toEqual([]);}
});

it("spectator and cancellation have no personal trade controls or fake scores",async()=>{
 const value=state(3,"cancelled");delete (value.raw.publicState as Record<string,unknown>).self;fixture.view={...value,canSubmit:false};show();await language("ja");expect(screen.queryByRole("button",{name:"提案を送る"})).toBeNull();expect(screen.queryByRole("heading",{name:"あなたの秘密の目標"})).toBeNull();expect(screen.getByText("ゲームは中止されました")).toBeTruthy();expect(fixture.api.createTrade).not.toHaveBeenCalled();
});

it("authored errors update with locale and never expose raw errors",async()=>{
 fixture.api.createTrade.mockRejectedValueOnce(new ApiError({status:422,code:"card-not-held",message:"private SQL",requestId:null}));show();await choose();fireEvent.click(screen.getByRole("button",{name:"发出提案"}));await screen.findByText("这张牌不在你手里");await language("ja");expect(screen.getByText("そのカードは手札にありません")).toBeTruthy();expect(document.body.textContent).not.toContain("private SQL");
});
