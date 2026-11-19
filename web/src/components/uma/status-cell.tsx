import "@/i18n/classic";
import { useTranslation } from "@/i18n";

import { Check, Minus, X } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export type CellState = "correct" | "near" | "wrong";

const STATE_SYMBOL: Record<CellState, typeof Check> = {
  correct: Check,
  near: Minus,
  wrong: X,
};

export type StatusCellProps = {
  /** 字段值（可能很长，必须可换行） */
  value: ReactNode;
  state: CellState;
  /** 字段名，用于屏幕阅读器与紧凑布局 */
  label?: string;
  /** 紧凑模式：字段名显示在上方，值在下方 */
  compact?: boolean;
  emphasized?: boolean;
  className?: string;
  /** 覆盖默认的状态文字（例如数值差方向） */
  stateText?: string;
};

export function StatusCell({
  value,
  state,
  label,
  compact = false,
  emphasized = false,
  className,
  stateText,
}: StatusCellProps) {
  const {t}=useTranslation("classic");
  const Symbol = STATE_SYMBOL[state];
  const text = stateText ?? t(`status.${state}`);

  return (
    <div
      data-state={state}
      data-emphasized={emphasized ? "true" : undefined}
      className={cn(
        "uma-state-cell",
        compact ? "flex-col items-start gap-0.5 py-2" : "justify-between",
        emphasized && "ring-2 ring-[var(--color-blue-deep)] ring-offset-1",
        className,
      )}
    >
      <div className={cn("flex items-center gap-1.5", compact ? "text-[length:var(--text-aux)]" : "text-[length:var(--text-aux)]")}>
        <Symbol aria-hidden="true" className="size-3.5 shrink-0" />
        {/* 状态文字：不只靠颜色区分 */}
        <span className="font-semibold">{text}</span>
        <span className="sr-only">{t(`statusSr.${state}`)}</span>
      </div>
      {label ? (
        <span className={cn("text-[var(--text-aux)] font-medium opacity-80", compact ? "order-first" : "sr-only")}>
          {label}
        </span>
      ) : null}
      <span className={cn("min-w-0 break-words font-semibold", compact ? "text-base" : "text-sm")}>{value}</span>
    </div>
  );
}
