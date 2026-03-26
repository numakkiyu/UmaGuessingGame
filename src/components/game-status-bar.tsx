import { RefreshCcw } from "lucide-react";

type Props = {
  gameState: {
    status: "playing" | "won" | "lost";
    remainingGuesses: number;
  } | null;
  disabled: boolean;
  onStartNewGame: () => void;
  questionBankSize: number;
  maxGuesses: number;
};

export function GameStatusBar({
  gameState,
  disabled,
  onStartNewGame,
  questionBankSize,
  maxGuesses,
}: Props) {
  const statusLabel =
    gameState?.status === "playing"
      ? "还在进行"
      : gameState?.status === "won"
        ? "已经猜中"
        : gameState?.status === "lost"
          ? "本局结束"
          : "等你开猜";

  return (
    <div className="grid gap-3">
      <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.78)] px-4 py-3">
          <p className="text-xs uppercase tracking-[0.18em] text-[var(--color-muted)]">
            剩余次数
          </p>
          <p className="mt-2 font-[var(--font-display)] text-3xl font-bold text-[var(--color-ink)]">
            {gameState?.remainingGuesses ?? "--"}
          </p>
        </div>

        <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.78)] px-4 py-3">
          <p className="text-xs uppercase tracking-[0.18em] text-[var(--color-muted)]">
            当前状态
          </p>
          <p className="mt-2 text-lg font-semibold text-[var(--color-ink)]">
            {statusLabel}
          </p>
        </div>

        <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.78)] px-4 py-3">
          <p className="text-xs uppercase tracking-[0.18em] text-[var(--color-muted)]">
            开局信息
          </p>
          <p className="mt-2 text-lg font-semibold text-[var(--color-ink)]">
            {questionBankSize} 位 / {maxGuesses} 次
          </p>
        </div>
      </div>

      <button
        type="button"
        onClick={onStartNewGame}
        disabled={disabled || questionBankSize === 0}
        className="inline-flex min-h-12 items-center justify-center gap-2 rounded-[22px] bg-[var(--color-brand)] px-5 py-3 text-sm font-semibold text-white transition hover:bg-[var(--color-brand-strong)] disabled:cursor-not-allowed disabled:opacity-60"
      >
        <RefreshCcw className="size-4" />
        {questionBankSize === 0 ? "题库整理中" : "开始新一局"}
      </button>
    </div>
  );
}
