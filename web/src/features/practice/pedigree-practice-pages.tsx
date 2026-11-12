import { SelectField } from "@/components/ui/select";
import { useTranslation } from "@/i18n";
import { pedigreeCopy, pedigreeSlot, localizePedigreePresentation } from "@/i18n/pedigree";
import { characterName } from "@/i18n/entities";
import { useMemo, useRef, useState } from "react";
import { RotateCcw, Save, Search, X } from "lucide-react";
import { AvatarImage } from "@/components/uma/avatar-image";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import { PEDIGREE_SLOTS, changePedigree, pedigreeResult, type PedigreeGame, type PedigreeSlotId } from "@/lib/domain/pedigree-practice";
import { practiceFinished } from "@/lib/domain/aptitude-practice";
import { createPedigreeGame, pedigreeStore, readPedigreePack, validatePedigreeArchive, volatilePedigreeGames, type PedigreeArchive, type PedigreePack } from "@/lib/local/pedigree-practice-store";
import { cn } from "@/lib/utils";
import { ActivityPracticeLoader, ActivityPracticeStart, PracticeGameHeading, PracticeInstructions, PracticeNotices, PracticeUtilityActions, useActivityPractice, type PracticeAdapter } from "./activity-practice-kit";
const rules = pedigreeCopy["zh-Hans"].rules;
const adapter: PracticeAdapter<PedigreePack, PedigreeGame> = localizePedigreePresentation({
  modeId: "pedigree", title: "原型血统拼图", summary: "沿着父系、母系两条分支，把史实原型马的祖先放回对应的位置", hint: "180 秒，可以查资料卡。先填空位，再保存这版作答；截止前还可以调整", prepareLabel: "准备原型资料", startLabel: "开始血统拼图", filePrefix: "umaguess-pedigree", rules,
  fetch: signal => api.getPedigreePracticePack(signal), read: raw => { const { pack, library } = readPedigreePack(raw); return { pack, media: library.families.map(f => ({ imagePath: `/assets/avatars/${f.subject.characterId}.webp` })) }; }, validate: validatePedigreeArchive, create: p => createPedigreeGame(p), store: pedigreeStore, volatile: volatilePedigreeGames,
});
export function PedigreePracticeStartPage() {
  const { t } = useTranslation("pedigree");
  const [count, setCount] = useState(4);
  return <ActivityPracticeStart adapter={adapter} createGame={p => createPedigreeGame(p, count)} options={<div className="space-y-2"><Label htmlFor="pedigree-count">{t("count")}</Label><SelectField id="pedigree-count" value={count} onValueChange={value => setCount(Number(value))} options={[...[4, 5, 6].map(n => ({value:n,label:t("countOption", { count: n, ancestors: n - 2 })}))]} /></div>} />;
}
export function PedigreePracticeGamePage() { return <ActivityPracticeLoader adapter={adapter}>{archive => <PedigreePracticeGame initial={archive} />}</ActivityPracticeLoader>; }
function PedigreePracticeGame({ initial }: { initial: PedigreeArchive }) {
  const { t } = useTranslation("pedigree");
  const p = useActivityPractice(adapter, initial, pack => createPedigreeGame(pack, initial.game.slotIds.length)), { game, now, busy, stale, act } = p;
  const { library } = useMemo(() => readPedigreePack(initial.pack), [initial.pack]);
  const family = library.families.find(f => f.subject.characterId === game.characterId)!;
  const horses = useMemo(() => new Map(library.horses.map(h => [h.horseId, h])), [library]);
  const candidateHeading = useRef<HTMLHeadingElement>(null), treeHeading = useRef<HTMLHeadingElement>(null);
  const [active, setActive] = useState<PedigreeSlotId>(game.slotIds[0]!), [query, setQuery] = useState("");
  const finished = practiceFinished(game, now), locked = finished || busy || stale, result = finished ? pedigreeResult(game, family) : null;
  const draft = new Map(game.draft.map(a => [a.slotId, a.horseId]));
  const same = JSON.stringify(game.draft) === JSON.stringify(game.submitted ?? []);
  const activeSlot = pedigreeSlot(active);
  const name = (id: string | null) => id ? horses.get(id)?.name ?? t("unselected") : t("unfilled");
  const visible = game.candidateIds.map(id => horses.get(id)!).filter(h => h.name.normalize("NFKC").toLocaleLowerCase().includes(query.normalize("NFKC").trim().toLocaleLowerCase()));
  async function assign(horseId: string) {
    const next = [...game.draft.filter(a => a.slotId !== active), { slotId: active, horseId }];
    if (await act((g, at) => changePedigree(g, next, false, at))) { const remaining = game.slotIds.find(id => !next.some(a => a.slotId === id)); if (remaining) setActive(remaining); }
  }
  function chooseSlot(id: PedigreeSlotId) {
    setActive(id);
    if (window.matchMedia("(max-width: 1279px)").matches) requestAnimationFrame(() => {
      candidateHeading.current?.focus({ preventScroll: true });
      candidateHeading.current?.scrollIntoView({ block: "start", behavior: "instant" });
    });
  }
  function saveButton(className?: string) { return <Button className={className} variant="primary" size="primary" block disabled={locked || !game.draft.length || same} onClick={() => void act((g, at) => changePedigree(g, g.draft, true, at))}><Save className="size-4" aria-hidden="true" />{t("save")}</Button>; }
  function slotCard(id: PedigreeSlotId) {
    const spec = { ...PEDIGREE_SLOTS.find(s => s.slotId === id)!, ...pedigreeSlot(id) }, selected = result ? result.rows.find(r => r.slotId === id)!.selected : draft.get(id) ?? null;
    const row = result?.rows.find(r => r.slotId === id);
    return <div key={id} className="min-w-0" data-pedigree-slot={id}><button type="button" aria-pressed={!finished && active === id} aria-label={t("fillSlot", { slot: spec.label, name: name(selected) })} disabled={locked} onClick={() => chooseSlot(id)} className={cn("min-h-24 w-full rounded-lg border-2 p-3 text-left transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)] disabled:cursor-default", row ? row.correct ? "border-[var(--color-action-green)] bg-[var(--color-brand-green-soft)]" : "border-[var(--color-line)] bg-[var(--color-panel-soft)]" : active === id ? "border-[var(--color-blue-deep)] bg-[var(--color-panel-soft)]" : "border-[var(--color-line)] bg-[var(--color-panel)]")}><span className="flex items-baseline justify-between gap-2"><span className="font-bold text-[var(--color-ink)]">{spec.label}</span><span className="text-xs text-[var(--color-muted)]">{t(spec.generation === 1 ? "firstGeneration" : "secondGeneration")}</span></span><span className="mt-1 block text-xs leading-5 text-[var(--color-muted)]">{spec.means}</span><span className="mt-2 block break-words text-sm font-semibold">{name(selected)}</span>{row ? <span className="mt-1 block text-xs font-bold">{t(row.correct ? "correct" : row.selected ? "incorrect" : "unanswered")}</span> : null}</button>{!finished && selected ? <Button variant="ghost" size="sm" disabled={locked} aria-label={t("clearSlot", { slot: spec.label })} onClick={() => void act((g, at) => changePedigree(g, g.draft.filter(a => a.slotId !== id), false, at))}><X className="size-3" aria-hidden="true" />{t("clear")}</Button> : null}{row && !row.correct ? <p className="mt-2 break-words text-sm leading-6 text-[var(--color-muted)]">{t("expected", { name: name(row.expected) })}</p> : null}</div>;
  }
  return <div className="uma-stack mx-auto max-w-[1280px]">
    <PracticeGameHeading title={adapter.title} modeId="pedigree" deadlineAt={game.deadlineAt} now={now} finished={finished} />
    <p className="rounded-lg border border-[var(--color-line)] bg-[var(--color-panel-soft)] px-4 py-3 text-sm leading-6">{t("disclaimer")}</p>
    <PracticeNotices saved={p.saved} stale={stale} message={p.message} />
    {result ? <section className="uma-panel uma-reveal-row space-y-3 p-4 sm:p-5" aria-labelledby="pedigree-result"><h2 id="pedigree-result" className="text-xl font-bold">{t(result.correct === result.total ? "complete" : "result")}</h2><p className="text-xl font-bold text-[var(--color-action-green)]">{t("score", { score: result.scoreUnits / 100, correct: result.correct, total: result.total })}</p><p className="text-sm leading-6 text-[var(--color-muted)]">{t(game.submitted ? "savedResult" : "unsavedResult")}</p><Button variant="primary" disabled={busy} onClick={() => void p.again()}><RotateCcw className="size-4" aria-hidden="true" />{t("again")}</Button></section> : null}
    <div className="grid min-w-0 items-start gap-5 xl:grid-cols-[minmax(0,1fr)_22rem]"><section className="uma-panel min-w-0 p-3 sm:p-5" aria-labelledby="pedigree-tree">
      <h2 ref={treeHeading} tabIndex={-1} id="pedigree-tree" className="scroll-mt-20 text-lg font-bold">{t(finished ? "checkBranches" : "arrange")}</h2>
      <div className="mx-auto mt-4 flex max-w-md items-center gap-3 rounded-xl border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3"><AvatarImage size="lg" src={`/assets/avatars/${family.subject.characterId}.webp`} alt="" /><div className="min-w-0"><p className="text-xs font-bold text-[var(--color-muted)]">{t("prototypeOf", { name: characterName({ displayName: family.subject.characterName, localizedNames: family.subject.localizedNames }) })}</p><h3 className="mt-1 break-words text-lg font-bold">{family.subject.prototypeName}</h3></div></div>
      <div className="mx-auto h-5 w-px bg-[var(--color-line)]" aria-hidden="true" />
      <div className="grid min-w-0 gap-5 sm:grid-cols-2">{(["sire", "dam"] as const).map(branch => <section key={branch} className="min-w-0 border-t-2 border-[var(--color-line)] pt-3" aria-label={t(branch === "sire" ? "sireBranch" : "damBranch")}><h3 className="mb-3 text-sm font-bold text-[var(--color-blue-deep)]">{t(branch === "sire" ? "sireBranch" : "damBranch")}</h3>{slotCard(branch)}<div className="ml-3 mt-3 space-y-3 border-l-2 border-[var(--color-line)] pl-3">{PEDIGREE_SLOTS.filter(s => s.branch === branch && s.generation === 2 && game.slotIds.includes(s.slotId)).map(s => slotCard(s.slotId))}</div></section>)}</div>
      {!finished ? <div className="mt-4 space-y-3 border-t border-[var(--color-line)] pt-4"><p className="text-sm leading-6" role="status">{t("draftCount", { filled: game.draft.length, total: game.slotIds.length })} {t(game.submitted ? same ? "savedCount" : "changedCount" : "unsaved", { saved: game.submitted?.length ?? 0 })}</p>{saveButton("hidden xl:inline-flex")}<p className="text-xs leading-5 text-[var(--color-muted)]">{t("saveHint")}</p></div> : null}
    </section><aside className="min-w-0 space-y-4">
      {!finished ? <section className="uma-panel space-y-3 p-3 sm:p-5" aria-labelledby="pedigree-candidates"><h2 ref={candidateHeading} tabIndex={-1} id="pedigree-candidates" className="scroll-mt-20 text-lg font-bold">{t("pickFor", { slot: activeSlot.label })}</h2><p className="text-sm leading-6 text-[var(--color-muted)]">{t("pickHint", { means: activeSlot.means })}</p><Label htmlFor="pedigree-search" className="sr-only">{t("search")}</Label><div className="relative"><Search className="pointer-events-none absolute left-3 top-3 size-4 text-[var(--color-muted)]" aria-hidden="true" /><Input id="pedigree-search" placeholder={t("search")} className="pl-9" value={query} onChange={e => setQuery(e.target.value)} /></div>
        <div className="hidden md:block"><table className="w-full table-fixed text-left text-sm"><caption className="sr-only">{t("candidatesCaption")}</caption><thead><tr className="border-b border-[var(--color-line)]"><th scope="col" className="py-2 pr-2">{t("candidate")}</th><th scope="col" className="w-24 py-2">{t("assignment")}</th></tr></thead><tbody>{visible.map(h => { const used = game.draft.find(a => a.horseId === h.horseId), label = used ? pedigreeSlot(used.slotId).label : ""; return <tr key={h.horseId} className="border-b border-[var(--color-line)] last:border-0"><th scope="row" className="break-words py-3 pr-2 font-medium">{h.name}</th><td className="py-2"><Button variant="secondary" size="sm" className="h-auto w-full whitespace-normal break-words leading-5" aria-label={t("selectName", { name: h.name })} disabled={locked || Boolean(used)} onClick={() => void assign(h.horseId)}>{used ? t("used", { slot: label }) : t("select")}</Button></td></tr>; })}</tbody></table></div>
        <ul className="space-y-2 md:hidden" aria-label={t("candidate")}>{visible.map(h => { const used = game.draft.find(a => a.horseId === h.horseId), label = used ? pedigreeSlot(used.slotId).label : ""; return <li key={h.horseId}><button type="button" aria-label={t("selectName", { name: h.name })} disabled={locked || Boolean(used)} onClick={() => void assign(h.horseId)} className="flex min-h-12 w-full items-center justify-between gap-3 rounded-lg border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3 text-left text-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)] disabled:cursor-default"><span className="min-w-0 break-words font-semibold">{h.name}</span><span className="shrink-0 text-xs text-[var(--color-muted)]">{used ? t("used", { slot: label }) : t("select")}</span></button></li>; })}</ul>{!visible.length ? <p className="text-sm text-[var(--color-muted)]">{t("noMatch")}</p> : null}
        <div className="space-y-2 border-t border-[var(--color-line)] pt-3 xl:hidden"><p role="status" className="text-sm leading-6">{t("draftCount", { filled: game.draft.length, total: game.slotIds.length })} {t(game.submitted ? same ? "savedCount" : "changedCount" : "unsaved", { saved: game.submitted?.length ?? 0 })}</p>{saveButton()}<Button variant="ghost" block className="whitespace-normal" onClick={() => { treeHeading.current?.focus({ preventScroll: true }); treeHeading.current?.scrollIntoView({ block: "start", behavior: "instant" }); }}>{t("back")}</Button></div>
      </section> : null}
      <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{t("reference")}</summary><p className="mt-3 text-xs leading-5 text-[var(--color-muted)]">{t("disclaimer")}</p><dl className="mt-3 divide-y divide-[var(--color-line)]">{PEDIGREE_SLOTS.map(s => <div key={s.slotId} className="py-2 text-sm leading-6"><dt className="font-bold">{pedigreeSlot(s.slotId).label} · {pedigreeSlot(s.slotId).means}</dt><dd className="break-words text-[var(--color-muted)]">{name(family.relations[s.slotId])}</dd></div>)}</dl><p className="mt-3 text-xs leading-5 text-[var(--color-muted)]">{t("source")}</p></details>
      <PracticeInstructions rules={adapter.rules} /><PracticeUtilityActions finished={finished} busy={busy} stale={stale} exportRecord={p.exportRecord} finish={p.finish} />
    </aside></div>
  </div>;
}
