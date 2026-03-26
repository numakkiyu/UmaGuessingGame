"use client";

import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { CharacterSearchInput } from "@/components/search-input";
import { GameHeader } from "@/components/game-header";
import { GameResultDialog } from "@/components/game-result-dialog";
import { GameStatusBar } from "@/components/game-status-bar";
import { GuessTable } from "@/components/guess-table";
import { HowToPlayPanel } from "@/components/how-to-play-panel";
import { LatestGuessHighlight } from "@/components/latest-guess-highlight";
import { TurnstileWidget } from "@/components/turnstile-widget";
import { publicConfigSchema, type SearchIndexEntry } from "@/lib/validation/schemas";

const GAME_STORAGE_KEY = "uma-guessing-game:current-game-id";

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

type GameState = {
  gameId: string;
  status: "playing" | "won" | "lost";
  remainingGuesses: number;
  guessRows: Array<{
    characterId: string;
    displayName: string;
    avatarUrl: string;
    cells: Record<string, { value: string; status: "correct" | "near" | "wrong" }>;
  }>;
  answerCharacterId?: string | null;
  answerDisplayName?: string | null;
};

type Props = {
  initialQuestionBankSize: number;
  initialSearchEntries: SearchIndexEntry[];
};

export function GameShell({ initialQuestionBankSize, initialSearchEntries }: Props) {
  const [config, setConfig] = useState<ReturnType<typeof publicConfigSchema.parse> | null>(null);
  const [gameState, setGameState] = useState<GameState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [shareNotice, setShareNotice] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [turnstileToken, setTurnstileToken] = useState<string | null>(null);
  const [turnstileResetSignal, setTurnstileResetSignal] = useState(0);
  const hasAttemptedAutoStart = useRef(false);

  const latestGuess = useMemo(
    () => gameState?.guessRows.at(-1) ?? null,
    [gameState],
  );
  const turnstileRequired = Boolean(config?.turnstileEnabled);
  const shareEnabled = Boolean(config?.featureFlags.enableShare);
  const autoStartGame = useEffectEvent(() => {
    void startNewGame();
  });

  useEffect(() => {
    fetch("/api/config/public")
      .then((response) => response.json())
      .then((data) => setConfig(publicConfigSchema.parse(data)))
      .catch(() => setConfig(null));
  }, []);

  useEffect(() => {
    const storedGameId = window.localStorage.getItem(GAME_STORAGE_KEY);
    if (!storedGameId) {
      return;
    }

    fetch(`/api/game/${storedGameId}`)
      .then(async (response) => {
        if (!response.ok) {
          throw new Error("当前对局已失效。");
        }
        return response.json();
      })
      .then((data) => setGameState(data))
      .catch(() => window.localStorage.removeItem(GAME_STORAGE_KEY));
  }, []);

  useEffect(() => {
    if (!gameState) {
      return;
    }

    window.localStorage.setItem(GAME_STORAGE_KEY, gameState.gameId);
  }, [gameState]);

  function getTurnstileTokenOrThrow(missingMessage: string) {
    if (!turnstileRequired) {
      return undefined;
    }

    if (!config?.turnstileSiteKey) {
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
      const response = await fetch("/api/game/new", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ turnstileToken: token }),
      });
      const data = await response.json();
      if (!response.ok) {
        throw new Error(data.error ?? "暂时还没法开始新一局。");
      }
      setGameState(data);
      window.localStorage.setItem(GAME_STORAGE_KEY, data.gameId);
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

  async function submitGuess(characterId: string) {
    if (!gameState) return;
    setSubmitting(true);
    setError(null);
    setShareNotice(null);
    let usedTurnstileToken = false;
    try {
      const token = getTurnstileTokenOrThrow("请先完成人机验证，再继续猜吧。");
      usedTurnstileToken = Boolean(token);
      const response = await fetch("/api/game/guess", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ gameId: gameState.gameId, characterId, turnstileToken: token }),
      });
      const data = await response.json();
      if (!response.ok) {
        throw new Error(data.error ?? "提交猜测失败。");
      }
      setGameState(data);
    } catch (nextError) {
      setError(
        nextError instanceof Error
          ? normalizePlayerError(nextError.message, "提交猜测失败。")
          : "提交猜测失败。",
      );
    } finally {
      if (usedTurnstileToken) {
        recycleTurnstileToken();
      }
      setSubmitting(false);
    }
  }

  useEffect(() => {
    if (hasAttemptedAutoStart.current) {
      return;
    }
    if (!config || turnstileRequired || gameState || initialQuestionBankSize === 0) {
      return;
    }
    if (window.localStorage.getItem(GAME_STORAGE_KEY)) {
      return;
    }

    hasAttemptedAutoStart.current = true;
    autoStartGame();
  }, [config, gameState, initialQuestionBankSize, turnstileRequired]);

  async function shareCurrentGame() {
    const siteName = config?.siteName ?? "赛马娘弗一把";
    const baseUrl =
      config?.shareBaseUrl ??
      (typeof window !== "undefined" ? window.location.origin : "http://localhost:3000");
    const maxGuesses = config?.maxGuesses ?? 8;
    const browserNavigator = typeof navigator !== "undefined" ? navigator : null;
    const text =
      gameState?.status === "won"
        ? `我在${siteName}里用了 ${gameState.guessRows.length}/${maxGuesses} 次猜中了 ${gameState.answerDisplayName ?? "答案"}，来试试看你要几次。`
        : gameState?.status === "lost"
          ? `我刚在${siteName}里翻车了，这局答案是 ${gameState.answerDisplayName ?? "这位马娘"}。来试试看你能不能更快找到她。`
          : `我正在 ${siteName} 里猜马娘，来一起试试看谁更快找到答案。`;

    try {
      if (browserNavigator?.share) {
        await browserNavigator.share({
          title: siteName,
          text,
          url: baseUrl,
        });
        setShareNotice("分享面板已经打开了。");
        return;
      }

      if (browserNavigator?.clipboard?.writeText) {
        await browserNavigator.clipboard.writeText(`${text}\n${baseUrl}`);
        setShareNotice("分享文案已经复制好了。");
        return;
      }

      setShareNotice("当前环境暂时不能直接分享。");
    } catch {
      setShareNotice("这次没有成功发出去，稍后再试一次吧。");
    }
  }

  return (
    <main className="relative mx-auto flex min-h-screen w-full max-w-[1360px] flex-col px-4 py-4 sm:px-6 lg:px-8">
      <div className="pointer-events-none absolute inset-x-6 top-0 -z-10 h-64 rounded-b-[48px] bg-[linear-gradient(180deg,rgba(239,209,172,0.42),rgba(245,239,226,0))]" />

      <GameHeader
        questionBankSize={initialQuestionBankSize}
        maxGuesses={config?.maxGuesses ?? 8}
      />

      <section className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <div className="rounded-[28px] border border-[var(--color-line)] bg-[var(--color-panel)] p-5 shadow-[var(--shadow-panel)] sm:p-6">
          <div className="space-y-4">
            <div>
              <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--color-brand-strong)]">
                开始这一局
              </p>
              <h2 className="mt-2 font-[var(--font-display)] text-2xl font-bold text-[var(--color-ink)]">
                搜索和状态都放在这里
              </h2>
              <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
                先看状态，再输入一位马娘。新线索会立刻出现在下方表格里。
              </p>
            </div>

            <GameStatusBar
              gameState={gameState}
              disabled={submitting}
              onStartNewGame={startNewGame}
              questionBankSize={initialQuestionBankSize}
              maxGuesses={config?.maxGuesses ?? 8}
            />

            <CharacterSearchInput
              disabled={!gameState || gameState.status !== "playing" || submitting}
              entries={initialSearchEntries}
              onSelect={submitGuess}
            />

            {turnstileRequired ? (
              <div className="rounded-[24px] border border-[var(--color-line)] bg-[var(--color-panel-strong)] px-4 py-3">
                <TurnstileWidget
                  siteKey={config?.turnstileSiteKey ?? ""}
                  resetSignal={turnstileResetSignal}
                  onTokenChange={setTurnstileToken}
                />
                <p className="mt-2 text-sm text-[var(--color-muted)]">
                  {turnstileToken
                    ? "验证完成，可以继续了。"
                    : gameState?.status === "playing"
                      ? "先完成人机验证，再继续猜吧。"
                      : "先完成人机验证，再开始新一局。"}
                </p>
              </div>
            ) : null}

            {initialQuestionBankSize === 0 ? (
              <p className="rounded-2xl border border-[rgba(200,108,53,0.18)] bg-[rgba(255,240,227,0.8)] px-4 py-3 text-sm text-[var(--color-ink)]">
                当前正式题库还在整理中，等资料补齐后就能正常开局。
              </p>
            ) : null}

            {latestGuess ? <LatestGuessHighlight latestGuess={latestGuess} /> : null}
            {error ? (
              <p className="rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                {error}
              </p>
            ) : null}
          </div>
        </div>

        <HowToPlayPanel
          config={config}
          gameState={gameState}
          onShare={shareCurrentGame}
          shareNotice={shareNotice}
        />
      </section>

      <section className="mt-4">
        <GuessTable rows={gameState?.guessRows ?? []} />
      </section>

      <GameResultDialog
        gameState={gameState}
        maxGuesses={config?.maxGuesses ?? 8}
        onRestart={startNewGame}
        onShare={shareCurrentGame}
        shareEnabled={shareEnabled}
        shareNotice={shareNotice}
      />
    </main>
  );
}
