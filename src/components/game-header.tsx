"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { SiteBrand } from "@/components/site-brand";
import { TurnstileDialog } from "@/components/turnstile-dialog";
import { writeGameViewerToken } from "@/lib/game/client-auth";

type Props = {
  questionBankSize: number;
  maxGuesses: number;
  turnstileEnabled: boolean;
  turnstileSiteKey: string;
  featureFlags: {
    enableMultiplayer: boolean;
    enableFriendBattle: boolean;
    enableShare: boolean;
  };
};

type HomeMode = "single" | "multi" | "friend";

type StartGameResponse = {
  roomCode: string;
  viewerToken?: string | null;
};

type MatchmakingStats = {
  queueSize: number;
  activeRooms: number;
};

const REQUEST_TIMEOUT_MS = 7000;

const modeCopy: Record<
  HomeMode,
  {
    title: string;
    description: string;
    buttonLabel: string;
    helper: string;
  }
> = {
  single: {
    title: "单人游戏",
    description: "系统会立刻藏好一位目标马娘，进房以后马上就能开始猜。",
    buttonLabel: "开始游戏",
    helper: "这局会直接开题，也可以把链接发给朋友一起看。",
  },
  multi: {
    title: "多人对战",
    description: "和另一位玩家拿到同一题，比谁更快把答案追出来。",
    buttonLabel: "开始多人对战",
    helper: "匹配成功后，双方都要点一次确认，才会正式进房。",
  },
  friend: {
    title: "好友对战",
    description: "自己建房，或者输入房间码，和朋友一起猜同一题。",
    buttonLabel: "进入好友对战",
    helper: "建好房以后，把房间码发给朋友就能直接一起玩。",
  },
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
      throw new Error(data?.error ?? "这会儿还没法开始，稍后再试一次吧。");
    }

    return data as T;
  } finally {
    window.clearTimeout(timeoutId);
  }
}

function normalizePlayerError(message: string, fallback: string) {
  if (!message) return fallback;
  if (message.includes("缺少 Turnstile token")) {
    return "请先完成人机验证。";
  }
  if (message.includes("Turnstile 校验失败")) {
    return "验证已过期，请重新完成验证。";
  }
  if (message.includes("question_bank_ready.json 为空")) {
    return "题库还在整理中，稍后再来试试。";
  }
  return message;
}

