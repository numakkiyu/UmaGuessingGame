/**
 * 联机房间的通知与恢复层：默认 SSE，可选 WebSocket，失败后游标轮询。
 * 单人本地执行适配器不挂载本 hook。业务写入始终走 REST，以持久回执为准。
 * 命名 SSE 事件逐个注册；每个房间仅保留一个主通道和一个在途轮询。
 */
import { useEffect, useMemo, useRef, useState } from "react";

/** 三种通道 */
export type Transport = "websocket" | "sse" | "polling";

/** 与 connection-bar 的 ConnectionStatus 对齐 */
export type RealtimeStatus = "connecting" | "online" | "reconnecting" | "offline" | "stale";

export type RealtimeMessage = {
  schemaVersion: string;
  eventId: string;
  type: string;
  roomId: string;
  roomVersion: number;
  serverEpoch: string;
  serverTime: string;
  cursor: string;
  resource?: string;
  roundId?: string;
  phaseVersion?: number;
  reason?: string;
};

export type RealtimeHandlers = {
  /** 收到需要重新拉快照的通知 */
  onSnapshotNeeded?: (message: RealtimeMessage) => void;
  /** 会话被撤销：清私有缓存、断开订阅 */
  onSessionRevoked?: (message: RealtimeMessage) => void;
  /** 通道或状态变化 */
  onStatusChange?: (status: RealtimeStatus, transport: Transport) => void;
};

export type UseRealtimeOptions = {
  roomId: string | null;
  /** 轮询间隔（毫秒），只在 polling 通道生效 */
  pollIntervalMs?: number;
  /** 是否启用；false 时不建立任何连接 */
  enabled?: boolean;
  /** Prefer WebSocket when explicitly enabled; default to SSE. */
  preferWebSocket?: boolean;
  /**
   * 是否优先尝试 SSE。默认 true：后端已支持无 Origin 时用 Referer 校验同源，
   * 浏览器 EventSource 可用。设为 false 可直接落到轮询。
   */
  preferSse?: boolean;
  handlers?: RealtimeHandlers;
};

export type UseRealtimeResult = {
  status: RealtimeStatus;
  transport: Transport;
  /** 最近一次收到的 serverEpoch，用于检测服务端重启 */
  serverEpoch: string | null;
  /** 最近一次成功同步时间 */
  lastSyncedAt: number | null;
  /** 手动重连 */
  reconnect: () => void;
};

const BASE = "/api/v1";

/**
 * 服务端 SSE 的命名事件类型。
 * 与 internal/realtime/message.go 的 MessageType 常量一一对应；
 * 服务端新增类型时这里要同步，否则新事件会被静默丢弃。
 */
const SSE_EVENT_TYPES = [
  "hello",
  "state.changed",
  "phase.changed",
  "presence.changed",
  "resync.required",
  "session.revoked",
  "pong",
] as const;

/** 退避：500ms → 1s → 2s → 4s → 8s 上限 15s */
function backoffDelay(attempt: number): number {
  return Math.min(500 * 2 ** Math.min(attempt, 6), 15_000) * (0.8 + Math.random() * 0.4);
}

function isRealtimeMessage(value: unknown): value is RealtimeMessage {
  if (typeof value !== "object" || value === null) return false;
  const record = value as Record<string, unknown>;
  return typeof record["type"] === "string" && typeof record["roomId"] === "string";
}

