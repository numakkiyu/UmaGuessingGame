import { dismissFault } from "@/test/dismiss-fault";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { ClassicEndPanel, ClassicRoomPage } from "@/features/classic/classic-room-page";
import { normalizeClassicRound, normalizeRowFromFeedback } from "@/lib/api/classic";
import { ApiError, TimeoutError } from "@/lib/api/client";
import { validClassicGuess } from "@/lib/api/game-commands";
import { changeLanguage, LANGUAGES } from ".";
import { classicOnlineCopy } from "./classic-online";

const fixture = vi.hoisted(() => ({
  api: { getRound: vi.fn(), createSubmission: vi.fn(), createSurrender: vi.fn() },
  room: { roomId:"original-room", name:"原房名／ルーム", currentRound:{roundId:"original-round"}, members:[] },
  characters:[{characterId:"original-character",displayName:"来源原名",localizedNames:{"zh-Hant":"已確認地區名",ja:"確認済みの名前"},aliases:[],avatarPath:null}],
}));
vi.mock("@/lib/api", () => ({useApi: () => ({api:fixture.api,session:{sessionId:"original-session"}})}));
vi.mock("@/lib/api/queries", () => ({useCharacters: () => ({data:{items:fixture.characters},isLoading:false,isError:false})}));
vi.mock("@/lib/api/room-queries", () => ({useRoom: () => ({data:{room:fixture.room},isLoading:false,isError:false,refetch:vi.fn()})}));
vi.mock("@/lib/realtime/use-realtime", () => ({useRealtime: () => ({status:"online"})}));
vi.mock("@/features/rooms/team-round-control", () => ({TeamRoundControl: () => null}));
vi.mock("@/components/uma/search-input", () => ({SearchInput: ({items,onSelect,disabled}:{items:{characterId:string}[];onSelect:(item:unknown)=>void;disabled:boolean}) => <>{items.map(item=><button key={item.characterId} disabled={disabled} onClick={() => onSelect(item)}>{item.characterId === "original-character" ? "选择原角色" : `选择 ${item.characterId}`}</button>)}</>}));

function view(overrides: Record<string, unknown> = {}) {
  return {
    round:{roundId:"original-round",modeId:"classic",phase:"collecting",phaseVersion:3,paused:false,deadlineAt:"2026-11-11T18:00:00Z"},
    publicState:{pool:[{id:"original-character",name:"来源原名",imagePath:""}],config:{wrongBudget:8},self:{attempts:0,remainingWrong:8,solved:false,failed:false,rows:[]},shadows:[]},
    privateState:{},submissionRevision:4,controlGeneration:2,canSubmit:true,viewerRole:"player",...overrides,
  };
}
function show() {return render(<MemoryRouter initialEntries={["/rooms/original-room"]}><Routes><Route path="/rooms/:roomId" element={<ClassicRoomPage/>}/></Routes></MemoryRouter>);}
const language = (lang: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lang); });
function leaves(value:unknown,prefix=""):Record<string,string>{if(typeof value==="string")return {[prefix]:value};return Object.assign({},...Object.entries(value as object).map(([k,v])=>leaves(v,`${prefix}.${k}`)));}

beforeEach(() => {vi.clearAllMocks();sessionStorage.clear();fixture.api.getRound.mockResolvedValue({data:view()});});
afterEach(async () => {cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();sessionStorage.clear();await changeLanguage("zh-Hans");});

it("搜索仅显示本局允许角色，目录未收录的旧局角色仍可选", async () => {
  const data = view();
  data.publicState.pool = [{id:"1127",name:"超常骏骥",imagePath:""}];
  fixture.api.getRound.mockResolvedValue({data});
  fixture.api.createSubmission.mockResolvedValueOnce({data:{outcome:"accepted",submissionRevision:5}});
  show();
  fireEvent.click(await screen.findByRole("button",{name:"选择 1127"}));
  expect(screen.queryByRole("button",{name:"选择原角色"})).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"猜一次"}));
  await waitFor(()=>expect(fixture.api.createSubmission).toHaveBeenCalledOnce());
  expect(fixture.api.createSubmission.mock.calls[0]?.[1].modeAction.characterId).toBe("1127");
});

