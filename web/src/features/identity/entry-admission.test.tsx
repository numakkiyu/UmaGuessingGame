import { StrictMode } from "react";
import { EntryAdmission as DeferredAdmission } from "./entry-admission";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import i18n from "@/i18n";
import "@/i18n/admission";
import { AdmissionController as EntryAdmission } from "./admission-controller";
import { ApiError, TimeoutError } from "@/lib/api/client";
const state=vi.hoisted(()=>({handler:null as null|((reason?: "entry" | "traffic")=>Promise<boolean>),options:null as null|Record<string,unknown>,config:vi.fn(),status:vi.fn(),verify:vi.fn(),render:vi.fn(),remove:vi.fn(),signal:vi.fn()}));
vi.mock("@/lib/api",()=>({api:{getPublicConfig:state.config,getAdmissionStatus:state.status,verifyAdmission:state.verify},setAdmissionHandler:(handler:typeof state.handler)=>{state.handler=handler}}));
vi.mock("./turnstile",()=>({loadTurnstile:async()=>({render:(element:HTMLElement,options:Record<string,unknown>)=>{state.options=options;state.render(element,options);return "widget"},remove:state.remove})}));
vi.mock("./browser-signal",()=>({browserSignal:state.signal}));
beforeEach(async()=>{await i18n.changeLanguage("zh-Hans");state.config.mockReset().mockResolvedValue({data:{admission:{enabled:true,siteKey:"public-site"}}});state.status.mockReset().mockResolvedValue({data:{enabled:true,required:false,verified:false}});state.signal.mockReset().mockResolvedValue("a".repeat(64));state.verify.mockReset().mockResolvedValue({data:{verified:true}});state.render.mockClear();state.remove.mockClear();state.options=null;state.handler=null});
afterEach(()=>{cleanup();vi.useRealTimers();});
async function open(){render(<EntryAdmission/>);let result:Promise<boolean>;await act(async()=>{result=state.handler!()});await screen.findByRole("dialog");await waitFor(()=>expect(state.render).toHaveBeenCalledOnce());return {result:result!}}
function solve(){act(()=>{(state.options?.callback as (token:string)=>void)("test-response")})}
describe("训练员入场",()=>{
 it("验证成功才继续，组件有主题标题并可读屏",async()=>{const {result}=await open();expect(screen.getByRole("heading",{name:"训练员，准备入场"})).toBeTruthy();expect((screen.getByRole("button",{name:"确认并继续"}) as HTMLButtonElement).disabled).toBe(true);solve();await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"确认并继续"}))});expect(await result).toBe(true);expect(state.verify).toHaveBeenCalledWith({token:"test-response"});await waitFor(()=>expect(state.remove).toHaveBeenCalledOnce())});
 it("同一时刻的多个操作共用一个面板，取消不调用校验",async()=>{const {result}=await open();const other=state.handler!();expect(other).toBe(result);fireEvent.click(screen.getByRole("button",{name:"先返回"}));expect(await result).toBe(false);expect(state.verify).not.toHaveBeenCalled()});
 it("过期后不能用旧结果继续",async()=>{const {result}=await open();solve();act(()=>{(state.options?.["expired-callback"] as ()=>void)()});expect((screen.getByRole("button",{name:"确认并继续"}) as HTMLButtonElement).disabled).toBe(true);expect(screen.getByText("等候有些久了，请重新完成入场确认")).toBeTruthy();fireEvent.click(screen.getByRole("button",{name:"先返回"}));await result});
 it("未知校验结果保留同一个确认结果，显式再确认",async()=>{state.verify.mockRejectedValueOnce(new TimeoutError(15000));const {result}=await open();solve();await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"确认并继续"}))});await screen.findByText(/还没收到确认结果/);expect(state.verify).toHaveBeenCalledTimes(1);await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"确认并继续"}))});expect(await result).toBe(true);expect(state.verify.mock.calls[0]?.[0]).toEqual(state.verify.mock.calls[1]?.[0])});
 it("确定失效要求重新验证，不把失败当成功",async()=>{state.verify.mockRejectedValueOnce(new ApiError({status:403,code:"admission-invalid",message:"Forbidden",requestId:null}));const {result}=await open();solve();await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"确认并继续"}))});await screen.findByText("这次确认已失效，请重新完成入场确认");expect((screen.getByRole("button",{name:"确认并继续"}) as HTMLButtonElement).disabled).toBe(true);fireEvent.click(screen.getByRole("button",{name:"先返回"}));expect(await result).toBe(false)});
 it("关闭配置不加载第三方组件",async()=>{state.config.mockResolvedValue({data:{admission:{enabled:false}}});render(<EntryAdmission/>);let result:Promise<boolean>;await act(async()=>{result=state.handler!()});expect(await result!).toBe(true);expect(state.render).not.toHaveBeenCalled()});
});


