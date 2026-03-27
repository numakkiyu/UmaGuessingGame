"use client";

import { useEffect, useMemo, useRef } from "react";
import type { RoomPresenceSummary } from "@/lib/validation/schemas";
import type { RoomRealtimeRole } from "./types";

type UseRoomPresenceOptions = {
  roomCode: string;
  enabled: boolean;
  role: RoomRealtimeRole;
  heartbeatIntervalMs: number;
  onPresence: (summary: RoomPresenceSummary, latencyMs: number | null) => void;
};

const SESSION_STORAGE_PREFIX = "uma-room-presence-session:";

function getSessionId(roomCode: string, role: RoomRealtimeRole) {
  if (typeof window === "undefined") {
    return `${role}-${roomCode}`;
  }

  const key = `${SESSION_STORAGE_PREFIX}${roomCode}:${role}`;
  const existing = window.sessionStorage.getItem(key);
  if (existing) {
    return existing;
  }

  const created =
    typeof crypto !== "undefined" && "randomUUID" in crypto
      ? crypto.randomUUID()
      : `${role}-${roomCode}-${Date.now()}`;
  window.sessionStorage.setItem(key, created);
  return created;
}

export function useRoomPresence({
  roomCode,
  enabled,
  role,
  heartbeatIntervalMs,
  onPresence,
}: UseRoomPresenceOptions) {
  const onPresenceRef = useRef(onPresence);
  const latencyRef = useRef<number | null>(null);
  const sessionId = useMemo(() => getSessionId(roomCode, role), [role, roomCode]);
  const presenceIntervalMs = Math.min(heartbeatIntervalMs, 4000);

  useEffect(() => {
    onPresenceRef.current = onPresence;
  }, [onPresence]);

  useEffect(() => {
    if (!enabled || typeof window === "undefined") {
      return;
    }

    let stopped = false;
    let timer: number | null = null;
    let summaryTimer: number | null = null;

    async function heartbeat() {
      const startedAt = performance.now();
      const previousLatency = latencyRef.current;

      try {
        const response = await fetch("/api/game/presence", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            roomCode,
            role,
            sessionId,
            latencyMs: previousLatency,
          }),
        });
        if (!response.ok) {
          return;
        }

        const summary = (await response.json()) as RoomPresenceSummary;
        const latencyMs = Math.max(1, Math.round(performance.now() - startedAt));
        latencyRef.current = latencyMs;

        if (!stopped) {
          onPresenceRef.current(summary, latencyMs);
        }

        if (previousLatency == null && latencyMs != null && !stopped) {
          window.setTimeout(() => {
            void heartbeat();
          }, 180);
        }
      } catch {}
    }

    async function refreshSummary() {
      try {
        const response = await fetch(`/api/game/presence?roomCode=${encodeURIComponent(roomCode)}`, {
          cache: "no-store",
        });
        if (!response.ok) {
          return;
        }

        const summary = (await response.json()) as RoomPresenceSummary;
        if (!stopped) {
          onPresenceRef.current(summary, latencyRef.current);
        }
      } catch {}
    }

    void heartbeat();
    timer = window.setInterval(() => {
      void heartbeat();
    }, presenceIntervalMs);

    if (role === "host") {
      summaryTimer = window.setInterval(() => {
        void refreshSummary();
      }, 1800);
    }

    return () => {
      stopped = true;
      if (timer !== null) {
        window.clearInterval(timer);
      }
      if (summaryTimer !== null) {
        window.clearInterval(summaryTimer);
      }
    };
  }, [enabled, presenceIntervalMs, role, roomCode, sessionId]);
}