it("本局未提供候选时不放开全角色搜索", async () => {
  const data = view();
  data.publicState.pool = [];
  fixture.api.getRound.mockResolvedValue({data});
  show();
  await screen.findByRole("button",{name:"猜一次"});
  expect(screen.queryByRole("button",{name:"选择原角色"})).toBeNull();
  expect(screen.getByRole("button",{name:"猜一次"}).hasAttribute("disabled")).toBe(true);
});

it("角色被拒绝时三语解释重新选择，不显示通用规则错误或后端细节", async () => {
  fixture.api.createSubmission.mockRejectedValueOnce(new ApiError({status:422,code:"unknown_character",message:"private pool detail",requestId:"request-character"}));
  show();
  fireEvent.click(await screen.findByRole("button",{name:"选择原角色"}));
  fireEvent.click(screen.getByRole("button",{name:"猜一次"}));
  await screen.findByText("这局不能选择这位角色");
  await language("zh-Hant");expect(document.body.textContent).toContain("這局無法選擇這位角色");
  await language("ja");expect(document.body.textContent).toContain("このゲームでは選べないウマ娘です");
  expect(document.body.textContent).not.toMatch(/private pool detail|unknown_character|内容还需要调整/);
  expect(fixture.api.createSubmission).toHaveBeenCalledOnce();
});

it("联机三语键与原参数齐全，旧内部阶段不透出", async () => {
  const source=leaves(classicOnlineCopy["zh-Hans"]);
  for(const lang of LANGUAGES){const target=leaves(classicOnlineCopy[lang]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[k,v]of Object.entries(source)){expect(target[k]?.trim()).toBeTruthy();const params=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(params(target[k]!)).toEqual(params(v));}}
  fixture.api.getRound.mockResolvedValue({data:view({round:{...view().round,phase:"private-phase-token"}})});
  show();await screen.findByText("正在确认进度");await language("ja");expect(document.body.textContent).not.toContain("private-phase-token");expect(document.body.textContent).toContain("進行状況を確認中");
});

it("已选角色切语保持原ID、准备中不重猜，接受后才记录反馈", async () => {
  let done!:(result:unknown)=>void;fixture.api.createSubmission.mockImplementationOnce(()=>new Promise(resolve=>{done=resolve;}));show();
  fireEvent.click(await screen.findByRole("button",{name:"选择原角色"}));await language("ja");expect(document.body.textContent).toContain("選択中：確認済みの名前");
  fireEvent.click(screen.getByRole("button",{name:"予想する"}));await waitFor(()=>expect(fixture.api.createSubmission).toHaveBeenCalledOnce());await language("zh-Hant");
  expect(fixture.api.createSubmission.mock.calls[0]?.[1]).toEqual({phaseVersion:3,submissionRevision:4,controlGeneration:2,modeAction:{kind:"classic.guess",characterId:"original-character"}});
  expect(screen.getByRole("button",{name:"正在送出……"}).hasAttribute("disabled")).toBe(true);
  await act(async()=>{done({data:{outcome:"accepted",submissionRevision:5}});});
  expect(fixture.api.createSubmission).toHaveBeenCalledOnce();expect(document.body.textContent).toContain("原房名／ルーム");
});

it("丢失响应切语和重新挂载后确认原请求，不被新阶段版本和新控制权覆盖", async () => {
  fixture.api.createSubmission.mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce({data:{outcome:"accepted",submissionRevision:5}});
  const rendered=show();fireEvent.click(await screen.findByRole("button",{name:"选择原角色"}));fireEvent.click(screen.getByRole("button",{name:"猜一次"}));await screen.findByRole("button",{name:"确认上次猜测"});
  const original=fixture.api.createSubmission.mock.calls[0];expect(validClassicGuess(original?.[1])).toBe(true);
  await language("ja");expect(screen.getByRole("button",{name:"前回の予想を確認"})).toBeTruthy();expect(fixture.api.createSubmission).toHaveBeenCalledOnce();rendered.unmount();
  fixture.api.getRound.mockResolvedValue({data:view({round:{...view().round,phaseVersion:9},submissionRevision:7,controlGeneration:8})});
  show();await screen.findByRole("button",{name:"前回の予想を確認"});await screen.findByText("あと間違えられる回数");
  fireEvent.click(screen.getByRole("button",{name:"前回の予想を確認"}));await waitFor(()=>expect(fixture.api.createSubmission).toHaveBeenCalledTimes(2));
  expect(fixture.api.createSubmission.mock.calls[1]).toEqual(original);
  await waitFor(()=>expect(screen.queryByRole("button",{name:"前回の予想を確認"})).toBeNull());expect(sessionStorage.length).toBe(0);
});

