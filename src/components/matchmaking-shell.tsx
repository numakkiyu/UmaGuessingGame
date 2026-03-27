"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { SiteBrand } from "@/components/site-brand";
import { SiteFooter } from "@/components/site-footer";
import { TurnstileDialog } from "@/components/turnstile-dialog";
import type { PublicConfig } from "@/config/public";
import {
  buildViewerTokenHeaders,
  readTicketViewerToken,
  withViewerTokenQuery,
  writeRoomViewerToken,
  writeTicketViewerToken,
} from "@/lib/battle/client-auth";
import type { MatchmakingStatus } from "@/lib/validation/schemas";

const REQUEST_TIMEOUT_MS = 7000;

type Stats = {
  queueSize: number;
  activeRooms: number;
};

async function requestJson<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
  const controller = new AbortController();
  const timeoutId = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);

  try {
    const response = await fetch(input, {
      ...init,
      credentials: "same-origin",
      signal: controller.signal,
    });
    const text = await response.text();
    const data = text ? (JSON.parse(text) as { error?: string }) : null;
    if (!response.ok) {
      throw new Error(data?.error ?? "这会儿还没法继续。");
    }
    return data as T;
  } finally {
    window.clearTimeout(timeoutId);
  }
}

function formatCountdown(deadline?: string | null) {
  if (!deadline) return null;
  const seconds = Math.max(0, Math.ceil((Date.parse(deadline) - Date.now()) / 1000));
  return seconds;
}

