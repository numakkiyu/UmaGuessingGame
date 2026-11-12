import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ChallengeStartPage } from "@/features/challenges/challenge-start-page";
import { ChallengeGamePage } from "@/features/challenges/challenge-game-page";
import { changeLanguage, LANGUAGES } from ".";
import { challenge } from "./challenge";

const fixture=vi.hoisted(()=>({
  api:{createChallenge:vi.fn(),getChallenge:vi.fn(),guessChallenge:vi.fn(),finishChallenge:vi.fn()},
  ensureSession:vi.fn(),
  characters:[{characterId:"original-character",displayName:"来源原名",localizedNames:{"zh-Hant":"已確認地區名",ja:"確認済みの名前"},aliases:[],avatarPath:null}],
}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:fixture.api,ensureSession:fixture.ensureSession,session:{sessionId:"original-session"},sessionLoading:false})}));
vi.mock("@/lib/api/queries",()=>({useCharacters:()=>({data:{items:fixture.characters},isPending:false,isError:false})}));
vi.mock("@/components/uma/search-input",()=>({SearchInput:({onSelect,disabled}:{onSelect:(item:unknown)=>void;disabled:boolean})=><button disabled={disabled} onClick={()=>onSelect(fixture.characters[0])}>选择原角色</button>}));

function view(overrides:Record<string,unknown>={}){return {challengeId:"original-challenge",modeId:"classic",ticket:"original-ticket",expiresAt:new Date(Date.now()+1200000).toISOString(),serverTime:new Date().toISOString(),wrongBudget:8,remainingWrong:8,revision:4,status:"active",attempts:0,rows:[],scoreUnits:0,characterIds:["original-character"],...overrides};}
function show(start=false){const client=new QueryClient({defaultOptions:{queries:{retry:false}}});return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[start?"/play/classic/verified":"/challenges/original-challenge"]}><Routes><Route path="/play/classic/verified" element={<ChallengeStartPage/>}/><Route path="/challenges/:challengeId" element={<ChallengeGamePage/>}/></Routes></MemoryRouter></QueryClientProvider>);}
const language=(lang:typeof LANGUAGES[number])=>act(async()=>{await changeLanguage(lang);});
beforeEach(()=>{vi.clearAllMocks();fixture.api.getChallenge.mockResolvedValue({data:view()});fixture.ensureSession.mockResolvedValue({sessionId:"original-session"});vi.stubGlobal("ResizeObserver",class{observe(){}disconnect(){}});});
afterEach(async()=>{cleanup();vi.unstubAllGlobals();await changeLanguage("zh-Hans");});

it("计分挑战三语参数完整，保留明确的计分与时间规则",()=>{
  const source=challenge["zh-Hans"];
  for(const locale of LANGUAGES){expect(Object.keys(challenge[locale]).sort()).toEqual(Object.keys(source).sort());for(const key of Object.keys(source) as (keyof typeof source)[]){const params=(value:string)=>[...value.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(params(challenge[locale][key])).toEqual(params(source[key]));}}
});

it("开局丢响应后切语不重复发送，确认仍沿用原始请求",async()=>{
  fixture.api.createChallenge.mockRejectedValue(new Error("private internal transport"));show(true);
  fireEvent.click(screen.getByRole("button",{name:"开始挑战"}));await screen.findByRole("button",{name:"确认刚才的开局"});const original=fixture.api.createChallenge.mock.calls[0];
  await language("ja");expect(fixture.api.createChallenge).toHaveBeenCalledOnce();expect(document.body.textContent).not.toContain("private internal transport");
  fireEvent.click(screen.getByRole("button",{name:"先ほどの開始結果を確認"}));await waitFor(()=>expect(fixture.api.createChallenge).toHaveBeenCalledTimes(2));expect(fixture.api.createChallenge.mock.calls[1]).toEqual(original);
});

it("作答未确认时切语保留角色、原票据和修订，只有显式确认才发送",async()=>{
  fixture.api.guessChallenge.mockRejectedValue(new Error("private wire error"));show();
  fireEvent.click(await screen.findByRole("button",{name:"选择原角色"}));await language("ja");expect(document.body.textContent).toContain("確認済みの名前");
  fireEvent.click(screen.getByRole("button",{name:"予想する"}));await screen.findByRole("button",{name:"先ほどの結果を確認"});const original=fixture.api.guessChallenge.mock.calls[0];
  expect(original?.[1]).toEqual({ticket:"original-ticket",expectedRevision:4,characterId:"original-character"});await language("zh-Hant");expect(fixture.api.guessChallenge).toHaveBeenCalledOnce();expect(fixture.api.getChallenge).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole("button",{name:"確認剛才的結果"}));await waitFor(()=>expect(fixture.api.guessChallenge).toHaveBeenCalledTimes(2));expect(fixture.api.guessChallenge.mock.calls[1]).toEqual(original);expect(document.body.textContent).not.toContain("private wire error");
});

it("已结束答案使用已确认地区名，切语不更改成绩或重新作答",async()=>{
  fixture.api.getChallenge.mockResolvedValue({data:view({status:"solved",attempts:2,scoreUnits:9200,answer:{characterId:"original-character",displayName:"来源原名",avatarPath:"/assets/avatars/1001.webp"}})});show();await screen.findByText("本局得分 92 分 · 2 次作答");await language("ja");expect(screen.getByText("確認済みの名前")).toBeTruthy();expect(screen.getByText("今回の得点 92 点・回答 2 回")).toBeTruthy();expect(fixture.api.getChallenge).toHaveBeenCalledOnce();expect(fixture.api.guessChallenge).not.toHaveBeenCalled();expect(fixture.api.finishChallenge).not.toHaveBeenCalled();
});