it("非法恢复数据不可提交，读取最新快照失败不能解锁", async () => {
  sessionStorage.setItem("uma:game-command:original-session:original-round:classic",JSON.stringify({key:"a".repeat(48),createdAt:Date.now(),body:{...view(),modeAction:{kind:"classic.guess",characterId:"changed"}}}));show();
  await screen.findByRole("button",{name:"查看最新记录"});fixture.api.getRound.mockRejectedValueOnce(new Error("private read failure"));
  fireEvent.click(screen.getByRole("button",{name:"查看最新记录"}));await waitFor(()=>expect(fixture.api.getRound).toHaveBeenCalledTimes(2));
  await dismissFault();expect(screen.getByRole("button",{name:"查看最新记录"})).toBeTruthy();expect(fixture.api.createSubmission).not.toHaveBeenCalled();expect(document.body.textContent).not.toContain("private read failure");
});

it("真实本人认输字段显示结束并禁止再猜，不把原始数据标识当文案", async () => {
  fixture.api.getRound.mockResolvedValue({data:view({privateState:{surrendered:true}})});show();await screen.findByRole("heading",{name:"已认输"});
  expect(screen.queryByRole("button",{name:"猜一次"})).toBeNull();await language("ja");expect(screen.getByRole("heading",{name:"降参しました"})).toBeTruthy();expect(screen.getByRole("button",{name:"降参する"}).hasAttribute("disabled")).toBe(true);
});

it("真实嵌套回执立即显示九项和剩余次数，不等待额外快照、不改原角色", async () => {
  const cells=["star","surface","distance","style","sex","g1","g23","grade","dormitory"].map(field=>({field,value:"原值",status:"wrong"}));
  const row={characterId:"original-character",displayName:"来源原名",attempt:1,correct:false,cells};
  expect(normalizeRowFromFeedback({row,remainingWrong:7})?.characterId).toBe("original-character");
  fixture.api.createSubmission.mockResolvedValueOnce({data:{outcome:"accepted",submissionRevision:5,feedback:{row,remainingWrong:7}}});
  fixture.api.getRound.mockResolvedValueOnce({data:view()}).mockImplementationOnce(()=>new Promise(()=>{}));
  show();fireEvent.click(await screen.findByRole("button",{name:"选择原角色"}));fireEvent.click(screen.getByRole("button",{name:"猜一次"}));
  await screen.findByText("共 1 次");expect(document.querySelectorAll('tbody td[data-state]')).toHaveLength(9);
  expect(document.body.textContent).toContain("7");expect(fixture.api.createSubmission).toHaveBeenCalledOnce();
});

it("分享不因自己猜中泄露尚未揭晓答案，已揭晓可用确认地区名且零猜测也可分享", async () => {
  const writeText=vi.fn().mockResolvedValue(undefined);vi.stubGlobal("navigator",{...navigator,clipboard:{writeText}});
  const base=normalizeClassicRound(view() as never)!;
  const result=render(<MemoryRouter><ClassicEndPanel round={{...base,solved:true,attempts:1,rows:[{characterId:"original-character",displayName:"不该泄露的名字",attempt:1,correct:true,cells:[],avatarPath:null,avatarRemoteUrl:null}]}}/></MemoryRouter>);
  fireEvent.click(screen.getByRole("button",{name:"复制战绩"}));await waitFor(()=>expect(writeText).toHaveBeenCalledOnce());expect(writeText.mock.calls[0]?.[0]).not.toMatch(/来源原名|不该泄露|original-character|答案是/);
  await language("ja");expect(writeText).toHaveBeenCalledOnce();result.unmount();
  render(<MemoryRouter><ClassicEndPanel round={{...base,phase:"settled",attempts:0,revealedAnswerCharacterId:"original-character",revealedAnswerName:"来源原名"}}/></MemoryRouter>);
  fireEvent.click(screen.getByRole("button",{name:"結果をコピー"}));await waitFor(()=>expect(writeText).toHaveBeenCalledTimes(2));expect(writeText.mock.calls[1]?.[0]).toContain("正解は「確認済みの名前」でした");expect(writeText.mock.calls[1]?.[0]).toContain("0 回");expect(writeText.mock.calls[1]?.[0]).not.toContain("original-character");
});

