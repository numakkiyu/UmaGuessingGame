"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { SiteBrand } from "@/components/site-brand";
import { SiteFooter } from "@/components/site-footer";
import { TurnstileDialog } from "@/components/turnstile-dialog";
import type { PublicConfig } from "@/config/public";
import { writeRoomViewerToken } from "@/lib/battle/client-auth";
import type { BattleRoomState } from "@/lib/validation/schemas";

const REQUEST_TIMEOUT_MS = 7000;

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

export function FriendBattleShell({
  initialConfig,
  initialInviteCode,
}: {
  initialConfig: PublicConfig;
  initialInviteCode: string;
}) {
  type PendingAction = "create" | "join" | null;

  const router = useRouter();
  const [inviteCode, setInviteCode] = useState(initialInviteCode);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [turnstileToken, setTurnstileToken] = useState<string | null>(null);
  const [turnstileResetSignal, setTurnstileResetSignal] = useState(0);
  const [turnstileDialogOpen, setTurnstileDialogOpen] = useState(false);
  const [pendingAction, setPendingAction] = useState<PendingAction>(null);
  const pendingVerifiedActionRef = useRef<((token: string) => Promise<void>) | null>(null);

  async function createRoom(verifiedToken?: string) {
    if (busy) return;

    if (initialConfig.turnstileEnabled && !verifiedToken) {
      setPendingAction("create");
      pendingVerifiedActionRef.current = (token) => createRoom(token);
      setTurnstileToken(null);
      setTurnstileResetSignal((value) => value + 1);
      setTurnstileDialogOpen(true);
      return;
    }

    setBusy(true);
    setError(null);
    try {
      const state = await requestJson<BattleRoomState>("/api/rooms/create", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          turnstileToken: initialConfig.turnstileEnabled ? verifiedToken : undefined,
        }),
      });
      writeRoomViewerToken(state.roomCode, state.viewerToken);
      router.push(
        state.viewerToken
          ? `/battle/${state.roomCode}?v=${encodeURIComponent(state.viewerToken)}`
          : `/battle/${state.roomCode}`,
      );
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "这会儿还没法建房。");
    } finally {
      setTurnstileToken(null);
      if (initialConfig.turnstileEnabled) {
        setTurnstileResetSignal((value) => value + 1);
      }
      setBusy(false);
    }
  }

  async function joinRoom(verifiedToken?: string) {
    if (busy) return;
    if (!inviteCode.trim()) {
      setError("先输入房间码，再继续。");
      return;
    }
    if (initialConfig.turnstileEnabled && !verifiedToken) {
      setPendingAction("join");
      pendingVerifiedActionRef.current = (token) => joinRoom(token);
      setTurnstileToken(null);
      setTurnstileResetSignal((value) => value + 1);
      setTurnstileDialogOpen(true);
      return;
    }

    setBusy(true);
    setError(null);
    try {
      const state = await requestJson<BattleRoomState>("/api/rooms/join", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          inviteCode: inviteCode.trim().toUpperCase(),
          turnstileToken: initialConfig.turnstileEnabled ? verifiedToken : undefined,
        }),
      });
      writeRoomViewerToken(state.roomCode, state.viewerToken);
      router.push(
        state.viewerToken
          ? `/battle/${state.roomCode}?v=${encodeURIComponent(state.viewerToken)}`
          : `/battle/${state.roomCode}`,
      );
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "这会儿还没法加入房间。");
    } finally {
      setTurnstileToken(null);
      if (initialConfig.turnstileEnabled) {
        setTurnstileResetSignal((value) => value + 1);
      }
      setBusy(false);
    }
  }

  useEffect(() => {
    if (!turnstileDialogOpen || !turnstileToken || !pendingAction || busy) {
      return;
    }

    const nextAction = pendingVerifiedActionRef.current;
    if (!nextAction) {
      return;
    }

    pendingVerifiedActionRef.current = null;
    setPendingAction(null);
    setTurnstileDialogOpen(false);
    setTurnstileToken(null);
    void nextAction(turnstileToken);
  }, [busy, pendingAction, turnstileDialogOpen, turnstileToken]);

  if (!initialConfig.featureFlags.enableFriendBattle) {
    return (
      <main className="relative mx-auto flex min-h-screen w-full max-w-[1200px] flex-col px-4 py-4 sm:px-6 lg:px-8">
        <section className="uma-panel overflow-hidden px-5 py-5 sm:px-7">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <SiteBrand href="/" className="max-w-[620px]" showTitle={false} subtitle="好友对战暂时还没开放，先去别的模式玩一局吧。" />
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
          <SiteBrand href="/" className="max-w-[620px]" showTitle={false} subtitle="把房间码发给朋友，等人到齐后就能一起猜同一题。" />
          <Link href="/" className="uma-ghost-button text-sm">
            返回主页
          </Link>
        </div>

        <div className="mt-5 grid gap-4 lg:grid-cols-[1.05fr_0.95fr]">
          <div className="space-y-4">
            <div>
              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
                好友对战
              </p>
              <h1 className="mt-2 font-[var(--font-display)] text-[2.4rem] font-bold leading-tight text-[var(--color-ink)]">
                约上朋友，一起追同一题
              </h1>
              <p className="mt-3 max-w-2xl text-sm leading-7 text-[var(--color-muted)]">
                你可以先建一个房间，再把房间码发给朋友；也可以直接输入对方给你的房间码，马上进房。
              </p>
            </div>

            {error ? (
              <p className="rounded-[20px] border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                {error}
              </p>
            ) : null}
          </div>

          <div className="grid gap-4">
            <div className="rounded-[28px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.8)] p-4 shadow-[var(--shadow-soft)] sm:p-5">
              <p className="text-lg font-semibold text-[var(--color-ink)]">创建房间</p>
              <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
                房间建好以后，会生成房间码。把它发出去，对方就能直接进来。
              </p>
              <button
                type="button"
                onClick={() => void createRoom()}
                disabled={busy}
                className="mt-4 inline-flex min-h-12 w-full items-center justify-center rounded-[20px] border border-[rgba(103,186,24,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-5 py-3 text-sm font-semibold text-[#244117] disabled:opacity-60"
              >
                创建房间
              </button>
            </div>

            <div className="rounded-[28px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.8)] p-4 shadow-[var(--shadow-soft)] sm:p-5">
              <p className="text-lg font-semibold text-[var(--color-ink)]">加入房间</p>
              <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
                输入朋友发来的房间码，就能直接加入这一局。
              </p>
              <div className="mt-4 rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-4 py-3 shadow-[var(--shadow-soft)]">
                <input
                  value={inviteCode}
                  onChange={(event) => setInviteCode(event.target.value.toUpperCase())}
                  placeholder="输入房间码"
                  className="h-8 w-full bg-transparent text-sm outline-none placeholder:text-[var(--color-muted)] sm:text-base"
                />
              </div>
              <button
                type="button"
                onClick={() => void joinRoom()}
                disabled={busy}
                className="mt-4 inline-flex min-h-12 w-full items-center justify-center rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-5 py-3 text-sm font-semibold text-[var(--color-brand-blue-deep)] disabled:opacity-60"
              >
                加入房间
              </button>
            </div>
          </div>
        </div>
      </section>

      <TurnstileDialog
        open={turnstileDialogOpen}
        siteKey={initialConfig.turnstileSiteKey}
        resetSignal={turnstileResetSignal}
        message={
          pendingAction === "join"
            ? "验证通过后会直接带你进入房间。"
            : "验证通过后会直接帮你建好房间。"
        }
        statusText={turnstileToken ? "验证完成，正在继续。" : "完成后会自动继续。"}
        onTokenChange={setTurnstileToken}
        onClose={() => {
          pendingVerifiedActionRef.current = null;
          setTurnstileDialogOpen(false);
          setPendingAction(null);
          setTurnstileToken(null);
          setTurnstileResetSignal((value) => value + 1);
        }}
      />

      <SiteFooter className="mt-5" />
    </main>
  );
}