export function useRealtime(options: UseRealtimeOptions): UseRealtimeResult {
  const { roomId, enabled = true, pollIntervalMs = 4000, preferWebSocket = false, preferSse = true } = options;

  const [status, setStatus] = useState<RealtimeStatus>("connecting");
  const [transport, setTransport] = useState<Transport>("polling");
  const [serverEpoch, setServerEpoch] = useState<string | null>(null);
  const [lastSyncedAt, setLastSyncedAt] = useState<number | null>(null);

  // handlers 用 ref 持有，避免每次渲染都重建连接。
  //
  // 赋值放在 effect 里而不是渲染体：渲染过程中写 ref 在并发渲染下可能被丢弃或
  // 提前生效，eslint 10 的 react-hooks/refs 会直接报错。effect 在提交后执行，
  // 此时这次渲染的 handlers 才是「已生效」的那一份。
  const handlersRef = useRef<RealtimeHandlers | undefined>(options.handlers);
  useEffect(() => {
    handlersRef.current = options.handlers;
  }, [options.handlers]);

  const cleanupRef = useRef<(() => void) | null>(null);
  const attemptRef = useRef(0);
  const [reconnectToken, setReconnectToken] = useState(0);

  const reconnect = useMemo(
    () => () => {
      attemptRef.current = 0;
      setReconnectToken((n) => n + 1);
    },
    [],
  );

  useEffect(() => {
    if (!enabled || !roomId) {
      setStatus("offline");
      return;
    }

    let disposed = false;
    let stopped = false;
    let probeAbort: AbortController | null = null;
    let socket: WebSocket | null = null;
    let source: EventSource | null = null;
    let pollTimer: number | null = null;
    let retryTimer: number | null = null;
    let currentTransport: Transport = "websocket";
    let currentStatus: RealtimeStatus = "connecting";
    let lastContact = 0;
    let pollInFlight = false;
    let pollAbort: AbortController | null = null;
    let pollingCursor = "";
    let streamCursor = "";
    let connectedAt: number | null = null;
    let notificationEpoch: string | null = null;
    let newestStateVersion = -1;
    const seenEvents = new Set<string>();

    const emitStatus = (next: RealtimeStatus) => {
      if (disposed) return;
      currentStatus = next;
      setStatus(next);
      setTransport(currentTransport);
      handlersRef.current?.onStatusChange?.(next, currentTransport);
    };

    const clearAll = () => {
      stopped = true;
      probeAbort?.abort();
      pollAbort?.abort();
      if (socket) {
        socket.onopen = null;
        socket.onmessage = null;
        socket.onerror = null;
        socket.onclose = null;
        socket.close();
        socket = null;
      }
      if (source) {
        source.close();
        source = null;
      }
      if (pollTimer !== null) {
        window.clearTimeout(pollTimer);
        pollTimer = null;
      }
      if (retryTimer !== null) {
        window.clearTimeout(retryTimer);
        retryTimer = null;
      }
    };

    /** 处理一条服务端通知（WS 与 SSE 共用） */
    const handleMessage = (raw: string) => {
      if (disposed || stopped) return;
      let parsed: unknown;
      try {
        parsed = JSON.parse(raw);
      } catch {
        return;
      }
      if (!isRealtimeMessage(parsed)) return;
      const message = parsed;
      if (message.roomId !== roomId) return;
      if (message.serverEpoch && message.serverEpoch !== notificationEpoch) {
        notificationEpoch = message.serverEpoch;
        newestStateVersion = -1;
        seenEvents.clear();
      }
      const stateEvent = message.type === "state.changed" || message.type === "phase.changed";
      // Older persisted notifications may omit the version or encode it as
      // zero. They still invalidate the snapshot, but cannot lower its baseline.
      const hasStateVersion = Number.isFinite(message.roomVersion) && message.roomVersion > 0;
      const deduplicate = stateEvent || message.type === "presence.changed";
      if (deduplicate) {
        if (message.eventId && seenEvents.has(message.eventId)) return;
        // Old state notifications must not regress the cursor or cause a new
        // snapshot storm. Presence can change independently of roomVersion.
        if (stateEvent && hasStateVersion && message.roomVersion < newestStateVersion) return;
        if (message.eventId) {
          seenEvents.add(message.eventId);
          if (seenEvents.size > 512) seenEvents.delete(seenEvents.values().next().value!);
        }
      }
      if ((stateEvent || message.type === "hello") && hasStateVersion) {
        newestStateVersion = Math.max(newestStateVersion, message.roomVersion);
      }

      lastContact = Date.now();
      setLastSyncedAt(lastContact);
      if (message.cursor) streamCursor = message.cursor;
      emitStatus("online");
      if (message.serverEpoch) {
        setServerEpoch((prev) => {
          // serverEpoch 变化说明服务端重启/切换，必须重新拉快照
          if (prev !== null && prev !== message.serverEpoch) {
            handlersRef.current?.onSnapshotNeeded?.(message);
          }
          return message.serverEpoch;
        });
      }

      if (message.type === "session.revoked") {
        handlersRef.current?.onSessionRevoked?.(message);
        // All game pages already refresh their authorised views through this
        // callback, even if they do not need a specialised revocation handler.
        handlersRef.current?.onSnapshotNeeded?.(message);
        clearAll();
        emitStatus("offline");
        return;
      }
      if (
        message.type === "state.changed" ||
        message.type === "phase.changed" ||
        message.type === "presence.changed" ||
        message.type === "resync.required"
      ) {
        handlersRef.current?.onSnapshotNeeded?.(message);
      }
      if (message.type === "hello") {
        handlersRef.current?.onSnapshotNeeded?.(message);
      }
    };

    /**
     * 轮询：拉房间事件流增量，作为流式通知不可用时的兜底。
     * 只在 roomVersion 真正变化时触发快照，避免每次轮询都重渲染整棵树。
     */
    let lastSeenVersion = -1;
    const startPolling = () => {
      currentTransport = "polling";
      emitStatus("connecting");
      let failures = 0;
      const interval = Math.max(1000, pollIntervalMs);
      const tick = async () => {
        if (disposed || stopped || pollInFlight) return;
        if (document.hidden || navigator.onLine === false) {
          pollTimer = window.setTimeout(() => void tick(), interval);
          return;
        }
        let nextDelay = interval;
        pollInFlight = true;
        pollAbort = new AbortController();
        try {
          const query = new URLSearchParams({ limit: "100" });
          if (pollingCursor) query.set("cursor", pollingCursor);
          const response = await fetch(`${BASE}/rooms/${encodeURIComponent(roomId)}/events?${query}`, {
            signal: AbortSignal.any([pollAbort.signal, AbortSignal.timeout(5000)]),
            credentials: "same-origin",
            cache: "no-store",
            headers: { Accept: "application/json" },
          });
          if (!response.ok) {
            if (response.status === 429 || response.status === 503) {
              const hint = response.headers.get("Retry-After");
              if (hint) {
                const seconds = Number(hint);
                const delay = Number.isFinite(seconds) ? seconds * 1000 : Date.parse(hint) - Date.now();
                if (Number.isFinite(delay)) nextDelay = Math.max(nextDelay, Math.min(delay, 2_147_483_647));
              }
            }
            if (response.status === 401 || response.status === 403 || response.status === 404) {
              const revoked: RealtimeMessage = {
                schemaVersion: "1", eventId: "poll-access-lost", type: "session.revoked",
                roomId, roomVersion: Math.max(0, lastSeenVersion), serverEpoch: "",
                serverTime: new Date().toISOString(), cursor: pollingCursor, reason: "authorization-changed",
              };
              handlersRef.current?.onSessionRevoked?.(revoked);
              handlersRef.current?.onSnapshotNeeded?.(revoked);
              clearAll();
              emitStatus("offline");
              return;
            }
            throw new Error(`poll ${response.status}`);
          }
          const body = (await response.json()) as {
            items?: { roomVersion?: number; type?: string; serverEpoch?: string; cursor?: string }[];
            nextCursor?: string;
            resyncRequired?: boolean;
          };
          if (disposed || stopped) return;
          failures = 0;
          lastContact = Date.now();
          setLastSyncedAt(lastContact);
          emitStatus("online");
          if (body.resyncRequired) pollingCursor = "";
          else if (body.nextCursor) pollingCursor = body.nextCursor;

          const latest = body.items?.at(-1);
          const version = typeof latest?.roomVersion === "number" ? latest.roomVersion : Math.max(0, lastSeenVersion);
          if (body.resyncRequired === true || lastSeenVersion === -1 || (version >= 0 && version !== lastSeenVersion)) {
            lastSeenVersion = version;
            // 只通知「需要刷新快照」，具体拉取由页面负责（实时层不持有业务状态）
            handlersRef.current?.onSnapshotNeeded?.({
              schemaVersion: "1",
              eventId: latest?.cursor ?? "poll",
              type: latest?.type ?? "state.changed",
              roomId,
              roomVersion: version,
              serverEpoch: latest?.serverEpoch ?? "",
              serverTime: new Date().toISOString(),
              cursor: body.nextCursor ?? "",
            });
          }
        } catch {
          if (!disposed && !stopped) {
            failures += 1;
            nextDelay = Math.max(nextDelay, backoffDelay(failures));
            emitStatus("reconnecting");
          }
        } finally {
          pollInFlight = false;
          pollAbort = null;
          if (!disposed && !stopped) pollTimer = window.setTimeout(() => void tick(), nextDelay);
        }
      };
      void tick();
    };

    // EventSource supplies session cookies; the server validates Origin or Referer for this read-only stream
    const startSse = () => {
      if (disposed || stopped || document.hidden || navigator.onLine === false) return;
      currentTransport = "sse";
      emitStatus("connecting");

      try {
        source = new EventSource(`${BASE}/rooms/${encodeURIComponent(roomId)}/stream${streamCursor ? `?cursor=${encodeURIComponent(streamCursor)}` : ""}`, {
          withCredentials: true,
        });
      } catch {
        startPolling();
        return;
      }

      source.onopen = () => {
        connectedAt = Date.now();
        emitStatus("online");
        lastContact = Date.now();
        setLastSyncedAt(lastContact);
      };

      /*
       * 逐个注册命名事件。服务端 writeSSE 发的是 `event: <type>`，
       * 属于命名事件，onmessage 收不到（实测只触发 onopen）。
       * 事件名来自 internal/realtime/message.go 的 MessageType 常量。
       */
      for (const type of SSE_EVENT_TYPES) {
        source.addEventListener(type, (event) => {
          if (event instanceof MessageEvent) handleMessage(String(event.data));
        });
      }
      // 兼容无 event 行的帧（服务端将来若改回默认事件类型）
      source.onmessage = (event) => handleMessage(String(event.data));

      source.onerror = () => {
        if (disposed || stopped) return;
        if (source) {
          source.close();
          source = null;
        }
        // A successful handshake or hello alone is not a stable connection.
        // Otherwise repeated open -> close resets the budget forever.
        if (connectedAt !== null && Date.now() - connectedAt >= 15000) attemptRef.current = 0;
        connectedAt = null;
        attemptRef.current += 1;
        // 两次退避重连仍失败就降级为轮询，保证状态最终能对齐
        if (attemptRef.current >= 2) {
          startPolling();
          return;
        }
        emitStatus("reconnecting");
        retryTimer = window.setTimeout(() => {
          if (!disposed) startSse();
        }, backoffDelay(attemptRef.current));
      };
    };

    const startWebSocket = () => {
      if (disposed || stopped || document.hidden || navigator.onLine === false) return;
      currentTransport = "websocket";
      emitStatus("connecting");
      const url = new URL(`${BASE}/rooms/${encodeURIComponent(roomId)}/socket`, window.location.href);
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";

      try {
        socket = new WebSocket(url.toString());
      } catch {
        startSse();
        return;
      }

      socket.onopen = () => {
        connectedAt = Date.now();
        emitStatus("online");
      };
      socket.onmessage = (event) => handleMessage(String(event.data));
      socket.onerror = () => {
        /* onclose 会跟着触发，统一在 onclose 处理 */
      };
      socket.onclose = () => {
        if (disposed || stopped) return;
        socket = null;
        if (connectedAt !== null && Date.now() - connectedAt >= 15000) attemptRef.current = 0;
        connectedAt = null;
        attemptRef.current += 1;
        // 两次失败后依次降级：WS → SSE → polling
        if (attemptRef.current === 1) {
          emitStatus("reconnecting");
          retryTimer = window.setTimeout(() => {
            if (!disposed) startSse();
          }, backoffDelay(attemptRef.current));
        } else if (attemptRef.current === 2) {
          emitStatus("reconnecting");
          retryTimer = window.setTimeout(() => {
            if (!disposed) startPolling();
          }, backoffDelay(attemptRef.current));
        } else {
          startPolling();
        }
      };
    };

    // 页面隐藏时暂停重连，回到前台立即重试一次
    const onVisibility = () => {
      if (document.visibilityState === "visible" && currentStatus !== "online") {
        reconnect();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);

    /*
     * 浏览器层断网检测（navigator.onLine + online/offline 事件）。
     *
     * 为什么必须有这一层：已建立的 SSE/WebSocket 连接在断网时不会立即报错。
     * 浏览器要等 TCP 超时或对端 RST 才会触发 onerror，实测在 CDP offline 下
     * 12 秒都不触发，界面会一直显示「已连接」——玩家看着连接正常，实际提交
     * 必然失败。这是真实缺陷，不能只靠 EventSource.onerror。
     *
     * offline 事件是浏览器给的即时信号，比等连接超时可靠得多。
     */
    const onBrowserOffline = () => {
      if (disposed) return;
      clearAll();
      emitStatus("offline");
    };
    const onBrowserOnline = () => {
      if (disposed) return;
      // 网络回来了：立即重建连接，不等退避计时
      attemptRef.current = 0;
      emitStatus("reconnecting");
      reconnect();
    };
    window.addEventListener("offline", onBrowserOffline);
    window.addEventListener("online", onBrowserOnline);

    // 挂载时若已经离线，直接显示离线（不假装在线）
    if (typeof navigator !== "undefined" && navigator.onLine === false) {
      emitStatus("offline");
    }

    /*
     * 主动可达性探测（补充 navigator.onLine 的盲区）。
     *
     * navigator.onLine 只反映本机网络接口，不反映服务端是否可达。
     * 实测：用 CDP 屏蔽 /api/v1/*（模拟服务端宕机、本机仍有网）时，
     * 界面会误显示「已连接」约 10 秒，直到 SSE 的 onerror 才纠正。
     *
     * 这个探测补上那段窗口：每 8 秒打一次轻量健康检查，
     * 连续失败就标记 stale（画面可能已过期），而不是继续显示「已连接」。
     * 它只是加速信号，SSE onerror 与轮询兜底仍然保留。
     */
    let probeFailures = 0;
    let probeTimer: number | null = null;
    const probe = async () => {
      if (disposed || stopped || currentTransport === "polling" || document.hidden || Date.now() - lastContact < 15000) return;
      // 本机明确离线时不必探测，offline 事件已经处理
      if (typeof navigator !== "undefined" && navigator.onLine === false) return;
      probeAbort = new AbortController();
      try {
        const res = await fetch("/health/live", {
          credentials: "same-origin",
          cache: "no-store",
          signal: AbortSignal.any([probeAbort.signal, AbortSignal.timeout(4000)]),
        });
        if (disposed || stopped) return;
        if (res.ok) {
          probeFailures = 0;
          lastContact = Date.now();
          // Reachability does not prove a stale game stream caught up. Rebuild
          // the channel and refresh its authorised state rather than fake online.
          if (currentStatus === "stale") reconnect();
          return;
        }
        probeFailures += 1;
      } catch {
        probeFailures += 1;
      }
      // 连续两次失败才降级，避免一次网络抖动就闪成「可能已过期」
      if (probeFailures >= 2 && !disposed) {
        emitStatus("stale");
      }
    };
    probeTimer = window.setInterval(() => void probe(), 8000);

    /*
     * 启动顺序：显式启用时优先 WS，否则 SSE；失败后轮询。
     * 切换连接时不并行保留多个主通道。
     */
    if (preferWebSocket) startWebSocket();
    else if (preferSse) startSse();
    else startPolling();

    cleanupRef.current = () => {
      disposed = true;
      clearAll();
    };

    return () => {
      disposed = true;
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("offline", onBrowserOffline);
      window.removeEventListener("online", onBrowserOnline);
      if (probeTimer !== null) window.clearInterval(probeTimer);
      clearAll();
      cleanupRef.current = null;
    };
    // status 有意不进依赖：只在挂载与重连令牌变化时重建，避免状态回环
  }, [roomId, enabled, pollIntervalMs, preferWebSocket, preferSse, reconnectToken, reconnect]);

  return { status, transport, serverEpoch, lastSyncedAt, reconnect };
}