it("a spent duel budget waits without revealing or sharing the answer, then shows the shared final result",async()=>{
 for(const lang of LANGUAGES){
  await language(lang);
  const source=view();source.publicState.config={wrongBudget:8,winPolicy:"first-correct"} as typeof source.publicState.config;
  source.publicState.self={...source.publicState.self,attempts:8,remainingWrong:0,failed:true};
  const waiting=normalizeClassicRound(source as never)!;
  const panel=render(<MemoryRouter><ClassicEndPanel round={waiting}/></MemoryRouter>);
  expect(screen.getByRole("heading",{name:classicOnlineCopy[lang].waitingOpponent})).toBeTruthy();
  expect(panel.container.textContent).not.toContain(classicOnlineCopy[lang].answer);
  expect(screen.queryByRole("link",{name:classicOnlineCopy[lang].home})).toBeNull();
  panel.rerender(<MemoryRouter><ClassicEndPanel round={{...waiting,phase:"settled",revealedAnswerCharacterId:"original-character",revealedAnswerName:"来源原名",anySolved:true}}/></MemoryRouter>);
  expect(screen.getByRole("heading",{name:classicOnlineCopy[lang].lost})).toBeTruthy();
  panel.rerender(<MemoryRouter><ClassicEndPanel round={{...waiting,phase:"settled",anySolved:false}}/></MemoryRouter>);
  expect(screen.getByRole("heading",{name:classicOnlineCopy[lang].noWinner})).toBeTruthy();panel.unmount();
 }
});

it("认输直接判对手胜，三语结果和分享不冒充猜中", async () => {
  const writeText=vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal("navigator",{...navigator,clipboard:{writeText}});
  for (const lang of LANGUAGES) {
    await language(lang);
    const base=view();
    const winning=normalizeClassicRound({...base,round:{...base.round,phase:"settled"},publicState:{...base.publicState,config:{wrongBudget:8,winPolicy:"first-correct"},subjects:[{subjectId:"winner"},{subjectId:"loser",surrendered:true}],self:{...base.publicState.self,subjectId:"winner",attempts:8,remainingWrong:0,failed:true},surrenderWinnerSubjectId:"winner"}} as never)!;
    expect(winning.wonBySurrender).toBe(true);
    expect(winning.solved).toBe(false);
    const panel=render(<MemoryRouter><ClassicEndPanel round={winning}/></MemoryRouter>);
    expect(screen.getByRole("heading",{name:classicOnlineCopy[lang].wonBySurrender})).toBeTruthy();
    expect(panel.container.textContent).toContain(classicOnlineCopy[lang].wonBySurrenderDetail);
    fireEvent.click(screen.getByRole("button",{name:lang==="ja"?"結果をコピー":lang==="zh-Hant"?"複製遊玩結果":"复制战绩"}));
    await waitFor(()=>expect(writeText).toHaveBeenLastCalledWith(classicOnlineCopy[lang].shareWonBySurrender));
    const lost=normalizeClassicRound({...base,round:{...base.round,phase:"settled"},publicState:{...base.publicState,config:{wrongBudget:8,winPolicy:"first-correct"},self:{...base.publicState.self,subjectId:"loser",surrendered:true},surrenderWinnerSubjectId:"winner"}} as never)!;
    expect(lost.lostBySurrender).toBe(true);
    panel.rerender(<MemoryRouter><ClassicEndPanel round={lost}/></MemoryRouter>);
    expect(panel.container.textContent).toContain(classicOnlineCopy[lang].lostBySurrenderDetail);
    expect(panel.container.textContent).not.toContain(classicOnlineCopy[lang].noWinnerDetail);
    panel.unmount();
  }
});
