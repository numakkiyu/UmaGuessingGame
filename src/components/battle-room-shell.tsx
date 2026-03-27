"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { AvatarImage } from "@/components/avatar-image";
import { CharacterSearchInput } from "@/components/search-input";
import { GuessTable } from "@/components/guess-table";
import { LatencyBadge } from "@/components/latency-badge";
import { SiteBrand } from "@/components/site-brand";
import { SiteFooter } from "@/components/site-footer";
import type { PublicConfig } from "@/config/public";
import {
  buildViewerTokenHeaders,
  readRoomViewerToken,
  withViewerTokenQuery,
  writeRoomViewerToken,
} from "@/lib/battle/client-auth";
import { excludeGuessedSearchEntries } from "@/lib/game/search";
import type {
  BattleRoomState,
  QuestionBankEntry,
  SearchIndexEntry,
} from "@/lib/validation/schemas";

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

function formatCountdown(deadline?: string | null) {
  if (!deadline) return null;
  return Math.max(0, Math.ceil((Date.parse(deadline) - Date.now()) / 1000));
}

function formatRoomStatus(state: BattleRoomState | null) {
  if (!state) return "正在整理房间";
  switch (state.status) {
    case "waiting":
      return "等待朋友加入";
    case "matched_pending_accept":
      return "等待双方确认";
    case "ready_confirm":
      return "准备开始";
    case "playing":
      return "正在对战";
    case "paused":
      return "暂时暂停";
    case "finished":
      return "本局已结束";
    default:
      return "准备中";
  }
}

function buildSummaryText(state: BattleRoomState) {
  if (state.status === "finished") {
    if (state.winnerSeat === state.viewerSeat) {
      return "这一局你先冲到了终点。";
    }
    if (state.loserSeat === state.viewerSeat && state.self.result === "surrendered") {
      return "这一局已经由你主动结束。";
    }
    if (state.loserSeat === state.viewerSeat) {
      return "这一局先被对手拿下了。";
    }
    return "这一局已经结束。";
  }

  if (state.status === "paused") {
    return "这局暂时停了一下，倒计时结束后会继续。";
  }

  return "把线索一格一格收紧，谁先猜中，谁就先拿下这一局。";
}

function statusClassName(status: "correct" | "near" | "wrong") {
  switch (status) {
    case "correct":
      return "bg-[#78c964] text-[#163015]";
    case "near":
      return "bg-[#ffd86c] text-[#4d3905]";
    case "wrong":
      return "bg-[#dce2ea] text-[#344253]";
    default:
      return "";
  }
}

