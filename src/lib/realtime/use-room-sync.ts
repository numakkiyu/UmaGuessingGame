"use client";

import { useEffect, useRef } from "react";
import type { GameState } from "@/lib/validation/schemas";
import type {
  RoomRealtimeClientMessage,
  RoomRealtimeRole,
  RoomRealtimeServerMessage,
} from "./types";

type UseRoomSyncOptions = {
  roomCode: string;
  enabled: boolean;
  role: RoomRealtimeRole;
  pollIntervalMs: number;
  heartbeatIntervalMs: number;
  wsUrl?: string;
  onState: (state: GameState) => void;
};

function canUseBrowserRealtime() {
  return typeof window !== "undefined";
}

export function useRoomSync({
  roomCode,
  enabled,
  role,
  pollIntervalMs,
  heartbeatIntervalMs,
  wsUrl,
  onState,
}: UseRoomSyncOptions) {
  const onStateRef = useRef(onState);

  useEffect(() => {
    onStateRef.current = onState;
  }, [onState]);

  useEffect(() => {
    if (!enabled || !canUseBrowserRealtime()) {
      return;
    }

    let stopped = false;
    let websocket: WebSocket | null = null;
    let pollTimer: number | null = null;
    let heartbeatTimer: number | null = null;

    async function refreshRoomState() {
      try {
        const response = await fetch(`/api/game/${roomCode}`, { cache: "no-store" });
        if (!response.ok) {
          return;
        }

        const state = (await response.json()) as GameState;
        if (!stopped) {
          onStateRef.current(state);
        }
      } catch {}
    }

    function stopPolling() {
      if (pollTimer !== null) {
        window.clearInterval(pollTimer);
        pollTimer = null;
      }
    }

    function startPolling() {
      stopPolling();
      void refreshRoomState();
      pollTimer = window.setInterval(() => {
        void refreshRoomState();
      }, pollIntervalMs);
    }

    function stopHeartbeat() {
      if (heartbeatTimer !== null) {
        window.clearInterval(heartbeatTimer);
        heartbeatTimer = null;
      }
    }

    function sendMessage(message: RoomRealtimeClientMessage) {
      if (websocket?.readyState !== WebSocket.OPEN) {
        return;
      }

      websocket.send(JSON.stringify(message));
    }

    function startHeartbeat() {
      stopHeartbeat();
      sendMessage({ type: "subscribe", roomCode, role });
      heartbeatTimer = window.setInterval(() => {
        sendMessage({
          type: "heartbeat",
          roomCode,
          role,
          sentAt: new Date().toISOString(),
        });
      }, heartbeatIntervalMs);
    }

    function openRealtimeSocket() {
      if (!wsUrl) {
        startPolling();
        return;
      }

      try {
        const target = new URL(wsUrl, window.location.origin);
        target.searchParams.set("roomCode", roomCode);
        target.searchParams.set("role", role);
        websocket = new WebSocket(target);
      } catch {
        startPolling();
        return;
      }

      websocket.addEventListener("open", () => {
        stopPolling();
        startHeartbeat();
      });

      websocket.addEventListener("message", (event) => {
        try {
          const message = JSON.parse(event.data) as RoomRealtimeServerMessage;
          if (message.type === "room-state" && message.roomCode === roomCode) {
            onStateRef.current(message.gameState);
          }
        } catch {}
      });

      websocket.addEventListener("close", () => {
        stopHeartbeat();
        startPolling();
      });

      websocket.addEventListener("error", () => {
        websocket?.close();
      });
    }

    openRealtimeSocket();

    return () => {
      stopped = true;
      stopPolling();
      stopHeartbeat();
      websocket?.close();
    };
  }, [enabled, heartbeatIntervalMs, pollIntervalMs, role, roomCode, wsUrl]);
}

