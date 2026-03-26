type Props = {
  questionBankSize: number;
  maxGuesses: number;
};

const spotlightCards = [
  {
    title: "开局就能猜",
    description: "不用等每日刷新，随时都能直接来一把。",
  },
  {
    title: "手机也顺手",
    description: "输入框始终摆在前面，结果表左右滑一下就能看全。",
  },
  {
    title: "猜中马上揭晓",
    description: "一旦命中答案，这局会立刻结算，不会继续拖回合。",
  },
];

export function GameHeader({ questionBankSize, maxGuesses }: Props) {
  return (
    <section className="relative overflow-hidden rounded-[32px] border border-[var(--color-line)] bg-[var(--color-panel)] px-5 py-5 shadow-[var(--shadow-panel)] sm:px-6 sm:py-6">
      <div className="pointer-events-none absolute inset-x-0 top-0 h-28 bg-[linear-gradient(90deg,rgba(239,209,172,0.52),rgba(255,250,242,0))]" />
      <div className="pointer-events-none absolute -right-14 top-6 h-36 w-36 rounded-full bg-[rgba(200,108,53,0.12)] blur-3xl" />

      <div className="relative grid gap-5 xl:grid-cols-[minmax(0,1.35fr)_340px] xl:items-start">
        <div>
          <div className="inline-flex rounded-full border border-[rgba(156,69,24,0.18)] bg-[rgba(255,245,232,0.88)] px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.28em] text-[var(--color-brand-strong)]">
            Uma Guessing Game
          </div>
          <h1 className="mt-3 font-[var(--font-display)] text-[2.5rem] font-bold leading-none tracking-[0.04em] text-[var(--color-ink)] sm:text-[3.35rem]">
            赛马娘弗一把
          </h1>
          <p className="mt-3 max-w-2xl text-sm leading-6 text-[var(--color-muted)] sm:text-base">
            输入一位马娘，顺着每一行线索把答案找出来。先从场地、距离和跑法缩范围，往往会更快有感觉。
          </p>

          <div className="mt-4 flex flex-wrap gap-2">
            <span className="rounded-full bg-[var(--color-panel-soft)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
              当前可猜 {questionBankSize} 位
            </span>
            <span className="rounded-full bg-[rgba(216,79,66,0.12)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
              每局 {maxGuesses} 次机会
            </span>
            <span className="rounded-full bg-[rgba(216,166,66,0.18)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
              支持中文、日文和常见别名
            </span>
          </div>
        </div>

        <div className="grid gap-3 sm:grid-cols-3 xl:grid-cols-1">
          {spotlightCards.map((card) => (
            <div
              key={card.title}
              className="rounded-[24px] border border-[var(--color-line)] bg-[rgba(255,252,247,0.86)] px-4 py-4 shadow-[var(--shadow-soft)]"
            >
              <p className="text-sm font-semibold text-[var(--color-ink)]">{card.title}</p>
              <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
                {card.description}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
