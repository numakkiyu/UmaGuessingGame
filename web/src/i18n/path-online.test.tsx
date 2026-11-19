import {act, cleanup, fireEvent, render, screen, waitFor, within} from "@testing-library/react";
import {afterEach, beforeEach, expect, it, vi} from "vitest";
import {MemoryRouter, Route, Routes} from "react-router";
import {CoRacePathRoundPage, PathRevealPanel, loadPathDraft, savePathDraft, missingPathStep} from "@/features/modes/co-race-path-round-page";
import {normalizePathRound, pathDraftValid} from "@/lib/api/co-race-path";
import {validPathRoute} from "@/lib/api/game-commands";
import {ApiError, TimeoutError} from "@/lib/api/client";
import {changeLanguage, LANGUAGES} from ".";
import {pathOnlineCopy} from "./path-online";
const f = vi.hoisted(() => ({api: {getRound: vi.fn(), getPathNeighbors: vi.fn(), createSubmission: vi.fn()}, room: {roomId: "room-path", currentRound: {roundId: "round-path"}}, characters: [{characterId: "a", displayName: "来源甲", localizedNames: {ja: "確認した名前甲", "zh-Hant": "來源甲"}}, {characterId: "b", displayName: "来源乙", localizedNames: {ja: "確認した名前乙", "zh-Hant": "來源乙"}}]}));
vi.mock("@/lib/api", () => ({useApi: () => ({api: f.api, session: {sessionId: "session-path"}})}));
vi.mock("@/lib/api/queries", () => ({useCharacters: () => ({data: {items: f.characters}})}));
vi.mock("@/lib/api/room-queries", () => ({useRoom: () => ({data: {room: f.room}, isLoading: false, isError: false, refetch: vi.fn()})}));
vi.mock("@/lib/realtime/use-realtime", () => ({useRealtime: () => ({status: "online"})}));
vi.mock("@/features/rooms/team-round-control", () => ({TeamRoundControl: () => null}));
const nodes = ["a", "b", "c", "d"].map((characterId, i) => ({characterId, name: ["来源甲", "来源乙", "来源丙", "来源丁"][i], imagePath: ""}));
const edges = [["a", "b"], ["b", "d"], ["b", "c"]].map(([from, to], i) => ({from, to, raceId: `race-${i}`, raceName: "有马纪念", date: "1989-12-24"}));
function view(openBook = false) {return {round: {roundId: "round-path", modeId: "co-race-path", phase: "collecting", phaseVersion: 3, deadlineAt: new Date(Date.now() + 180000).toISOString(), paused: false}, publicState: {config: {openBook, maxEdges: 6, questionSeconds: 180}, start: nodes[0], goal: nodes[3], nodes, edges: openBook ? edges : [], shortestEdges: 2, self: {subjectId: "p1", submitted: false, revisions: 0, path: [] as string[], edges: 0, arrived: false, scoreUnits: 0}}, canSubmit: true, submissionRevision: 0, controlGeneration: 0};}
const show = () => render(<MemoryRouter initialEntries={["/rooms/room-path"]}><Routes><Route path="/rooms/:roomId" element={<CoRacePathRoundPage/>}/></Routes></MemoryRouter>);
const lang = (value: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(value); });
const connect = (name: string) => fireEvent.click(screen.getAllByRole("button", {name: `连接「${name}」`})[0]!);
function leaves(value: unknown, path = ""): Record<string, string> {if (typeof value === "string") return {[path]: value}; return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${path}.${key}`)));}
beforeEach(() => {vi.clearAllMocks(); localStorage.clear(); sessionStorage.clear(); f.api.getRound.mockResolvedValue({data: view()});});
afterEach(async () => {cleanup(); vi.restoreAllMocks(); localStorage.clear(); sessionStorage.clear(); await changeLanguage("zh-Hans");});

it("三语键插值一致，闭卷列角色不读取或显示同场关系，切语不改变题目", async () => {
  const source = leaves(pathOnlineCopy["zh-Hans"]);
  for (const language of LANGUAGES) {const target = leaves(pathOnlineCopy[language]); expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort()); for (const [key, text] of Object.entries(source)) {const vars = (s: string) => [...s.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort(); expect(vars(target[key]!)).toEqual(vars(text));}}
  const data = view(); data.publicState.edges = edges; f.api.getRound.mockResolvedValue({data}); show(); await screen.findAllByRole("button", {name: "连接「来源乙」"}); expect(screen.getAllByRole("button", {name: "连接「来源丙」"}).length).toBe(2); expect(document.body.textContent).not.toContain("有马纪念"); await lang("ja"); expect(screen.getAllByRole("button", {name: "「確認した名前乙」につなぐ"}).length).toBe(2); expect(screen.queryByText("ほかの出走記録を調べる")).toBeNull(); expect(f.api.getPathNeighbors).not.toHaveBeenCalled(); expect(f.api.getRound).toHaveBeenCalledOnce(); expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("开卷只列当前节点的公开邻居并保留查其他记录功能，检索切语保持", async () => {
  f.api.getRound.mockResolvedValue({data: view(true)}); show(); await screen.findAllByRole("button", {name: "连接「来源乙」"}); expect(screen.queryByRole("button", {name: "连接「来源丙」"})).toBeNull(); expect(screen.getAllByText(/有马纪念/).length).toBeGreaterThan(0);
  fireEvent.change(screen.getByRole("searchbox", {name: "搜索角色"}), {target: {value: "確認した名前乙"}}); await lang("ja"); expect((screen.getByRole("searchbox", {name: "ウマ娘を探す"}) as HTMLInputElement).value).toBe("確認した名前乙"); expect(screen.getAllByRole("button", {name: "「確認した名前乙」につなぐ"}).length).toBe(2); expect(screen.getByText("ほかの出走記録を調べる")).toBeTruthy(); expect(f.api.getPathNeighbors).not.toHaveBeenCalled();
});
it("未确认草稿按身份恢复，切语不提交，成功后可继续修订而不锁定", async () => {
  const rendered = show(); await screen.findAllByRole("button", {name: "连接「来源乙」"}); connect("来源乙"); const raw = localStorage.getItem("umaguess:path-draft:session-path:round-path:path:p1"); expect(JSON.parse(raw!).path).toEqual(["a", "b"]); await lang("zh-Hant"); expect(localStorage.getItem("umaguess:path-draft:session-path:round-path:path:p1")).toBe(raw); rendered.unmount(); show(); const trail = await screen.findByRole("list", {name: "從起點出發的路線"}); expect(within(trail).getAllByRole("listitem")).toHaveLength(2); expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("丢响应后刷新切语和版本改变仍只确认原body/key，已提交路线可再保存", async () => {
  f.api.createSubmission.mockRejectedValueOnce(new TimeoutError(100)).mockResolvedValueOnce({data: {outcome: "accepted"}}); const rendered = show(); await screen.findAllByRole("button", {name: "连接「来源乙」"}); connect("来源乙"); fireEvent.click(screen.getByRole("button", {name: "保存本次路线"})); await screen.findByRole("button", {name: "确认上次路线"}); const original = f.api.createSubmission.mock.calls[0]; expect(validPathRoute(original?.[1])).toBe(true); await lang("ja"); rendered.unmount();
  const data = view(); data.round.phaseVersion = 9; data.submissionRevision = 1; data.controlGeneration = 2; Object.assign(data.publicState.self, {submitted: true, revisions: 1, path: ["a", "b"]}); f.api.getRound.mockResolvedValue({data}); show(); fireEvent.click(await screen.findByRole("button", {name: "前回のルートを確認"})); await waitFor(() => expect(f.api.createSubmission).toHaveBeenCalledTimes(2)); expect(f.api.createSubmission.mock.calls[1]).toEqual(original); await waitFor(() => expect(screen.getByRole("button", {name: "今回のルートを保存"}).hasAttribute("disabled")).toBe(false)); expect(screen.getAllByRole("button", {name: "「来源丁」につなぐ"}).every(button => !button.hasAttribute("disabled"))).toBe(true);
});
it("拒绝明确第几步但不直出原错误或机器字段，已显示错误随切语更新", async () => {
  const error = new ApiError({status: 422, code: "missing_edge", message: "private answer and SQL", requestId: null, fieldErrors: [{field: "modeAction.characterIds[2]", message: "missing_edge"}]}); f.api.createSubmission.mockRejectedValueOnce(error); show(); await screen.findAllByRole("button", {name: "连接「来源丙」"}); connect("来源丙"); connect("来源丁"); fireEvent.click(screen.getByRole("button", {name: "保存本次路线"})); await screen.findByText("第 2 步没有找到本局收录的同场记录，请调整这一步"); await lang("ja"); expect(screen.getByText("2 歩目に同じレースへの出走記録がありません。この区間を変えてみてください")).toBeTruthy(); expect(document.body.textContent).not.toMatch(/private answer|SQL|modeAction|missing_edge/); expect(screen.getByRole("button", {name: "ひとつ戻る"}).hasAttribute("disabled")).toBe(false); expect(missingPathStep(new ApiError({status: 422, code: "missing_edge", message: "step 4 private", requestId: null}))).toBeNull();
});
it("存储失败不假称草稿保存，畸形重复/过期/外题路线不恢复", async () => {
  const set = vi.spyOn(localStorage, "setItem").mockImplementation(() => { throw Error("blocked"); }); expect(savePathDraft("bad", ["a", "b"])).toBe(false); show(); fireEvent.click(await screen.findByRole("button", {name: "保存草稿（不计分）"})); await screen.findByText(pathOnlineCopy["zh-Hans"].saveFailed); expect(screen.queryByText(pathOnlineCopy["zh-Hans"].savedDraft)).toBeNull(); set.mockRestore();
  for (const value of [{path: ["a", "a"], savedAt: Date.now()}, {path: ["a"], savedAt: Date.now() - 86400001}, {path: ["a"], savedAt: Date.now() + 120000}]) {localStorage.setItem("umaguess:path-draft:bad", JSON.stringify(value)); expect(loadPathDraft("bad")).toBeNull();}
  expect(pathDraftValid(["a", "private-node"], normalizePathRound(view() as never)!)).toBe(false); expect(validPathRoute({phaseVersion: 1, submissionRevision: 0, controlGeneration: 0, modeAction: {kind: "path.route", characterIds: ["a", "a"]}})).toBe(false);
});
it("讲解暂停只读都禁新操作，过期命令刷新失败不能解除保护", async () => {
  for (const kind of ["briefing", "paused", "readonly"]) {const data = view(); if (kind === "briefing") data.round.phase = "briefing"; if (kind === "paused") data.round.paused = true; if (kind === "readonly") data.canSubmit = false; f.api.getRound.mockResolvedValue({data}); const rendered = show(); await screen.findAllByRole("button", {name: "连接「来源乙」"}); expect(screen.getAllByRole("button", {name: "连接「来源乙」"}).every(button => button.hasAttribute("disabled"))).toBe(true); rendered.unmount();}
  f.api.getRound.mockResolvedValue({data: view()}); sessionStorage.setItem("uma:game-command:session-path:round-path:path", JSON.stringify({key: "a".repeat(48), createdAt: Date.now(), body: {phaseVersion: 1, submissionRevision: 0, controlGeneration: 0, modeAction: {kind: "path.route", characterIds: ["a", "a"]}}})); show(); await screen.findByRole("button", {name: "查看最新记录"}); f.api.getRound.mockRejectedValueOnce(Error("private")); fireEvent.click(screen.getByRole("button", {name: "查看最新记录"})); await screen.findByRole("button", {name: "查看最新记录"}); expect(f.api.createSubmission).not.toHaveBeenCalled();
});
it("揭晓只显示最后确认的路线及其依据，不猜旧档缺失证据，取消不显示解", async () => {
  const data = view(); Object.assign(data.publicState, {answer: {path: ["a", "b", "d"], evidence: [edges[0], edges[1]]}}); Object.assign(data.publicState.self, {submitted: true, path: ["a", "b"], arrived: false, edges: 1, evidence: [edges[0]]}); const round = normalizePathRound(data as never)!;
  expect(round.self?.evidence).toHaveLength(1); expect(round.answer?.evidence).toHaveLength(2); const rendered = render(<MemoryRouter><PathRevealPanel round={round} scoreUnits={0}/></MemoryRouter>); expect(within(screen.getByRole("region", {name: "你的路线记录"})).getAllByRole("listitem")).toHaveLength(2); await lang("ja"); expect(screen.getAllByText(/有馬記念/).length).toBeGreaterThan(0); rendered.unmount(); render(<MemoryRouter><PathRevealPanel round={round} cancelled scoreUnits={10000}/></MemoryRouter>); expect(screen.queryByText("100")).toBeNull(); expect(screen.queryByRole("list")).toBeNull(); expect(screen.getByText(pathOnlineCopy.ja.cancelledDetail)).toBeTruthy();
});