describe("入场确认的语言切换", () => {
 it.each([["zh-Hans", "zh-CN"], ["zh-Hant", "zh-TW"], ["ja", "ja"]])("新入场使用 %s", async (language, widgetLanguage) => {
  await i18n.changeLanguage(language);
  const { result } = await open();
  expect(state.options?.language).toBe(widgetLanguage);
  cleanup();
  expect(await result).toBe(false);
  expect(state.verify).not.toHaveBeenCalled();
 });
 it("确认途中切换语言不重建验证，也不重复提交", async () => {
  let done!: (value: unknown) => void;
  state.verify.mockImplementationOnce(() => new Promise(resolve => { done = resolve; }));
  const { result } = await open(); solve();
  fireEvent.click(screen.getByRole("button", { name: "确认并继续" }));
  await waitFor(() => expect(state.verify).toHaveBeenCalledOnce());
  await act(async () => { await i18n.changeLanguage("ja"); });
  expect(screen.getByRole("button", { name: "確認しています…" }).hasAttribute("disabled")).toBe(true);
  expect(screen.getByRole("heading", { name: "トレーナーさん、入場前の確認です" })).toBeTruthy();
  expect(state.render).toHaveBeenCalledOnce(); expect(state.remove).not.toHaveBeenCalled();
  await act(async () => { done({ data: { verified: true } }); });
  expect(await result).toBe(true); expect(state.verify).toHaveBeenCalledOnce();
 });
 it("未知结果切换语言后仍用原确认结果重试，并保留浏览器特征说明", async () => {
  state.config.mockResolvedValue({data:{admission:{enabled:true,siteKey:"public-site",browserSignalsEnabled:true}}});
  state.verify.mockRejectedValueOnce(new TimeoutError(15000));
  const { result } = await open(); solve();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "确认并继续" })); });
  await screen.findByText(/还没收到确认结果/);
  await act(async () => { await i18n.changeLanguage("zh-Hant"); });
  expect(screen.getByText(/不必重做下方驗證/)).toBeTruthy();
  expect(screen.getByText(/瀏覽器特徵/)).toBeTruthy();
  expect(state.render).toHaveBeenCalledOnce(); expect(state.verify).toHaveBeenCalledOnce();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "確認並繼續" })); });
  expect(await result).toBe(true);
  expect(state.verify.mock.calls[1]?.[0]).toEqual(state.verify.mock.calls[0]?.[0]);
 });
});


