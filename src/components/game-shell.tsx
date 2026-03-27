"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { CharacterSearchInput } from "@/components/search-input";
import { GameResultDialog } from "@/components/game-result-dialog";
import { GameStatusBar } from "@/components/game-status-bar";
import { GuessTable } from "@/components/guess-table";
import { SiteBrand } from "@/components/site-brand";
import { SiteFooter } from "@/components/site-footer";
import { TurnstileDialog } from "@/components/turnstile-dialog";
import type { PublicConfig } from "@/config/public";
import {
  buildGameViewerHeaders,
  readGameViewerToken,
  withGameViewerTokenQuery,
  writeGameViewerToken,
} from "@/lib/game/client-auth";
import { excludeGuessedSearchEntries } from "@/lib/game/search";
import { useRoomPresence } from "@/lib/realtime/use-room-presence";
import {
  defaultRoomSyncPreset,
  resolveRoomSyncPollInterval,
  roomSyncPresetOptions,
  type RoomSyncPresetId,
} from "@/lib/realtime/presets";
import { useRoomSync } from "@/lib/realtime/use-room-sync";
import type {
  GameState,
  QuestionBankEntry,
  SearchIndexEntry,
} from "@/lib/validation/schemas";

const REQUEST_TIMEOUT_MS = 7000;
const ROOM_SYNC_PRESET_KEY = "uma-room-sync-preset";

type Props = {
  canEdit: boolean;
  initialViewerToken: string | null;
  initialConfig: PublicConfig;
  initialGameState: GameState;
  initialQuestionBank: QuestionBankEntry[];
  initialQuestionBankSize: number;
  initialSearchEntries: SearchIndexEntry[];
};

type PendingProtectedAction = { type: "new-game" };

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
      throw new Error(data?.error ?? "这会儿有点忙，稍后再试一次吧。");
    }

    return data as T;
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") {
      throw new Error("这会儿有点忙，稍后再试一次吧。");
    }
    if (error instanceof SyntaxError) {
      throw new Error("这会儿有点忙，稍后再试一次吧。");
    }
    throw error;
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
  if (message.includes("当前题库中找不到对应角色")) {
    return "这位马娘暂时不在当前题库里。";
  }
  if (message.includes("question_bank_ready.json 为空")) {
    return "题库还在整理中，稍后再来试试。";
  }
  return message;
}

function formatDuration(startedAt?: string, finishedAt?: string | null) {
  if (!startedAt) {
    return "这局刚刚开始。";
  }

  const started = Date.parse(startedAt);
  const ended = finishedAt ? Date.parse(finishedAt) : Date.now();
  if (!Number.isFinite(started) || !Number.isFinite(ended) || ended <= started) {
    return "这局的用时还在统计中。";
  }

  const totalSeconds = Math.max(Math.round((ended - started) / 1000), 1);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;

  if (minutes <= 0) {
    return `这局用了 ${seconds} 秒。`;
  }

  return `这局用了 ${minutes} 分 ${seconds} 秒。`;
}

