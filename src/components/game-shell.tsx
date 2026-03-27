"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { CharacterSearchInput } from "@/components/search-input";
import { GameResultDialog } from "@/components/game-result-dialog";
import { GameStatusBar } from "@/components/game-status-bar";
import { GuessTable } from "@/components/guess-table";
import { SiteFooter } from "@/components/site-footer";
import { TurnstileWidget } from "@/components/turnstile-widget";
import type { PublicConfig } from "@/config/public";
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
  initialConfig: PublicConfig;
  initialGameState: GameState;
  initialQuestionBank: QuestionBankEntry[];
  initialQuestionBankSize: number;
  initialSearchEntries: SearchIndexEntry[];
};

async function requestJson<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
  const controller = new AbortController();
  const timeoutId = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);

  try {
    const response = await fetch(input, {
      ...init,
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
  initialConfig,
  initialGameState,
  initialQuestionBank,
  initialQuestionBankSize,
  initialSearchEntries,
}: Props) {
  const router = useRouter();
  const [gameState, setGameState] = useState<GameState>(initialGameState);
  const [error, setError] = useState<string | null>(null);
  const [shareNotice, setShareNotice] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [turnstileToken, setTurnstileToken] = useState<string | null>(null);
  const [turnstileResetSignal, setTurnstileResetSignal] = useState(0);
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
  const turnstileRequired = canEdit && Boolean(initialConfig.turnstileEnabled);
  const shareEnabled = Boolean(initialConfig.featureFlags.enableShare);
  const shouldSyncRoom = !canEdit && gameState.status === "playing";
  const effectivePollIntervalMs = resolveRoomSyncPollInterval(
    initialConfig.realtime.pollIntervalMs,
    roomSyncPreset,
  );

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
    role: canEdit ? "host" : "spectator",
    pollIntervalMs: effectivePollIntervalMs,
    heartbeatIntervalMs: initialConfig.realtime.heartbeatIntervalMs,
    wsUrl: initialConfig.realtime.wsUrl || undefined,
    onState: setGameState,
  });

  useRoomPresence({
    roomCode: gameState.roomCode,
    enabled: gameState.status === "playing",
    role: canEdit ? "host" : "spectator",
    heartbeatIntervalMs: initialConfig.realtime.heartbeatIntervalMs,
    onPresence: (summary, nextLatencyMs) => {
      setSpectatorCount(summary.spectatorCount);
      setAverageSpectatorLatencyMs(summary.averageLatencyMs);
      setLatencyMs(nextLatencyMs);
    },
  });

  function getTurnstileTokenOrThrow(missingMessage: string) {
    if (!turnstileRequired) {
      return undefined;
    }

    if (!initialConfig.turnstileSiteKey) {
      throw new Error("验证功能暂时不可用，请稍后再试。");
    }

    if (!turnstileToken) {
      throw new Error(missingMessage);
    }

    return turnstileToken;
  }

  function recycleTurnstileToken() {
    if (!turnstileRequired) {
      return;
    }

    setTurnstileToken(null);
    setTurnstileResetSignal((value) => value + 1);
  }

  async function startNewGame() {
    if (initialQuestionBankSize === 0) {
      setError("题库还在整理中，稍后再来试试。");
      return;
    }

    setSubmitting(true);
    setError(null);
    setShareNotice(null);
    let usedTurnstileToken = false;

    try {
      const token = getTurnstileTokenOrThrow("请先完成人机验证，再开始新一局。");
      usedTurnstileToken = Boolean(token);
      const data = await requestJson<GameState>("/api/game/new", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ turnstileToken: token }),
      });
      setGameState(data);
      router.replace(`/single/${data.roomCode}`);
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
      headers: { "Content-Type": "application/json" },
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
    let usedTurnstileToken = false;

    try {
      const token = getTurnstileTokenOrThrow("请先完成人机验证，再继续猜吧。");
      usedTurnstileToken = Boolean(token);
      const data = await requestJson<GameState>("/api/game/guess", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ gameId: gameState.gameId, characterId, turnstileToken: token }),
      });
      setGameState(data);
    } catch (nextError) {
      setError(
        nextError instanceof Error
          ? normalizePlayerError(nextError.message, "这一猜没送出去，再试一次吧。")
          : "这一猜没送出去，再试一次吧。",
      );
    } finally {
      if (usedTurnstileToken) {
        recycleTurnstileToken();
      }
      setSubmitting(false);
    }
  }

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
      <div className="mobile-rotate-guard">
        <div className="mobile-rotate-card">
          <p className="mobile-rotate-eyebrow">单人战局</p>
          <h2>把手机横过来再开始</h2>
          <p>这一局横着玩会更舒服。把手机横过来以后，画面会更顺手，猜起来也更连贯。</p>
          <Link href="/" className="mobile-rotate-link">
            先回主页
          </Link>
        </div>
      </div>

      <main className="mobile-landscape-shell relative mx-auto flex min-h-screen w-full max-w-[1320px] flex-col px-3 py-3 sm:px-6 sm:py-4 lg:px-8">
        <div className="pointer-events-none absolute inset-x-6 top-0 -z-10 h-56 rounded-b-[48px] bg-[linear-gradient(180deg,rgba(239,209,172,0.42),rgba(245,239,226,0))]" />
        <div className="pointer-events-none absolute left-0 right-0 top-16 -z-10 h-px bg-[linear-gradient(90deg,rgba(156,69,24,0),rgba(156,69,24,0.2),rgba(156,69,24,0))]" />

        <section className="battle-top-card rounded-[28px] border border-[var(--color-line)] bg-[var(--color-panel)] px-4 py-4 shadow-[var(--shadow-panel)] sm:rounded-[32px] sm:px-6 sm:py-6">
          <div className="battle-top-layout flex flex-col gap-4">
            <div className="battle-top-intro flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
              <div className="battle-top-heading">
                <Link
                  href="/"
                  className="inline-flex items-center gap-2 rounded-full border border-[var(--color-line)] bg-white/84 px-3 py-1.5 text-sm font-medium text-[var(--color-ink)] transition hover:bg-white"
                >
                  返回主页
                </Link>
                <p className="mt-4 text-xs font-semibold uppercase tracking-[0.24em] text-[var(--color-brand-strong)]">
                  单人战局
                </p>
                <h1 className="mt-2 font-[var(--font-display)] text-[1.9rem] font-bold leading-tight text-[var(--color-ink)] sm:text-[2.8rem]">
                  {!canEdit ? "看看这一局" : gameState.status === "ended" ? "这一局已结束" : "直接开猜"}
                </h1>
                <p className="battle-top-copy mt-2 max-w-2xl text-sm leading-6 text-[var(--color-muted)]">
                  {!canEdit
                    ? gameState.status === "ended"
                      ? "这是一条已经结束的房间链接，现在只能看看这局最后留下的记录。"
                      : "这是一条朋友发来的观战链接。你可以看进度，但不能替他继续往下猜。"
                    : gameState.status === "ended"
                      ? "这一局已经结束了，想继续玩就回主页再开一局。"
                      : "这一局已经就位了。输入名字往下猜，想换题就直接换一局，想把同一局发给朋友就带上房间码。"}
                </p>
              </div>

              <div className="battle-top-badges flex flex-wrap gap-2">
                <span className="rounded-full border border-[rgba(104,79,48,0.12)] bg-[var(--color-panel-soft)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
                  当前可猜 {initialQuestionBankSize} 位
                </span>
                <span className="rounded-full border border-[rgba(216,79,66,0.12)] bg-[rgba(216,79,66,0.1)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
                  每局 {initialConfig.maxGuesses} 次机会
                </span>
                <span className="rounded-full border border-[rgba(200,108,53,0.14)] bg-[rgba(255,245,231,0.86)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
                  房间码 {gameState.roomCode}
                </span>
                {canEdit && shareEnabled && gameState.status !== "ended" ? (
                  <button
                    type="button"
                    onClick={shareCurrentGame}
                    className="inline-flex items-center justify-center rounded-full border border-[rgba(200,108,53,0.18)] bg-white px-3 py-1.5 text-sm font-semibold text-[var(--color-brand-strong)] transition hover:bg-[rgba(255,248,240,0.96)]"
                  >
                    发给朋友
                  </button>
                ) : null}
              </div>
            </div>

            <div className="battle-status-shell rounded-[24px] border border-[rgba(104,79,48,0.12)] bg-[rgba(255,255,255,0.64)] px-4 py-4">
              <GameStatusBar
                gameState={gameState}
                canEdit={canEdit}
                disabled={submitting}
                onStartNewGame={startNewGame}
                onEndCurrentGame={endCurrentGame}
                questionBankSize={initialQuestionBankSize}
                spectatorCount={spectatorCount}
                latencyMs={canEdit ? averageSpectatorLatencyMs : latencyMs}
              />
            </div>
          </div>
        </section>

        <section className="battle-main-grid mt-3 grid gap-3 lg:mt-4">
          <div className="battle-main-column flex flex-col gap-3">
            <div className="battle-input-card rounded-[24px] border border-[var(--color-line)] bg-[var(--color-panel)] p-4 shadow-[var(--shadow-panel)] sm:rounded-[28px] sm:p-6">
              <div className="space-y-4">
                {turnstileRequired ? (
                  <div className="rounded-[24px] border border-[var(--color-line)] bg-[var(--color-panel-strong)] px-4 py-3">
                    <TurnstileWidget
                      siteKey={initialConfig.turnstileSiteKey}
                      resetSignal={turnstileResetSignal}
                      onTokenChange={setTurnstileToken}
                    />
                    <p className="mt-2 text-sm text-[var(--color-muted)]">
                      {turnstileToken ? "验证完成，可以继续了。" : "先完成人机验证，再继续往下猜。"}
                    </p>
                  </div>
                ) : null}

                <CharacterSearchInput
                  disabled={!canEdit || gameState.status !== "playing" || submitting}
                  disabledHint={
                    !canEdit
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

                {!canEdit ? (
                  <div className="space-y-3 rounded-2xl border border-[rgba(200,108,53,0.18)] bg-[rgba(255,246,234,0.9)] px-4 py-3 text-sm text-[var(--color-ink)]">
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
                                    ? "border-[rgba(200,108,53,0.28)] bg-white text-[var(--color-brand-strong)]"
                                    : "border-[rgba(104,79,48,0.14)] bg-[rgba(255,255,255,0.72)] text-[var(--color-ink)] hover:bg-white",
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
                  <p className="rounded-2xl border border-[rgba(200,108,53,0.18)] bg-[rgba(255,246,234,0.9)] px-4 py-3 text-sm text-[var(--color-ink)]">
                    {shareNotice}
                  </p>
                ) : null}

                {error ? (
                  <p className="rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                    {error}
                  </p>
                ) : null}

                {initialQuestionBankSize === 0 ? (
                  <p className="rounded-2xl border border-[rgba(200,108,53,0.18)] bg-[rgba(255,240,227,0.8)] px-4 py-3 text-sm text-[var(--color-ink)]">
                    当前正式题库还在整理中，等资料补齐后就能正常开局。
                  </p>
                ) : null}
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
          canEdit={canEdit}
          onRestart={startNewGame}
          onShare={shareCurrentGame}
          shareEnabled={shareEnabled}
          shareNotice={shareNotice}
        />

        <SiteFooter className="battle-footer mt-5" />
      </main>
    </>
  );
}
