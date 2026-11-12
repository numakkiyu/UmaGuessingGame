// Matching is server-owned; background tabs pause status reads and refresh on return
import "@/i18n/matchmaking";
import { useTranslation } from "@/i18n";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { CircleAlert, Loader2, Radio, Users, X } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingBlock } from "@/components/uma/page-state";
import { useApi } from "@/lib/api";
import { createIdempotencyKey, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import { cn } from "@/lib/utils";

const TICKET_STATUSES = ["queueing", "matched-pending-accept", "ready", "playing", "finished", "cancelled", "expired"];

const QUEUE_POLL_MS = 3000;

type Ticket = {
  ticketId: string;
  status: string;
  roomId: string | null;
  expiresAt: string;
  serverTime: string;
};

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}

function parseTicket(body: unknown): Ticket | null {
  const record = asRecord(body);
  if (!record) return null;
  const ticketId = typeof record["ticketId"] === "string" ? record["ticketId"] : null;
  if (!ticketId) return null;
  return {
    ticketId,
    status: typeof record["status"] === "string" ? record["status"] : "queueing",
    roomId: typeof record["roomId"] === "string" ? record["roomId"] : null,
    expiresAt: typeof record["expiresAt"] === "string" ? record["expiresAt"] : "",
    serverTime: typeof record["serverTime"] === "string" ? record["serverTime"] : "",
  };
}

type PageState =
  | { kind: "idle" }
  | { kind: "creating" }
  | { kind: "queued"; ticket: Ticket }
  | { kind: "error"; failure: Failure; unreadable?: true };

