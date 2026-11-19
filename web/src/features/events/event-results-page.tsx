/** Official standings are read-only projections. Locale changes presentation only. */
import { useState } from "react";
import { Link, useParams } from "react-router";
import { Loader2, RotateCcw, Trophy } from "lucide-react";
import { EmptyView, ErrorView, ForbiddenView, LoadingBlock } from "@/components/uma/page-state";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { describeFailure } from "@/lib/api/client";
import { cn } from "@/lib/utils";
import { useEventData, type SegmentRef, type StandingItem } from "./use-event-data";
import { EntertainmentResults } from "./entertainment-results";
import { EventSubjectName, EventTieArrangement, eventSegmentLabel } from "./event-presentation";
import { formatScoreUnits as score } from "@/lib/format-score";
import { useTranslation } from "@/i18n";
import "@/i18n/event-results";

function rankTone(rank:number):string {return rank===1?"bg-[var(--color-gold)] text-[var(--color-ink-strong)]":"bg-[var(--color-panel-soft)] text-[var(--color-ink)]";}
export function EventResultsPage(){
 const {t}=useTranslation("eventResults"),{eventId}=useParams<{eventId:string}>();
 const {event,standings,segments,isLoading,isError,error,refetch}=useEventData(eventId??null);
 const [refreshing,setRefreshing]=useState(false);
 async function refresh(){if(refreshing)return;setRefreshing(true);try{await refetch();}finally{setRefreshing(false);}}
 if(!eventId)return <ErrorView failure={{kind:"error",title:t("missingTitle"),detail:t("missingDetail"),diagnosticId:null,retryable:false}}/>;
 if(isLoading)return <LoadingBlock label={t("loading")} rows={4}/>;
 if(isError){const info=describeFailure(error);return info.kind==="forbidden"?<ForbiddenView failure={info} onRetry={()=>void refresh()}/>:<ErrorView failure={info} onRetry={()=>void refresh()}/>;}
 if(!event)return <EmptyView title={t("missingEvent")} detail={t("missingEventDetail")} action={<Button asChild variant="secondary"><Link to="/">{t("home")}</Link></Button>}/>;
 const items=standings?.items??[];
 const status=["draft","lobby","running","completed","cancelled"].includes(event.status)?t(event.status):t("unknown");
 return <div className="uma-stack">
  <section className="uma-scene overflow-hidden rounded-[var(--radius-panel-lg)] border border-[var(--color-line)] bg-[var(--color-panel)] shadow-[var(--shadow-panel)]">
   <div className="uma-scene__image" style={{backgroundImage:"url(/assets/ui/scenes/results-riverside.jpg)"}} aria-hidden="true"/><div className="uma-scene__veil" aria-hidden="true"/>
   <div className="relative p-5 sm:p-6 lg:p-8"><p className="inline-flex items-center gap-2 rounded-full bg-[var(--color-brand-green-soft)] px-3 py-1 text-xs font-bold">{t("badge")}</p><h1 className="u-fluid-title mt-3">{event.name||t("title")}</h1><p className="mt-1 text-sm text-[var(--color-muted)]">{status}{items.length?` · ${t("entrants",{count:items.length})}`:""}</p><EventTieArrangement event={event}/></div>
  </section>
  {event.status==="cancelled"?<Alert tone="warning" title={status}><p>{t("cancelledDetail")}</p></Alert>:event.status!=="completed"?<p role="status" className="text-sm leading-6 text-[var(--color-muted)]">{t("liveDetail")}</p>:null}
  <section className="uma-panel p-4" aria-labelledby="results-heading"><div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><h2 id="results-heading" className="text-lg font-bold">{t("results")}</h2><p className="mt-1 text-sm text-[var(--color-muted)]">{t("intro")}</p></div><Button variant="secondary" disabled={refreshing} onClick={()=>void refresh()}>{refreshing?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:<RotateCcw aria-hidden="true" className="size-4"/>}{t("refresh")}</Button></div></section>
  {!items.length?<EmptyView title={t("emptyTitle")} detail={t("emptyDetail")} action={event.roomId?<Button asChild variant="secondary"><Link to={`/rooms/${encodeURIComponent(event.roomId)}`}>{t("room")}</Link></Button>:undefined}/>:<Tabs defaultValue="overall"><TabsList><TabsTrigger value="overall">{t("overall")}</TabsTrigger><TabsTrigger value="segments">{t("detail")}</TabsTrigger></TabsList><TabsContent value="overall"><OverallTable items={items}/></TabsContent><TabsContent value="segments"><SegmentDetail items={items} segments={segments}/></TabsContent></Tabs>}
  <EntertainmentResults segments={segments}/>
  {standings&&standings.correctionIds.length>0?<Alert tone="info" title={t("corrected")}><p>{t("correctedDetail",{count:standings.correctionIds.length})}</p></Alert>:null}
  <div className="flex flex-col gap-2 border-t border-[var(--color-line)] pt-4 sm:flex-row"><Button asChild variant="secondary" size="primary"><Link to="/">{t("home")}</Link></Button><Button asChild variant="secondary" size="primary"><Link to={`/events/${encodeURIComponent(eventId)}/display`}>{t("screen")}</Link></Button></div>
 </div>;
}
function OverallTable({items}:{items:readonly StandingItem[]}){
 const {t}=useTranslation("eventResults");
 return <section aria-label={t("overallRegion")} className="mt-3 min-w-0">
  <div className="hidden rounded-[var(--radius-card)] border border-[var(--color-line)] md:block"><table className="w-full table-fixed border-collapse text-left text-sm"><caption className="sr-only">{t("overallCaption",{count:items.length})}</caption><thead className="bg-[var(--color-panel-soft)]"><tr><th className="w-20 px-3 py-2">{t("rank")}</th><th className="px-3 py-2">{t("team")}</th><th className="w-24 px-3 py-2 text-right">{t("total")}</th><th className="w-20 px-3 py-2 text-right">{t("itemCount")}</th></tr></thead><tbody>{items.map(item=><tr key={item.subjectId} className="border-t border-[var(--color-line)]"><td className="px-3 py-2"><span aria-label={t("rankValue",{rank:item.rank})} className={cn("inline-flex items-center rounded-full px-2 py-0.5 font-bold",rankTone(item.rank))}>{item.rank===1?<Trophy aria-hidden="true" className="mr-1 size-3.5"/>:null}{item.rank>0?item.rank:"—"}</span></td><th scope="row" className="break-words px-3 py-2 font-semibold"><EventSubjectName subject={item}/></th><td className="px-3 py-2 text-right tabular-nums font-bold">{score(item.scoreUnits)}</td><td className="px-3 py-2 text-right">{item.segments.length}</td></tr>)}</tbody></table></div>
  <ol className="space-y-2 md:hidden">{items.map(item=><li key={item.subjectId} className="uma-panel min-w-0 p-3"><div className="flex flex-wrap items-center justify-between gap-2"><span className={cn("rounded-full px-2 py-1 text-sm font-bold",rankTone(item.rank))}>{item.rank>0?t("rankValue",{rank:item.rank}):"—"}</span><span className="text-sm font-bold tabular-nums">{t("points",{points:score(item.scoreUnits)})}</span></div><h3 className="mt-2 break-words font-semibold"><EventSubjectName subject={item}/></h3><p className="mt-1 text-sm text-[var(--color-muted)]">{t("itemCount")}：{item.segments.length}</p></li>)}</ol>
 </section>;
}
function SegmentDetail({items,segments}:{items:readonly StandingItem[];segments:readonly SegmentRef[]}){
 const {t}=useTranslation("eventResults");
 const columns=[...new Set([...segments.filter(s=>s.competitive).map(s=>s.segmentId),...items.flatMap(item=>item.segments.map(s=>s.segmentId))])];
 const label=(id:string)=>{const index=segments.findIndex(s=>s.segmentId===id);return index>=0?eventSegmentLabel(segments[index]!,index):t("fallbackItem");};
 if(!columns.length)return <EmptyView title={t("noSegments")} detail={t("noSegmentsDetail")} className="mt-3"/>;
 return <div className="mt-3 min-w-0 space-y-3">
  {segments.some(s=>!s.name)?<Alert tone="info" title={t("unnamedItems")}><p>{t("unnamedItemsDetail")}</p></Alert>:null}
  <div className="uma-scroll-x hidden rounded-[var(--radius-card)] border border-[var(--color-line)] md:block" tabIndex={0} role="region" aria-label={t("segmentRegion")}><table className="w-full min-w-[40rem] border-collapse text-left text-sm"><caption className="sr-only">{t("segmentCaption")}</caption><thead className="bg-[var(--color-panel-soft)]"><tr><th className="sticky left-0 z-[1] bg-[var(--color-panel-soft)] px-3 py-2">{t("team")}</th>{columns.map(id=><th key={id} className="min-w-28 px-3 py-2 text-right">{label(id)}</th>)}<th className="px-3 py-2 text-right">{t("sum")}</th></tr></thead><tbody>{items.map(item=><tr key={item.subjectId} className="border-t border-[var(--color-line)]"><th scope="row" className="sticky left-0 z-[1] max-w-48 break-words bg-[var(--color-panel)] px-3 py-2"><EventSubjectName subject={item}/></th>{columns.map(id=>{const row=item.segments.find(s=>s.segmentId===id);return <td key={id} className="px-3 py-2 text-right tabular-nums">{row?`${score(row.scoreUnits)} / ${score(row.maxUnits)}`:"—"}</td>;})}<td className="px-3 py-2 text-right font-bold tabular-nums">{score(item.scoreUnits)}</td></tr>)}</tbody></table></div>
  <ul className="space-y-2 md:hidden">{items.map(item=><li key={item.subjectId} className="uma-panel min-w-0 p-3"><h3 className="break-words font-semibold"><EventSubjectName subject={item}/></h3><dl className="mt-2 space-y-2">{columns.map(id=>{const row=item.segments.find(s=>s.segmentId===id);return <div key={id} className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 border-t border-[var(--color-line)] pt-2 text-sm"><dt className="min-w-0 break-words">{label(id)}</dt><dd className="tabular-nums">{row?`${score(row.scoreUnits)} / ${score(row.maxUnits)}`:"—"}</dd></div>;})}<div className="flex flex-wrap justify-between gap-2 border-t border-[var(--color-line)] pt-2 font-bold"><dt>{t("sum")}</dt><dd>{t("points",{points:score(item.scoreUnits)})}</dd></div></dl></li>)}</ul>
 </div>;
}
