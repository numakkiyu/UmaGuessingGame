import { dismissFault } from "@/test/dismiss-fault";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { RapportRoundPage } from "./rapport-round-page";
import type * as Snapshot from "@/lib/api/round-snapshot";
import {saveRapportDraft,loadRapportDraft} from "@/lib/local/rapport-online-draft";
import { TimeoutError,ApiError } from "@/lib/api/client";
import {changeLanguage} from "@/i18n";
import {rapportOnlineCopy} from "@/i18n/rapport-online";
import { normalizeRapportRound } from "@/lib/api/modes-g09-g12";

const f = vi.hoisted(() => ({ api: { createSubmission: vi.fn() }, view: {} as Record<string, unknown>, refresh: vi.fn(), room: { roomId: "room", name: "默契游玩会", currentRound: { roundId: "round" }, members: [{ membershipId: "me", displayName: "特别周", publicCode: "000123" }, { membershipId: "partner", displayName: "特别周", publicCode: "456789" }], teams: [] } }));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: f.api, session: { sessionId: "person" } }) }));
vi.mock("@/lib/api/room-queries", () => ({ useRoom: () => ({ data: { room: f.room } }) }));
vi.mock("@/lib/api/round-snapshot", async original => ({ ...await original<typeof Snapshot>(), useRoundSnapshot: () => ({ ...f.view, refetch: f.refresh, refreshAfterSubmit: f.refresh }) }));
vi.mock("@/lib/realtime/use-realtime", () => ({ useRealtime: () => ({ status: "online", transport: "sse" }) }));
vi.mock("@/features/rooms/team-round-control", () => ({ TeamRoundControl: () => null }));
const question = { questionId: "q1", prompt: "训练后想做什么？", options: ["吃饭", "补觉", "加练", "看电影"].map((label, i) => ({ optionId: `q1-o${i}`, label })), chooserId: "partner", selfIsChooser: false, answered: false, locked: false };
const ps = () => (f.view.raw as { publicState: Record<string, unknown> }).publicState;
function show() { Object.assign(f.view.raw as object,{round:f.view.meta}); return render(<MemoryRouter initialEntries={["/rooms/room"]}><Routes><Route path="/rooms/:roomId" element={<RapportRoundPage />} /></Routes></MemoryRouter>); }
beforeEach(async () => {
  localStorage.clear();await changeLanguage("zh-Hans");
  sessionStorage.clear(); vi.clearAllMocks(); f.refresh.mockResolvedValue(true);
  f.view = { meta: { roundId:"round",phase: "collecting", phaseVersion: 3, paused: false }, canSubmit: true, submissionRevision: 0, submissionRevisionsBySlot: {}, controlGeneration: 0, raw: { publicState: { currentQuestion: 1, questionCount: 5, totalSlots: 5, questions: [question], self: { subjectId: "me", answers: [] }, groups: [{ groupId: "party", chooserId: "partner", answered: 0, members: 2 }], groupId: "party" } } };
});
afterEach(cleanup);
it("selects a draft without sending, then explicitly confirms a personal prediction", async () => {
  f.api.createSubmission.mockResolvedValue({ data: { outcome: "accepted" } });show();
  expect(screen.getByText(/猜猜特别周 · 456789/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "吃饭" }));
  expect(f.api.createSubmission).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "确认我的预测" }));
  await waitFor(() => expect(f.api.createSubmission).toHaveBeenCalledTimes(1));
  expect(f.api.createSubmission.mock.calls[0]?.[1].modeAction).toEqual({ kind: "rapport.answer", optionId: "q1-o0" });
});
it("restores the original uncertain answer after a new question and refresh, without automatic retry", async () => {
  f.api.createSubmission.mockRejectedValueOnce(new TimeoutError(10)).mockResolvedValueOnce({ data: { outcome: "accepted" } });
  const page = show();fireEvent.click(screen.getByRole("button", { name: "吃饭" }));fireEvent.click(screen.getByRole("button", { name: "确认我的预测" }));
  await screen.findByRole("button", { name: "确认刚才的选择" });const original = f.api.createSubmission.mock.calls[0];page.unmount();
  ps().currentQuestion = 2;ps().questions = [{ ...question, locked: true }, { ...question, questionId: "q2", options: question.options.map(o => ({ ...o, optionId: "q2-" + o.optionId })) }];
  show();expect(f.api.createSubmission).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("button", { name: "吃饭" }).hasAttribute("disabled")).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "确认刚才的选择" }));
  await waitFor(() => expect(f.api.createSubmission).toHaveBeenCalledTimes(2));expect(f.api.createSubmission.mock.calls[1]).toEqual(original);
});
it("refresh displays the recorded own option", () => {
  ps().questions = [{ ...question, answered: true, selfOptionId: "q1-o1" }];f.view.canSubmit = false;
  show();expect(screen.getByRole("button", { name: "补觉" }).getAttribute("aria-pressed")).toBe("true");expect(screen.queryByRole("button", { name: "确认我的预测" })).toBeNull();
});
it("a new question discards the old draft and pause disables the selection", () => {
  let page = show();fireEvent.click(screen.getByRole("button", { name: "吃饭" }));
  expect(screen.getByRole("button", { name: "吃饭" }).getAttribute("aria-pressed")).toBe("true");
  ps().currentQuestion = 2;ps().questions = [{ ...question, locked: true }, { ...question, questionId: "q2" }];
  page.unmount();page=show();
  expect(screen.getByRole("button", { name: "吃饭" }).getAttribute("aria-pressed")).toBe("false");
  f.view.meta = { roundId:"round",phase: "collecting", phaseVersion: 3, paused: true };
  page.unmount();show();
  expect(screen.getByRole("button", { name: "吃饭" }).hasAttribute("disabled")).toBe(true);
});
it("paused and briefing views cannot submit, and chooser uses a distinct action label", () => {
  ps().questions = [{ ...question, selfIsChooser: true, chooserId: "me" }];f.view.meta = { roundId:"round",phase: "briefing", phaseVersion: 2, paused: false };
  show();expect(screen.getByRole("button", { name: "确认我的偏好" }).hasAttribute("disabled")).toBe(true);expect(screen.getByRole("button", { name: "吃饭" }).hasAttribute("disabled")).toBe(true);
});
it("empty preference reveals are readable and premature reveals are discarded", () => {
  const raw = { publicState: { ...ps(), questions: [{ ...question, reveal: { chosenOptionId: "q1-o0", chosenLabel: "吃饭", correctPredictorIds: [] } }] } };
  expect(normalizeRapportRound(raw).questions[0]?.reveal).toBeUndefined();
  ps().questions = [{ ...question, locked: true, reveal: { chosenOptionId: "", chosenLabel: "", correctPredictorIds: [] } }];ps().currentQuestion = 2;
  show();expect(screen.getAllByText("这题伙伴没有确认偏好，因此所有预测均不得分")[0]).toBeTruthy();expect(screen.getAllByText("你这题没有确认")[0]).toBeTruthy();
});
it("observers see progress and final scores without personal prediction feedback", () => {
  delete ps().self;ps().questions = [{ ...question, locked: true }];ps().currentQuestion = 6;f.view.meta = { roundId:"round",phase: "settled", phaseVersion: 6 };f.view.canSubmit = false;
  Object.assign(f.view.raw as object, { results: [{ subjectId: "red", scoreUnits: 8000, rank: 1 }], subjectNames: { red: "胡萝卜队" } });
  show();expect(screen.getByRole("heading", { name: "本局成绩" })).toBeTruthy();expect(screen.getAllByText("80 分")).toHaveLength(2);expect(screen.queryByText("这题没猜中，下次再试试")).toBeNull();expect(screen.queryByRole("button", { name: "确认我的预测" })).toBeNull();
});

