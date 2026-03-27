import { AvatarImage } from "@/components/avatar-image";
import type { QuestionBankEntry } from "@/lib/validation/schemas";

type Props = {
  gameState: {
    status: "playing" | "won" | "lost" | "ended";
    answerDisplayName?: string | null;
    answerCharacterId?: string | null;
    guessRows: Array<unknown>;
  } | null;
  maxGuesses: number;
  answerEntry: QuestionBankEntry | null;
  summaryText: string;
  canEdit: boolean;
  onRestart: () => void;
  onShare: () => void;
  shareEnabled: boolean;
  shareNotice: string | null;
};

export function GameResultDialog({
  gameState,
  maxGuesses,
  answerEntry,
  summaryText,
  canEdit,
  onRestart,
  onShare,
  shareEnabled,
  shareNotice,
}: Props) {
  if (!gameState || gameState.status === "playing" || gameState.status === "ended" || !canEdit) {
    return null;
  }

  const detailItems = answerEntry
    ? [
        { label: "星级", value: `${answerEntry.star}星` },
        { label: "场地", value: answerEntry.surface_group.join("/") },
        { label: "距离", value: answerEntry.distance_group.join("/") },
        { label: "跑法", value: answerEntry.running_style_group.join("/") },
        { label: "牡/牝", value: answerEntry.sex_type },
        { label: "GI档", value: answerEntry.g1_bracket },
        { label: "GII/GIII档", value: answerEntry.g23_bracket },
        { label: "学年", value: answerEntry.school_grade },
        { label: "宿舍", value: answerEntry.dormitory },
      ]
    : [];

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center overflow-y-auto bg-[rgba(23,42,75,0.36)] px-3 py-3 backdrop-blur-[4px] sm:items-center sm:px-4 sm:py-6">
      <div className="relative w-full max-w-2xl overflow-hidden rounded-[28px] border border-[var(--color-line)] bg-[var(--color-panel-strong)] p-4 shadow-[var(--shadow-panel)] sm:rounded-[30px] sm:p-6">
        <div className="pointer-events-none absolute inset-x-0 top-0 h-1 bg-[linear-gradient(90deg,var(--color-brand),var(--color-brand-blue))]" />
        <div className="pointer-events-none absolute -right-16 top-10 h-36 w-36 rounded-full bg-[rgba(243,199,84,0.18)] blur-3xl" />
        <p className="font-[var(--font-display)] text-sm uppercase tracking-[0.3em] text-[var(--color-brand-strong)]">
          {gameState.status === "won" ? `第 ${gameState.guessRows.length} 猜命中` : "答案揭晓"}
        </p>
        <h2 className="mt-2 font-[var(--font-display)] text-[2rem] font-bold sm:text-3xl">
          {gameState.status === "won" ? "就是她" : "这局先到这里"}
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
        <p className="mt-2 text-sm text-[var(--color-muted)]">{summaryText}</p>

        <div className="mt-5 rounded-[24px] border border-[var(--color-line)] bg-[linear-gradient(180deg,rgba(229,244,255,0.72),rgba(255,255,255,0.92))] p-4 sm:rounded-[26px]">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
            <div className="mx-auto flex w-full max-w-[180px] flex-col items-center text-center sm:mx-0">
              <div className="flex size-28 items-center justify-center overflow-hidden rounded-[26px] border border-[var(--color-line)] bg-white shadow-[var(--shadow-soft)] ring-4 ring-[rgba(255,255,255,0.72)]">
                <AvatarImage
                  primarySrc={answerEntry?.image_local_path}
                  proxySrc={answerEntry?.image_local_path}
                  remoteSrc={answerEntry?.image_url}
                  alt={gameState.answerDisplayName ?? "答案马娘"}
                  loading="eager"
                  className="size-full object-cover"
                />
              </div>
            </div>

            <div className="flex-1">
              <p className="text-sm font-semibold text-[var(--color-ink)]">
                {gameState.status === "won" ? "这局总结" : "这次的目标"}
              </p>
              <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
                {gameState.status === "won"
                  ? "你已经把这局成功收掉了，下面这张卡片会把答案的主要信息一次摆给你。"
                  : "这局没能在规定次数里找到她，先把答案卡片看完整，下一局会更容易缩范围。"}
              </p>

              {detailItems.length > 0 ? (
                <div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                  {detailItems.map((item) => (
                    <div
                      key={item.label}
                      className="rounded-[18px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.88)] px-3 py-3 shadow-[var(--shadow-soft)]"
                    >
                      <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--color-muted)]">
                        {item.label}
                      </p>
                      <p className="mt-1 text-sm font-semibold text-[var(--color-ink)]">
                        {item.value}
                      </p>
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
          </div>
        </div>

        <div className={`mt-5 grid gap-3 ${shareEnabled ? "sm:grid-cols-2" : ""}`}>
          {shareEnabled ? (
            <button
              type="button"
              onClick={onShare}
              className="min-h-12 rounded-[20px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.92)] px-4 py-3 text-sm font-semibold text-[var(--color-brand-blue-deep)] shadow-[var(--shadow-soft)]"
            >
              分享这一局
            </button>
          ) : null}
          <button
            type="button"
            onClick={onRestart}
            className="min-h-12 rounded-[20px] border border-[rgba(103,186,24,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-4 py-3 text-sm font-semibold text-[#244117] shadow-[0_16px_28px_rgba(116,194,22,0.18)]"
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