export function GameHeader({
  questionBankSize,
  maxGuesses,
  turnstileEnabled,
  turnstileSiteKey,
  featureFlags,
}: Props) {
  const router = useRouter();
  const [selectedMode, setSelectedMode] = useState<HomeMode>("single");
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [turnstileToken, setTurnstileToken] = useState<string | null>(null);
  const [turnstileResetSignal, setTurnstileResetSignal] = useState(0);
  const [turnstileDialogOpen, setTurnstileDialogOpen] = useState(false);
  const pendingVerifiedActionRef = useRef<((token: string) => Promise<void>) | null>(null);
  const [matchmakingStats, setMatchmakingStats] = useState<MatchmakingStats>({
    queueSize: 0,
    activeRooms: 0,
  });

  const currentMode = useMemo(() => modeCopy[selectedMode], [selectedMode]);
  const modeAvailability = useMemo(
    () => ({
      single: true,
      multi: featureFlags.enableMultiplayer,
      friend: featureFlags.enableFriendBattle,
    }),
    [featureFlags.enableFriendBattle, featureFlags.enableMultiplayer],
  );

  useEffect(() => {
    if (!featureFlags.enableMultiplayer) {
      return;
    }

    let active = true;

    async function loadStats() {
      try {
        const stats = await requestJson<MatchmakingStats>("/api/matchmaking/stats");
        if (active) {
          setMatchmakingStats(stats);
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
  }, [featureFlags.enableMultiplayer]);

  async function startSingleGame(verifiedToken?: string) {
    if (questionBankSize === 0) {
      setError("题库还在整理中，稍后再来试试。");
      return;
    }

    if (turnstileEnabled && !verifiedToken) {
      pendingVerifiedActionRef.current = (token) => handleStart(token);
      setTurnstileToken(null);
      setTurnstileResetSignal((value) => value + 1);
      setTurnstileDialogOpen(true);
      return;
    }

    const data = await requestJson<StartGameResponse>("/api/game/new", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        turnstileToken: turnstileEnabled ? verifiedToken : undefined,
      }),
    });
    if (data.viewerToken) {
      writeGameViewerToken(data.roomCode, data.viewerToken);
    }
    router.push(
      data.viewerToken
        ? `/single/${data.roomCode}?v=${encodeURIComponent(data.viewerToken)}`
        : `/single/${data.roomCode}`,
    );
  }

  useEffect(() => {
    if (!turnstileDialogOpen || !turnstileToken || selectedMode !== "single" || starting) {
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
  }, [selectedMode, starting, turnstileDialogOpen, turnstileToken]);

  async function handleStart(verifiedToken?: string) {
    if (starting) return;
    setStarting(true);
    setError(null);

    try {
      if (selectedMode === "single") {
        await startSingleGame(verifiedToken);
      } else if (selectedMode === "multi") {
        router.push("/multi");
      } else {
        router.push("/friend");
      }
    } catch (nextError) {
      setError(
        nextError instanceof Error
          ? normalizePlayerError(nextError.message, "这会儿还没法开始，稍后再试一次吧。")
          : "这会儿还没法开始，稍后再试一次吧。",
      );
      setTurnstileToken(null);
      if (turnstileEnabled) {
        setTurnstileResetSignal((value) => value + 1);
      }
    } finally {
      setStarting(false);
    }
  }

  return (
    <section className="uma-hero-shell relative overflow-hidden px-5 py-6 sm:px-7 sm:py-7 lg:px-9 lg:py-8">
      <div className="pointer-events-none absolute inset-x-0 top-0 h-1 bg-[linear-gradient(90deg,var(--color-brand),var(--color-brand-blue))]" />
      <div className="pointer-events-none absolute inset-0 bg-[linear-gradient(180deg,rgba(255,255,255,0.28),rgba(255,255,255,0))]" />
      <div className="pointer-events-none absolute inset-x-0 top-0 h-[180px] bg-[url('/assets/ui/backgrounds/bwiki-main-bg.png')] bg-cover bg-top opacity-[0.08]" />
      <div className="pointer-events-none absolute inset-x-0 top-0 h-[180px] bg-[linear-gradient(180deg,rgba(148,216,28,0.12),rgba(255,255,255,0))]" />

      <div className="relative grid gap-6 lg:grid-cols-[1.08fr_0.92fr]">
        <div>
          <div className="flex flex-wrap items-start justify-between gap-4">
            <SiteBrand
              className="max-w-[620px]"
              showTitle={false}
              subtitle="选好模式以后，按下开始，就能直接进到这一局。"
            />
            <div className="uma-chip shrink-0">当前题库 {questionBankSize} 位</div>
          </div>

          <div className="mt-6 max-w-3xl">
            <h1 className="font-[var(--font-display)] text-[2.7rem] font-bold leading-[0.94] tracking-[0.02em] text-[var(--color-ink)] sm:text-[3.4rem] lg:text-[4rem]">
              赛马娘猜猜乐
            </h1>
            <p className="mt-3 max-w-xl text-base leading-7 text-[var(--color-muted)] sm:text-lg">
              先选模式，再按开始。无论是自己慢慢猜，还是和别人同题竞速，这里都会直接把你送进房间。
            </p>
          </div>

          <div className="mt-6 flex flex-wrap gap-2.5">
            <span className="uma-chip">当前题库 {questionBankSize} 位</span>
            <span className="uma-chip uma-chip--green">每局 {maxGuesses} 次机会</span>
            <span className="uma-chip uma-chip--gold">
              {selectedMode === "multi"
                ? `当前匹配中 ${matchmakingStats.queueSize} 人`
                : selectedMode === "friend"
                  ? "支持建房和输入房间码"
                  : "支持常见别名"}
            </span>
          </div>

          <div className="mt-8 flex flex-col gap-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
              <button
                type="button"
                onClick={() => void handleStart()}
                disabled={starting || (selectedMode === "single" && questionBankSize === 0)}
                className="inline-flex min-h-13 items-center justify-center rounded-[18px] border border-[rgba(115,192,22,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-6 py-3 text-base font-semibold text-[#244117] shadow-[0_16px_28px_rgba(116,194,22,0.22)] transition hover:brightness-[1.02] disabled:cursor-not-allowed disabled:opacity-60"
              >
                {starting ? "正在进入" : currentMode.buttonLabel}
              </button>
              <p className="text-sm leading-6 text-[var(--color-muted)]">{currentMode.helper}</p>
            </div>

            {error ? (
              <p className="max-w-[560px] rounded-[20px] border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                {error}
              </p>
            ) : null}
          </div>
        </div>

        <div className="uma-menu-card rounded-[32px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.8)] p-4 shadow-[var(--shadow-soft)] sm:p-5">
          <div className="relative overflow-hidden rounded-[26px] border border-[rgba(96,147,192,0.14)] bg-[var(--color-panel-strong)]">
            <div className="pointer-events-none absolute inset-x-0 top-0 h-1 bg-[linear-gradient(90deg,var(--color-brand),var(--color-brand-blue))]" />
            <div className="border-b border-[var(--color-line)] px-4 py-4">
              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
                选择模式
              </p>
              <p className="mt-2 text-lg font-semibold text-[var(--color-ink)]">
                先选好这一局怎么玩
              </p>
            </div>

            <div className="p-4">
              {(["single", "multi", "friend"] as HomeMode[]).map((mode) => {
                const active = selectedMode === mode;
                const item = modeCopy[mode];
                const enabled = modeAvailability[mode];
                return (
                  <button
                    key={mode}
                    type="button"
                    onClick={() => {
                      if (!enabled) return;
                      setSelectedMode(mode);
                    }}
                    disabled={!enabled}
                    className={[
                      "mb-3 flex w-full items-center justify-between gap-4 rounded-[22px] border px-4 py-4 text-left transition sm:px-5",
                      active
                        ? "border-[rgba(103,186,24,0.24)] bg-[rgba(240,251,220,0.92)] shadow-[0_12px_28px_rgba(116,194,22,0.12)]"
                        : enabled
                          ? "border-[rgba(96,147,192,0.14)] bg-white/84 hover:bg-[rgba(229,244,255,0.74)]"
                          : "cursor-not-allowed border-[rgba(120,133,154,0.14)] bg-[rgba(239,242,246,0.86)] opacity-75",
                    ].join(" ")}
                  >
                    <div>
                      <p className="text-xl font-semibold text-[var(--color-ink)]">{item.title}</p>
                      <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
                        {item.description}
                      </p>
                    </div>
                    <span
                      className={[
                        "shrink-0 rounded-[14px] px-3 py-1 text-xs font-semibold",
                        active
                          ? "border border-[rgba(103,186,24,0.24)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] text-[#244117]"
                          : enabled
                            ? "bg-[rgba(227,233,240,0.92)] text-[var(--color-muted)]"
                            : "border border-[rgba(120,133,154,0.12)] bg-[rgba(225,230,236,0.9)] text-[#6b7788]",
                      ].join(" ")}
                    >
                      {active ? "当前选择" : enabled ? "点这里切换" : "暂未开启"}
                    </span>
                  </button>
                );
              })}

              <div className="rounded-[22px] border border-[rgba(96,147,192,0.14)] bg-white/84 px-4 py-4">
                <p className="text-sm font-semibold text-[var(--color-ink)]">{currentMode.title}</p>
                <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
                  {currentMode.description}
                </p>
                {selectedMode === "multi" ? (
                  <div className="mt-3 flex flex-wrap gap-2">
                    <span className="rounded-full border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-3 py-1 text-xs font-medium text-[var(--color-ink)]">
                      当前匹配中 {matchmakingStats.queueSize} 人
                    </span>
                    <span className="rounded-full border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-3 py-1 text-xs font-medium text-[var(--color-ink)]">
                      当前对战中 {matchmakingStats.activeRooms} 人
                    </span>
                  </div>
                ) : null}
              </div>
            </div>
          </div>
        </div>
      </div>

      <TurnstileDialog
        open={turnstileDialogOpen}
        siteKey={turnstileSiteKey}
        resetSignal={turnstileResetSignal}
        message="验证通过后会直接开始这一局，不用再点一次开始。"
        statusText={turnstileToken ? "验证完成，正在继续。" : "完成后会直接进入这一局。"}
        onTokenChange={setTurnstileToken}
        onClose={() => {
          pendingVerifiedActionRef.current = null;
          setTurnstileDialogOpen(false);
          setTurnstileToken(null);
          setTurnstileResetSignal((value) => value + 1);
        }}
      />
    </section>
  );
}
