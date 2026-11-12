import "@/i18n/classic";
import { useTranslation } from "@/i18n";
import { characterName } from "@/i18n/entities";
import { displayClassicValue } from "@/i18n/classic-values";
/**
 * 九字段反馈卡片。
 *
 * 手机默认：头像 + 名字 + 核心四项，其余五项折叠但展开后完整可达。
 * 桌面：九项平铺。
 * 状态同时有符号 + 文字（✓ 对上 / ≈ 接近 / × 不符 / — 未知），不只靠颜色。
 */
import { Check, ChevronDown, Minus, X } from "lucide-react";
import { useId, useState } from "react";
import { AvatarImage } from "@/components/uma/avatar-image";
import {
  CORE_FIELDS,
  SECONDARY_FIELDS,
  type CellStatus,
  type ClassicField,
  type NormalizedCell,
  type NormalizedGuessRow,
} from "@/lib/api/classic";
import { cn } from "@/lib/utils";

/**
 * 三态视觉。
 *
 * 背景色用 的 correct/near/wrong 令牌，文字一律用 --color-ink-strong：
 * 实测对比 correct 6.29:1、near 9.68:1、wrong 5.43:1，而白字在三态底上只有
 * 2.51/1.64/2.91，不达标（见 contrast 测试的反例断言）。
 *
 * 颜色只是附加编码：每个单元同时有图标与文字，色盲用户不依赖颜色也能读。
 */
const STATUS_META: Record<
  CellStatus,
  { Icon: typeof Check; cellClass: string }
> = {
  correct: {
    Icon: Check,
    cellClass: "border-[var(--color-correct)] bg-[var(--color-correct)] text-[var(--color-ink-strong)]",
  },
  near: {
    Icon: Minus,
    cellClass: "border-[var(--color-near)] bg-[var(--color-near)] text-[var(--color-ink-strong)]",
  },
  wrong: {
    Icon: X,
    cellClass: "border-[var(--color-wrong)] bg-[var(--color-wrong)] text-[var(--color-ink-strong)]",
  },
  unknown: {
    Icon: Minus,
    cellClass: "border-dashed border-[var(--color-line-strong)] bg-[var(--color-panel-soft)] text-[var(--color-ink)]",
  },
};

function cellState(cell: NormalizedCell): "correct" | "near" | "wrong" | null {
  return cell.status === "unknown" ? null : cell.status;
}

function FieldCell({ cell, compact }: { cell: NormalizedCell; compact: boolean }) {
  const {t}=useTranslation("classic");
  const meta = STATUS_META[cell.status];
  const { Icon } = meta;
  const state = cellState(cell);

  return (
    <div
      data-field-cell={cell.field}
      data-state={state ?? undefined}
      className={cn(
        "flex min-w-0 items-center justify-between gap-2 rounded-[var(--radius-control)] border px-2.5 py-2",
        // Status colours share STATUS_META with the table.
        meta.cellClass,
        compact ? "text-[length:var(--text-aux)]" : "text-sm",
      )}
    >
      <span className="min-w-0 break-keep font-semibold">{displayClassicValue(cell)}</span>
      <span className="flex shrink-0 items-center gap-1">
        <Icon aria-hidden="true" className="size-3.5" />
        <span className="font-semibold">{t(`status.${cell.status}`)}</span>
        <span className="sr-only">{t(`statusSr.${cell.status}`)}</span>
      </span>
    </div>
  );
}

function FieldRow({ cells, fields, compact }: { cells: NormalizedCell[]; fields: readonly ClassicField[]; compact: boolean }) {
  const {t}=useTranslation("classic");
  const byField = new Map(cells.map((cell) => [cell.field, cell]));
  return (
    <div className={cn("grid gap-1.5", compact ? "grid-cols-2" : "grid-cols-3")}>
      {fields.map((field) => {
        const cell = byField.get(field) ?? { field, value: "—", status: "unknown" as const };
        return (
          <div key={field} className="min-w-0">
            <p className="mb-0.5 truncate text-[length:var(--text-aux)] text-[var(--color-muted)]">
              {t(`fields.${field}`)}
            </p>
            <FieldCell cell={cell} compact={compact} />
          </div>
        );
      })}
    </div>
  );
}

export type GuessCardProps = {
  row: NormalizedGuessRow;
  /** 最新一条高亮 */
  latest?: boolean;
  className?: string;
};

