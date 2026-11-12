import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRealtime } from "./use-realtime";

class FakeSource {
  static instances: FakeSource[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  listeners = new Map<string, (event: MessageEvent) => void>();
  closed = false;
  constructor(readonly url: string) { FakeSource.instances.push(this); }
  addEventListener(type: string, listener: (event: MessageEvent) => void) { this.listeners.set(type, listener); }
  close() { this.closed = true; }
  send(type: string, extra: Record<string, unknown> = {}) {
    this.listeners.get(type)?.(new MessageEvent(type, { data: JSON.stringify({ type, roomId: "r", cursor: "scope:1", roomVersion: 1, ...extra }) }));
  }
}

beforeEach(() => {
  vi.useFakeTimers();
  FakeSource.instances = [];
  vi.stubGlobal("EventSource", FakeSource);
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.useRealTimers(); });

describe("联机同步恢复", () => {
  it("健康空闲连接超过旧20秒超时后仍在线，使用真实健康地址", async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response("ok"));
    vi.stubGlobal("fetch", fetcher);
    const { result, unmount } = renderHook(() => useRealtime({ roomId: "r" }));
    act(() => FakeSource.instances[0]?.onopen?.());
    await act(async () => { await vi.advanceTimersByTimeAsync(40000); });
    expect(result.current.status).toBe("online");
    expect(FakeSource.instances).toHaveLength(1);
    expect(fetcher).toHaveBeenCalled();
    expect(fetcher.mock.calls.every(([url]) => url === "/health/live")).toBe(true);
    const count = fetcher.mock.calls.length;
    unmount();
    await act(async () => { await vi.advanceTimersByTimeAsync(16000); });
    expect(fetcher).toHaveBeenCalledTimes(count);
    expect(FakeSource.instances[0]?.closed).toBe(true);
  });

