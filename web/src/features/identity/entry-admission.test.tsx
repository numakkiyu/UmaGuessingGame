import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { EntryAdmission } from "./entry-admission";
import { ApiError, TimeoutError } from "@/lib/api/client";
const state=vi.hoisted(()=>({handler:null as null|(()=>Promise<boolean>),options:null as null|Record<string,unknown>,config:vi.fn(),verify:vi.fn(),render:vi.fn(),remove:vi.fn()}));
vi.mock("@/lib/api",()=>({api:{getPublicConfig:state.config,verifyAdmission:state.verify},setAdmissionHandler:(handler:typeof state.handler)=>{state.handler=handler}}));
vi.mock("./turnstile",()=>({loadTurnstile:async()=>({render:(element:HTMLElement,options:Record<string,unknown>)=>{state.options=options;state.render(element,options);return "widget"},remove:state.remove})}));
vi.mock("./browser-signal",()=>({browserSignal:async()=>"a".repeat(64)}));
beforeEach(()=>{state.config.mockReset().mockResolvedValue({data:{admission:{enabled:true,siteKey:"public-site"}}});state.verify.mockReset().mockResolvedValue({data:{verified:true}});state.render.mockClear();state.remove.mockClear();state.options=null;state.handler=null});
afterEach(()=>cleanup());
async function open(){render(<EntryAdmission/>);let result:Promise<boolean>;act(()=>{result=state.handler!()});await screen.findByRole("dialog");await waitFor(()=>expect(state.render).toHaveBeenCalledOnce());return {result:result!}}
function solve(){act(()=>{(state.options?.callback as (token:string)=>void)("test-response")})}
describe("训练员入场",()=>{
 it("验证成功才继续，组件有主题标题并可读屏",async()=>{const {result}=await open();expect(screen.getByRole("heading",{name:"训练员，准备入场"})).toBeTruthy();expect((screen.getByRole("button",{name:"确认并继续"}) as HTMLButtonElement).disabled).toBe(true);solve();fireEvent.click(screen.getByRole("button",{name:"确认并继续"}));expect(await result).toBe(true);expect(state.verify).toHaveBeenCalledWith({token:"test-response"});await waitFor(()=>expect(state.remove).toHaveBeenCalledOnce())});
 it("同一时刻的多个操作共用一个面板，取消不调用校验",async()=>{const {result}=await open();const other=state.handler!();expect(other).toBe(result);fireEvent.click(screen.getByRole("button",{name:"先返回"}));expect(await result).toBe(false);expect(state.verify).not.toHaveBeenCalled()});
 it("过期后不能用旧结果继续",async()=>{const {result}=await open();solve();act(()=>{(state.options?.["expired-callback"] as ()=>void)()});expect((screen.getByRole("button",{name:"确认并继续"}) as HTMLButtonElement).disabled).toBe(true);expect(screen.getByText("等候有些久了，请重新完成入场确认")).toBeTruthy();fireEvent.click(screen.getByRole("button",{name:"先返回"}));await result});
 it("未知校验结果保留同一个确认结果，显式再确认",async()=>{state.verify.mockRejectedValueOnce(new TimeoutError(15000));const {result}=await open();solve();fireEvent.click(screen.getByRole("button",{name:"确认并继续"}));await screen.findByText(/暂时没能确认/);expect(state.verify).toHaveBeenCalledTimes(1);fireEvent.click(screen.getByRole("button",{name:"确认并继续"}));expect(await result).toBe(true);expect(state.verify.mock.calls[0]?.[0]).toEqual(state.verify.mock.calls[1]?.[0])});
 it("确定失效要求重新验证，不把失败当成功",async()=>{state.verify.mockRejectedValueOnce(new ApiError({status:403,code:"admission-invalid",message:"Forbidden",requestId:null}));const {result}=await open();solve();fireEvent.click(screen.getByRole("button",{name:"确认并继续"}));await screen.findByText("这次确认已失效，请重新完成入场确认");expect((screen.getByRole("button",{name:"确认并继续"}) as HTMLButtonElement).disabled).toBe(true);fireEvent.click(screen.getByRole("button",{name:"先返回"}));expect(await result).toBe(false)});
 it("关闭配置不加载第三方组件",async()=>{state.config.mockResolvedValue({data:{admission:{enabled:false}}});render(<EntryAdmission/>);let result:Promise<boolean>;act(()=>{result=state.handler!()});expect(await result!).toBe(true);expect(state.render).not.toHaveBeenCalled()});
});
