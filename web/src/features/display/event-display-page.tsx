import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useParams } from "react-router";
import { Loader2, Monitor } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useApi } from "@/lib/api";
import { ApiError, describeFailure } from "@/lib/api/client";
import { useEventData, displayNameOf, publicCodeOf, type StandingItem } from "@/features/events/use-event-data";
import { EventTieArrangement, eventSegmentLabel } from "@/features/events/event-presentation";
import { formatScoreUnits } from "@/lib/format-score";
import { LanguageSelect } from "@/i18n/language-select";
import { useTranslation } from "@/i18n";
import "@/i18n/display";
import { PublicRoundStage } from "./public-round-stage";

function subscribeViewport(callback:()=>void){window.addEventListener("resize",callback);return()=>window.removeEventListener("resize",callback);}
function viewportLimit(){return window.innerWidth>=1024?(window.innerHeight<=800?2:window.innerHeight<=1200?4:8):8;}

type ScreenState="checking"|"need-grant"|"redeeming"|"granted";
type PairNotice="codeRequired"|"invalidCode"|"usedCode"|"expiredCode"|"wrongEvent"|"separate"|"unknownPair"|"notPaired"|"checkFailed";
export function EventDisplayPage(){const {eventId}=useParams<{eventId:string}>();return <DisplayWorkspace key={eventId??"missing"} eventId={eventId??null}/>;}
function DisplayWorkspace({eventId}:{eventId:string|null}){
 const {api}=useApi(),{t,i18n}=useTranslation("display"),{t:rt}=useTranslation("eventResults");
 const [state,setState]=useState<ScreenState>(()=>eventId?"checking":"need-grant"),[grantCode,setGrantCode]=useState("");
 const [pairNotice,setPairNotice]=useState<PairNotice|null>(null),[pairError,setPairError]=useState<unknown>(null),[refreshing,setRefreshing]=useState(false);
 const inFlight=useRef(false),alive=useRef(true);
 const displayLimit=useSyncExternalStore(subscribeViewport,viewportLimit,()=>8);
 const data=useEventData(state==="granted"?eventId:null,{pollIntervalMs:5000,displayOnly:true});
 const {event,standings,segments,isLoading,isError,error,loadedAt,refetch}=data;
 // A successful cookie-backed read restores this screen after refresh. Neither
 // a saved Boolean nor an ordinary player's identity can authorise the stage.
 useEffect(()=>{
  alive.current=true;
  const abort=new AbortController();
  if(!eventId)return()=>{alive.current=false;};
  void api.getEvent(eventId,abort.signal,true).then(response=>{
   if(!abort.signal.aborted&&alive.current)setState(response.data.eventId===eventId?"granted":"need-grant");
  }).catch(()=>{if(!abort.signal.aborted&&alive.current)setState("need-grant");});
  return()=>{alive.current=false;abort.abort();};
 },[api,eventId]);
 // The cursor is only hidden during viewing; it is restored on interaction,
 // leaving this page, or opening connection controls.
 useEffect(()=>{
  if(state!=="granted")return;
  let timer:number;
  const show=()=>{document.body.style.cursor="";window.clearTimeout(timer);timer=window.setTimeout(()=>{document.body.style.cursor="none";},3000);};
  show();document.addEventListener("mousemove",show);document.addEventListener("pointerdown",show);document.addEventListener("keydown",show);
  return()=>{window.clearTimeout(timer);document.body.style.cursor="";document.removeEventListener("mousemove",show);document.removeEventListener("pointerdown",show);document.removeEventListener("keydown",show);};
 },[state]);
 async function checkPair(){
  if(!eventId||inFlight.current)return;inFlight.current=true;setState("checking");setPairError(null);setPairNotice(null);
  try{const response=await api.getEvent(eventId,undefined,true);if(!alive.current)return;if(response.data.eventId!==eventId){setState("need-grant");setPairNotice("notPaired");return;}setState("granted");setGrantCode("");}
  catch(err){if(alive.current){setState("need-grant");setPairNotice(err instanceof ApiError&&[401,403,404].includes(err.status)?"notPaired":"checkFailed");setPairError(err);}}
  finally{inFlight.current=false;}
 }
 async function redeem(){
  if(!eventId||inFlight.current||state==="checking")return;
  const code=grantCode.trim();if(code.length<16||code.length>128){setPairNotice("codeRequired");return;}
  inFlight.current=true;setState("redeeming");setPairNotice(null);setPairError(null);
  try{
   const response=await api.redeemDisplayGrant({grantCode:code,eventId});if(!alive.current)return;
   if(response.data.eventId!==eventId){setState("need-grant");setPairNotice("notPaired");return;}
   setGrantCode("");setState("granted");
  }catch(err){
   if(!alive.current)return;setState("need-grant");setPairError(err);
   const code=err instanceof ApiError?err.code:"";
   setPairNotice(code==="use-separate-profile"?"separate":code==="grant-event-mismatch"?"wrongEvent":code==="grant-expired"?"expiredCode":code==="grant-already-used"?"usedCode":code==="grant-not-found"?"invalidCode":"unknownPair");
  }finally{inFlight.current=false;}
 }
 async function refresh(){if(refreshing)return;setRefreshing(true);try{await refetch();}finally{if(alive.current)setRefreshing(false);}}
 const pairInfo=pairError?describeFailure(pairError):null;
 const info=isError?describeFailure(error):null;
 const authProblem=info&&["forbidden","unauthenticated","not-found"].includes(info.kind);
 const language=<LanguageSelect/>;
 if(state!=="granted")return <div className="mx-auto flex min-h-dvh max-w-2xl flex-col justify-center gap-5 px-4 py-8 sm:px-6">
  <div className="flex justify-end">{language}</div><header className="flex items-start gap-3"><Monitor aria-hidden="true" className="mt-1 size-9 shrink-0 text-[var(--color-action-green)]"/><div className="min-w-0"><h1 className="u-fluid-title">{t("title")}</h1><p className="mt-2 text-base leading-7 text-[var(--color-muted)]">{t("pairIntro")}</p></div></header>
  {state==="checking"?<p role="status" className="flex items-center gap-2"><Loader2 aria-hidden="true" className="size-5 motion-safe:animate-spin"/>{t("checking")}</p>:!eventId?<Alert tone="warning" title={t("title")}><p>{t("missing")}</p></Alert>:<section className="uma-panel min-w-0 space-y-4 p-5" aria-labelledby="grant-heading"><h2 id="grant-heading" className="text-lg font-bold">{t("pairHeading")}</h2><p className="text-sm leading-6 text-[var(--color-muted)]">{t("pairHint")}</p><form noValidate onSubmit={e=>{e.preventDefault();void redeem();}} className="space-y-3"><div className="space-y-2"><Label htmlFor="grant-code">{t("code")}</Label><Input id="grant-code" value={grantCode} onChange={e=>setGrantCode(e.target.value)} placeholder={t("placeholder")} autoComplete="off" spellCheck={false} disabled={state==="redeeming"}/></div><Button type="submit" size="primary" block disabled={state==="redeeming"}>{state==="redeeming"?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:null}{t(state==="redeeming"?"pairing":"open")}</Button></form>
   {pairNotice?<Alert tone={pairNotice==="unknownPair"?"warning":"danger"} title={t("pairFailed")}><p>{t(pairNotice)}</p>{pairInfo?.diagnosticId?<p className="mt-2 text-sm">{t("diagnostic")}：<span className="uma-code">{pairInfo.diagnosticId}</span></p>:null}</Alert>:null}
   <Button variant="secondary" className="h-auto min-h-11 whitespace-normal" block onClick={()=>void checkPair()}>{t("checkPair")}</Button>
  </section>}
 </div>;
 if(isLoading||!event&&!isError)return <div className="flex min-h-dvh flex-col items-center justify-center gap-4 p-6"><div className="self-end">{language}</div><Loader2 aria-hidden="true" className="size-10 motion-safe:animate-spin"/><p role="status" className="text-2xl">{t("checking")}</p></div>;
 if(authProblem||!event)return <div className="mx-auto flex min-h-dvh max-w-3xl flex-col justify-center gap-5 p-6"><div className="self-end">{language}</div><h1 className="text-3xl font-bold">{t(authProblem?"authMissing":"unavailable")}</h1><p className="text-xl leading-relaxed">{t("authHint")}</p><div className="flex flex-wrap gap-3"><Button variant="secondary" size="primary" disabled={refreshing} onClick={()=>void refresh()}>{t("retry")}</Button><Button variant="secondary" size="primary" onClick={()=>{setPairNotice(null);setPairError(null);setState("need-grant");}}>{t("changeCode")}</Button></div></div>;
 const items=standings?.items??[],top=items.slice(0,displayLimit),leaders=items.filter(item=>item.rank===1);
 const closed=event.status==="completed"||event.status==="cancelled";
 const active=closed?undefined:segments.find(s=>s.status==="running"&&s.roundId);
 const status=["draft","lobby","running","completed","cancelled"].includes(event.status)?rt(event.status):rt("unknown");
 const time=loadedAt?new Date(loadedAt).toLocaleTimeString(i18n.language,{hour:"2-digit",minute:"2-digit",second:"2-digit"}):"—";
 return <div className="uma-display-stage relative isolate flex min-h-dvh w-full min-w-0 flex-col bg-[var(--color-page)]" style={{paddingInline:"clamp(1rem,3vw,4rem)",paddingBlock:"clamp(1rem,2.5vh,2.5rem)",gap:"clamp(.75rem,1.6vh,1.5rem)"}}>
  <div aria-hidden="true" className="pointer-events-none absolute inset-0 -z-20 bg-cover bg-center opacity-30" style={{backgroundImage:"url(/assets/ui/scenes/event-stage.jpg)"}}/>
  <header className="flex min-w-0 flex-wrap items-start justify-between gap-3"><div className="min-w-0 flex-1"><h1 className="break-words font-bold leading-tight" style={{fontSize:"clamp(2rem,3.2vw + 1rem,4.5rem)"}}>{event.name||rt("unnamedEvent")}</h1><p className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[length:var(--display-aux)] text-[var(--color-muted)]"><span>{status}</span><span>{t("lastUpdate",{time})}</span></p></div>{language}</header>
  {event.status==="cancelled"?<Alert tone="warning" title={status}><p className="text-[length:var(--display-aux)]">{t("cancelledHint")}</p></Alert>:null}
  {isError?<Alert fault tone="warning" title={t("unavailable")} action={<Button variant="secondary" onClick={()=>void refresh()} disabled={refreshing}>{t("retry")}</Button>}><p className="text-[length:var(--display-aux)]">{t("stale")}</p></Alert>:null}
  <div className="uma-display-body">
  <main className="relative flex min-w-0 flex-1 flex-col justify-center gap-4 rounded-[var(--radius-panel-lg)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-4 sm:p-6" aria-label={t("stage")}>
   {active?.roundId&&active.modeId?<><h2 className="break-words text-[length:var(--display-team)] font-semibold">{eventSegmentLabel(active,segments.indexOf(active))}</h2><PublicRoundStage key={active.roundId} roundId={active.roundId} modeId={active.modeId} competitive={active.hasDetail?active.competitive:undefined}/></>:top.length?<>
    <h2 className="text-center text-[length:var(--display-team)] font-bold">{t(event.status==="cancelled"?"recorded":event.status==="completed"?"winners":"leaders")}</h2>
    <div className={leaders.length>1?"grid grid-cols-1 gap-3 text-center xl:grid-cols-2":"text-center"}>{leaders.slice(0,displayLimit).map(item=><div key={item.subjectId} className="min-w-0"><p className={leaders.length>1?"break-words text-[length:var(--display-team)] font-bold leading-tight":"break-words text-[length:var(--display-hero)] font-bold leading-tight"}>{displayNameOf(item)}</p>{publicCodeOf(item.publicCode)?<p className="mt-1 text-[length:var(--display-aux)] text-[var(--color-muted)]">{t("codeLabel",{code:item.publicCode})}</p>:null}</div>)}</div>{leaders.length>displayLimit?<p className="text-center text-[length:var(--display-aux)]">{t("moreLeaders",{shown:displayLimit,total:leaders.length})}</p>:null}<p className="text-center text-[length:var(--display-task)] font-bold text-[var(--color-action-green)]">{t("points",{points:formatScoreUnits(top[0]!.scoreUnits)})}</p>
   </>:<div className="space-y-4 text-center"><h2 className="text-[length:var(--display-task)] font-bold">{t(closed?"emptyFinal":"waiting")}</h2><p className="text-[length:var(--display-aux)] leading-relaxed text-[var(--color-muted)]">{t(closed?"emptyFinalHint":"waitingHint")}</p>{segments.length?<><h3 className="text-[length:var(--display-aux)] font-semibold">{t("programme",{count:segments.length})}</h3><ol className="mx-auto max-w-5xl space-y-2 text-left">{segments.slice(0,6).map((s,index)=><li key={s.segmentId} className="flex min-w-0 flex-wrap items-baseline justify-between gap-2 rounded-lg bg-[var(--color-panel)] px-4 py-2 text-[length:var(--display-team)]"><span className="min-w-0 break-words">{eventSegmentLabel(s,index)}</span><span className="text-[length:var(--display-aux)] text-[var(--color-muted)]">{s.status==="completed"?rt("completed"):s.status==="voided"||s.status==="void"?rt("voided"):s.status==="running"?rt("running"):rt("idle")}</span></li>)}</ol>{segments.length>6?<p className="text-[length:var(--display-aux)]">{t("programmeMore",{count:segments.length-6})}</p>:null}</>:null}</div>}
  </main>
  {top.length?<DisplayStandings items={top} total={items.length}/>:null}
  </div>
  <footer className="min-w-0 border-t border-[var(--color-line)] pt-3 text-[length:var(--display-aux)]"><EventTieArrangement event={event} className="mt-0 border-0 pt-0 text-[length:var(--display-aux)] leading-snug"/><p className="mt-2 text-[var(--color-muted)]">{t("readOnly")}</p></footer>
 </div>;
}
function DisplayStandings({items,total}:{items:StandingItem[];total:number}){
 const {t}=useTranslation("display");
 function name(item:StandingItem){return <span className="block min-w-0 break-words"><span className="block">{displayNameOf(item)}</span>{publicCodeOf(item.publicCode)?<span className="block text-[length:var(--display-aux)] font-normal text-[var(--color-muted)]">{t("codeLabel",{code:item.publicCode})}</span>:null}</span>;}
 return <section className="min-w-0 rounded-[var(--radius-panel-lg)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-4" aria-label={t("standings")}><h2 className="text-[length:var(--display-team)] font-bold">{t("standings")}</h2><p className="mt-1 text-[length:var(--display-aux)] text-[var(--color-muted)]">{t("topHint",{shown:items.length,total})}</p><table className="mt-2 hidden w-full table-fixed border-collapse text-left text-[length:var(--display-team)] md:table"><thead className="text-[length:var(--display-aux)]"><tr className="border-b border-[var(--color-line)]"><th className="w-20 py-2">{t("rank")}</th><th className="py-2">{t("name")}</th><th className="w-28 py-2 text-right">{t("score")}</th></tr></thead><tbody>{items.map(item=><tr key={item.subjectId} className="border-b border-[var(--color-line)]"><td className="py-2 font-bold">{item.rank}</td><th scope="row" className="py-2 pr-3 font-semibold">{name(item)}</th><td className="py-2 text-right font-bold tabular-nums">{formatScoreUnits(item.scoreUnits)}</td></tr>)}</tbody></table><ol className="mt-2 space-y-2 md:hidden">{items.map(item=><li key={item.subjectId} className="rounded-xl border border-[var(--color-line)] bg-[var(--color-panel)] p-3"><div className="flex flex-wrap justify-between gap-2 text-[length:var(--display-aux)] font-semibold"><span>{t("rankValue",{rank:item.rank})}</span><span>{t("points",{points:formatScoreUnits(item.scoreUnits)})}</span></div><div className="mt-2 text-[length:var(--display-team)] font-semibold">{name(item)}</div></li>)}</ol></section>;
}