export function MatchmakingPage() {
  const { t } = useTranslation("matchmaking");
  const { api, session, ensureSession } = useApi();
  const navigate = useNavigate();

  const [displayName, setDisplayName] = useState(session?.displayName ?? "");
  const [wrongBudget, setWrongBudget] = useState(8);
  const [state, setState] = useState<PageState>({ kind: "idle" });
  const [respondFailure, setRespondFailure] = useState<Failure | null>(null);
  const [responding, setResponding] = useState(false);
  const [cancelling, setCancelling] = useState(false);

  /** 幂等键：同一张票据的创建只发一次，重试复用 */
  const createKeyRef = useRef<string | null>(null);

  const startQueue = useCallback(async () => {
    if (state.kind === "creating") return;
    setState({ kind: "creating" });
    setRespondFailure(null);

    if (!createKeyRef.current) createKeyRef.current = createIdempotencyKey();

    try {
      const name = displayName.trim() || t("player");
      await ensureSession(name);
      const response = await api.createMatchmakingTicket(
        {
          // 契约要求 ClassicConfig：字段必须齐全，多传会被拒
          modeConfig: {
            modeId: "classic",
            maxWrongGuesses: wrongBudget,
            winPolicy: "first-correct",
            durationSeconds: 0,
            shadowEnabled: false,
          },
        },
        createKeyRef.current,
      );
      const ticket = parseTicket(response.data);
      if (!ticket) {
        setState({
          kind: "error",
          unreadable: true,
          failure: {
            kind: "error",
            title: "",
            detail: "",
            diagnosticId: response.requestId,
            retryable: true,
          },
        });
        return;
      }
      // 新票据需要新的幂等键
      createKeyRef.current = null;
      setState({ kind: "queued", ticket });
    } catch (error) {
      setState({ kind: "error", failure: describeFailure(error) });
    }
  }, [api, displayName, ensureSession, state.kind, wrongBudget, t]);

  const refresh = useCallback(async () => {
    if (state.kind !== "queued") return;
    const ticketId = state.ticket.ticketId;
    try {
      const response = await api.getMatchmakingTicket(ticketId);
      const ticket = parseTicket(response.data);
      if (ticket) setState({ kind: "queued", ticket });
    } catch (error) {
      // 票据消失（过期/被清理）：回到可重排状态，但不谎称匹配成功
      const failure = describeFailure(error);
      if (failure.kind === "not-found") {
        setState({ kind: "idle" });
        return;
      }
      setState({ kind: "error", failure });
    }
  }, [api, state]);

  /*
   * 排队中定时刷新状态。
   *
   * 只在 queueing / matched-pending-accept 时轮询：
   * ready 且 roomId 为空只代表本人已确认，仍需等待对手确认后读取房间。
   */
  const shouldPoll =
    state.kind === "queued" &&
    (state.ticket.status === "queueing" || state.ticket.status === "matched-pending-accept" ||
      (state.ticket.status === "ready" && !state.ticket.roomId));

  useEffect(() => {
    if (!shouldPoll) return;
    const timer = window.setInterval(() => {
      // 后台标签页不轮询
      if (document.visibilityState === "visible") void refresh();
    }, QUEUE_POLL_MS);

    const onVisible = () => {
      if (document.visibilityState === "visible") void refresh();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [refresh, shouldPoll]);

  /* 房间就绪后自动进入 */
  useEffect(() => {
    if (state.kind !== "queued") return;
    if (state.ticket.status === "ready" && state.ticket.roomId) {
      navigate(`/rooms/${encodeURIComponent(state.ticket.roomId)}`);
    }
  }, [navigate, state]);

  const respond = useCallback(
    async (accept: boolean) => {
      if (state.kind !== "queued") return;
      setResponding(true);
      setRespondFailure(null);
      try {
        const response = await api.respondToMatch(state.ticket.ticketId, { accept });
        const ticket = parseTicket(response.data);
        if (ticket) setState({ kind: "queued", ticket });
        else await refresh();
      } catch (error) {
        setRespondFailure(describeFailure(error));
        await refresh();
      } finally {
        setResponding(false);
      }
    },
    [api, refresh, state],
  );

  const cancel = useCallback(async () => {
    if (state.kind !== "queued") return;
    setCancelling(true);
    try {
      await api.cancelMatchmakingTicket(state.ticket.ticketId);
      setState({ kind: "idle" });
    } catch (error) {
      const failure = describeFailure(error);
      // 已经不在队列里也算成功取消，不报错吓人
      if (failure.kind === "not-found") setState({ kind: "idle" });
      else setState({ kind: "error", failure });
    } finally {
      setCancelling(false);
    }
  }, [api, state]);

  const respondInfo = respondFailure ? localizeFailure(respondFailure) : null;
  if (state.kind === "creating") {
    return (
      <div className="uma-prose mx-auto flex flex-col gap-4">
        <h1 className="u-fluid-title text-[var(--color-ink)]">{t("title")}</h1>
        <LoadingBlock label={t("entering")} rows={2} />
        <p className="text-sm text-[var(--color-muted)]">{t("enteringDetail")}</p>
      </div>
    );
  }

  if (state.kind === "error") {
    const info = state.unreadable ? { ...state.failure, title: t("unreadableTitle"), detail: t("unreadableDetail") } : localizeFailure(state.failure);
    return (
      <div className="uma-prose mx-auto flex flex-col gap-4">
        <h1 className="u-fluid-title text-[var(--color-ink)]">{t("title")}</h1>
        <Alert
          tone={info.kind === "offline" ? "warning" : "danger"}
          title={info.title}
          action={
            <Button variant="secondary" onClick={() => void startQueue()}>
              {t("retry")}
            </Button>
          }
        >
          <p>{info.detail}</p>
          {info.diagnosticId ? (
            <p className="mt-1 text-[length:var(--text-aux)]">
              {t("problemId")}：<span className="uma-code">{info.diagnosticId}</span>
            </p>
          ) : null}
        </Alert>
        <Button asChild variant="secondary" size="primary">
          <Link to="/">{t("home")}</Link>
        </Button>
      </div>
    );
  }

  if (state.kind === "queued") {
    const knownStatus = TICKET_STATUSES.includes(state.ticket.status);
    const awaitingOpponent = state.ticket.status === "ready" && !state.ticket.roomId;
    const status = { label: t(awaitingOpponent ? "acceptedLabel" : knownStatus ? `statuses.${state.ticket.status}.label` : "unknownLabel"), hint: t(awaitingOpponent ? "acceptedHint" : knownStatus ? `statuses.${state.ticket.status}.hint` : "unknownHint") };
    const needsAccept = state.ticket.status === "matched-pending-accept";
    const terminal = ["cancelled", "expired", "finished"].includes(state.ticket.status);

    return (
      <div className="uma-prose mx-auto flex flex-col gap-5">
        <header>
          <h1 className="u-fluid-title text-[var(--color-ink)]">{t("title")}</h1>
        </header>

        <section
          className={cn(
            "uma-panel flex flex-col gap-3 p-5",
            needsAccept && "border-[var(--color-gold)] ring-2 ring-[var(--color-gold)]",
          )}
          aria-labelledby="queue-status"
        >
          <div className="flex items-start gap-3">
            {needsAccept ? (
              <CircleAlert aria-hidden="true" className="mt-0.5 size-6 shrink-0 text-[var(--color-near-edge)]" />
            ) : (
              <Radio
                aria-hidden="true"
                className={cn(
                  "mt-0.5 size-6 shrink-0 text-[var(--color-blue-deep)]",
                  shouldPoll && "motion-safe:animate-pulse",
                )}
              />
            )}
            <div className="min-w-0">
              <h2 id="queue-status" className="text-lg font-bold text-[var(--color-ink)]">
                {status.label}
              </h2>
              <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{status.hint}</p>
            </div>
          </div>

          {needsAccept ? (
            <div className="flex flex-col gap-2 sm:flex-row">
              <Button
                variant="primary"
                size="primary"
                onClick={() => void respond(true)}
                disabled={responding}
                className="h-auto min-h-12 whitespace-normal sm:flex-1"
              >
                {responding ? (
                  <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin" />
                ) : (
                  <Users aria-hidden="true" className="size-4" />
                )}
                {t("accept")}
              </Button>
              <Button
                variant="secondary"
                size="primary"
                onClick={() => void respond(false)}
                disabled={responding}
                className="h-auto min-h-12 whitespace-normal sm:flex-1"
              >
                {t("decline")}
              </Button>
            </div>
          ) : null}

          {respondInfo ? (
            <Alert tone="warning" title={respondInfo.title}>
              <p>{respondInfo.detail}</p>
              {respondInfo.diagnosticId ? (
                <p className="mt-1 text-[length:var(--text-aux)]">
                  {t("problemId")}：<span className="uma-code">{respondInfo.diagnosticId}</span>
                </p>
              ) : null}
            </Alert>
          ) : null}

          {state.ticket.status === "ready" && state.ticket.roomId ? (
            <p className="text-sm font-semibold text-[var(--color-action-green)]">
              {t("joining")}
            </p>
          ) : null}

          {shouldPoll ? (
            <div className="flex flex-col gap-1.5">
              <p className="flex items-center gap-1.5 text-[length:var(--text-aux)] text-[var(--color-muted)]">
                <Loader2 aria-hidden="true" className="size-3.5 motion-safe:animate-spin" />
                {t("waiting")}
              </p>
              {/* Pairing is implemented by the server. Keep alternatives helpful
                  without claiming the active matcher is unavailable. */}
              <p className="text-[length:var(--text-aux)] leading-5 text-[var(--color-muted)]">
                {t("waitingHint")}
              </p>
            </div>
          ) : null}

          {!terminal ? (
            <Button variant="secondary" onClick={() => void cancel()} disabled={cancelling} block>
              <X aria-hidden="true" className="size-4" />
              {t(cancelling ? "cancelling" : "cancel")}
            </Button>
          ) : null}

          {terminal ? (
            <Button variant="primary" size="primary" onClick={() => void startQueue()} block>
              {t("again")}
            </Button>
          ) : null}
        </section>

        <div className="flex flex-col gap-2 border-t border-[var(--color-line)] pt-4 sm:flex-row">
          <Button asChild variant="secondary" size="primary">
            <Link to="/play/classic">{t("solo")}</Link>
          </Button>
          <Button asChild variant="secondary" size="primary">
            <Link to="/">{t("home")}</Link>
          </Button>
        </div>
      </div>
    );
  }

  // idle：排队表单
  return (
    <div className="uma-prose mx-auto flex flex-col gap-5">
      <header>
        <h1 className="u-fluid-title text-[var(--color-ink)]">{t("title")}</h1>
        <p className="mt-2 text-base leading-7 text-[var(--color-ink)]">
          {t("intro")}
        </p>
      </header>

      <section className="uma-card p-4" aria-labelledby="match-rules">
        <h2 id="match-rules" className="text-base font-bold text-[var(--color-ink)]">
          {t("rules")}
        </h2>
        <ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]">
          <li>{t("wrongRule", { count: wrongBudget })}</li>
          <li>{t("timeRule")}</li>
          <li>{t("acceptRule")}</li>
          <li>{t("cancelRule")}</li>
        </ul>
      </section>

      <section className="uma-panel flex flex-col gap-4 p-5" aria-labelledby="queue-start">
        <h2 id="queue-start" className="text-base font-bold text-[var(--color-ink)]">
          {t("enter")}
        </h2>

        <div className="flex flex-col gap-2">
          <Label htmlFor="match-name">{t("name")}</Label>
          <Input
            id="match-name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            placeholder={t("namePlaceholder")}
            maxLength={24}
            autoComplete="nickname"
          />
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="match-budget">{t("budget")}</Label>
          <div className="flex items-center gap-3">
            <Input
              id="match-budget"
              type="number"
              min={4}
              max={12}
              value={wrongBudget}
              onChange={(event) => {
                const next = Number(event.target.value);
                if (Number.isFinite(next)) setWrongBudget(Math.min(12, Math.max(4, Math.round(next))));
              }}
              className="w-24"
            />
            <span className="text-sm text-[var(--color-muted)]">{t("budgetHint")}</span>
          </div>
        </div>

        <Button variant="primary" size="primary" onClick={() => void startQueue()} block>
          <Users aria-hidden="true" className="size-4" />
          {t("start")}
        </Button>

        <p className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{t("formHint")}</p>
      </section>

      <div className="flex flex-col gap-2 border-t border-[var(--color-line)] pt-4 sm:flex-row">
        <Button asChild variant="secondary" size="primary">
          <Link to="/play/classic">{t("solo")}</Link>
        </Button>
        <Button asChild variant="secondary" size="primary">
          <Link to="/">{t("home")}</Link>
        </Button>
      </div>
    </div>
  );
}