export function MatchmakingShell({ initialConfig }: { initialConfig: PublicConfig }) {
  const router = useRouter();
  const [stats, setStats] = useState<Stats>({ queueSize: 0, activeRooms: 0 });
  const [ticket, setTicket] = useState<MatchmakingStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [turnstileToken, setTurnstileToken] = useState<string | null>(null);
  const [turnstileResetSignal, setTurnstileResetSignal] = useState(0);
  const [turnstileDialogOpen, setTurnstileDialogOpen] = useState(false);
  const pendingVerifiedActionRef = useRef<((token: string) => Promise<void>) | null>(null);
  const [viewerToken, setViewerToken] = useState<string | null>(null);

  const countdown = useMemo(
    () => formatCountdown(ticket?.acceptanceDeadline ?? null),
    [ticket?.acceptanceDeadline],
  );

  useEffect(() => {
    let active = true;

    async function loadStats() {
      try {
        const data = await requestJson<Stats>("/api/matchmaking/stats");
        if (active) {
          setStats(data);
        }
      } catch {}
    }

    void loadStats();
    const timer = window.setInterval(() => {
      void loadStats();
    }, 3000);

    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    if (!ticket?.ticketId) {
      return;
    }

    const currentTicketId = ticket.ticketId;
    let active = true;

    async function refreshStatus() {
      try {
        const currentViewerToken = viewerToken ?? readTicketViewerToken(currentTicketId);
        const data = await requestJson<MatchmakingStatus>(
          withViewerTokenQuery(
            `/api/matchmaking/status?ticketId=${encodeURIComponent(currentTicketId)}`,
            currentViewerToken,
          ),
        );
        if (!active) {
          return;
        }
        setTicket(data);
        setViewerToken(data.viewerToken);
        writeTicketViewerToken(data.ticketId, data.viewerToken);
        if (data.roomCode) {
          writeRoomViewerToken(data.roomCode, data.viewerToken);
        }
        setStats({ queueSize: data.queueSize, activeRooms: data.activeRooms });

        if (
          data.roomCode &&
          ["playing", "paused", "finished", "ready_confirm"].includes(data.status)
        ) {
          router.replace(
            data.viewerToken
              ? `/battle/${data.roomCode}?v=${encodeURIComponent(data.viewerToken)}`
              : `/battle/${data.roomCode}`,
          );
        }
      } catch (nextError) {
        if (active) {
          setError(nextError instanceof Error ? nextError.message : "这次匹配没有继续下去。");
        }
      }
    }

    void refreshStatus();
    const timer = window.setInterval(() => {
      void refreshStatus();
    }, ticket.status === "matched_pending_accept" ? 1000 : 1800);

    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [router, ticket?.status, ticket?.ticketId, viewerToken]);

  useEffect(() => {
    if (!ticket?.ticketId || typeof window === "undefined" || !("EventSource" in window)) {
      return;
    }

    const source = new EventSource(
      withViewerTokenQuery(
        `/api/matchmaking/stream?ticketId=${encodeURIComponent(ticket.ticketId)}`,
        viewerToken ?? readTicketViewerToken(ticket.ticketId),
      ),
    );

    source.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data) as MatchmakingStatus | { error?: string };
        if ("error" in payload && payload.error) {
          setError(payload.error);
          return;
        }

        const nextStatus = payload as MatchmakingStatus;
        setTicket(nextStatus);
        setViewerToken(nextStatus.viewerToken);
        writeTicketViewerToken(nextStatus.ticketId, nextStatus.viewerToken);
        if (nextStatus.roomCode) {
          writeRoomViewerToken(nextStatus.roomCode, nextStatus.viewerToken);
        }
        setStats({ queueSize: nextStatus.queueSize, activeRooms: nextStatus.activeRooms });

        if (
          nextStatus.roomCode &&
          ["playing", "paused", "finished", "ready_confirm"].includes(nextStatus.status)
        ) {
          router.replace(
            nextStatus.viewerToken
              ? `/battle/${nextStatus.roomCode}?v=${encodeURIComponent(nextStatus.viewerToken)}`
              : `/battle/${nextStatus.roomCode}`,
          );
        }
      } catch {}
    };

    source.onerror = () => {
      source.close();
    };

    return () => {
      source.close();
    };
  }, [router, ticket?.ticketId, viewerToken]);

  async function startMatchmaking(verifiedToken?: string) {
    if (busy) return;
    if (initialConfig.turnstileEnabled && !verifiedToken) {
      pendingVerifiedActionRef.current = (token) => startMatchmaking(token);
      setTurnstileToken(null);
      setTurnstileResetSignal((value) => value + 1);
      setTurnstileDialogOpen(true);
      return;
    }

    setBusy(true);
    setError(null);
    try {
      const data = await requestJson<MatchmakingStatus>("/api/matchmaking/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          turnstileToken: initialConfig.turnstileEnabled ? verifiedToken : undefined,
        }),
      });
      setTicket(data);
      setViewerToken(data.viewerToken);
      writeTicketViewerToken(data.ticketId, data.viewerToken);
      if (data.roomCode) {
        writeRoomViewerToken(data.roomCode, data.viewerToken);
      }
      setStats({ queueSize: data.queueSize, activeRooms: data.activeRooms });
      setTurnstileToken(null);
      if (initialConfig.turnstileEnabled) {
        setTurnstileResetSignal((value) => value + 1);
      }
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "这会儿还没法开始匹配。");
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    if (!turnstileDialogOpen || !turnstileToken || busy) {
      return;
    }

    const nextAction = pendingVerifiedActionRef.current;
    if (!nextAction) {
      return;
    }

    pendingVerifiedActionRef.current = null;
    setTurnstileDialogOpen(false);
    setTurnstileToken(null);
    void nextAction(turnstileToken);
  }, [busy, turnstileDialogOpen, turnstileToken]);

  async function confirmMatch() {
    if (!ticket?.ticketId || busy) return;
    setBusy(true);
    setError(null);
    try {
      const data = await requestJson<MatchmakingStatus>("/api/matchmaking/confirm", {
        method: "POST",
        headers: buildViewerTokenHeaders(viewerToken ?? readTicketViewerToken(ticket.ticketId), {
          "Content-Type": "application/json",
        }),
        body: JSON.stringify({ ticketId: ticket.ticketId }),
      });
      setTicket(data);
      setViewerToken(data.viewerToken);
      writeTicketViewerToken(data.ticketId, data.viewerToken);
      if (data.roomCode) {
        writeRoomViewerToken(data.roomCode, data.viewerToken);
      }
      if (data.roomCode && ["playing", "ready_confirm"].includes(data.status)) {
        router.replace(
          data.viewerToken
            ? `/battle/${data.roomCode}?v=${encodeURIComponent(data.viewerToken)}`
            : `/battle/${data.roomCode}`,
        );
      }
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "这次确认没有成功。");
    } finally {
      setBusy(false);
    }
  }

  async function cancelMatch() {
    if (!ticket?.ticketId || busy) return;
    setBusy(true);
    setError(null);
    try {
      await requestJson<MatchmakingStatus>("/api/matchmaking/cancel", {
        method: "POST",
        headers: buildViewerTokenHeaders(viewerToken ?? readTicketViewerToken(ticket.ticketId), {
          "Content-Type": "application/json",
        }),
        body: JSON.stringify({ ticketId: ticket.ticketId }),
      });
      setTicket(null);
      setViewerToken(null);
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "这会儿还没法取消匹配。");
    } finally {
      setBusy(false);
    }
  }

  const inQueue = ticket && ["queueing", "matched_pending_accept"].includes(ticket.status);

  if (!initialConfig.featureFlags.enableMultiplayer) {
    return (
      <main className="relative mx-auto flex min-h-screen w-full max-w-[1200px] flex-col px-4 py-4 sm:px-6 lg:px-8">
        <section className="uma-panel overflow-hidden px-5 py-5 sm:px-7">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <SiteBrand href="/" className="max-w-[620px]" showTitle={false} subtitle="多人对战暂时还没开放，先去别的模式玩一局吧。" />
            <Link href="/" className="uma-ghost-button text-sm">
              返回主页
            </Link>
          </div>
          <div className="mt-5 rounded-[24px] border border-[var(--color-line)] bg-[rgba(239,242,246,0.88)] px-4 py-5 text-sm leading-7 text-[var(--color-muted)]">
            这个模式现在还没开放，等准备好以后会在主页亮起来。
          </div>
        </section>
        <SiteFooter className="mt-5" />
      </main>
    );
  }

  return (
    <main className="relative mx-auto flex min-h-screen w-full max-w-[1200px] flex-col px-4 py-4 sm:px-6 lg:px-8">
      <section className="uma-panel overflow-hidden px-5 py-5 sm:px-7">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <SiteBrand href="/" className="max-w-[620px]" showTitle={false} subtitle="选好模式以后，这里就会帮你去找同一局的对手。" />
          <Link href="/" className="uma-ghost-button text-sm">
            返回主页
          </Link>
        </div>

        <div className="mt-5 grid gap-4 lg:grid-cols-[1.1fr_0.9fr]">
          <div className="space-y-4">
            <div>
              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
                多人对战
              </p>
              <h1 className="mt-2 font-[var(--font-display)] text-[2.4rem] font-bold leading-tight text-[var(--color-ink)]">
                找一位对手，直接开跑
              </h1>
              <p className="mt-3 max-w-2xl text-sm leading-7 text-[var(--color-muted)]">
                系统会帮你找一位正在排队的玩家。你们会拿到同一题，比谁更快把答案追出来。
              </p>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="rounded-[24px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-4 py-4 shadow-[var(--shadow-soft)]">
                <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-muted)]">
                  当前匹配中
                </p>
                <p className="mt-2 font-[var(--font-display)] text-[2rem] font-bold text-[var(--color-ink)]">
                  {stats.queueSize}
                </p>
              </div>
              <div className="rounded-[24px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-4 py-4 shadow-[var(--shadow-soft)]">
                <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-muted)]">
                  当前对战中
                </p>
                <p className="mt-2 font-[var(--font-display)] text-[2rem] font-bold text-[var(--color-ink)]">
                  {stats.activeRooms}
                </p>
              </div>
            </div>

            {error ? (
              <p className="rounded-[20px] border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                {error}
              </p>
            ) : null}
          </div>

          <div className="rounded-[28px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.78)] p-4 shadow-[var(--shadow-soft)] sm:p-5">
            {!ticket ? (
              <div className="space-y-4">
                <p className="text-lg font-semibold text-[var(--color-ink)]">准备开始匹配</p>
                <p className="text-sm leading-6 text-[var(--color-muted)]">
                  开始以后会进入匹配池。找到对手后，你们都要点一次确认，才能正式进入对战。
                </p>
                <button
                  type="button"
                  onClick={() => void startMatchmaking()}
                  disabled={busy}
                  className="inline-flex min-h-12 w-full items-center justify-center rounded-[20px] border border-[rgba(103,186,24,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-5 py-3 text-sm font-semibold text-[#244117] shadow-[0_16px_28px_rgba(116,194,22,0.18)] disabled:opacity-60"
                >
                  开始匹配
                </button>
              </div>
            ) : ticket.status === "matched_pending_accept" ? (
              <div className="space-y-4">
                <p className="text-lg font-semibold text-[var(--color-ink)]">已经找到对手</p>
                <p className="text-sm leading-6 text-[var(--color-muted)]">
                  请在 {countdown ?? initialConfig.multiplayer.acceptConfirmSeconds} 秒内确认开始，双方都确认后就会立刻进入对战。
                </p>
                <div className="grid gap-3 sm:grid-cols-2">
                  <button
                    type="button"
                    onClick={confirmMatch}
                    disabled={busy}
                    className="inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[rgba(103,186,24,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-5 py-3 text-sm font-semibold text-[#244117]"
                  >
                    确认开始
                  </button>
                  <button
                    type="button"
                    onClick={cancelMatch}
                    disabled={busy}
                    className="inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-5 py-3 text-sm font-semibold text-[var(--color-brand-blue-deep)]"
                  >
                    暂不开始
                  </button>
                </div>
              </div>
            ) : (
              <div className="space-y-4">
                <p className="text-lg font-semibold text-[var(--color-ink)]">正在匹配</p>
                <p className="text-sm leading-6 text-[var(--color-muted)]">
                  已经排队 {ticket.waitSeconds} 秒。先别离开，找到对手后会第一时间提醒你确认开始。
                </p>
                <button
                  type="button"
                  onClick={cancelMatch}
                  disabled={busy || !inQueue}
                  className="inline-flex min-h-12 w-full items-center justify-center rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-5 py-3 text-sm font-semibold text-[var(--color-brand-blue-deep)] disabled:opacity-60"
                >
                  取消匹配
                </button>
              </div>
            )}
          </div>
        </div>
      </section>

      <TurnstileDialog
        open={turnstileDialogOpen}
        siteKey={initialConfig.turnstileSiteKey}
        resetSignal={turnstileResetSignal}
        message="验证通过后会直接开始匹配，不用再点一次。"
        statusText={turnstileToken ? "验证完成，正在继续。" : "完成后会自动开始匹配。"}
        onTokenChange={setTurnstileToken}
        onClose={() => {
          pendingVerifiedActionRef.current = null;
          setTurnstileDialogOpen(false);
          setTurnstileToken(null);
          setTurnstileResetSignal((value) => value + 1);
        }}
      />

      <SiteFooter className="mt-5" />
    </main>
  );
}