describe("异常流量的入场确认", () => {
 it("已确认的正常访问不会弹窗或加载验证组件", async () => {
  state.config.mockResolvedValue({ data: { admission: { enabled: true, adaptiveEnabled: true, siteKey: "public-site" } } });
  state.status.mockResolvedValue({ data: { enabled: true, required: false, verified: true } });
  render(<EntryAdmission />);
  await waitFor(() => expect(state.status).toHaveBeenCalledOnce());
  expect(screen.queryByRole("dialog")).toBeNull(); expect(state.render).not.toHaveBeenCalled();
 });
 it("重新打开页面时按后端状态显示红色全屏，确认后恢复同一个操作", async () => {
  state.config.mockResolvedValue({ data: { admission: { enabled: true, adaptiveEnabled: true, siteKey: "public-site" } } });
  state.status.mockResolvedValue({ data: { enabled: true, required: true, verified: false } });
  const prepareSession = vi.fn().mockResolvedValue({ sessionId: "same" });
  render(<EntryAdmission prepareSession={prepareSession} />);
  await screen.findByRole("heading", { name: "训练员，稍等一下" });
  await waitFor(() => expect(state.render).toHaveBeenCalledOnce());
  expect(screen.getByRole("dialog").getAttribute("data-traffic")).toBe("true");
  let result!: Promise<boolean>; act(() => { result = state.handler!("traffic"); }); solve();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "确认并继续" })); });
  expect(prepareSession).toHaveBeenCalledOnce();
  expect(screen.getByRole("dialog").getAttribute("data-success")).toBe("true");
  expect(screen.getByRole("heading", { name: "确认好了，一起开跑" })).toBeTruthy();
  await act(async () => { expect(await result).toBe(true); });
  expect(screen.queryByRole("dialog")).toBeNull();
 });
 it("普通入场升级成异常确认共用原有挑战，不重新挂载或提交", async () => {
  const { result } = await open();
  let escalated!: Promise<boolean>; act(() => { escalated = state.handler!("traffic"); });
  expect(escalated).toBe(result); expect(screen.getByRole("heading", { name: "训练员，稍等一下" })).toBeTruthy();
  expect(state.render).toHaveBeenCalledOnce(); expect(state.remove).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "先返回" })); expect(await result).toBe(false);
 });
 it("另一页面已完成确认时直接继续，不重复加载验证", async () => {
  state.status.mockResolvedValue({ data: { enabled: true, required: false, verified: true } });
  render(<EntryAdmission />); let result!: Promise<boolean>;
  await act(async () => { result = state.handler!(); });
  expect(await result).toBe(true); expect(state.render).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).toBeNull();
 });
 it("身份已过期时保留说明，不发送验证请求", async () => {
  const prepareSession = vi.fn().mockResolvedValue(null);
  render(<EntryAdmission prepareSession={prepareSession} />);
  let result!: Promise<boolean>; await act(async () => { result = state.handler!(); });
  await waitFor(() => expect(state.render).toHaveBeenCalledOnce()); solve();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "确认并继续" })); });
  expect(screen.getByText("离开有些久了，请先返回，再重新进入房间")).toBeTruthy();
  expect(state.verify).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "先返回" })); expect(await result).toBe(false);
 });
 it("关闭后没有空白占位，也不会查询异常状态", async () => {
  state.config.mockResolvedValue({ data: { admission: { enabled: false } } });
  render(<EntryAdmission />);
  let result!: Promise<boolean>; await act(async () => { result = state.handler!("traffic"); });
  expect(await result).toBe(true); expect(screen.queryByRole("dialog")).toBeNull();
  expect(document.querySelector(".uma-admission-edge")).toBeNull(); expect(state.status).not.toHaveBeenCalled(); expect(state.render).not.toHaveBeenCalled();
 });
});


describe("入场界面的延后加载", () => {
 it("StrictMode 首次按需加载仍保留原操作，不会被挂载检查取消", async () => {
  render(<StrictMode><DeferredAdmission /></StrictMode>);
  let result!: Promise<boolean>;
  await act(async () => { result = state.handler!(); });
  await screen.findByRole("dialog"); await waitFor(() => expect(state.render).toHaveBeenCalledOnce());
  solve(); await act(async () => { fireEvent.click(screen.getByRole("button", { name: "确认并继续" })); });
  await act(async () => { expect(await result).toBe(true); });
  expect(state.verify).toHaveBeenCalledOnce();
 });
 it("加载期间多次点击只保留一个请求，退出页面能结束等待", async () => {
  const view = render(<DeferredAdmission />);
  let first!: Promise<boolean>; let second!: Promise<boolean>;
  act(() => { first = state.handler!(); second = state.handler!("traffic"); });
  expect(first).toBe(second); view.unmount(); expect(await first).toBe(false);
 });
});


