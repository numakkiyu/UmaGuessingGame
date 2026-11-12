import "@/i18n/memory-online";
import { useTranslation } from "@/i18n";
import { characterName } from "@/i18n/entities";
import { useCharacters } from "@/lib/api/queries";
import { TeamRoundControl } from "@/features/rooms/team-round-control";
import { MemoryCardButton } from "./memory-card";
/* 记忆对局页：隐藏牌不带图片路径或答案文本，配对由服务端判定；每次提交后刷新版本再继续 */
import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import { Loader2, RotateCcw } from "lucide-react";
import { ConnectionBar } from "@/components/uma/connection-bar";
import { EmptyView, ErrorView, ForbiddenView, LoadingBlock } from "@/components/uma/page-state";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useGameCommand } from "@/lib/hooks/use-game-command";
import { validMemoryFlip, type MemoryFlipCommand } from "@/lib/api/game-commands";
import { useCountdown } from "@/lib/hooks/use-countdown";
import { useApi } from "@/lib/api";
import { RejoinPrompt, isCredentialRefreshFailure } from "@/features/modes/rejoin-prompt";
import { connectionDetail } from "@/lib/copy";
import {
  ApiError,
  describeFailure,
  localizeFailure,
  type Failure,
} from "@/lib/api/client";
import {
  displayScore,
  isViewingPhase,
  normalizeMemoryRound,
  type MemoryRound,
} from "@/lib/api/memory";
import { useRoom } from "@/lib/api/room-queries";
import {
  isSubmittablePhase,
  isTerminalPhase,
  phaseLabel,
  publicStateOf,
  useRoundSnapshot,
} from "@/lib/api/round-snapshot";
import type { components } from "@/lib/api/types";
import { useRealtime } from "@/lib/realtime/use-realtime";
import { cn } from "@/lib/utils";

/** 进度条：已配对/8 */
function PairProgress({ round }: { round: MemoryRound }) {
  const { t } = useTranslation("memoryOnline");
  const total = Math.max(1, round.cards.length / 2);
  const done = round.self?.matchedPairs ?? 0;
  const percent = Math.round((done / total) * 100);

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-sm font-semibold text-[var(--color-ink)]">{t("matched")}</span>
        <span className="uma-code text-base font-bold text-[var(--color-ink)]">
          {done} / {total}
        </span>
      </div>
      <div
        className="h-2.5 w-full overflow-hidden rounded-full bg-[var(--color-line)]"
        role="progressbar"
        aria-valuenow={done}
        aria-valuemin={0}
        aria-valuemax={total}
        aria-label={t("progress")}
      >
        <div
          className="h-full rounded-full bg-[var(--color-action-green)] transition-[width] duration-[var(--duration-standard)] ease-[var(--ease-primary)]"
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
}

