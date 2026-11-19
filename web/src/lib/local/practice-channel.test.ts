import { afterEach, expect, it, vi } from "vitest";
import { publishPracticeChange, subscribePracticeChanges } from "./practice-channel";

afterEach(() => vi.unstubAllGlobals());
it("浏览器禁止跨页通信时订阅与通知均不打断游玩", () => {
  vi.stubGlobal("BroadcastChannel",class {constructor(){throw new DOMException("blocked","SecurityError");}});
  expect(() => subscribePracticeChanges("test",vi.fn())()).not.toThrow();
  expect(() => publishPracticeChange("test",{gameId:"g",writerId:"w"})).not.toThrow();
});
it("发送失败也关闭频道，不把已完成的存储操作标成失败", () => {
  const close=vi.fn();
  vi.stubGlobal("BroadcastChannel",class {close=close;postMessage(){throw new Error("failed");}});
  expect(() => publishPracticeChange("test",{gameId:"g",writerId:"w"})).not.toThrow();
  expect(close).toHaveBeenCalledTimes(1);
});
it("忽略畸形消息，只交付有效的记录变更", () => {
  const channel: {onmessage: ((e:{data:unknown})=>void)|null; close:()=>void} = {onmessage:null, close:vi.fn()};
  vi.stubGlobal("BroadcastChannel",vi.fn(function(){return channel;}));
  const receive=vi.fn(),close=subscribePracticeChanges("test",receive);
  for(const data of [null,{},"g",{gameId:"g"},{gameId:1,writerId:"w"}])channel!.onmessage!({data});
  expect(receive).not.toHaveBeenCalled();
  channel!.onmessage!({data:{gameId:"g",writerId:"w"}});
  expect(receive).toHaveBeenCalledExactlyOnceWith({gameId:"g",writerId:"w"});
  close();
});