export function GameShell({
  canEdit,
  initialViewerToken,
  initialConfig,
  initialGameState,
  initialQuestionBank,
  initialQuestionBankSize,
  initialSearchEntries,
}: Props) {
  const router = useRouter();
  const [canEditState, setCanEditState] = useState(canEdit);
  const [gameState, setGameState] = useState<GameState>(initialGameState);
  const [error, setError] = useState<string | null>(null);
  const [shareNotice, setShareNotice] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [turnstileToken, setTurnstileToken] = useState<string | null>(null);
  const [turnstileResetSignal, setTurnstileResetSignal] = useState(0);
  const [turnstileDialogOpen, setTurnstileDialogOpen] = useState(false);
  const [pendingProtectedAction, setPendingProtectedAction] =
    useState<PendingProtectedAction | null>(null);
  const pendingVerifiedActionRef = useRef<((token: string) => Promise<void>) | null>(null);
  const [viewerToken, setViewerToken] = useState<string | null>(initialViewerToken);
  const [roomSyncPreset, setRoomSyncPreset] =
    useState<RoomSyncPresetId>(defaultRoomSyncPreset);
  const [spectatorCount, setSpectatorCount] = useState(0);
  const [latencyMs, setLatencyMs] = useState<number | null>(null);
  const [averageSpectatorLatencyMs, setAverageSpectatorLatencyMs] =
    useState<number | null>(null);

  const questionBankMap = useMemo(
    () => new Map(initialQuestionBank.map((entry) => [entry.id, entry])),
    [initialQuestionBank],
  );
  const availableSearchEntries = useMemo(
    () => excludeGuessedSearchEntries(initialSearchEntries, gameState.guessRows ?? []),
    [gameState.guessRows, initialSearchEntries],
  );
  const answerEntry = gameState.answerCharacterId
    ? questionBankMap.get(gameState.answerCharacterId) ?? null
    : null;
  const turnstileRequired = canEditState && Boolean(initialConfig.turnstileEnabled);
  const shareEnabled = Boolean(initialConfig.featureFlags.enableShare);
  const shouldSyncRoom = !canEditState && gameState.status === "playing";
  const effectivePollIntervalMs = resolveRoomSyncPollInterval(
    initialConfig.realtime.pollIntervalMs,
    roomSyncPreset,
  );

  useEffect(() => {
    if (initialViewerToken) {
      writeGameViewerToken(initialGameState.roomCode, initialViewerToken);
      setViewerToken(initialViewerToken);
      return;
    }

    setViewerToken(readGameViewerToken(initialGameState.roomCode));
  }, [initialGameState.roomCode, initialViewerToken]);

  useEffect(() => {
    if (typeof window === "undefined") {
      return;
    }

    const storedPreset = window.localStorage.getItem(ROOM_SYNC_PRESET_KEY);
    if (
      storedPreset &&
      roomSyncPresetOptions.some((option) => option.id === storedPreset)
    ) {
      setRoomSyncPreset(storedPreset as RoomSyncPresetId);
    }
  }, []);

  function updateRoomSyncPreset(preset: RoomSyncPresetId) {
    setRoomSyncPreset(preset);
    if (typeof window !== "undefined") {
      window.localStorage.setItem(ROOM_SYNC_PRESET_KEY, preset);
    }
  }

  useRoomSync({
    roomCode: gameState.roomCode,
    enabled: shouldSyncRoom,
    role: canEditState ? "host" : "spectator",
    pollIntervalMs: effectivePollIntervalMs,
    heartbeatIntervalMs: initialConfig.realtime.heartbeatIntervalMs,
    wsUrl: initialConfig.realtime.wsUrl || undefined,
    onState: setGameState,
  });

  useRoomPresence({
    roomCode: gameState.roomCode,
    enabled: gameState.status === "playing",
    role: canEditState ? "host" : "spectator",
    heartbeatIntervalMs: initialConfig.realtime.heartbeatIntervalMs,
    onPresence: (summary, nextLatencyMs) => {
      setSpectatorCount(summary.spectatorCount);
      setAverageSpectatorLatencyMs(summary.averageLatencyMs);
      setLatencyMs(nextLatencyMs);
    },
  });

  function recycleTurnstileToken() {
    setTurnstileToken(null);
    if (turnstileRequired) {
      setTurnstileResetSignal((value) => value + 1);
    }
  }

  function openTurnstileDialog(
    action: PendingProtectedAction,
    onVerified: (token: string) => Promise<void>,
  ) {
    if (!turnstileRequired) {
      return;
    }

    if (!initialConfig.turnstileSiteKey) {
      throw new Error("验证功能暂时不可用，请稍后再试。");
    }

    setPendingProtectedAction(action);
    pendingVerifiedActionRef.current = onVerified;
    setTurnstileDialogOpen(true);
    setTurnstileToken(null);
    setTurnstileResetSignal((value) => value + 1);
  }

  async function startNewGame(verifiedToken?: string) {
    if (initialQuestionBankSize === 0) {
      setError("题库还在整理中，稍后再来试试。");
      return;
    }

    if (turnstileRequired && !verifiedToken) {
      openTurnstileDialog({ type: "new-game" }, (token) => startNewGame(token));
      return;
    }

    setSubmitting(true);
    setError(null);
    setShareNotice(null);
    let usedTurnstileToken = false;

    try {
      usedTurnstileToken = Boolean(verifiedToken);
      const data = await requestJson<GameState>("/api/game/new", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ turnstileToken: verifiedToken }),
      });
      if (data.viewerToken) {
        writeGameViewerToken(data.roomCode, data.viewerToken);
        setViewerToken(data.viewerToken);
      }
      setCanEditState(Boolean(data.canEdit ?? true));
      setGameState(data);
      router.replace(
        data.viewerToken
          ? `/single/${data.roomCode}?v=${encodeURIComponent(data.viewerToken)}`
          : `/single/${data.roomCode}`,
      );
    } catch (nextError) {
      setError(
        nextError instanceof Error
          ? normalizePlayerError(nextError.message, "暂时还没法开始新一局。")
          : "暂时还没法开始新一局。",
      );
    } finally {
      if (usedTurnstileToken) {
        recycleTurnstileToken();
      }
      setSubmitting(false);
    }
  }

  function endCurrentGame() {
    setSubmitting(true);
    setError(null);
    setShareNotice(null);

    requestJson<GameState>("/api/game/end", {
      method: "POST",
      headers: buildGameViewerHeaders(viewerToken ?? readGameViewerToken(gameState.roomCode), {
        "Content-Type": "application/json",
      }),
      body: JSON.stringify({ gameId: gameState.gameId }),
    })
      .then((data) => {
        setGameState(data);
        recycleTurnstileToken();
        router.push("/");
      })
      .catch((nextError) => {
        setError(
          nextError instanceof Error
            ? normalizePlayerError(nextError.message, "这会儿还没法结束这一局。")
            : "这会儿还没法结束这一局。",
        );
      })
      .finally(() => {
        setSubmitting(false);
      });
  }

  async function submitGuess(characterId: string) {
    setSubmitting(true);
    setError(null);
    setShareNotice(null);

    try {
      const data = await requestJson<GameState>("/api/game/guess", {
        method: "POST",
        headers: buildGameViewerHeaders(viewerToken ?? readGameViewerToken(gameState.roomCode), {
          "Content-Type": "application/json",
        }),
        body: JSON.stringify({
          gameId: gameState.gameId,
          characterId,
        }),
      });
      setGameState(data);
      if (data.viewerToken) {
        writeGameViewerToken(data.roomCode, data.viewerToken);
        setViewerToken(data.viewerToken);
      }
      if (typeof data.canEdit === "boolean") {
        setCanEditState(data.canEdit);
      }
    } catch (nextError) {
      setError(
        nextError instanceof Error
          ? normalizePlayerError(nextError.message, "这一猜没送出去，再试一次吧。")
          : "这一猜没送出去，再试一次吧。",
      );
    } finally {
      setSubmitting(false);
    }
  }

  useEffect(() => {
    if (!turnstileDialogOpen || !turnstileToken || !pendingProtectedAction || submitting) {
      return;
    }

    const nextAction = pendingVerifiedActionRef.current;
    if (!nextAction) {
      return;
    }

    pendingVerifiedActionRef.current = null;
    setPendingProtectedAction(null);
    setTurnstileDialogOpen(false);
    setTurnstileToken(null);
    void nextAction(turnstileToken);
  }, [pendingProtectedAction, submitting, turnstileDialogOpen, turnstileToken]);

  useEffect(() => {
    if (canEditState || !viewerToken) {
      return;
    }

    let cancelled = false;

    async function refreshEditableState() {
      try {
        const data = await requestJson<GameState>(
          withGameViewerTokenQuery(`/api/game/${gameState.roomCode}`, viewerToken),
        );
        if (cancelled) {
          return;
        }

        if (typeof data.canEdit === "boolean") {
          setCanEditState(data.canEdit);
        }
        if (data.viewerToken) {
          writeGameViewerToken(data.roomCode, data.viewerToken);
          setViewerToken(data.viewerToken);
        }
        setGameState(data);
        if (window.location.search.includes("v=")) {
          window.history.replaceState({}, "", `/single/${gameState.roomCode}`);
        }
      } catch {}
    }

    void refreshEditableState();

    return () => {
      cancelled = true;
    };
  }, [canEditState, gameState.roomCode, viewerToken]);

  useEffect(() => {
    if (typeof window === "undefined" || !canEditState) {
      return;
    }

    if (window.location.search.includes("v=") || window.location.search.includes("viewerToken=")) {
      window.history.replaceState({}, "", `/single/${gameState.roomCode}`);
    }
  }, [canEditState, gameState.roomCode]);

  async function shareCurrentGame() {
    const siteName = initialConfig.siteName ?? "赛马娘猜猜乐";
    const baseUrl =
      initialConfig.shareBaseUrl ??
      (typeof window !== "undefined" ? window.location.origin : "http://localhost:3000");
    const shareUrl = new URL(`/single/${gameState.roomCode}`, baseUrl).toString();
    const maxGuesses = initialConfig.maxGuesses ?? 8;
    const browserNavigator = typeof navigator !== "undefined" ? navigator : null;
    const text =
      gameState.status === "won"
        ? `我在${siteName}里用了 ${gameState.guessRows.length}/${maxGuesses} 次猜中了 ${gameState.answerDisplayName ?? "答案"}，房间码是 ${gameState.roomCode}。`
        : gameState.status === "lost"
          ? `我刚在${siteName}里翻车了，这局答案是 ${gameState.answerDisplayName ?? "这位马娘"}，房间码是 ${gameState.roomCode}。`
          : gameState.status === "ended"
            ? `这一局已经结束了，房间码是 ${gameState.roomCode}。`
            : `我正在 ${siteName} 里猜马娘，房间码是 ${gameState.roomCode}。这是观战链接，只能看进度，不能帮我落猜。`;

    try {
      if (browserNavigator?.share) {
        await browserNavigator.share({
          title: siteName,
          text,
          url: shareUrl,
        });
        setShareNotice(`观战链接已经带着房间码 ${gameState.roomCode} 发出去了。`);
        return;
      }

      if (browserNavigator?.clipboard?.writeText) {
        await browserNavigator.clipboard.writeText(`${text}\n${shareUrl}`);
        setShareNotice(`观战链接和房间码 ${gameState.roomCode} 已经复制好了。`);
        return;
      }

      setShareNotice("当前环境暂时不能直接分享。");
    } catch {
      setShareNotice("这次没有成功发出去，稍后再试一次吧。");
    }
  }

  return (
    <>
      <main className="mobile-landscape-shell relative mx-auto flex min-h-screen w-full max-w-[1320px] flex-col px-3 py-3 sm:px-6 sm:py-4 lg:px-8">
        <div className="pointer-events-none absolute inset-x-6 top-0 -z-10 h-56 rounded-b-[48px] bg-[linear-gradient(180deg,rgba(151,216,28,0.16),rgba(237,247,255,0))]" />
        <div className="pointer-events-none absolute left-0 right-0 top-16 -z-10 h-px bg-[linear-gradient(90deg,rgba(115,192,22,0),rgba(115,192,22,0.24),rgba(63,136,247,0.24),rgba(115,192,22,0))]" />

        <section className="battle-top-card uma-panel px-4 py-4 sm:px-6 sm:py-5">
          <div className="battle-top-layout flex flex-col gap-3">
            <div className="battle-top-intro flex flex-col gap-3">
              <div className="battle-top-heading">
                <div className="mb-3 flex flex-wrap items-start justify-between gap-3">
                  <SiteBrand
                    compact
                    href="/"
                    className="max-w-[620px]"
                    showTitle={false}
                    subtitle={!canEditState ? "这是一条观战链接，只能看这一局的进度。" : "房间已就位，直接输入名字开始猜。"}
                  />

                  <Link
                    href="/"
                    className="uma-ghost-button text-sm"
                  >
                    返回主页
                  </Link>
                </div>
                <p className="mt-1 text-xs font-semibold uppercase tracking-[0.24em] text-[var(--color-brand-strong)]">
                  单人战局
                </p>
                <h1 className="mt-1 font-[var(--font-display)] text-[1.9rem] font-bold leading-tight text-[var(--color-ink)] sm:text-[2.6rem]">
                  {!canEditState ? "看看这一局" : gameState.status === "ended" ? "这一局已结束" : "直接开猜"}
                </h1>
                <p className="battle-top-copy mt-1 max-w-2xl text-sm leading-6 text-[var(--color-muted)]">
                  {!canEditState
                    ? gameState.status === "ended"
                      ? "这是一条已经结束的房间链接，现在只能看看这局最后留下的记录。"
                      : "这是一条朋友发来的观战链接。你可以看进度，但不能替他继续往下猜。"
                    : gameState.status === "ended"
                      ? "这一局已经结束了，想继续玩就回主页再开一局。"
                      : "这一局已经就位了。输入名字往下猜，想换题就直接换一局，想把同一局发给朋友就带上房间码。"}
                </p>
                <div className="battle-top-badges mt-3 flex flex-wrap gap-2">
                  <span className="uma-chip uma-chip--green">
                    当前可猜 {initialQuestionBankSize} 位
                  </span>
                  <span className="uma-chip uma-chip--gold">
                    每局 {initialConfig.maxGuesses} 次机会
                  </span>
                  <span className="uma-chip uma-chip--blue">
                    房间码 {gameState.roomCode}
                  </span>
                  {canEditState && shareEnabled && gameState.status !== "ended" ? (
                    <button
                      type="button"
                      onClick={shareCurrentGame}
                      className="uma-ghost-button text-sm"
                    >
                      发给朋友
                    </button>
                  ) : null}
                </div>
              </div>
            </div>

            <div className="battle-status-shell uma-panel-soft bg-[rgba(239,247,255,0.68)] px-4 py-4">
              <GameStatusBar
                gameState={gameState}
                canEdit={canEditState}
                disabled={submitting}
                onStartNewGame={startNewGame}
                onEndCurrentGame={endCurrentGame}
                questionBankSize={initialQuestionBankSize}
                spectatorCount={spectatorCount}
                latencyMs={canEditState ? averageSpectatorLatencyMs : latencyMs}
              />
            </div>
          </div>
        </section>

        <section className="battle-main-grid mt-3 grid gap-3 lg:mt-4">
          <div className="battle-main-column flex flex-col gap-3">
            <div className="single-search-sticky sticky top-2 z-30">
            <div className="battle-input-card uma-panel p-4 sm:p-5">
              <div className="space-y-4">
                <CharacterSearchInput
                  disabled={!canEditState || gameState.status !== "playing" || submitting}
                  disabledHint={
                    !canEditState
                      ? gameState.status === "ended"
                        ? "这一局已经结束了"
                        : "这是观战链接，不能替别人落猜"
                      : gameState.status === "ended"
                        ? "这一局已经结束了"
                        : undefined
                  }
                  entries={availableSearchEntries}
                  onSelect={submitGuess}
                />

                {!canEditState ? (
                  <div className="space-y-3 rounded-2xl border border-[var(--color-line)] bg-[rgba(239,247,255,0.82)] px-4 py-3 text-sm text-[var(--color-ink)]">
                    <p>
                    {gameState.status === "ended"
                      ? "这一局已经结束了，现在只能看看最后留下的线索。"
                      : "这是朋友发来的观战链接，你可以看进度，但不能帮他继续落猜。"}
                    </p>

                    {gameState.status === "playing" ? (
                      <div className="space-y-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-brand-strong)]">
                            更新节奏
                          </span>
                          <span className="text-xs text-[var(--color-muted)]">
                            人多一起看时，可以调慢一点。
                          </span>
                        </div>
                        <div className="flex flex-wrap gap-2">
                          {roomSyncPresetOptions.map((option) => {
                            const active = option.id === roomSyncPreset;
                            return (
                              <button
                                key={option.id}
                                type="button"
                                onClick={() => updateRoomSyncPreset(option.id)}
                                className={[
                                  "min-h-10 rounded-full border px-3 py-2 text-sm font-medium transition",
                                  active
                                    ? "border-[rgba(63,136,247,0.24)] bg-white text-[var(--color-brand-blue-deep)]"
                                    : "border-[var(--color-line)] bg-[rgba(255,255,255,0.72)] text-[var(--color-ink)] hover:bg-white",
                                ].join(" ")}
                              >
                                {option.label}
                              </button>
                            );
                          })}
                        </div>
                        <p className="text-xs leading-5 text-[var(--color-muted)]">
                          {
                            roomSyncPresetOptions.find(
                              (option) => option.id === roomSyncPreset,
                            )?.helper
                          }
                        </p>
                      </div>
                    ) : null}
                  </div>
                ) : null}

                {shareNotice ? (
                  <p className="rounded-2xl border border-[var(--color-line)] bg-[rgba(239,247,255,0.82)] px-4 py-3 text-sm text-[var(--color-ink)]">
                    {shareNotice}
                  </p>
                ) : null}

                {error ? (
                  <p className="rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                    {error}
                  </p>
                ) : null}

                {initialQuestionBankSize === 0 ? (
                  <p className="rounded-2xl border border-[rgba(243,199,84,0.24)] bg-[rgba(255,244,181,0.72)] px-4 py-3 text-sm text-[var(--color-ink)]">
                    当前正式题库还在整理中，等资料补齐后就能正常开局。
                  </p>
                ) : null}
              </div>
            </div>
            </div>

            <GuessTable rows={gameState.guessRows ?? []} />
          </div>
        </section>

        <GameResultDialog
          gameState={gameState}
          maxGuesses={initialConfig.maxGuesses}
          answerEntry={answerEntry}
          summaryText={formatDuration(gameState.startedAt, gameState.finishedAt)}
          canEdit={canEditState}
          onRestart={startNewGame}
          onShare={shareCurrentGame}
          shareEnabled={shareEnabled}
          shareNotice={shareNotice}
        />

        <TurnstileDialog
          open={turnstileDialogOpen}
          siteKey={initialConfig.turnstileSiteKey}
          resetSignal={turnstileResetSignal}
          message="验证通过后会直接帮你换一局。"
          statusText={turnstileToken ? "验证完成，正在继续。" : "完成后会自动继续。"}
          onTokenChange={setTurnstileToken}
          onClose={() => {
            pendingVerifiedActionRef.current = null;
            setTurnstileDialogOpen(false);
            setPendingProtectedAction(null);
            recycleTurnstileToken();
          }}
        />

        <SiteFooter className="battle-footer mt-5" />
      </main>
    </>
  );
}
