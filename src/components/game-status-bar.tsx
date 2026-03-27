import { RefreshCcw } from "lucide-react";
import { LatencyBadge } from "@/components/latency-badge";

type Props = {
  gameState: {
    roomCode: string;
    status: "playing" | "won" | "lost" | "ended";
    remainingGuesses: number;
  } | null;
  canEdit: boolean;
  disabled: boolean;
  onStartNewGame: () => void;
  onEndCurrentGame: () => void;
  questionBankSize: number;
  spectatorCount?: number;
  latencyMs?: number | null;
};

export function GameStatusBar({
  gameState,
  canEdit,
  disabled,
  onStartNewGame,
  onEndCurrentGame,
  questionBankSize,
  spectatorCount = 0,
  latencyMs = null,
}: Props) {
  const statusLabel =
    gameState?.status === "playing"
      ? "正在寻找"
      : gameState?.status === "won"
        ? "已经找到了"
        : gameState?.status === "lost"
          ? "这一局结束了"
          : gameState?.status === "ended"
            ? "已结束"
            : "准备开猜";

  return (
    <div className="status-bar grid gap-2.5">
      <div className="status-bar-metrics grid gap-2.5 sm:grid-cols-3 lg:grid-cols-4">
        <div className="status-metric overflow-hidden rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.88)] px-4 py-3 shadow-[var(--shadow-soft)]">
          <div className="mb-3 h-1 rounded-full bg-[linear-gradient(90deg,var(--color-brand),rgba(151,216,28,0))]" />
          <p className="text-[11px] uppercase tracking-[0.18em] text-[var(--color-muted)]">
            剩余次数
          </p>
          <p className="mt-1 font-[var(--font-display)] text-[1.9rem] font-bold leading-none text-[var(--color-ink)]">
            {gameState?.remainingGuesses ?? "--"}
          </p>
        </div>

        <div className="status-metric overflow-hidden rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.88)] px-4 py-3 shadow-[var(--shadow-soft)]">
          <div className="mb-3 h-1 rounded-full bg-[linear-gradient(90deg,var(--color-brand-blue),rgba(63,136,247,0))]" />
          <p className="text-[11px] uppercase tracking-[0.18em] text-[var(--color-muted)]">
            当前状态
          </p>
          <p className="mt-1 text-base font-semibold text-[var(--color-ink)]">
            {statusLabel}
          </p>
        </div>

        <div className="status-metric overflow-hidden rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.88)] px-4 py-3 shadow-[var(--shadow-soft)] sm:col-auto">
          <div className="mb-3 h-1 rounded-full bg-[linear-gradient(90deg,var(--color-gold),rgba(243,199,84,0))]" />
          <p className="text-[11px] uppercase tracking-[0.18em] text-[var(--color-muted)]">
            房间码
          </p>
          <p className="mt-1 text-base font-semibold text-[var(--color-ink)]">
            {gameState?.roomCode ?? "--"}
          </p>
        </div>

        <div className="status-metric overflow-hidden rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.88)] px-4 py-3 shadow-[var(--shadow-soft)]">
          <div className="mb-3 h-1 rounded-full bg-[linear-gradient(90deg,var(--color-brand-blue-deep),rgba(37,95,203,0))]" />
          <p className="text-[11px] uppercase tracking-[0.18em] text-[var(--color-muted)]">
            {canEdit ? "围观情况" : "当前延迟"}
          </p>
          {canEdit ? (
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <p className="text-base font-semibold text-[var(--color-ink)]">
                {spectatorCount} 人在看
              </p>
              <LatencyBadge latencyMs={latencyMs} compact />
            </div>
          ) : (
            <div className="mt-2">
              <LatencyBadge latencyMs={latencyMs} />
            </div>
          )}
        </div>
      </div>

      <div className="status-bar-actions grid gap-2.5 sm:grid-cols-2">
        {!canEdit ? (
          <div className="status-action-secondary inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-5 py-3 text-sm font-semibold text-[var(--color-ink)] shadow-[var(--shadow-soft)]">
            {gameState?.status === "ended" ? "这一局已结束" : "这是观战链接"}
          </div>
        ) : null}

        {canEdit && gameState?.status !== "ended" ? (
          <button
            type="button"
            onClick={onStartNewGame}
            disabled={disabled || questionBankSize === 0}
            className="status-action-primary inline-flex min-h-12 items-center justify-center gap-2 rounded-[20px] border border-[rgba(103,186,24,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-5 py-3 text-sm font-semibold text-[#244117] shadow-[0_16px_28px_rgba(116,194,22,0.18)] transition hover:brightness-[1.03] disabled:cursor-not-allowed disabled:opacity-60"
          >
            <RefreshCcw className="size-4" />
            换一局试试
          </button>
        ) : null}

        {canEdit && gameState?.status !== "ended" ? (
          <button
            type="button"
            onClick={onEndCurrentGame}
            disabled={disabled || !gameState || gameState.status !== "playing"}
            className="status-action-secondary inline-flex min-h-12 items-center justify-center rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.86)] px-5 py-3 text-sm font-semibold text-[var(--color-brand-blue-deep)] shadow-[var(--shadow-soft)] transition hover:bg-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            结束这一局
          </button>
        ) : null}
      </div>
    </div>
  );
}
