import { GameStage } from "@/components/uma/game-stage";
import "@/i18n/aptitude";
import { useTranslation } from "@/i18n";
import { cardName, cardMatches } from "@/i18n/entities";
import { aptitudeValue } from "@/i18n/aptitude-values";
import { useMemo, useRef, useState } from "react";
import { Check, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { BoardCell } from "@/features/modes/grid-board-cell";
import type { GridCell } from "@/lib/api/aptitude-grid";
import { claimGrid, gridConditions, gridFinished, gridSolution, type AptitudeCard } from "@/lib/domain/aptitude-practice";
import { readGridPack, type GridArchive } from "@/lib/local/aptitude-practice-store";
import { cn } from "@/lib/utils";
import { gridAdapter } from "./aptitude-adapters";
import { ActivityPracticeLoader, ActivityPracticeStart, PracticeGameHeading, PracticeInstructions, PracticeNotices, PracticeUtilityActions, useActivityPractice } from "./activity-practice-kit";

export function GridPracticeStartPage() { return <ActivityPracticeStart adapter={gridAdapter} />; }
export function GridPracticeGamePage() { return <ActivityPracticeLoader adapter={gridAdapter}>{archive => <GridPracticeGame initial={archive} />}</ActivityPracticeLoader>; }

function GridPracticeGame({ initial }: { initial: GridArchive }) {
  const {t}=useTranslation("aptitude",{keyPrefix:"ui"});
  const p = useActivityPractice(gridAdapter, initial), { game, now, busy, stale } = p;
  const cards = useMemo(() => { const all = readGridPack(initial.pack).cards; return initial.game.cardIds.map(id => all.find(c => c.id === id)!); }, [initial]);
  const [selected, setSelected] = useState<number | null>(null), [picked, setPicked] = useState<string | null>(null), [open, setOpen] = useState(false);
  const boardHeading = useRef<HTMLHeadingElement>(null);
  const finished = gridFinished(game, now), disabled = busy || stale || finished;
  const used = new Set(game.claims.map(c => c.characterId));
  // The reference answer is only computed and rendered once play is over.
  const solution = useMemo(() => finished ? gridSolution(cards, game.rowAxis) : null, [cards, game.rowAxis, finished]);
  const cells: GridCell[] = Array.from({ length: 9 }, (_, index) => {
    const row = Math.floor(index / 3), col = index % 3, condition = gridConditions(game.rowAxis, index), claim = game.claims.find(c => c.cellIndex === index), card = cards.find(c => c.id === claim?.characterId);
    return { index, row, col, cellId: String(index), rowCondition: { axis: game.rowAxis, value: game.rowAxis === "distance" ? condition.distance : condition.style }, colCondition: { axis: game.rowAxis === "distance" ? "style" : "distance", value: game.rowAxis === "distance" ? condition.style : condition.distance }, claim: claim && card ? { subjectId: "practice", characterId: card.id, name: card.name, localizedNames: card.localizedNames, imagePath: card.imagePath, turn: game.claims.indexOf(claim) + 1, satisfied: true } : null };
  });
  function choose(cell: GridCell) { setSelected(cell.index); setPicked(null); if (window.matchMedia("(max-width: 1023px)").matches) setOpen(true); }
  async function confirm() { if (selected === null || !picked) return; if (await p.act((g, at) => claimGrid(g, cards, selected, picked, at))) { setOpen(false); setSelected(null); setPicked(null); boardHeading.current?.focus(); } }
  const condition = selected === null ? null : gridConditions(game.rowAxis, selected);
  const picker = <GridPracticePicker cards={cards.filter(c => !used.has(c.id))} selected={picked} onPick={setPicked} disabled={disabled || selected === null} />;
  const confirmButton = <Button variant="primary" block disabled={disabled || !picked || selected === null} onClick={() => void confirm()}><Check className="size-4" aria-hidden="true" />{t("confirmCell")}</Button>;
  return <GameStage mode="aptitude-grid" className="uma-stack">
    <PracticeGameHeading title={gridAdapter.title} modeId={game.modeId} deadlineAt={game.deadlineAt} now={now} finished={finished} />
    <PracticeNotices saved={p.saved} stale={stale} message={p.message} />
    {finished ? <section className="uma-panel space-y-3 p-4 sm:p-5" aria-labelledby="grid-result"><h2 id="grid-result" className="text-xl font-bold">{t(game.claims.length === 9 ? "gridComplete" : "gridResult")}</h2><p className="text-3xl font-bold text-[var(--color-action-green)]">{Math.floor(game.claims.length * 100 / 9)}<span className="ml-1 text-base">{t("points")}</span></p><p className="text-sm text-[var(--color-muted)]">{t("cellsResult",{count:game.claims.length})}</p><Button variant="primary" disabled={busy} onClick={() => void p.again()}><RotateCcw className="size-4" aria-hidden="true" />{t("again")}</Button></section> : null}
    <div className="grid min-w-0 items-start gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <section className="uma-panel min-w-0 p-3 sm:p-5" aria-labelledby="grid-board-heading"><div className="mb-3 flex flex-wrap items-center justify-between gap-2"><h2 id="grid-board-heading" tabIndex={-1} ref={boardHeading} className="font-bold">{t(finished ? "boardEnded" : "board")}</h2><p className="text-sm font-semibold text-[var(--color-muted)]">{t("occupied",{count:game.claims.length})}</p></div><p className="mb-3 text-sm leading-6 text-[var(--color-muted)]">{t("boardHint")}</p><div className="mx-auto grid max-w-xl grid-cols-3 gap-2" aria-label={t("boardLabel")}>{cells.map(cell => <BoardCell key={cell.index} cell={cell} selected={selected === cell.index} disabled={disabled} onSelect={choose} showConditions />)}</div></section>
      {!finished ? <section className="uma-panel hidden min-w-0 space-y-3 p-4 lg:block" aria-labelledby="grid-candidates"><h2 id="grid-candidates" className="font-bold">{t("candidates")}</h2><p className="text-sm leading-6 text-[var(--color-muted)]">{condition ? t("selectedCondition", {distance:aptitudeValue("distance",condition.distance),style:aptitudeValue("style",condition.style)}) : t("chooseCell")}</p>{picker}{confirmButton}</section> : null}
    </div>
    {finished && solution ? <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{t("gridReference")}</summary><p className="mt-3 text-sm leading-6 text-[var(--color-muted)]">{t("gridReferenceHint")}</p><ol className="mt-3 grid gap-2 sm:grid-cols-3">{solution.map((id, i) => { const c = cards.find(c => c.id === id)!, cond = gridConditions(game.rowAxis, i); return <li key={i} className="flex min-w-0 items-center gap-2 rounded-lg border border-[var(--color-line)] p-2"><img src={c.imagePath} alt="" className="size-10 shrink-0 object-contain" loading="lazy" /><div className="min-w-0"><p className="text-xs text-[var(--color-muted)]">{t("condition",{distance:aptitudeValue("distance",cond.distance),style:aptitudeValue("style",cond.style)})}</p><p className="break-words text-sm font-bold">{cardName(c)}</p></div></li>; })}</ol></details> : null}
    <PracticeUtilityActions finished={finished} busy={busy} stale={stale} exportRecord={p.exportRecord} finish={p.finish} />
    <PracticeInstructions rules={gridAdapter.rules} />
    <Sheet open={open && !finished} onOpenChange={setOpen}><SheetContent closeLabel={t("close")} footer={<div className="space-y-2"><PracticeNotices saved={p.saved} stale={stale} message={p.message} />{picked ? <p className="truncate text-sm font-semibold">{cardName(cards.find(c => c.id === picked)!)}</p> : null}{confirmButton}</div>} onCloseAutoFocus={e => { e.preventDefault(); boardHeading.current?.focus(); }}><SheetHeader><SheetTitle>{t("cellPicker")}</SheetTitle><SheetDescription>{condition ? t("cellPickerHint",{distance:aptitudeValue("distance",condition.distance),style:aptitudeValue("style",condition.style)}) : t("cellPickerEmpty")}</SheetDescription></SheetHeader>{picker}</SheetContent></Sheet>
  </GameStage>;
}
function GridPracticePicker({ cards, selected, onPick, disabled }: { cards: readonly AptitudeCard[]; selected: string | null; onPick: (id: string) => void; disabled: boolean }) {
  const {t}=useTranslation("aptitude",{keyPrefix:"ui"});
  const [query, setQuery] = useState("");
  const matching = cards.filter(c => cardMatches(c,query));
  return <div className="space-y-3"><label className="block text-sm font-semibold">{t("search")}<Input className="mt-1" value={query} onChange={e => setQuery(e.target.value)} placeholder={t("searchPlaceholder")} autoComplete="off" inputMode="search" /></label><p className="text-xs text-[var(--color-muted)]">{t("unused",{count:cards.length})}</p><div className="grid grid-cols-2 gap-2 lg:max-h-[28rem] lg:overflow-y-auto">{matching.map(c => <button key={c.id} type="button" aria-pressed={selected === c.id} disabled={disabled} onClick={() => onPick(c.id)} className={cn("flex min-h-16 min-w-0 items-center gap-2 rounded-lg border-2 p-2 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]", selected === c.id ? "border-[var(--color-action-green)] bg-[var(--color-brand-green-soft)]" : "border-[var(--color-line)] hover:bg-[var(--color-panel-soft)]")}><img src={c.imagePath} alt="" className="size-9 shrink-0 object-contain" loading="lazy" decoding="async" /><span className="min-w-0 break-words text-sm font-bold">{cardName(c)}</span></button>)}</div>{!matching.length ? <p className="text-sm text-[var(--color-muted)]">{t("noResult")}</p> : null}</div>;
}