  it("重新建立SSE携带游标，hello要求刷新授权快照", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("ok")));
    const snapshot = vi.fn();
    renderHook(() => useRealtime({ roomId: "r", handlers: { onSnapshotNeeded: snapshot } }));
    act(() => { FakeSource.instances[0]?.send("hello"); FakeSource.instances[0]?.onerror?.(); });
    expect(snapshot).toHaveBeenCalledTimes(1);
    await act(async () => { await vi.advanceTimersByTimeAsync(1500); });
    expect(FakeSource.instances[0]?.closed).toBe(true);
    expect(FakeSource.instances[1]?.url).toContain("cursor=scope%3A1");
  });

  it("轮询只读取增量游标，使用最后事件而不反复读取首条旧状态", async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: [{ roomVersion: 1 }, { roomVersion: 3 }], nextCursor: "c3" })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: [], nextCursor: "c3" })));
    vi.stubGlobal("fetch", fetcher);
    const snapshot = vi.fn();
    renderHook(() => useRealtime({ roomId: "r", preferSse: false, handlers: { onSnapshotNeeded: snapshot } }));
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(snapshot).toHaveBeenCalledTimes(1);
    expect(snapshot.mock.calls[0]?.[0].roomVersion).toBe(3);
    await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
    expect(String(fetcher.mock.calls[1]?.[0])).toContain("cursor=c3");
    expect(snapshot).toHaveBeenCalledTimes(1);
  });

  /*
   * 服务端重启后前端必须察觉：serverEpoch 变化要触发重新拉快照。
   *
   * 要求这一点。如果漏了，服务端重启（或切到另一台机器）后，玩家会继续
   * 看到重启前的房间状态——名单、阶段、分数都停在旧值，而页面看起来完全正常。
   * 此前测试 0 处涉及 serverEpoch。
   *
   * 同时锁定一个容易写错的边界：首次收到 serverEpoch 不该触发重取（那只是
   * 建立基线），只有「之前有值、现在变了」才算重启。
   */
  it("serverEpoch 变化时额外触发一次快照（服务端重启）", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("ok")));
    const snapshot = vi.fn();
    renderHook(() => useRealtime({ roomId: "r", handlers: { onSnapshotNeeded: snapshot } }));
    const source = FakeSource.instances[0]!;

    /*
     * 注意：state.changed / phase.changed / presence.changed / resync.required / hello
     * 本身都会触发 onSnapshotNeeded。所以要单独验证「epoch 变化」这一条，必须用一个
     * 已注册、但不参与上述分支的类型，否则分不清是哪个原因触发的。
     * "pong" 正好符合：它在 SSE_EVENT_TYPES 里（所以 FakeSource 能投递），
     * 但 handleMessage 对它只更新 epoch/游标，不会触发快照。
     */
    act(() => { source.send("pong", { serverEpoch: "epoch-A" }); });
    const baseline = snapshot.mock.calls.length;

    // 同一 epoch 的心跳：不触发
    act(() => { source.send("pong", { serverEpoch: "epoch-A" }); });
    expect(snapshot.mock.calls.length).toBe(baseline);

    // epoch 变化 = 服务端重启，必须触发一次
    act(() => { source.send("pong", { serverEpoch: "epoch-B" }); });
    expect(snapshot.mock.calls.length).toBe(baseline + 1);

    // 之后同一新 epoch 不再重复触发
    act(() => { source.send("pong", { serverEpoch: "epoch-B" }); });
    expect(snapshot.mock.calls.length).toBe(baseline + 1);
  });

  /*
   * SSE 反复连不上时，必须真的降级到轮询并继续收到更新。
   *
   * 这是玩家最常遇到的真实路径：投影仪、手机切后台、场馆 Wi-Fi 抖动都可能让
   * EventSource 建不起来。现有测试覆盖了「直接用轮询」（preferSse: false）和
   * 「SSE 断开后重连」，但没有一条证明「SSE 一直失败 → 切轮询 → 仍然拿到事件」。
   * 如果这段降级坏了，页面会停在旧数据上，看起来像「大家都卡住了」。
   */
  it("SSE 反复失败后降级到轮询，并仍然收到房间事件", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [{ roomVersion: 7 }], nextCursor: "c7" })),
    );
    vi.stubGlobal("fetch", fetcher);
    const snapshot = vi.fn();
    const transports: string[] = [];
    renderHook(() =>
      useRealtime({
        roomId: "r",
        handlers: {
          onSnapshotNeeded: snapshot,
          onStatusChange: (_status, transport) => transports.push(transport),
        },
      }),
    );

    // 让 EventSource 连续失败，逼它放弃流式通道。
    await act(async () => {
      for (let i = 0; i < 5; i += 1) {
        FakeSource.instances.at(-1)?.onerror?.();
        await vi.advanceTimersByTimeAsync(3000);
      }
    });

    // 降级发生过，且最终真的用轮询拿到了事件。
    expect(transports).toContain("polling");
    expect(fetcher.mock.calls.some(([url]) => String(url).includes("/rooms/r/events"))).toBe(true);
    expect(snapshot).toHaveBeenCalled();
  });

  it("等待中的请求不重叠，卸载会取消请求", async () => {
    let signal: AbortSignal | undefined;
    const fetcher = vi.fn((_url: string, init: RequestInit) => {
      signal = init.signal as AbortSignal;
      return new Promise<Response>(() => {});
    });
    vi.stubGlobal("fetch", fetcher);
    const { unmount } = renderHook(() => useRealtime({ roomId: "r", preferSse: false }));
    await act(async () => { await vi.advanceTimersByTimeAsync(4000); });
    expect(fetcher).toHaveBeenCalledTimes(1);
    unmount();
    expect(signal?.aborted).toBe(true);
  });

  it("过载尊重Retry-After，不在等待期间反复轮询", async () => {
    const fetcher = vi.fn()
      .mockResolvedValueOnce(new Response("busy", { status: 429, headers: { "Retry-After": "30" } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: [] })));
    vi.stubGlobal("fetch", fetcher);
    const { result } = renderHook(() => useRealtime({ roomId: "r", preferSse: false }));
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(result.current.status).toBe("reconnecting");
    await act(async () => { await vi.advanceTimersByTimeAsync(29999); });
    expect(fetcher).toHaveBeenCalledTimes(1);
    await act(async () => { await vi.advanceTimersByTimeAsync(1); });
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(result.current.status).toBe("online");
  });

  it("权限失效后停止网络请求，不无限重试", async () => {
    const fetcher = vi.fn().mockResolvedValue(new Response("denied", { status: 403 }));
    vi.stubGlobal("fetch", fetcher);
    const { result } = renderHook(() => useRealtime({ roomId: "r", preferSse: false }));
    await act(async () => { await vi.advanceTimersByTimeAsync(40000); });
    expect(result.current.status).toBe("offline");
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it.each([401,403,404])("轮询权限失效%d通知页面清理授权视图且不重试",async(status)=>{
    const fetcher=vi.fn().mockResolvedValue(new Response("denied",{status}));
    vi.stubGlobal("fetch",fetcher);
    const revoked=vi.fn(),refresh=vi.fn();
    const{result}=renderHook(()=>useRealtime({roomId:"r",preferSse:false,handlers:{onSessionRevoked:revoked,onSnapshotNeeded:refresh}}));
    await act(async()=>{await vi.advanceTimersByTimeAsync(40000);});
    expect(result.current.status).toBe("offline");
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(revoked).toHaveBeenCalledTimes(1);
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(refresh.mock.calls[0]?.[0]).toMatchObject({roomId:"r",type:"session.revoked"});
  });

  it("SSE撤权即使页面没有专用处理也会重新读取授权视图",async()=>{
    const fetcher=vi.fn(),refresh=vi.fn();vi.stubGlobal("fetch",fetcher);
    const{result}=renderHook(()=>useRealtime({roomId:"r",handlers:{onSnapshotNeeded:refresh}}));
    act(()=>FakeSource.instances[0]?.send("session.revoked"));
    await act(async()=>{await vi.advanceTimersByTimeAsync(40000);});
    expect(refresh).toHaveBeenCalledTimes(1);
    expect(FakeSource.instances[0]?.closed).toBe(true);
    expect(result.current.status).toBe("offline");
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("重复和乱序状态不重复刷新或倒退游标，新epoch仍接受较小版本",()=>{
    const refresh=vi.fn();renderHook(()=>useRealtime({roomId:"r",handlers:{onSnapshotNeeded:refresh}}));
    const s=FakeSource.instances[0]!;
    act(()=>s.send("state.changed",{eventId:"new",serverEpoch:"A",roomVersion:8,cursor:"c8"}));
    act(()=>s.send("state.changed",{eventId:"new",serverEpoch:"A",roomVersion:8,cursor:"c8"}));
    act(()=>s.send("phase.changed",{eventId:"old",serverEpoch:"A",roomVersion:7,cursor:"c7"}));
    act(()=>s.send("state.changed",{roomId:"another-room",eventId:"foreign",serverEpoch:"A",roomVersion:99}));
    expect(refresh).toHaveBeenCalledTimes(1);
    act(()=>s.send("state.changed",{eventId:"new",serverEpoch:"B",roomVersion:1,cursor:"c1"}));
    expect(refresh.mock.calls.some(([m])=>m.roomVersion===1&&m.serverEpoch==="B")).toBe(true);
  });

  it("旧通知缺少版本仍刷新且不降低hello建立的版本基线", () => {
    const refresh = vi.fn();
    renderHook(() => useRealtime({ roomId: "r", handlers: { onSnapshotNeeded: refresh } }));
    const source = FakeSource.instances[0]!;
    act(() => source.send("hello", { serverEpoch: "A", roomVersion: 8 }));
    act(() => source.send("state.changed", { eventId: "zero", serverEpoch: "A", roomVersion: 0 }));
    act(() => source.send("phase.changed", { eventId: "missing", serverEpoch: "A", roomVersion: undefined }));
    expect(refresh).toHaveBeenCalledTimes(3);
    act(() => source.send("state.changed", { eventId: "zero", serverEpoch: "A", roomVersion: 0 }));
    act(() => source.send("phase.changed", { eventId: "old", serverEpoch: "A", roomVersion: 7 }));
    expect(refresh).toHaveBeenCalledTimes(3);
    act(() => source.send("state.changed", { eventId: "next", serverEpoch: "A", roomVersion: 9 }));
    expect(refresh).toHaveBeenCalledTimes(4);
  });

  it("未启用的本地执行分支不创建连接或探测请求", async () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    renderHook(() => useRealtime({ roomId: null, enabled: false }));
    await act(async () => { await vi.advanceTimersByTimeAsync(40000); });
    expect(FakeSource.instances).toHaveLength(0);
    expect(fetcher).not.toHaveBeenCalled();
  });
});

it("short-lived successful handshakes do not reset the fallback budget forever",async()=>{
 const fetcher=vi.fn().mockResolvedValue(new Response(JSON.stringify({items:[],nextCursor:""})));
 vi.stubGlobal("fetch",fetcher);
 const {result}=renderHook(()=>useRealtime({roomId:"r"}));
 act(()=>{const s=FakeSource.instances[0]!;s.onopen?.();s.send("hello");s.onerror?.();});
 await act(async()=>{await vi.advanceTimersByTimeAsync(1500);});
 expect(FakeSource.instances).toHaveLength(2);
 act(()=>{const s=FakeSource.instances[1]!;s.onopen?.();s.send("hello");s.onerror?.();});
 await act(async()=>{await vi.advanceTimersByTimeAsync(0);});
 expect(result.current.transport).toBe("polling");
 expect(FakeSource.instances.every(s=>s.closed)).toBe(true);
 expect(fetcher.mock.calls.some(([url])=>String(url).includes("/rooms/r/events"))).toBe(true);
 await act(async()=>{await vi.advanceTimersByTimeAsync(12000);});
 expect(FakeSource.instances).toHaveLength(2);
});
it("browser offline closes its stream and cancels pending reconnection until online",async()=>{
 vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response("ok")));
 const{result}=renderHook(()=>useRealtime({roomId:"r"}));
 act(()=>{FakeSource.instances[0]?.onopen?.();window.dispatchEvent(new Event("offline"));});
 expect(result.current.status).toBe("offline");
 expect(FakeSource.instances[0]?.closed).toBe(true);
 await act(async()=>{await vi.advanceTimersByTimeAsync(30000);});
 expect(FakeSource.instances).toHaveLength(1);
 act(()=>window.dispatchEvent(new Event("online")));
 expect(FakeSource.instances).toHaveLength(2);
});
