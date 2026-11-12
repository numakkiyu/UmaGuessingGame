import "@/i18n/aptitude";
import { useTranslation } from "@/i18n";
import { axisName, aptitudeValue } from "@/i18n/aptitude-values";
import { cardName } from "@/i18n/entities";
import { Check, X } from "lucide-react";
import { type GridCell } from "@/lib/api/aptitude-grid";
import { cn } from "@/lib/utils";

/** 一格的显示内容：占领信息或空白 */
function CellContent({ cell, ownerLabel }: { cell: GridCell; ownerLabel?: string }) {
  const {t}=useTranslation("aptitude",{keyPrefix:"ui"});
  if (!cell.claim) return null;
  return (
    <span className="flex min-w-0 flex-col items-center gap-0.5">
      {cell.claim.imagePath ? (
        <img
          src={cell.claim.imagePath}
          alt=""
          aria-hidden="true"
          className="size-8 shrink-0 object-contain"
          loading="lazy"
          decoding="async"
        />
      ) : null}
      <span className="break-words text-[length:var(--text-aux)] font-semibold leading-tight text-[var(--color-ink-strong)]">
        {cardName(cell.claim)}
      </span>
      {ownerLabel ? <span className="break-words text-[0.625rem] leading-tight text-[var(--color-ink-strong)]">{ownerLabel}</span> : null}
      {/* 队伍/归属标记：不只靠头像表达谁占格 */}
      <span className="uma-code text-[0.625rem] leading-none text-[var(--color-ink-strong)]">
        {t("turn",{count:cell.claim.turn})}
      </span>
      {cell.claim.satisfied !== null ? (
        <span
          className={cn(
            "mt-0.5 inline-flex items-center gap-0.5 rounded-full px-1.5 py-0.5 text-[0.625rem] font-bold leading-none",
            cell.claim.satisfied
              ? "bg-[var(--color-correct)] text-[var(--color-ink-strong)]"
              : "bg-[var(--color-wrong)] text-[var(--color-ink-strong)]",
          )}
        >
          {cell.claim.satisfied ? (
            <>
              <Check aria-hidden="true" className="size-2.5" />
              {t("satisfies")}
            </>
          ) : (
            <>
              <X aria-hidden="true" className="size-2.5" />
              {t("mismatch")}
            </>
          )}
        </span>
      ) : null}
    </span>
  );
}

/** 一格 */
export function BoardCell({
  cell,
  selected,
  disabled,
  onSelect,
  showConditions = false,
  ownerLabel,
}: {
  showConditions?: boolean;
  ownerLabel?: string;
  cell: GridCell;
  selected: boolean;
  disabled: boolean;
  onSelect: (cell: GridCell) => void;
}) {
  const {t}=useTranslation("aptitude",{keyPrefix:"ui"});
  const claimed = cell.claim !== null;

  return (
    <button
      type="button"
      onClick={() => onSelect(cell)}
      disabled={disabled || claimed}
      aria-pressed={selected}
      aria-label={t("cellAria",{row:cell.row+1,col:cell.col+1,conditions:`${axisName(cell.rowCondition.axis)} ${aptitudeValue(cell.rowCondition.axis,cell.rowCondition.value)} × ${axisName(cell.colCondition.axis)} ${aptitudeValue(cell.colCondition.axis,cell.colCondition.value)}`,occupancy:cell.claim ? t("claimedBy",{name:ownerLabel ? `${cardName(cell.claim)} · ${ownerLabel}` : cardName(cell.claim)}) : t("empty")})}
      className={cn(
        // 格子最短边 ≥84px
        "relative flex min-h-[84px] min-w-0 flex-col items-center justify-center gap-1 p-1",
        "rounded-[var(--radius-control)] border-2 text-center",
        showConditions && "min-h-32",
        "transition-[border-color,background-color,box-shadow] duration-[var(--duration-quick)] ease-[var(--ease-primary)]",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",
        claimed
          ? "border-[var(--color-action-green)] bg-[var(--color-brand-green-soft)]"
          : selected
            ? // 选中先强调边界
              "border-[var(--color-blue-deep)] bg-[var(--color-page-tint)] ring-2 ring-[var(--color-blue-deep)] ring-offset-1"
            : "border-[var(--color-line-strong)] bg-[var(--color-panel)] hover:bg-[var(--color-panel-soft)]",
        !disabled && !claimed && "motion-safe:active:scale-[0.98]",
      )}
    >
      {showConditions ? <span className="text-xs font-bold text-[var(--color-muted)]">{aptitudeValue(cell.rowCondition.axis,cell.rowCondition.value)} A × {aptitudeValue(cell.colCondition.axis,cell.colCondition.value)} A</span> : null}
      <CellContent cell={cell} ownerLabel={ownerLabel} />
      {!claimed && !selected ? (
        <span aria-hidden="true" className="text-[length:var(--text-aux)] text-[var(--color-muted)]">
          {t("empty")}
        </span>
      ) : null}
    </button>
  );
}