it("保存与清空选择，暂停恢复切语刷新均保留原题草稿且不发送",async()=>{
 let page=show();fireEvent.click(screen.getByRole("button",{name:"吃饭"}));page.unmount();
 f.view.meta={roundId:"round",phase:"collecting",phaseVersion:5,paused:true};
 await act(async()=>{await changeLanguage("ja")});page=show();
 expect(screen.getByRole("button",{name:"吃饭"}).getAttribute("aria-pressed")).toBe("true");
 expect(screen.getByRole("button",{name:"吃饭"}).hasAttribute("disabled")).toBe(true);page.unmount();
 f.view.meta={roundId:"round",phase:"collecting",phaseVersion:6,paused:false};page=show();
 fireEvent.click(screen.getByRole("button",{name:rapportOnlineCopy.ja.clear}));page.unmount();show();
 expect(screen.getByRole("button",{name:"吃饭"}).getAttribute("aria-pressed")).toBe("false");
 expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("取消不显示当前未完成题目、揭晓或正式成绩，旧提前收卷提示重开",()=>{
 f.view.meta={roundId:"round",phase:"cancelled",phaseVersion:5};
 Object.assign(f.view.raw as object,{results:[{subjectId:"me",scoreUnits:10000,rank:1}]});
 const page=show();expect(screen.getByText(rapportOnlineCopy["zh-Hans"].cancelledDetail)).toBeTruthy();
 expect(screen.queryByRole("heading",{name:"本局成绩"})).toBeNull();expect(screen.queryByRole("button",{name:"吃饭"})).toBeNull();page.unmount();
 f.view.meta={roundId:"round",phase:"locked",phaseVersion:5};show();expect(screen.getByText(rapportOnlineCopy["zh-Hans"].legacy)).toBeTruthy();
});
it("到时禁止新提交",async()=>{
 f.view.meta={roundId:"round",phase:"collecting",phaseVersion:3,deadlineAt:new Date(Date.now()-1000).toISOString()};show();
 await waitFor(()=>expect(screen.getByRole("button",{name:"吃饭"}).hasAttribute("disabled")).toBe(true));
 expect(f.api.createSubmission).not.toHaveBeenCalled();
});

it("确认旧操作使用原body与key，不清除新题草稿，读取失败仍保留确认入口",async()=>{
 f.api.createSubmission.mockRejectedValueOnce(new TimeoutError(10)).mockResolvedValueOnce({data:{outcome:"accepted"}});
 const page=show();fireEvent.click(screen.getByRole("button",{name:"吃饭"}));fireEvent.click(screen.getByRole("button",{name:"确认我的预测"}));await screen.findByRole("button",{name:"确认刚才的选择"});const original=f.api.createSubmission.mock.calls[0];page.unmount();
 ps().currentQuestion=2;ps().questions=[{...question,locked:true},{...question,questionId:"q2"}];f.view.meta={roundId:"round",phase:"collecting",phaseVersion:11,paused:true};f.view.isError=true;f.view.error=new TimeoutError(10);f.view.submissionRevision=1;
 saveRapportDraft("person:round:rapport:me:q2","q1-o1");await act(async()=>{await changeLanguage("ja")});show();
 await dismissFault();fireEvent.click(screen.getByRole("button",{name:rapportOnlineCopy.ja.confirmAction}));await screen.findByText(rapportOnlineCopy.ja.accepted);expect(f.api.createSubmission.mock.calls[1]).toEqual(original);expect(loadRapportDraft("person:round:rapport:me:q2",["q1-o1"])).toEqual({optionId:"q1-o1"});expect(screen.getByRole("button",{name:"补觉"}).getAttribute("aria-pressed")).toBe("true");
});
it("错误随语言切换、拒绝内部异常，草稿保存失败如实提示",async()=>{
 f.api.createSubmission.mockRejectedValue(new ApiError({status:422,code:"duplicate_answer",message:"RAW db stack",requestId:null}));show();fireEvent.click(screen.getByRole("button",{name:"吃饭"}));fireEvent.click(screen.getByRole("button",{name:"确认我的预测"}));await screen.findByText(rapportOnlineCopy["zh-Hans"].errors.duplicate_answer);await act(async()=>{await changeLanguage("ja")});expect(screen.getByText(rapportOnlineCopy.ja.errors.duplicate_answer)).toBeTruthy();expect(document.body.textContent).not.toMatch(/RAW|duplicate_answer|phaseVersion/);cleanup();
 vi.spyOn(localStorage,"setItem").mockImplementation(()=>{throw Error("full")});show();fireEvent.click(screen.getByRole("button",{name:"吃饭"}));expect(screen.getByText(rapportOnlineCopy.ja.saveFailed)).toBeTruthy();vi.restoreAllMocks();
});


it("does not report a zero-slot tally before any question is revealed", () => {
  Object.assign(ps(), { revealedSlots: 0, revealedHits: 0 });
  const page = show();
  expect(screen.queryByText(rapportOnlineCopy["zh-Hans"].tally.replace("{{hits}}", "0").replace("{{slots}}", "0"))).toBeNull();
  Object.assign(ps(), { revealedSlots: 2, revealedHits: 1 });
  page.unmount();show();
  expect(screen.getByText(rapportOnlineCopy["zh-Hans"].tally.replace("{{hits}}", "1").replace("{{slots}}", "2"))).toBeTruthy();
});