function OpponentShadowBoard({ state }: { state: BattleRoomState }) {
  return (
    <div className="rounded-[24px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] p-4 shadow-[var(--shadow-soft)]">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-brand-strong)]">
            对手进度
          </p>
          <p className="mt-1 text-lg font-semibold text-[var(--color-ink)]">
            {state.opponent.displayName}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <LatencyBadge latencyMs={state.opponent.latencyMs} compact />
          <span className="rounded-full border border-[var(--color-line)] bg-white px-3 py-1 text-xs font-medium text-[var(--color-ink)]">
            剩余 {state.opponent.remainingGuesses} 次
          </span>
        </div>
      </div>

      <div className="mt-4 space-y-2">
        {state.opponent.shadowRows.length === 0 ? (
          <div className="rounded-[18px] border border-dashed border-[var(--color-line)] bg-[rgba(239,247,255,0.68)] px-4 py-4 text-sm leading-6 text-[var(--color-muted)]">
            对手的进度会在这里慢慢亮起来。
          </div>
        ) : (
          state.opponent.shadowRows.map((row) => (
            <div
              key={`${row.attempt}-${row.submittedAt}`}
              className="rounded-[18px] border border-[var(--color-line)] bg-[rgba(248,251,255,0.86)] px-3 py-3"
            >
              <p className="mb-2 text-xs font-semibold uppercase tracking-[0.16em] text-[var(--color-muted)]">
                第 {row.attempt} 猜
              </p>
              <div className="grid grid-cols-3 gap-2 sm:grid-cols-5">
                {Object.values(row.statuses).map((status, index) => (
                  <div
                    key={`${row.attempt}-${index}`}
                    className={`flex min-h-9 items-center justify-center rounded-[14px] px-2 py-2 text-xs font-semibold ${statusClassName(status)}`}
                  >
                    {status === "correct" ? "准" : status === "near" ? "近" : "偏"}
                  </div>
                ))}
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function PlayerSeatCard({
  title,
  isSelf,
  displayName,
  online,
  latencyMs,
  remainingGuesses,
  acceptedStart,
  requestedRematch,
  result,
}: {
  title: string;
  isSelf: boolean;
  displayName: string;
  online: boolean;
  latencyMs: number | null;
  remainingGuesses: number;
  acceptedStart: boolean;
  requestedRematch: boolean;
  result: BattleRoomState["self"]["result"];
}) {
  const statusLabel =
    result === "won"
      ? "已拿下"
      : result === "lost"
        ? "本局落后"
        : result === "surrendered"
          ? "已结束本局"
          : requestedRematch
            ? "想再来一局"
            : acceptedStart
              ? "已确认"
              : online
                ? "已在线"
                : "暂未在线";

  return (
    <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.9)] px-4 py-4 shadow-[var(--shadow-soft)]">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-brand-strong)]">
            {title}
          </p>
          <p className="mt-1 text-base font-semibold text-[var(--color-ink)]">
            {displayName}
            {isSelf ? "（你）" : ""}
          </p>
        </div>
        <span
          className={[
            "rounded-full px-3 py-1 text-xs font-semibold",
            result === "won"
              ? "bg-[#dff4da] text-[#1f5a1a]"
              : result === "lost" || result === "surrendered"
                ? "bg-[#e7ebf0] text-[#435062]"
                : requestedRematch
                  ? "bg-[#fff0bf] text-[#735300]"
                  : acceptedStart
                    ? "bg-[#eef7ff] text-[#245fc9]"
                    : online
                      ? "bg-[#ecf9f6] text-[#12685d]"
                      : "bg-[#eef1f5] text-[#5b6675]",
          ].join(" ")}
        >
          {statusLabel}
        </span>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <LatencyBadge latencyMs={latencyMs} compact />
        <span className="rounded-full border border-[var(--color-line)] bg-white px-3 py-1 text-xs font-medium text-[var(--color-ink)]">
          剩余 {remainingGuesses} 次
        </span>
      </div>
    </div>
  );
}

function AnswerCard({
  state,
  answerEntry,
}: {
  state: BattleRoomState;
  answerEntry: QuestionBankEntry | null;
}) {
  if (state.status !== "finished" || !answerEntry) {
    return null;
  }

  const detailItems = [
    { label: "星级", value: `${answerEntry.star}星` },
    { label: "场地", value: answerEntry.surface_group.join("/") },
    { label: "距离", value: answerEntry.distance_group.join("/") },
    { label: "跑法", value: answerEntry.running_style_group.join("/") },
    { label: "牡/牝", value: answerEntry.sex_type },
    { label: "GI档", value: answerEntry.g1_bracket },
    { label: "GII/GIII档", value: answerEntry.g23_bracket },
    { label: "学年", value: answerEntry.school_grade },
    { label: "宿舍", value: answerEntry.dormitory },
  ];

  return (
    <div className="rounded-[28px] border border-[var(--color-line)] bg-[linear-gradient(180deg,rgba(255,255,255,0.92),rgba(240,247,255,0.94))] p-4 shadow-[var(--shadow-soft)] sm:p-5">
      <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
        这一局的答案
      </p>
      <div className="mt-4 flex flex-col gap-4 lg:flex-row">
        <div className="flex w-full max-w-[180px] items-center justify-center overflow-hidden rounded-[24px] border border-[var(--color-line)] bg-white shadow-[var(--shadow-soft)]">
          <AvatarImage
            primarySrc={answerEntry.image_local_path}
            proxySrc={answerEntry.image_local_path}
            remoteSrc={answerEntry.image_url}
            alt={answerEntry.name_zh}
            className="size-full object-cover"
          />
        </div>
        <div className="flex-1">
          <h2 className="font-[var(--font-display)] text-[1.8rem] font-bold text-[var(--color-ink)]">
            {answerEntry.name_zh}
          </h2>
          <p className="mt-1 text-sm text-[var(--color-muted)]">{answerEntry.name_jp}</p>
          <div className="mt-4 grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {detailItems.map((item) => (
              <div
                key={item.label}
                className="rounded-[18px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.88)] px-3 py-3"
              >
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-[var(--color-muted)]">
                  {item.label}
                </p>
                <p className="mt-1 text-sm font-semibold text-[var(--color-ink)]">{item.value}</p>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

export function BattleRoomShell({
  roomCode,
  initialViewerToken,
  initialConfig,
  initialQuestionBank,
  initialSearchEntries,
}: {
  roomCode: string;
  initialViewerToken: string | null;
  initialConfig: PublicConfig;
  initialQuestionBank: QuestionBankEntry[];
  initialSearchEntries: SearchIndexEntry[];
}) {
  const [state, setState] = useState<BattleRoomState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [viewerToken, setViewerToken] = useState<string | null>(initialViewerToken);
  const [viewerTokenReady, setViewerTokenReady] = useState(false);

  const questionBankMap = useMemo(
    () => new Map(initialQuestionBank.map((entry) => [entry.id, entry])),
    [initialQuestionBank],
  );
  const availableSearchEntries = useMemo(
    () =>
      state
        ? excludeGuessedSearchEntries(initialSearchEntries, state.self.guessRows)
        : initialSearchEntries,
    [initialSearchEntries, state],
  );
  const answerEntry =
    state?.answerCharacterId ? questionBankMap.get(state.answerCharacterId) ?? null : null;
  const acceptanceCountdown = formatCountdown(state?.acceptanceDeadline ?? null);
  const pauseCountdown = formatCountdown(state?.pausedUntil ?? null);
  const canGuess = state?.status === "playing";
  const roomStateReady = Boolean(state?.roomCode);

  useEffect(() => {
    if (initialViewerToken) {
      writeRoomViewerToken(roomCode, initialViewerToken);
      setViewerToken(initialViewerToken);
      setViewerTokenReady(true);
      return;
    }

    setViewerToken(readRoomViewerToken(roomCode));
    setViewerTokenReady(true);
  }, [initialViewerToken, roomCode]);

  useEffect(() => {
    if (
      !state ||
      typeof window === "undefined" ||
      (!window.location.search.includes("v=") &&
        !window.location.search.includes("viewerToken="))
    ) {
      return;
    }

    window.history.replaceState({}, "", `/battle/${roomCode}`);
  }, [roomCode, state]);

  useEffect(() => {
    if (!viewerTokenReady) {
      return;
    }

    let active = true;

    async function loadState() {
      try {
        const resolvedViewerToken = viewerToken ?? readRoomViewerToken(roomCode);
        if (!resolvedViewerToken) {
          if (active) {
            setError("请先从匹配或房间入口进入这一局。");
          }
          return;
        }

        const data = await requestJson<BattleRoomState>(
          withViewerTokenQuery(`/api/rooms/${roomCode}`, resolvedViewerToken),
        );
        if (active) {
          setState(data);
          setViewerToken(data.viewerToken);
          writeRoomViewerToken(roomCode, data.viewerToken);
          setError(null);
        }
      } catch (nextError) {
        if (active) {
          setError(nextError instanceof Error ? nextError.message : "这会儿还读不到房间。");
        }
      }
    }

    void loadState();
    const timer = window.setInterval(() => {
      void loadState();
    }, 1500);

    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [roomCode, viewerToken, viewerTokenReady]);

  useEffect(() => {
    if (
      !viewerTokenReady ||
      typeof window === "undefined" ||
      !("EventSource" in window)
    ) {
      return;
    }

    const resolvedViewerToken = viewerToken ?? readRoomViewerToken(roomCode);
    if (!resolvedViewerToken) {
      return;
    }

    const source = new EventSource(
      withViewerTokenQuery(`/api/rooms/${roomCode}/stream`, resolvedViewerToken),
    );
    source.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data) as BattleRoomState | { error?: string };
        if ("error" in payload && payload.error) {
          setError(payload.error);
          return;
        }

        const nextState = payload as BattleRoomState;
        setState(nextState);
        setViewerToken(nextState.viewerToken);
        writeRoomViewerToken(roomCode, nextState.viewerToken);
      } catch {}
    };
    source.onerror = () => {
      source.close();
    };

    return () => {
      source.close();
    };
  }, [roomCode, viewerToken, viewerTokenReady]);

  useEffect(() => {
    if (!roomStateReady || !viewerTokenReady) {
      return;
    }

    let active = true;
    let timer: number | null = null;

    async function heartbeat() {
      const startedAt = performance.now();

      try {
        const resolvedViewerToken = viewerToken ?? readRoomViewerToken(roomCode);
        if (!resolvedViewerToken) {
          return;
        }

        const data = await requestJson<BattleRoomState>(`/api/rooms/${roomCode}/action`, {
          method: "POST",
          headers: buildViewerTokenHeaders(resolvedViewerToken, {
            "Content-Type": "application/json",
          }),
          body: JSON.stringify({
            type: "heartbeat",
            latencyMs: Math.max(1, Math.round(performance.now() - startedAt)),
          }),
        });
        if (active) {
          setState(data);
          setViewerToken(data.viewerToken);
          writeRoomViewerToken(roomCode, data.viewerToken);
        }
      } catch {}
    }

    void heartbeat();
    timer = window.setInterval(() => {
      void heartbeat();
    }, 4000);

    return () => {
      active = false;
      if (timer !== null) {
        window.clearInterval(timer);
      }
    };
  }, [roomCode, roomStateReady, viewerToken, viewerTokenReady]);

  async function sendAction(
    body:
      | { type: "confirm-start" }
      | { type: "request-pause" }
      | { type: "respond-pause"; accept: boolean }
      | { type: "resume-now" }
      | { type: "surrender" }
      | { type: "request-rematch" }
      | { type: "guess"; characterId: string },
  ) {
    setBusy(true);
    setError(null);
    try {
      const data = await requestJson<BattleRoomState>(`/api/rooms/${roomCode}/action`, {
        method: "POST",
        headers: buildViewerTokenHeaders(viewerToken ?? readRoomViewerToken(roomCode), {
          "Content-Type": "application/json",
        }),
        body: JSON.stringify(body),
      });
      setState(data);
      setViewerToken(data.viewerToken);
      writeRoomViewerToken(roomCode, data.viewerToken);
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "这会儿还没法继续。");
    } finally {
      setBusy(false);
    }
  }

  async function copyInviteLink() {
    if (!state) return;
    const shareUrl = `${window.location.origin}/friend?code=${state.inviteCode}`;
    try {
      await navigator.clipboard.writeText(shareUrl);
    } catch {
      setError("这次没能复制邀请链接。");
    }
  }

  if (!state) {
    return (
      <main className="relative mx-auto flex min-h-screen w-full max-w-[1200px] flex-col px-4 py-4 sm:px-6 lg:px-8">
        <section className="uma-panel px-5 py-5 sm:px-7">
          {error ? (
            <div className="space-y-4">
              <div className="rounded-[22px] border border-red-200 bg-red-50 px-4 py-4 text-sm leading-7 text-red-700">
                <p className="font-semibold">这会儿还没能进到这一局。</p>
                <p className="mt-1">{error}</p>
              </div>
              <div className="flex flex-wrap gap-3">
                <Link href={`/friend?code=${encodeURIComponent(roomCode)}`} className="uma-button text-sm">
                  回到好友对战
                </Link>
                <Link href="/multi" className="uma-ghost-button text-sm">
                  回到多人对战
                </Link>
              </div>
            </div>
          ) : (
            <p className="text-sm text-[var(--color-muted)]">正在整理这一间房。</p>
          )}
        </section>
      </main>
    );
  }

  const pauseRequesterIsSelf =
    state.pauseRequest && state.pauseRequest.requesterSeat === state.viewerSeat;

  return (
    <main className="relative mx-auto flex min-h-screen w-full max-w-[1280px] flex-col px-4 py-4 sm:px-6 lg:px-8">
      <section className="uma-panel overflow-hidden px-5 py-5 sm:px-7">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <SiteBrand href="/" className="max-w-[620px]" showTitle={false} subtitle={buildSummaryText(state)} />
          <Link href="/" className="uma-ghost-button text-sm">
            返回主页
          </Link>
        </div>

        <div className="mt-5 grid gap-4 lg:grid-cols-[1.18fr_0.82fr]">
          <div className="space-y-4">
            <div>
              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
                {state.mode === "friend" ? "好友对战" : "多人对战"}
              </p>
              <h1 className="mt-2 font-[var(--font-display)] text-[2.3rem] font-bold leading-tight text-[var(--color-ink)]">
                {formatRoomStatus(state)}
              </h1>
            </div>

            <div className="rounded-[24px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-4 py-4 shadow-[var(--shadow-soft)] md:hidden">
              <div className="flex items-center justify-between gap-3 border-b border-[var(--color-line)] pb-3">
                <div>
                  <p className="text-xs font-semibold tracking-[0.08em] text-[var(--color-muted)]">房间号</p>
                  <p className="mt-1 text-base font-semibold text-[var(--color-ink)]">{state.inviteCode}</p>
                </div>
                <div className="text-right">
                  <p className="text-xs font-semibold tracking-[0.08em] text-[var(--color-muted)]">你的延迟</p>
                  <div className="mt-1">
                    <LatencyBadge latencyMs={state.self.latencyMs} compact />
                  </div>
                </div>
              </div>

              <div className="flex items-end justify-between gap-4 pt-3">
                <div>
                  <p className="text-xs font-semibold tracking-[0.08em] text-[var(--color-muted)]">当前状态</p>
                  <p className="mt-1 text-[2rem] font-[var(--font-display)] font-bold leading-none text-[var(--color-ink)]">
                    {formatRoomStatus(state)}
                  </p>
                </div>
                <div className="shrink-0 rounded-[22px] bg-[linear-gradient(180deg,#9fe13b,#79c61a)] px-4 py-3 text-center shadow-[0_14px_26px_rgba(116,194,22,0.18)]">
                  <p className="text-[11px] font-semibold tracking-[0.06em] text-[#3b6218]">剩余次数</p>
                  <p className="mt-1 text-[2.1rem] font-[var(--font-display)] font-bold leading-none text-[#244117]">
                    {state.self.remainingGuesses}
                  </p>
                </div>
              </div>
            </div>

            <div className="grid gap-3 md:grid-cols-2">
              <PlayerSeatCard
                title="你的状态"
                isSelf
                displayName={state.self.displayName}
                online={state.self.online}
                latencyMs={state.self.latencyMs}
                remainingGuesses={state.self.remainingGuesses}
                acceptedStart={state.self.acceptedStart}
                requestedRematch={state.self.requestedRematch}
                result={state.self.result}
              />
              <PlayerSeatCard
                title="对手状态"
                isSelf={false}
                displayName={state.opponent.displayName}
                online={state.opponent.online}
                latencyMs={state.opponent.latencyMs}
                remainingGuesses={state.opponent.remainingGuesses}
                acceptedStart={state.opponent.acceptedStart}
                requestedRematch={state.opponent.requestedRematch}
                result={state.opponent.result}
              />
            </div>

            <div className="hidden gap-3 sm:grid-cols-2 xl:grid-cols-4 md:grid">
              <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-4 py-4">
                <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-muted)]">
                  房间码
                </p>
                <p className="mt-2 text-lg font-semibold text-[var(--color-ink)]">{state.inviteCode}</p>
              </div>
              <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-4 py-4">
                <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-muted)]">
                  当前局数
                </p>
                <p className="mt-2 text-lg font-semibold text-[var(--color-ink)]">第 {state.roundNumber} 局</p>
              </div>
              <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-4 py-4">
                <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-muted)]">
                  你的延迟
                </p>
                <div className="mt-2">
                  <LatencyBadge latencyMs={state.self.latencyMs} />
                </div>
              </div>
              <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-4 py-4">
                <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-muted)]">
                  剩余次数
                </p>
                <p className="mt-2 text-lg font-semibold text-[var(--color-ink)]">{state.self.remainingGuesses}</p>
              </div>
            </div>

            {error ? (
              <p className="rounded-[20px] border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                {error}
              </p>
            ) : null}

            {state.mode === "friend" && state.status !== "playing" ? (
              <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.82)] px-4 py-4 text-sm text-[var(--color-ink)]">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <p>把房间码或邀请链接发给朋友，对方就能直接进房。</p>
                  <button type="button" onClick={copyInviteLink} className="uma-ghost-button text-sm">
                    复制邀请链接
                  </button>
                </div>
              </div>
            ) : null}

            {state.status === "playing" || state.status === "paused" || state.status === "finished" ? (
              <>
                <div className="sticky top-2 z-30">
                  <CharacterSearchInput
                    entries={availableSearchEntries}
                    onSelect={(characterId) => void sendAction({ type: "guess", characterId })}
                    disabled={!canGuess || busy}
                    disabledHint={
                      state.status === "paused"
                        ? "这局正在暂停中"
                        : state.status === "finished"
                          ? "这一局已经结束了"
                          : undefined
                    }
                  />
                </div>
                <GuessTable rows={state.self.guessRows} />
              </>
            ) : (
              <div className="rounded-[24px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-4 py-5 text-sm leading-7 text-[var(--color-muted)]">
                {state.status === "waiting"
                  ? "房间已经准备好了，等另一位玩家进来以后，就能一起开始。"
                  : state.status === "matched_pending_accept"
                    ? `已经找到对手了，请在 ${acceptanceCountdown ?? initialConfig.multiplayer.acceptConfirmSeconds} 秒内确认开始。`
                    : "两边都准备好以后，就能进入这一局。"}
              </div>
            )}

            <AnswerCard state={state} answerEntry={answerEntry} />
          </div>

          <div className="space-y-4">
            <div className="rounded-[24px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] p-4 shadow-[var(--shadow-soft)]">
              <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-brand-strong)]">
                房间操作
              </p>
              <div className="mt-4 grid gap-3">
                {["waiting", "ready_confirm", "matched_pending_accept"].includes(state.status) ? (
                  <button
                    type="button"
                    onClick={() => void sendAction({ type: "confirm-start" })}
                    disabled={busy}
                    className="inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[rgba(103,186,24,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-5 py-3 text-sm font-semibold text-[#244117] disabled:opacity-60"
                  >
                    {state.status === "matched_pending_accept" ? "确认开始" : "准备开始"}
                  </button>
                ) : null}

                {state.status === "playing" && !state.pauseRequest && !state.pauseUsed ? (
                  <button
                    type="button"
                    onClick={() => void sendAction({ type: "request-pause" })}
                    disabled={busy}
                    className="inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-5 py-3 text-sm font-semibold text-[var(--color-brand-blue-deep)]"
                  >
                    发起暂停
                  </button>
                ) : null}

                {state.pauseRequest ? (
                  <div className="rounded-[20px] border border-[var(--color-line)] bg-[rgba(248,251,255,0.9)] px-4 py-4 text-sm text-[var(--color-ink)]">
                    <p>
                      {pauseRequesterIsSelf
                        ? "你已经发起暂停，正在等对方点头。"
                        : "对手想先停一下，你要同意吗？"}
                    </p>
                    {!pauseRequesterIsSelf ? (
                      <div className="mt-3 grid gap-2 sm:grid-cols-2">
                        <button
                          type="button"
                          onClick={() => void sendAction({ type: "respond-pause", accept: true })}
                          className="inline-flex min-h-11 items-center justify-center rounded-[18px] border border-[rgba(103,186,24,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-4 py-2 text-sm font-semibold text-[#244117]"
                        >
                          同意暂停
                        </button>
                        <button
                          type="button"
                          onClick={() => void sendAction({ type: "respond-pause", accept: false })}
                          className="inline-flex min-h-11 items-center justify-center rounded-[18px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-4 py-2 text-sm font-semibold text-[var(--color-brand-blue-deep)]"
                        >
                          继续对战
                        </button>
                      </div>
                    ) : null}
                  </div>
                ) : null}

                {state.status === "paused" ? (
                  <button
                    type="button"
                    onClick={() => void sendAction({ type: "resume-now" })}
                    disabled={busy}
                    className="inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[rgba(103,186,24,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-5 py-3 text-sm font-semibold text-[#244117]"
                  >
                    继续这一局{pauseCountdown != null ? ` · ${pauseCountdown} 秒` : ""}
                  </button>
                ) : null}

                {(state.status === "playing" || state.status === "paused") ? (
                  <button
                    type="button"
                    onClick={() => void sendAction({ type: "surrender" })}
                    disabled={busy}
                    className="inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-5 py-3 text-sm font-semibold text-[var(--color-brand-blue-deep)]"
                  >
                    投降
                  </button>
                ) : null}

                {state.status === "finished" ? (
                  <>
                    <button
                      type="button"
                      onClick={() => void sendAction({ type: "request-rematch" })}
                      disabled={busy || state.self.requestedRematch}
                      className="inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[rgba(103,186,24,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-5 py-3 text-sm font-semibold text-[#244117] disabled:opacity-60"
                    >
                      {state.self.requestedRematch ? "正在等对方同意" : "新一局"}
                    </button>
                    <Link
                      href={state.mode === "friend" ? "/friend" : "/multi"}
                      className="inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-5 py-3 text-sm font-semibold text-[var(--color-brand-blue-deep)]"
                    >
                      离开房间
                    </Link>
                  </>
                ) : null}
              </div>
            </div>

            <OpponentShadowBoard state={state} />
          </div>
        </div>
      </section>

      <SiteFooter className="mt-5" />
    </main>
  );
}