export function MemoryRoundPage() {
  const { t } = useTranslation("memoryOnline");
  const { roomId } = useParams<{ roomId: string }>();
  const { api, session } = useApi();

  const roomQuery = useRoom(roomId ?? null);
  const roundId = roomQuery.data?.room.currentRound?.roundId ?? null;
  const snapshot = useRoundSnapshot(roundId);

  const round = useMemo(() => {
    const publicState = publicStateOf(snapshot.raw);
    if (!publicState) return null;
    // normalizeMemoryRound 需要 RoundView 形状；这里用 publicState 直接喂一个最小视图
    return normalizeMemoryRound({ publicState } as unknown as components["schemas"]["RoundView"]);
  }, [snapshot.raw]);

  const [failure, setFailure] = useState<Failure | null>(null);
  const [rejected, setRejected] = useState(false);
  const info = failure ? rejected ? { ...failure, title: t("flipRejected"), detail: t("notAccepted") } : localizeFailure(failure) : null;
  const [needsRejoin, setNeedsRejoin] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const command = useGameCommand(
    session && roundId ? `${session.sessionId}:${roundId}:memory` : null,
    validMemoryFlip,
    (body, key) => api.createSubmission(roundId!, body, key),
    snapshot.refreshAfterSubmit,
  );
  const phase = { kind: command.busy || refreshing ? "submitting" : command.pending ? "confirming" : "idle" };

  const realtime = useRealtime({
    roomId: roomId ?? null,
    enabled: Boolean(roomId),
    handlers: {
      onSnapshotNeeded: () => {
        void snapshot.refetch();
        void roomQuery.refetch();
      },
    },
  });

  const currentPhase = snapshot.meta?.phase ?? null;
  const ended = isTerminalPhase(currentPhase) || (round?.finished ?? false);
  const submittable = isSubmittablePhase(currentPhase) && !(snapshot.meta?.paused ?? false);
  const viewing = round !== null && isViewingPhase(round, currentPhase);

  // Refreshing preserves the shared viewing deadline; it never grants extra time.
  const viewingCountdown = useCountdown(snapshot.meta?.deadlineAt, viewing && !(snapshot.meta?.paused ?? false));
  // 作答倒计时：用服务端 deadlineAt（唯一权威时间源）
  const answerCountdown = useCountdown(snapshot.meta?.deadlineAt, submittable && !viewing);

  // One bounded refresh turns a confirmed mismatch back for every teammate.
  // The deadline comes from the response, so refresh cannot extend the reveal.
  const revealUntil = round?.self?.revealUntil;
  const refreshBoard = snapshot.refetch;
  useEffect(() => {
    if (!revealUntil) return;
    const serverTime = snapshot.serverTime ? Date.parse(snapshot.serverTime) : Date.now();
    const timer = window.setTimeout(() => void refreshBoard(), Math.max(0, revealUntil - serverTime) + 30);
    return () => window.clearTimeout(timer);
  }, [revealUntil, snapshot.serverTime, refreshBoard]);

  async function submitFlip(body?: MemoryFlipCommand) {
    if (refreshing) return;
    setFailure(null); setRejected(false); setNotice(null); setRefreshing(true);
    try {
      const result = await command.run(body);
      if (!result) return;
      if (!result.ok) {
        setFailure(describeFailure(result.error));
        if (isCredentialRefreshFailure(result.error)) setNeedsRejoin(true);
        if (!result.uncertain && result.error instanceof ApiError && result.error.isConflict) await snapshot.refreshAfterSubmit();
        return;
      }
      const response = result.value;
      if (response.data?.outcome !== "accepted") {
        setRejected(true);
        setFailure({ kind: "error", title: "", detail: "", diagnosticId: response.requestId, retryable: true });
        return;
      }
      const feedback = response.data.feedback as Record<string, unknown> | undefined;
      if (feedback?.["resolved"] === true) {
        setNotice(feedback["matched"] === true ? "matchedNotice" : "mismatchNotice");
      }
    } finally { setRefreshing(false); }
  }

  function handleFlip(cardId: string) {
    if (!roundId || !round || phase.kind !== "idle" || command.expired || snapshot.isError || !submittable || snapshot.canSubmit === false || round.self?.revealUntil || round.self?.finished) return;
    if (round.cards.find(card => card.cardId === cardId)?.state !== "hidden") return;
    void submitFlip({
      phaseVersion: snapshot.meta?.phaseVersion ?? 1,
      submissionRevision: snapshot.submissionRevision,
      controlGeneration: snapshot.controlGeneration,
      modeAction: { kind: "memory.flip", cardId },
    });
  }

  if (!roomId) {
    return (
      <ErrorView
        failure={{
          kind: "error",
          title: t("missingTitle"),
          detail: t("missingDetail"),
          diagnosticId: null,
          retryable: false,
        }}
      />
    );
  }

  if (roomQuery.isLoading) return <LoadingBlock label={t("loadingRoom")} rows={3} />;

  if (roomQuery.isError) {
    const info = describeFailure(roomQuery.error);
    return info.kind === "forbidden" ? (
      <ForbiddenView failure={info} onRetry={() => void roomQuery.refetch()} />
    ) : (
      <ErrorView failure={info} onRetry={() => void roomQuery.refetch()} />
    );
  }

  const connectionStatus =
    realtime.status === "online"
      ? "online"
      : realtime.status === "reconnecting" || realtime.status === "connecting"
        ? "reconnecting"
        : realtime.status === "stale"
          ? "stale"
          : "offline";

  const busy = phase.kind !== "idle" || command.expired || snapshot.isError || snapshot.canSubmit === false || Boolean(revealUntil);
  const attempts = round?.self?.attempts ?? 0;
  const maxAttempts = round?.self?.maxAttempts ?? round?.maxAttempts ?? 20;
  const wrongAttempts = round?.self?.wrongAttempts ?? 0;
  const score = displayScore(round?.self?.scoreUnits ?? 0);

  return (
    <div className="uma-stack">
      <TeamRoundControl room={roomQuery.data?.room} raw={snapshot.raw} onChanged={snapshot.refetch}/>
      <ConnectionBar
        status={connectionStatus}
        detail={connectionDetail(roomQuery.data?.room.name ?? t("room"), realtime.transport)}
      />

      {/* 状态头：阶段 + 倒计时 + 尝试数，都在首屏 */}
      <section className="uma-panel p-4" aria-labelledby="memory-heading">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <h1 id="memory-heading" className="u-fluid-task text-[var(--color-ink)]">
              {viewing
                ? t("viewingTitle")
                : ended
                  ? t("ended")
                  : (round?.self?.matchedPairs ?? 0) > 0
                    ? t("nextPair")
                    : t("playingTitle")}
            </h1>
            <p className="mt-1 break-words text-sm text-[var(--color-muted)]">
              {phaseLabel(currentPhase)}
              {snapshot.meta?.paused ? ` · ${t("paused")}` : ""}
            </p>
          </div>

          <div className="flex min-w-0 flex-col items-end">
            <span className="text-[length:var(--text-aux)] text-[var(--color-muted)]">
              {t(viewing ? "viewRemaining" : "answerRemaining")}
            </span>
            <span
              className={cn(
                "uma-code text-2xl font-bold tabular-nums",
                (viewing ? viewingCountdown.expired : answerCountdown.expired)
                  ? "text-[var(--color-danger)]"
                  : "text-[var(--color-ink)]",
              )}
              // 每秒更新但不打断读屏
              aria-live="off"
            >
              {viewing ? viewingCountdown.text : answerCountdown.text}
            </span>
          </div>
        </div>

        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          <PairProgress round={round ?? { cards: [], self: null } as unknown as MemoryRound} />
          <dl className="grid grid-cols-2 gap-2">
            <div className="rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] px-3 py-2">
              <dt className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{t("attempts")}</dt>
              <dd className="uma-code text-base font-bold text-[var(--color-ink)]">
                {attempts} / {maxAttempts}
              </dd>
            </div>
            <div className="rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] px-3 py-2">
              <dt className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{t("wrong")}</dt>
              <dd className="uma-code text-base font-bold text-[var(--color-ink)]">{wrongAttempts}</dd>
            </div>
          </dl>
        </div>
      </section>

      {viewing ? (
        <Alert tone="warning" title={t("remember")}>
          <p>
            {t("rememberDetail", { time: viewingCountdown.text })}
          </p>
        </Alert>
      ) : null}

      {command.pending && !command.busy ? <Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button variant="secondary" disabled={refreshing} onClick={() => void submitFlip()}>{t("confirmAction")}</Button></Alert> : null}
      {command.expired ? <Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" onClick={() => { void snapshot.refetch().then(command.acknowledgeExpired); }}>{t("latest")}</Button></Alert> : null}
      {command.storageLimited ? <p className="text-sm text-[var(--color-muted)]">{t("limited")}</p> : null}

      {!roundId ? (
        <EmptyView title={t("notStarted")} detail={t("notStartedDetail")} />
      ) : null}

      {roundId && snapshot.isLoading ? <LoadingBlock label={t("loadingRound")} rows={3} /> : null}

      {roundId && snapshot.isError ? (
        (() => {
          const info = describeFailure(snapshot.error);
          return info.kind === "forbidden" ? (
            <ForbiddenView failure={info} onRetry={() => void snapshot.refetch()} />
          ) : (
            <ErrorView failure={info} onRetry={() => void snapshot.refetch()} />
          );
        })()
      ) : null}

      {round && round.cards.length > 0 ? (
        <div className="grid gap-4 lg:grid-cols-[minmax(0,32rem)_minmax(0,18rem)] lg:items-start lg:justify-center">
          <section className="uma-panel min-w-0 p-4" aria-labelledby="board-heading">
            <div className="uma-section-head">
              <h2 id="board-heading" className="text-base font-bold text-[var(--color-ink)]">
                {t("board")}
              </h2>
              <p className="text-[length:var(--text-aux)] text-[var(--color-muted)]">
                {t(viewing ? "allVisible" : "turnOver")}
              </p>
            </div>

            {/*
              4×4 网格。手机 320 宽下每格约 60px（>44px 触摸区要求）；
              不显示文字，只有图 —— 60px 放不下可读的中文名。
            */}
            <div
              className="mx-auto mt-3 grid w-full max-w-[32rem] grid-cols-4 gap-2"
              role="group"
              aria-label={t("grid")}
            >
              {round.cards.map((card, index) => (
                <MemoryCardButton
                  key={card.cardId}
                  card={card}
                  index={index}
                  disabled={snapshot.canSubmit === false || busy || ended || viewing || !submittable}
                  onFlip={(cardId) => void handleFlip(cardId)}
                />
              ))}
            </div>

            {phase.kind === "submitting" ? (
              <p className="mt-2 flex items-center gap-1.5 text-sm text-[var(--color-muted)]" role="status">
                <Loader2 aria-hidden="true" className="size-3.5 motion-safe:animate-spin" />
                {t("confirming")}
              </p>
            ) : null}
          </section>

          <aside className="uma-stack uma-stack--tight min-w-0">
            {notice ? (
              <Alert tone="info" title={t("notice")}>
                <p>{t(notice)}</p>
              </Alert>
            ) : null}

            {needsRejoin ? <RejoinPrompt roomId={roomId} /> : null}

            {info && !command.pending ? (
              <Alert
                tone={info.kind === "timeout" ? "warning" : "danger"}
                title={info.title}
                action={!command.pending ? <Button variant="secondary" onClick={() => void snapshot.refetch()}>{t("refresh")}</Button> : undefined}
              >
                <p>{info.detail}</p>
                {info.diagnosticId ? (
                  <p className="mt-1 text-[length:var(--text-aux)]">
                    {t("problemId")}：<span className="uma-code">{info.diagnosticId}</span>
                  </p>
                ) : null}
              </Alert>
            ) : null}

            <section className="uma-card p-4">
              <h2 className="text-base font-bold text-[var(--color-ink)]">{t("scoring")}</h2>
              <ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]">
                <li>{t("pairScore")}</li>
                <li>{t("wrongScore")}</li>
                <li>{t("attemptRule")}</li>
                <li>{t("currentScore", { score })}</li>
              </ul>
            </section>

            {ended ? (
              <Button asChild variant="secondary" block>
                <Link to="/play/memory">{t("again")}</Link>
              </Button>
            ) : (
              <Button
                variant="secondary"
                block
                onClick={() => void snapshot.refetch()}
                disabled={phase.kind !== "idle"}
              >
                <RotateCcw aria-hidden="true" className="size-4" />
                {t("refresh")}
              </Button>
            )}
          </aside>
        </div>
      ) : null}

      {ended && round ? <MemoryResult round={round} /> : null}
    </div>
  );
}

