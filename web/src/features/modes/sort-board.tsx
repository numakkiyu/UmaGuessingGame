import { useTranslation } from "@/i18n";
import { sortPosition } from "@/i18n/sort";
import { cardName } from "@/i18n/entities";
// Table and card layouts share controls and state; button-based moves also support touch and keyboard input
import { ArrowDown, ArrowUp, Link2, Unlink } from "lucide-react";
import { AvatarImage } from "@/components/uma/avatar-image";
import { Button } from "@/components/ui/button";
import type { SortCard, SortDraft } from "@/lib/api/sort";
import { cn } from "@/lib/utils";

export type SortGroupProps = {
  draft: SortDraft;
  positionKind?: "rank" | "position";
  readOnly?: boolean;
  cards: readonly SortCard[];
  /** 提交后锁定，不允许再改 */
  locked: boolean;
  onChange: (next: SortDraft) => void;
  onMoveUp: (index: number) => void;
  onMoveDown: (index: number) => void;
  onMerge: (index: number) => void;
  onSplit: (index: number) => void;
};

/** 名次槽位：并列组共享同一个名次，下一组跳过相应数量 */
function rankLabels(draft: SortDraft, position: boolean): string[] {
  const labels: string[] = [];
  let rank = 1;
  for (const group of draft) {
    labels.push(sortPosition(rank,group.length>1,position));
    rank += group.length;
  }
  return labels;
}

export function SortBoard({
  draft,
  cards,
  locked,
  onMoveUp,
  onMoveDown,
  onMerge,
  onSplit,
  positionKind = "rank",
  readOnly = false,
}: SortGroupProps) {
  const {t}=useTranslation("sort");
  const byId = new Map(cards.map((card) => [card.characterId, card]));
  const labels = rankLabels(draft,positionKind === "position");

  return (
    <div className="uma-sort-board">
    <table className="uma-sort-table">
      <caption className="sr-only">{t("board")}</caption>
      <thead><tr><th scope="col">{t("orderColumn")}</th><th scope="col">{t("characterColumn")}</th>{!readOnly ? <th scope="col">{t("controls")}</th> : null}</tr></thead>
      <tbody>
      {draft.map((group, index) => {
        const label = labels[index] ?? sortPosition(index+1,false,positionKind === "position");
        const isMerged = group.length > 1;

        return (
          <tr key={group.join("+")} className={cn("uma-sort-table__row", isMerged && "uma-sort-table__row--tied")}>
              {/* 名次槽位：显式数字，不靠卡片位置隐含 */}
              <th scope="row" className="uma-sort-table__rank">
                <span className="uma-code text-sm font-bold text-[var(--color-ink)]">
                  {draft.slice(0,index).reduce((total,g)=>total+g.length,1)}
                </span>
                <span className="mt-0.5 text-[length:var(--text-aux)] leading-tight text-[var(--color-muted)]">
                  {t(isMerged ? "tied" : positionKind === "position" ? "positionUnit" : "rankUnit")}
                </span>
              </th>

              {/* 卡片内容 */}
              <td className="uma-sort-table__characters"><div className="flex min-w-0 flex-col gap-1.5">
                {group.map((id) => {
                  const card = byId.get(id);
                  return (
                    <div key={id} className="flex min-w-0 items-center gap-2">
                      <AvatarImage
                        characterId={id}
                        alt={card?.name ? cardName(card) : t("unknownCard")}
                        size="sm"
                        src={card?.imagePath ?? null}
                        remoteSrc={card?.remoteImageUrl ?? null}
                      />
                      <div className="min-w-0 flex-1">
                        {/*
                          名字不截断成一行。
                          排序玩法里「这是谁」是判断依据本身；之前用 truncate
                          在 390 竖屏下把「机伶」「统治」这类名字切掉一半，
                          玩家看不出卡是谁，就没法排序。
                          允许折两行：完整换行；日语长名也不能省掉用于辨认的后半部分。
                        */}
                        <p className="break-words text-sm font-semibold text-[var(--color-ink)]">
                          {card?.name ? cardName(card) : t("unknownCard")}
                        </p>
                        {/*
                          闭卷模式下服务端不下发 display（那是答案），
                          此时整行不渲染——而不是显示「排序值 —」，
                          那会让玩家以为界面坏了。
                        */}
                        {card?.display ? (
                          <p className="flex flex-wrap gap-x-1 text-[length:var(--text-aux)] text-[var(--color-muted)]">
                            <span>{t("value")}</span><span className="uma-code whitespace-nowrap font-bold">{card.display}</span>
                          </p>
                        ) : null}
                      </div>
                    </div>
                  );
                })}
              </div>{isMerged ? <p className="mt-1 text-[length:var(--text-aux)] text-[var(--color-muted)]">{t("tiedNote",{label,count:group.length})}</p> : null}</td>

              {/*
                操作区：上下移动 + 并列/拆开。全部是按钮，键盘可达。
                三个按钮横排而不是竖排：竖排时每行被撑到 ~180px，
                手机 390 下 6 张卡要滚 1000px+ 才能排完，排序时看不到全貌。
                横排后每行降到约 100px，一屏能看到大部分卡，操作也集中在一处。
              */}
              {!readOnly ? <td className="uma-sort-table__actions"><div className="flex items-center justify-end gap-0.5">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={locked || index === 0}
                  onClick={() => onMoveUp(index)}
                  aria-label={t("moveUp",{label})}
                >
                  <ArrowUp aria-hidden="true" className="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={locked || index === draft.length - 1}
                  onClick={() => onMoveDown(index)}
                  aria-label={t("moveDown",{label})}
                >
                  <ArrowDown aria-hidden="true" className="size-4" />
                </Button>
                {isMerged ? (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    disabled={locked}
                    onClick={() => onSplit(index)}
                    aria-label={t("split",{label})}
                  >
                    <Unlink aria-hidden="true" className="size-4" />
                  </Button>
                ) : (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    disabled={locked || index === draft.length - 1}
                    onClick={() => onMerge(index)}
                    aria-label={t("merge",{label})}
                  >
                    <Link2 aria-hidden="true" className="size-4" />
                  </Button>
                )}
              </div></td> : null}
          </tr>
        );
      })}
      </tbody>
    </table>
    </div>
  );
}