export function GuessCard({ row, latest = false, className }: GuessCardProps) {
  const {t}=useTranslation("classic");
  /* 自动展开最新记录；玩家手动展开或收起的选择优先，新增结果不覆盖该选择 */
  const [override, setOverride] = useState<boolean | null>(null);
  const detailId = useId();

  // latest 可能在首次渲染后才确定（快照异步到达）。用「上一次 latest」对比而不是
  // effect，避免 setState-in-effect 的级联渲染。
  const [lastLatest, setLastLatest] = useState(latest);
  if (latest !== lastLatest) {
    setLastLatest(latest);
    // 成为最新时回到自动展开，让「最新一条总是完整」这条默认重新生效。
    // 失去最新时不动 override：用户主动打开的历史记录保持打开。
    if (latest) setOverride(null);
  }

  const expanded = override ?? latest;

  return (
    <article
      data-guess-row="true"
      data-latest={latest ? "true" : "false"}
      data-attempt={row.attempt}
      className={cn(
        "uma-card overflow-hidden p-3",
        latest && "ring-2 ring-[var(--color-blue-deep)]",
        className,
      )}
    >
      <header className="flex items-center gap-3">
        <AvatarImage
          characterId={row.characterId}
          // 已提交的猜测是本人公开信息，可用名字
          alt={characterName(row)}
          size="sm"
          loading={latest ? "eager" : "lazy"}
        />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <p className="truncate font-bold text-[var(--color-ink)]">{characterName(row)}</p>
            <span className="text-[length:var(--text-aux)] text-[var(--color-muted)]">
              {t("feedback.attempt",{count:row.attempt})}
            </span>
            {latest ? (
              <span className="rounded-full bg-[var(--color-brand-green-soft)] px-2 py-0.5 text-[length:var(--text-aux)] font-bold text-[var(--color-ink)]">
                {t("feedback.latest")}
              </span>
            ) : null}
          </div>
          {row.correct ? (
            <p className="mt-0.5 flex items-center gap-1 text-sm font-semibold text-[var(--color-action-green)]">
              <Check aria-hidden="true" className="size-4" />
              {t("feedback.correct")}
            </p>
          ) : null}
        </div>
      </header>

      {/* 手机：核心四项直接可见 */}
      <div className="mt-3">
        <FieldRow cells={row.cells} fields={CORE_FIELDS} compact />
      </div>

      {/* 其余五项：可折叠，展开后完整可达（ 要求不得丢失） */}
      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={detailId}
        onClick={() => setOverride(!expanded)}
        className={cn(
          "mt-2 flex min-h-11 w-full items-center justify-between gap-2 rounded-[var(--radius-control)] px-2",
          "text-[length:var(--text-aux)] font-semibold text-[var(--color-blue-deep)]",
          "hover:bg-[var(--color-panel-soft)]",
        )}
      >
        <span>{t(expanded ? "feedback.collapse" : "feedback.expand")}</span>
        <ChevronDown
          aria-hidden="true"
          className={cn(
            "size-4 shrink-0 transition-transform duration-[var(--duration-quick)] ease-[var(--ease-primary)]",
            expanded && "rotate-180",
          )}
        />
      </button>
      <div id={detailId} hidden={!expanded} className="mt-1">
        {expanded ? <FieldRow cells={row.cells} fields={SECONDARY_FIELDS} compact /> : null}
      </div>
    </article>
  );
}

/** 完整表格视图 */
export function GuessTable({ rows }: { rows: NormalizedGuessRow[] }) {
  const {t}=useTranslation("classic");
  return (
    <div
      className="uma-scroll-x w-full rounded-[var(--radius-card)] border border-[var(--color-line)]"
      tabIndex={0}
      role="region"
      aria-label={t("feedback.table")}
    >
      <table className="w-full min-w-[46rem] border-collapse text-left text-sm">
        <caption className="sr-only">
          {t("feedback.caption",{count:rows.length})}
        </caption>
        <thead className="bg-[var(--color-panel-soft)]">
          <tr>
            <th scope="col" className="sticky left-0 z-[1] bg-[var(--color-panel-soft)] px-3 py-2 font-semibold">
              {t("feedback.character")}
            </th>
            <th scope="col" className="px-2 py-2 font-semibold">
              {t("feedback.attemptColumn")}
            </th>
            {[...CORE_FIELDS, ...SECONDARY_FIELDS].map((field) => (
              <th key={field} scope="col" className="whitespace-nowrap px-2 py-2 font-semibold">
                {t(`fields.${field}`)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-[var(--color-line)]">
          {rows.map((row) => {
            const byField = new Map(row.cells.map((cell) => [cell.field, cell]));
            return (
              <tr key={`${row.characterId}-${row.attempt}`}>
                <th
                  scope="row"
                  className="sticky left-0 z-[1] max-w-48 bg-[var(--color-panel)] px-3 py-2 text-left font-semibold"
                >
                  {/* 头像 + 名字：表格没有图就只有文字，读起来像数据表而不是游戏记录。
                      头像走统一回退（外链 → 本地 WebP → 品牌占位），失败不显示破图。 */}
                  <span className="flex items-center gap-2">
                    <AvatarImage
                      characterId={row.characterId}
                      src={row.avatarPath}
                      remoteSrc={row.avatarRemoteUrl}
                      // 表格里是已提交的公开猜测，名字已在相邻单元格显示，
                      // 这里作为装饰图，避免屏幕阅读器重复朗读同一名字
                      alt=""
                      size="xs"
                    />
                    <span className="truncate">{characterName(row)}</span>
                  </span>
                </th>
                <td className="px-2 py-2 text-[var(--color-muted)]">{row.attempt}</td>
                {[...CORE_FIELDS, ...SECONDARY_FIELDS].map((field) => {
                  const cell = byField.get(field);
                  const meta = STATUS_META[cell?.status ?? "unknown"];
                  const { Icon } = meta;
                  return (
                    <td
                      key={field}
                      data-state={cell?.status ?? "unknown"}
                      // The table cell carries the same status colour as the card,
                      // so a row can be scanned by colour without reading every
                      // value. Text and icon remain, so colour is never the only
                      // signal .
                      className={cn("whitespace-nowrap px-2 py-2", meta.cellClass)}
                    >
                      <span className="inline-flex items-center gap-1">
                        <Icon aria-hidden="true" className="size-3.5" />
                        <span className="font-semibold">{cell ? displayClassicValue(cell) : "—"}</span>
                        <span className="text-[length:var(--text-aux)] opacity-80">{t(`status.${cell?.status ?? "unknown"}`)}</span>
                      </span>
                    </td>
                  );
                })}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
