import { useTranslation } from "@/i18n";
import { raceCopy, localizeRacePresentation } from "@/i18n/race";
import { raceName, raceCourse, raceDate } from "@/i18n/race-names";
import { cardName } from "@/i18n/entities";
import { aptitudeValue } from "@/i18n/aptitude-values";
import { useMemo, useState } from "react";
import { Check, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { SortBoard } from "@/features/modes/sort-board";
import { mergeWithNext, moveDown, moveUp, splitGroup, type SortDraft } from "@/lib/api/sort";
import { practiceFinished } from "@/lib/domain/aptitude-practice";
import { changeRace, raceCards, raceClues, raceResult, type RaceGame } from "@/lib/domain/race-practice";
import { createRaceGame, raceStore, readRacePack, validateRaceArchive, volatileRaceGames, type RaceArchive, type RacePack } from "@/lib/local/race-practice-store";
import { ActivityPracticeLoader, ActivityPracticeStart, PracticeGameHeading, PracticeInstructions, PracticeNotices, PracticeUtilityActions, useActivityPractice, type PracticeAdapter } from "./activity-practice-kit";
const rules = raceCopy["zh-Hans"].rules;
const raceAdapter: PracticeAdapter<RacePack, RaceGame> = localizeRacePresentation({
  modeId: "race-rebuild", title: "历史赛况复原", summary: "回到那场比赛，在收录参赛者中排出真实先后", hint: "每局 180 秒，可以凭记忆挑战，也可以跟着线索练习。名次以收录资料为准，不计排名", prepareLabel: "准备赛事资料", startLabel: "开始复原", filePrefix: "umaguess-race", rules,
  fetch: signal => api.getRacePracticePack(signal), read: raw => { const r = readRacePack(raw); return { pack: r.pack, media: r.races.flatMap(r => r.entries) }; }, validate: validateRaceArchive, create: pack => createRaceGame(pack), store: raceStore, volatile: volatileRaceGames,
});
export function RacePracticeStartPage() {
  const {t}=useTranslation("race");
  const [style, setStyle] = useState<RaceGame["style"]>("closed");
  return <ActivityPracticeStart adapter={raceAdapter} createGame={p => createRaceGame(p, style)} options={<fieldset className="space-y-2"><legend className="text-sm font-bold">{t("challenge")}</legend><div className="flex flex-wrap gap-x-5 gap-y-2">{([["closed", t("closedOption")], ["clues", t("cluesOption")]] as const).map(([value, label]) => <label key={value} className="inline-flex min-h-11 cursor-pointer items-center gap-2 text-sm"><input type="radio" name="race-style" value={value} checked={style === value} onChange={() => setStyle(value)} />{label}</label>)}</div></fieldset>} />;
}
export function RacePracticeGamePage() { return <ActivityPracticeLoader adapter={raceAdapter}>{archive => <RacePracticeGame initial={archive} />}</ActivityPracticeLoader>; }
function RacePracticeGame({ initial }: { initial: RaceArchive }) {
  const {t,i18n}=useTranslation("race");
  const p = useActivityPractice(raceAdapter, initial, pack => createRaceGame(pack, initial.game.style)), { game, now, busy, stale } = p;
  const race = useMemo(() => readRacePack(initial.pack).races.find(r => r.raceId === initial.game.raceId)!, [initial]);
  const cards = useMemo(() => raceCards(initial.game, race), [initial.game, race]);
  const finished = practiceFinished(game, now), result = finished ? raceResult(game, race) : null, locked = finished || busy || stale;
  const savedMatches = game.submitted !== null && JSON.stringify(game.submitted) === JSON.stringify(game.draft);
  const clues = useMemo(() => game.style === "clues" ? raceClues(race,i18n.resolvedLanguage) : [], [game.style, race,i18n.resolvedLanguage]);
  function draft(next: SortDraft) { void p.act((g, at) => changeRace(g, next, false, at)); }
  const controls = { draft: finished && game.submitted ? game.submitted : game.draft, cards, locked, positionKind: "position" as const, onChange: draft, onMoveUp: (i: number) => draft(moveUp(game.draft, i)), onMoveDown: (i: number) => draft(moveDown(game.draft, i)), onMerge: (i: number) => draft(mergeWithNext(game.draft, i)), onSplit: (i: number) => draft(splitGroup(game.draft, i)) };
  const missing = result ? Array.from({ length: Math.max(...result.ranked.map(e => e.finishRank)) }, (_, i) => i + 1).filter(rank => !result.ranked.some(e => e.finishRank === rank)) : [];
  return <div className="mx-auto w-full max-w-6xl space-y-4">
    <PracticeGameHeading title={raceAdapter.title} modeId={game.modeId} deadlineAt={game.deadlineAt} now={now} finished={finished} />
    <PracticeNotices saved={p.saved} stale={stale} message={p.message} />
    <section className="uma-panel relative overflow-hidden p-4 sm:p-5" aria-labelledby="race-facts"><div className="pointer-events-none absolute inset-0 bg-[url('/assets/ui/scenes/lobby-classroom.jpg')] bg-cover bg-center opacity-10" aria-hidden="true" /><div className="relative"><p className="text-sm font-semibold text-[var(--color-blue-deep)]">{t(game.style === "closed" ? "closed" : "clues")}</p><h2 id="race-facts" className="mt-1 text-xl font-bold">{raceName(race.name,undefined,{raceId:race.raceId,date:race.date})}</h2><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{raceDate(race.date)} · {[raceCourse(race.course), aptitudeValue("surface",race.surface), race.distance].filter(Boolean).join(" · ")}</p><p className="mt-1 text-sm leading-6">{t("question",{count:race.entries.length})}</p></div></section>
    {result ? <section className="uma-panel uma-reveal-row space-y-3 p-4 sm:p-5" aria-labelledby="race-practice-result"><h2 id="race-practice-result" className="text-xl font-bold">{t(result.scoreUnits === 10000 ? "complete" : "result")}</h2><p className="text-3xl font-bold text-[var(--color-action-green)]">{t("score",{score:result.scoreUnits/100})}</p><p className="text-sm leading-6 text-[var(--color-muted)]">{game.submitted ? t("savedScore",{correct:result.correct,total:result.total}) : t("unsavedScore")}</p><Button variant="primary" disabled={busy} onClick={() => void p.again()}><RotateCcw className="size-4" aria-hidden="true" />{t("again")}</Button></section> : null}
    <div className="grid min-w-0 items-start gap-4 lg:grid-cols-[minmax(0,1fr)_19rem]">
      <section className="uma-panel min-w-0 space-y-3 p-3 sm:p-5" aria-labelledby="race-practice-order"><h2 id="race-practice-order" className="text-lg font-bold">{t(finished ? game.submitted ? "scoredOrder" : "unsentDraft" : "arrange")}</h2><p className="text-sm leading-6 text-[var(--color-muted)]">{t(finished ? game.submitted ? "orderEnded" : "orderUnsent" : "orderHint")}</p><SortBoard {...controls} readOnly={finished} />{!finished ? <div className="space-y-2 border-t border-[var(--color-line)] pt-3"><Button variant="primary" size="primary" block disabled={locked || savedMatches} onClick={() => void p.act((g, at) => changeRace(g, g.draft, true, at))}><Check className="size-4" aria-hidden="true" />{t("save")}</Button><p role="status" className="text-sm leading-6 text-[var(--color-muted)]">{t(savedMatches ? "saved" : game.submitted ? "changed" : "unsaved")}</p></div> : null}</section>
      <aside className="min-w-0 space-y-4">{clues.length ? <section className="uma-card p-4" aria-labelledby="race-clues"><h2 id="race-clues" className="font-bold">{t("clueTitle")}</h2><ol className="mt-3 list-inside list-decimal space-y-2 text-sm leading-6">{clues.map(c => <li key={c}>{c}</li>)}</ol></section> : null}<PracticeInstructions rules={raceAdapter.rules} /><PracticeUtilityActions finished={finished} busy={busy} stale={stale} exportRecord={p.exportRecord} finish={p.finish} /></aside>
    </div>
    {result ? <section className="uma-panel min-w-0 space-y-3 p-3 sm:p-5" aria-labelledby="race-recorded-ranks"><h2 id="race-recorded-ranks" className="text-lg font-bold">{t("recordedRanks")}</h2>{missing.length ? <p className="text-sm leading-6 text-[var(--color-muted)]">{t("missingRanks",{ranks:missing.join("、")})}</p> : null}<div className="hidden md:block"><table className="w-full border-collapse text-left text-sm"><caption className="sr-only">{t("caption",{date:raceDate(race.date),name:raceName(race.name,undefined,{raceId:race.raceId,date:race.date})})}</caption><thead><tr className="border-b border-[var(--color-line)]"><th scope="col" className="px-3 py-2">{t("rankHeading")}</th><th scope="col" className="px-3 py-2">{t("participant")}</th><th scope="col" className="px-3 py-2">{t("note")}</th></tr></thead><tbody>{result.ranked.map(e => <tr key={e.characterId} className="border-b border-[var(--color-line)] last:border-0"><th scope="row" className="px-3 py-3 text-lg text-[var(--color-action-green)]">{e.finishRank}</th><td className="px-3 py-3"><span className="flex items-center gap-2"><img src={e.imagePath} alt="" className="size-10 object-contain" loading="lazy" />{cardName(e)}</span></td><td className="px-3 py-3">{result.ranked.filter(other => other.finishRank === e.finishRank).length > 1 ? t("tied") : ""}</td></tr>)}</tbody></table></div><ol className="space-y-2 md:hidden">{result.ranked.map(e => <li key={e.characterId} className="flex min-w-0 items-center gap-2 rounded-lg border border-[var(--color-line)] p-3"><span className="w-16 shrink-0 whitespace-nowrap text-center text-sm font-bold text-[var(--color-action-green)]">{t("rank",{rank:e.finishRank})}</span><img src={e.imagePath} alt="" className="size-10 shrink-0 object-contain" loading="lazy" /><div className="min-w-0"><p className="break-words text-sm font-bold">{cardName(e)}</p>{result.ranked.filter(other => other.finishRank === e.finishRank).length > 1 ? <p className="text-xs text-[var(--color-muted)]">{t("tied")}</p> : null}</div></li>)}</ol>{race.excludedCount ? <p className="text-sm leading-6 text-[var(--color-muted)]">{t("excluded",{count:race.excludedCount})}</p> : null}</section> : null}
  </div>;
}
