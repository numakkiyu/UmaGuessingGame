import type { SegmentRef } from "./use-event-data";
import { formatScoreUnits } from "@/lib/format-score";
import { useTranslation } from "@/i18n";
import "@/i18n/event-results";
import { EventSubjectName, eventSegmentLabel } from "./event-presentation";

export function EntertainmentResults({segments}:{segments:readonly SegmentRef[]}) {
 const {t}=useTranslation("eventResults"),items=segments.filter(item=>!item.competitive);
 if(!items.length)return null;
 return <section className="uma-panel min-w-0 space-y-4 p-4" aria-labelledby="entertainment-heading">
  <header><h2 id="entertainment-heading" className="text-lg font-bold">{t("fun")}</h2><p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("funIntro")}</p></header>
  {items.map((item,index)=><section key={item.segmentId} className="min-w-0 space-y-2 border-t border-[var(--color-line)] pt-3" aria-label={eventSegmentLabel(item,index)}>
   <h3 className="break-words font-semibold">{eventSegmentLabel(item,index)}</h3>
   <p className="text-sm text-[var(--color-muted)]">{t("funProgress",{done:item.completedRounds,total:item.roundCount??1})}{(item.status==="voided"||item.status==="void")?` · ${t("voided")}`:item.status!=="completed"?` · ${t("accumulating")}`:""}</p>
   {item.results.length?<>
    <table className="hidden w-full table-fixed text-left text-sm md:table"><caption className="sr-only">{t("funCaption",{name:eventSegmentLabel(item,index)})}</caption><thead><tr className="border-b border-[var(--color-line)] text-[var(--color-muted)]"><th className="py-2 font-medium">{t("team")}</th><th className="w-32 py-2 text-right font-medium">{t("itemScore")}</th></tr></thead><tbody>{item.results.map(row=><tr key={row.subjectId} className="border-b border-[var(--color-line)]"><th scope="row" className="break-words py-2 pr-3 font-semibold"><EventSubjectName subject={row}/></th><td className="py-2 text-right tabular-nums">{formatScoreUnits(row.scoreUnits)} / {formatScoreUnits(row.maxUnits)}</td></tr>)}</tbody></table>
    <ul className="space-y-2 md:hidden">{item.results.map(row=><li key={row.subjectId} className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-[var(--color-panel-soft)] p-3"><span className="min-w-0 break-words text-sm font-semibold"><EventSubjectName subject={row}/></span><span className="text-sm tabular-nums">{t("points",{points:`${formatScoreUnits(row.scoreUnits)} / ${formatScoreUnits(row.maxUnits)}`})}</span></li>)}</ul>
   </>:<p className="text-sm leading-6 text-[var(--color-muted)]">{t((item.status==="voided"||item.status==="void")?"funVoid":"funWaiting")}</p>}
  </section>)}
 </section>;
}