/** 结果面板：揭晓全部配对（服务端 answer 里才给出真名与完整配对） */
function MemoryResult({ round }: { round: MemoryRound }) {
  const { t } = useTranslation("memoryOnline");
  // Fetch public display metadata only after the server has revealed the pairs.
  // Ambiguous or absent names keep their source spelling; never infer from media.
  const characters = useCharacters();
  function pairName(label: string) {
    const matches = characters.data?.items.filter(c => c.displayName === label);
    return matches?.length === 1 ? characterName(matches[0]!) : label || t("unnamed");
  }
  const total = Math.max(1, round.cards.length / 2);
  const done = round.self?.matchedPairs ?? 0;
  const score = displayScore(round.self?.scoreUnits ?? 0);

  return (
    <section id="memory-result" className="uma-panel p-5" tabIndex={-1} aria-labelledby="memory-result-heading">
      <h2 id="memory-result-heading" className="u-fluid-task text-[var(--color-ink)]">
        {t(done >= total ? "complete" : "ended")}
      </h2>
      {done < total ? (
        <p className="mt-1 break-words text-sm text-[var(--color-muted)]">
          {t("resultHint", { done, total })}
        </p>
      ) : null}

      <dl className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <div className="rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3">
          <dt className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{t("score")}</dt>
          <dd className="uma-code text-xl font-bold text-[var(--color-ink)]">{score}</dd>
        </div>
        <div className="rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3">
          <dt className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{t("pairs")}</dt>
          <dd className="uma-code text-xl font-bold text-[var(--color-ink)]">
            {done}/{total}
          </dd>
        </div>
        <div className="rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3">
          <dt className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{t("attempts")}</dt>
          <dd className="uma-code text-xl font-bold text-[var(--color-ink)]">
            {round.self?.attempts ?? 0}/{round.self?.maxAttempts ?? round.maxAttempts}
          </dd>
        </div>
        <div className="rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3">
          <dt className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{t("wrong")}</dt>
          <dd className="uma-code text-xl font-bold text-[var(--color-ink)]">{round.self?.wrongAttempts ?? 0}</dd>
        </div>
      </dl>

      {round.revealedPairs.length > 0 ? (
        <div className="mt-4">
          <h3 className="text-sm font-bold text-[var(--color-ink)]">{t("answerPairs")}</h3>
          <ul className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
            {round.revealedPairs.map((pair, index) => (
              // 使用索引作为 key，避免将配对标识放进 DOM 属性
              <li
                key={index}
                className="flex items-center gap-2 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel)] p-2"
              >
                {pair.cards.map((card, cardIndex) => (
                  <img
                    key={cardIndex}
                    src={card.imagePath}
                    alt=""
                    aria-hidden="true"
                    className="size-10 shrink-0 object-contain"
                    loading="lazy"
                    decoding="async"
                  />
                ))}
                <span className="min-w-0 break-words text-sm text-[var(--color-ink)]">{pairName(pair.label)}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <div className="mt-4 flex flex-col gap-2 sm:flex-row">
        <Button asChild variant="primary" size="primary">
          <Link to="/play/memory">{t("again")}</Link>
        </Button>
        <Button asChild variant="secondary" size="primary">
          <Link to="/">{t("home")}</Link>
        </Button>
      </div>
    </section>
  );
}

/** 提交中的加载指示（保留导出，供 room-entry 复用同一语义） */
export function MemorySubmittingHint() {
  const { t } = useTranslation("memoryOnline");
  return (
    <p className="flex items-center gap-2 text-sm text-[var(--color-muted)]" role="status">
      <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin" />
      {t("confirming")}
    </p>
  );
}
