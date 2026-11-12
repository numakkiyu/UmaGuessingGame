import "@/i18n/aptitude";
import { useTranslation } from "@/i18n";
import { cardName } from "@/i18n/entities";
import { taskConditions, axisName } from "@/i18n/aptitude-values";
import { Circle, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { taskPointsFor, type DraftCard, type DraftTask } from "@/lib/api/draft";
import { cn } from "@/lib/utils";

/** 等级徽标：颜色区分 A/B/C，同时带字母（不靠颜色单独表意） */
export function GradeBadge({ grade, dim }: { grade: string; dim?: boolean }) {
  if (!grade) {
    return (
      <span className="inline-flex size-6 items-center justify-center rounded border border-dashed border-[var(--color-line-strong)] text-[length:var(--text-aux)] text-[var(--color-muted)]">
        —
      </span>
    );
  }
  return (
    <span
      className={cn(
        "inline-flex size-6 items-center justify-center rounded border text-sm font-bold",
        grade === "A" && "border-[var(--color-correct-edge)] bg-[var(--color-correct)] text-[var(--color-ink-strong)]",
        grade === "B" && "border-[var(--color-near-edge)] bg-[var(--color-near)] text-[var(--color-ink-strong)]",
        grade === "C" && "border-[var(--color-wrong-edge)] bg-[var(--color-wrong)] text-[var(--color-ink-strong)]",
        grade !== "A" && grade !== "B" && grade !== "C" &&
          "border-[var(--color-line-strong)] bg-[var(--color-panel-soft)] text-[var(--color-muted)]",
        dim && "opacity-60",
      )}
    >
      {grade}
    </span>
  );
}

/** 任务卡：一个任务槽 */
export function TaskCard({
  task,
  index,
  card,
  problem,
  problemSeverity,
  disabled,
  onOpen,
  onClear,
}: {
  task: DraftTask;
  index: number;
  card: DraftCard | null;
  problem: string | null;
  problemSeverity: "pending" | "error" | null;
  disabled: boolean;
  onOpen: () => void;
  onClear: () => void;
}) {
  const {t}=useTranslation("aptitude",{keyPrefix:"ui"});
  const points = card ? taskPointsFor(card, task) : null;

  return (
    <li
      className={cn(
        "min-w-0 rounded-[var(--radius-card)] border-2 bg-[var(--color-panel)] p-3",
        problemSeverity === "error"
          ? "border-[var(--color-danger)]"
          : card
            ? "border-[var(--color-line)]"
            : "border-dashed border-[var(--color-line-strong)]",
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="text-sm font-bold text-[var(--color-ink)]">{t("task",{count:index+1})}</h3>
          <p className="mt-0.5 break-words text-[length:var(--text-aux)] text-[var(--color-muted)]">
            {taskConditions(task)}
          </p>
        </div>
        {card ? (
          <span className="uma-code shrink-0 rounded-full bg-[var(--color-brand-green-soft)] px-2 py-0.5 text-[length:var(--text-aux)] font-bold text-[var(--color-ink)]">
            {t("price",{price:card.price})}
          </span>
        ) : null}
      </div>

      <div className="mt-2 flex flex-col gap-2">
        {card ? (
          <>
            <div className="flex min-w-0 items-center gap-2">
              {card.imagePath ? (
                <img
                  src={card.imagePath}
                  alt=""
                  aria-hidden="true"
                  className="size-10 shrink-0 object-contain"
                  loading="lazy"
                  decoding="async"
                />
              ) : (
                <span aria-hidden="true" className="size-10 shrink-0 rounded-full bg-[var(--color-panel-soft)]" />
              )}
              <span className="min-w-0 flex-1 break-words text-sm font-semibold text-[var(--color-ink)]">
                {cardName(card)}
              </span>
            </div>

            {/* 三维等级：这一格的适配情况，A/B/C 都有字母 */}
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{axisName("surface")}</span>
              <GradeBadge grade={card.aptitudes.surface[task.surface] ?? ""} />
              <span className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{axisName("distance")}</span>
              <GradeBadge grade={card.aptitudes.distance[task.distance] ?? ""} />
              <span className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{axisName("style")}</span>
              <GradeBadge grade={card.aptitudes.style[task.style] ?? ""} />
              <span className="uma-code ml-auto text-sm font-bold text-[var(--color-ink)]">
                {points?.total ?? 0}/9
              </span>
            </div>
          </>
        ) : (
          <p className="text-sm text-[var(--color-muted)]">{t("emptyTask")}</p>
        )}

        {problem ? (
          /* Empty slots use neutral styling; invalid choices use error styling */
          <p
            className={cn(
              "flex items-start gap-1 text-[length:var(--text-aux)]",
              problemSeverity === "error"
                ? "font-semibold text-[var(--color-danger)]"
                : "text-[var(--color-muted)]",
            )}
          >
            {problemSeverity === "error" ? (
              <X aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
            ) : (
              <Circle aria-hidden="true" className="mt-0.5 size-3.5 shrink-0" />
            )}
            <span className="min-w-0 break-words">{problem}</span>
          </p>
        ) : null}

        <div className="flex flex-col gap-2 sm:flex-row">
          <Button variant="secondary" onClick={onOpen} disabled={disabled} className="sm:flex-1">
            <Search aria-hidden="true" className="size-4" />
            {t(card ? "replace" : "choose")}
          </Button>
          {card ? (
            <Button variant="ghost" onClick={onClear} disabled={disabled} className="sm:w-auto">
              {t("clear")}
            </Button>
          ) : null}
        </div>
      </div>
    </li>
  );
}
