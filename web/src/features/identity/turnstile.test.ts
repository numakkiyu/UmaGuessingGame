import { afterEach, describe, expect, it, vi } from "vitest";
import type { Turnstile } from "./turnstile";

afterEach(() => { delete window.turnstile; document.querySelectorAll('script[src*="challenges.cloudflare.com"]').forEach(script=>script.remove()); vi.useRealTimers(); });
describe("验证脚本的失败恢复", () => {
 it("超时脚本的迟到事件不会干扰下一次加载", async () => {
  vi.resetModules();vi.useFakeTimers();
  const {loadTurnstile}=await import("./turnstile");
  const first=loadTurnstile();const failure=expect(first).rejects.toThrow();
  const old=document.querySelector("script")!;
  const late=old.onerror as OnErrorEventHandler;
  await vi.advanceTimersByTimeAsync(12_000);await failure;
  const next=loadTurnstile();
  late?.call(old,new Event("error"));
  expect(loadTurnstile()).toBe(next);
  expect(document.querySelectorAll('script[src*="challenges.cloudflare.com"]').length).toBe(1);
  window.turnstile={render:vi.fn(),reset:vi.fn(),remove:vi.fn()} as Turnstile;
  document.querySelector("script")!.dispatchEvent(new Event("load"));
  await expect(next).resolves.toBe(window.turnstile);
 });
 it("加载成功后迟到的错误不会撤销已完成加载", async()=>{
  vi.resetModules();
  const {loadTurnstile}=await import("./turnstile");
  const promise=loadTurnstile();const script=document.querySelector("script")!;
  const late=script.onerror as OnErrorEventHandler;
  const api={render:vi.fn(),reset:vi.fn(),remove:vi.fn()} as Turnstile;
  window.turnstile=api;script.dispatchEvent(new Event("load"));
  await expect(promise).resolves.toBe(api);
  late?.call(script,new Event("error"));
  expect(script.isConnected).toBe(true);
 });
});
