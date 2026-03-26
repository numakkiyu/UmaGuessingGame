type Props = {
  gameState: {
    status: "playing" | "won" | "lost";
    answerDisplayName?: string | null;
    guessRows: Array<unknown>;
  } | null;
  maxGuesses: number;
  onRestart: () => void;
  onShare: () => void;
  shareEnabled: boolean;
  shareNotice: string | null;
};

export function GameResultDialog({
  gameState,
  maxGuesses,
  onRestart,
  onShare,
  shareEnabled,
  shareNotice,
}: Props) {
  if (!gameState || gameState.status === "playing") {
    return null;
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(34,24,12,0.34)] px-4 backdrop-blur-[3px]">
      <div className="w-full max-w-md rounded-[30px] border border-[var(--color-line)] bg-[var(--color-panel-strong)] p-6 shadow-[var(--shadow-panel)]">
        <p className="font-[var(--font-display)] text-sm uppercase tracking-[0.3em] text-[var(--color-brand-strong)]">
          {gameState.status === "won" ? `第 ${gameState.guessRows.length} 猜命中` : "答案揭晓"}
        </p>
        <h2 className="mt-2 font-[var(--font-display)] text-3xl font-bold">
          {gameState.status === "won" ? "你猜中了" : "这局先到这里"}
        </h2>
        <p className="mt-3 text-sm leading-6 text-[var(--color-muted)]">
          {gameState.status === "won"
            ? `你用了 ${gameState.guessRows.length} / ${maxGuesses} 次机会找到答案。`
            : `你已经用完 ${gameState.guessRows.length} / ${maxGuesses} 次机会。`}
          答案是
          <span className="font-semibold text-[var(--color-ink)]">
            {" "}
            {gameState.answerDisplayName ?? "这位马娘"}
          </span>
          。
        </p>

        <div className={`mt-5 grid gap-3 ${shareEnabled ? "sm:grid-cols-2" : ""}`}>
          {shareEnabled ? (
            <button
              type="button"
              onClick={onShare}
              className="min-h-12 rounded-[20px] border border-[rgba(200,108,53,0.18)] bg-white px-4 py-3 text-sm font-semibold text-[var(--color-brand-strong)]"
            >
              分享这一局
            </button>
          ) : null}
          <button
            type="button"
            onClick={onRestart}
            className="min-h-12 rounded-[20px] bg-[var(--color-brand)] px-4 py-3 text-sm font-semibold text-white"
          >
            再来一局
          </button>
        </div>

        {shareNotice ? (
          <p className="mt-3 text-sm text-[var(--color-muted)]">{shareNotice}</p>
        ) : null}
      </div>
    </div>
  );
}