describe("入场确认的异步取消", () => {
 it("取消后才返回的身份恢复失败不能污染新面板", async () => {
  let complete!: (value: unknown) => void;
  const prepare = vi.fn().mockImplementationOnce(() => new Promise(resolve => {complete = resolve;}));
  render(<EntryAdmission prepareSession={prepare} />);
  let first!: Promise<boolean>; await act(async () => { first = state.handler!(); });
  await waitFor(() => expect(state.render).toHaveBeenCalledOnce()); solve();
  fireEvent.click(screen.getByRole("button", {name:"确认并继续"}));
  fireEvent.click(screen.getByRole("button", {name:"先返回"})); expect(await first).toBe(false);
  let next!: Promise<boolean>; await act(async () => {next = state.handler!();});
  await waitFor(() => expect(state.render).toHaveBeenCalledTimes(2));
  await act(async () => {complete(null);});
  expect(screen.queryByText("离开有些久了，请先返回，再重新进入房间")).toBeNull();
  expect(state.verify).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", {name:"先返回"})); expect(await next).toBe(false);
 });
 it.each(["cancel", "unmount"])("%s 后等待中的浏览器特征不能发送验证", async action => {
  let complete!: (value: string) => void;
  state.config.mockResolvedValue({data:{admission:{enabled:true,siteKey:"public-site",browserSignalsEnabled:true}}});
  state.signal.mockImplementationOnce(() => new Promise(resolve => {complete=resolve;}));
  const view=render(<EntryAdmission />);
  let result!:Promise<boolean>;await act(async()=>{result=state.handler!();});
  await waitFor(()=>expect(state.render).toHaveBeenCalledOnce());solve();
  fireEvent.click(screen.getByRole("button",{name:"确认并继续"}));
  await waitFor(()=>expect(state.signal).toHaveBeenCalledOnce());
  if(action==="unmount")view.unmount();else fireEvent.click(screen.getByRole("button",{name:"先返回"}));
  expect(await result).toBe(false);
  await act(async()=>{complete("a".repeat(64));});
  expect(state.verify).not.toHaveBeenCalled();
 });
 it("已完成验证后跨越屏幕宽度档位仍保留原结果",async()=>{
  let width=400;
  let notify:(()=>void)|undefined;
  class Observer {
   constructor(private callback:()=>void){}
   observe(element:HTMLElement){if(element.classList.contains("uma-admission-widget-host"))notify=this.callback;}
   disconnect(){}
  }
  vi.stubGlobal("ResizeObserver",Observer);
  vi.spyOn(HTMLElement.prototype,"clientWidth","get").mockImplementation(()=>width);
  try {
   const {result}=await open();solve();
   act(()=>{width=250;notify?.();});
   expect(state.render).toHaveBeenCalledOnce();
   expect(state.remove).not.toHaveBeenCalled();
   expect(screen.getByRole("button",{name:"确认并继续"}).hasAttribute("disabled")).toBe(false);
   fireEvent.click(screen.getByRole("button",{name:"先返回"}));expect(await result).toBe(false);
  }finally{cleanup();vi.unstubAllGlobals();}
 });
});


describe("验证控件的中断恢复",()=>{
 it("未知结果不会被第三方过期回调清掉，仍可确认原结果",async()=>{
  state.verify.mockRejectedValueOnce(new TimeoutError(15000));
  const {result}=await open();solve();
  await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"确认并继续"}));});
  act(()=>{(state.options?.["expired-callback"] as ()=>void)();});
  expect(screen.getByText(/还没收到确认结果/)).toBeTruthy();
  await act(async()=>{fireEvent.click(screen.getByRole("button",{name:"确认并继续"}));});
  expect(await result).toBe(true);expect(state.verify.mock.calls[1]?.[0]).toEqual(state.verify.mock.calls[0]?.[0]);
 });
 it("不支持或无响应的控件保留重试和返回入口",async()=>{
  const {result}=await open();
  act(()=>{(state.options?.["unsupported-callback"] as ()=>void)();});
  expect(screen.getByText("入场确认暂时没能完成，请重试")).toBeTruthy();
  expect(screen.getByRole("button",{name:"重新确认"}).hasAttribute("disabled")).toBe(false);
  fireEvent.click(screen.getByRole("button",{name:"重新确认"}));
  await waitFor(()=>expect(state.render).toHaveBeenCalledTimes(2));
  vi.useFakeTimers();
  // The retry's effect has already started; exercise its timer through a new retry.
  act(()=>{(state.options?.["error-callback"] as ()=>void)();});
  fireEvent.click(screen.getByRole("button",{name:"重新确认"}));
  await act(async()=>{await vi.advanceTimersByTimeAsync(45_000);});
  expect(screen.getByText("入场确认暂时没能完成，请重试")).toBeTruthy();
  fireEvent.click(screen.getByRole("button",{name:"先返回"}));expect(await result).toBe(false);vi.useRealTimers();
 });
});
