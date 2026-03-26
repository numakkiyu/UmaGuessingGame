type Props = {
  config: {
    maxGuesses: number;
    featureFlags: {
      enableShare: boolean;
    };
  } | null;
  gameState: {
    status: "playing" | "won" | "lost";
    guessRows: Array<unknown>;
  } | null;
  onShare: () => void;
  shareNotice: string | null;
};

export function HowToPlayPanel({ config, gameState, onShare, shareNotice }: Props) {
  const shareEnabled = Boolean(config?.featureFlags.enableShare);

  return (
    <aside className="w-full rounded-[28px] border border-[var(--color-line)] bg-[var(--color-panel)] p-5 shadow-[var(--shadow-panel)] lg:sticky lg:top-4 lg:max-w-[360px]">
      <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--color-brand-strong)]">
        小提示
      </p>
      <h2 className="mt-2 font-[var(--font-display)] text-2xl font-bold">先把范围缩小</h2>

      <div className="mt-4 space-y-3">
        <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.72)] px-4 py-3">
          <p className="text-sm font-semibold text-[var(--color-ink)]">第一眼先看大方向</p>
          <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
            星级、场地、距离和跑法通常最适合先排除一大片角色。
          </p>
        </div>

        <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.72)] px-4 py-3">
          <p className="text-sm font-semibold text-[var(--color-ink)]">手机上直接左右滑</p>
          <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
            结果表放在独立滚动区里，先看左边几列，再慢慢补完整体线索就好。
          </p>
        </div>

        <div className="rounded-[22px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.72)] px-4 py-3">
          <p className="text-sm font-semibold text-[var(--color-ink)]">这一局的节奏</p>
          <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
            你有 {config?.maxGuesses ?? 8} 次机会。猜中会立刻结算，没猜中就继续沿着表格里的线索追。
          </p>
          <p className="mt-2 text-xs text-[var(--color-muted)]">
            {gameState
              ? `当前已经留下 ${gameState.guessRows.length} 行线索。`
              : "开始新一局后，这里会帮你一起看节奏。"}
          </p>
        </div>
      </div>

      <div className="mt-4 rounded-[24px] border border-[rgba(200,108,53,0.16)] bg-[rgba(255,245,231,0.84)] px-4 py-4">
        <p className="text-sm font-semibold text-[var(--color-ink)]">
          {shareEnabled ? "这局顺手发给朋友" : "分享功能还在准备中"}
        </p>
        <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
          {shareEnabled
            ? "猜完后可以直接把这一局带去群里，也可以先发个链接喊朋友一起试。"
            : "先把这局猜出来，后面再把分享入口补得更完整。"}
        </p>
        {shareEnabled ? (
          <button
            type="button"
            onClick={onShare}
            className="mt-3 min-h-11 w-full rounded-[18px] border border-[rgba(200,108,53,0.18)] bg-white px-4 py-3 text-sm font-semibold text-[var(--color-brand-strong)] transition hover:bg-[rgba(255,248,240,0.96)]"
          >
            分享这一局
          </button>
        ) : null}
        {shareNotice ? (
          <p className="mt-2 text-sm text-[var(--color-muted)]">{shareNotice}</p>
        ) : null}
      </div>
    </aside>
  );
}
