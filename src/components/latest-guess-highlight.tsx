type Props = {
  latestGuess: {
    displayName: string;
    cells: Record<string, { value: string; status: "correct" | "near" | "wrong" }>;
  };
};

const cellLabels: Record<string, string> = {
  star: "星级",
  surface: "场地",
  distance: "距离",
  style: "跑法",
  sex: "牡/牝",
  g1: "GI档",
  g23: "GII/GIII档",
  grade: "学年",
  dormitory: "宿舍",
};

export function LatestGuessHighlight({ latestGuess }: Props) {
  const correctCount = Object.values(latestGuess.cells).filter(
    (cell) => cell.status === "correct",
  ).length;
  const closeCount = Object.values(latestGuess.cells).filter(
    (cell) => cell.status !== "wrong",
  ).length;
  const visibleHints = Object.entries(latestGuess.cells)
    .filter(([, cell]) => cell.status !== "wrong")
    .slice(0, 4);

  return (
    <div className="latest-guess-card rounded-[24px] border border-[rgba(200,108,53,0.18)] bg-[linear-gradient(180deg,rgba(255,244,228,0.96),rgba(255,249,241,0.9))] px-4 py-4 text-sm text-[var(--color-ink)] shadow-[var(--shadow-soft)]">
      <p className="text-xs font-semibold uppercase tracking-[0.2em] text-[var(--color-brand-strong)]">
        刚刚落下的一行
      </p>
      <p className="latest-guess-copy mt-2 leading-6">
        刚刚猜的是 <span className="font-semibold">{latestGuess.displayName}</span>。这一行里已经对上{" "}
        <span className="font-semibold">{correctCount}</span> 项，另外还有{" "}
        <span className="font-semibold">{Math.max(closeCount - correctCount, 0)}</span> 项很接近。
      </p>
      {visibleHints.length > 0 ? (
        <div className="latest-guess-tags mt-3 flex flex-wrap gap-2">
          {visibleHints.map(([key, cell]) => (
            <span
              key={key}
              className="rounded-full border border-[rgba(200,108,53,0.16)] bg-white/80 px-3 py-1 text-xs font-medium text-[var(--color-ink)]"
            >
              {cellLabels[key]} · {cell.value}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}
