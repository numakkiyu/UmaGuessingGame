import { useConfirmation } from "@/lib/confirmation";
import "@/i18n/challenge";
import "@/i18n/classic";
import { useTranslation } from "@/i18n";
import { characterName } from "@/i18n/entities";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams } from "react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Flag, RefreshCw } from "lucide-react";
import { useApi } from "@/lib/api";
import { useCharacters } from "@/lib/api/queries";
import { ApiError, createIdempotencyKey, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import { normalizeRowFromFeedback } from "@/lib/api/classic";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { SearchInput } from "@/components/uma/search-input";
import { AvatarImage } from "@/components/uma/avatar-image";
import { ErrorView, LoadingBlock } from "@/components/uma/page-state";
import { GuessCard, GuessTable } from "@/features/classic/guess-card";
import type { Character } from "@/lib/api/types";

type Pending={key:string;revision:number;ticket:string;characterId:string|null};
export function ChallengeGamePage(){
 const {t}=useTranslation("challenge"),{t:tc}=useTranslation("classic");
 const {challengeId=""}=useParams<{challengeId:string}>();const {api,session,sessionLoading}=useApi();const queryClient=useQueryClient();
 const queryKey=useMemo(()=>["challenge",challengeId,session?.sessionId??""],[challengeId,session?.sessionId]);
 const result=useQuery({queryKey,enabled:!!session,queryFn:async({signal})=>{const response=await api.getChallenge(challengeId,signal);if(!response.data)throw new Error(t("missing"));return {view:response.data,receivedAt:Date.now()};},staleTime:Infinity,refetchOnWindowFocus:false,refetchOnReconnect:false,retry:false});
 const charactersQuery=useCharacters();const [selected,setSelected]=useState<Character|null>(null),[failure,setFailure]=useState<Failure|null>(null),[pending,setPending]=useState<Pending|null>(null),[busy,setBusy]=useState(false),[manual,setManual]=useState<"table"|"cards"|null>(null),[wide,setWide]=useState(false),[now,setNow]=useState(Date.now);
 const inFlight=useRef(false),historyRef=useRef<HTMLDivElement>(null);
 const view=result.data?.view;
 const remainingMs=view?Math.max(0,Date.parse(view.expiresAt)-Date.parse(view.serverTime)-(now-(result.data?.receivedAt??now))):0;
 const active=view?.status==="active"&&remainingMs>0;
  const confirmAction = useConfirmation(`${challengeId}:${view?.revision}:${active}:${busy}:${!!pending}`);
 const rows=useMemo(()=>(view?.rows??[]).map((row,i)=>normalizeRowFromFeedback(row,i)).filter(row=>row!==null).map(row=>{const known=charactersQuery.data?.items.find(c=>c.characterId===row.characterId);return known?{...row,displayName:known.displayName,localizedNames:known.localizedNames}:row;}),[view?.rows,charactersQuery.data]);
 const excluded=useMemo(()=>new Set(rows.map(row=>row.characterId)),[rows]);
 const choices=useMemo(()=>{const allowed=new Set(view?.characterIds??[]);return (charactersQuery.data?.items??[]).filter(c=>allowed.has(c.characterId));},[charactersQuery.data,view?.characterIds]);
 useEffect(()=>{
  if(view?.status!=="active")return;
  const timer=window.setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer);
 },[view?.status]);
 // The observer attaches after the query has produced the gameplay region.
 const measure=useCallback((element:HTMLDivElement|null)=>{historyRef.current=element;},[]);
 useEffect(()=>{const element=historyRef.current;if(!element)return;const observer=new ResizeObserver(entries=>setWide((entries[0]?.contentRect.width??0)>=720));observer.observe(element);return()=>observer.disconnect();},[view?.challengeId]);
 async function submit(action:Pending){
  if(inFlight.current||!view)return;inFlight.current=true;setBusy(true);setFailure(null);setPending(action);
  try{
   const body={ticket:action.ticket,expectedRevision:action.revision};
   const response=action.characterId===null?await api.finishChallenge(challengeId,body,action.key):await api.guessChallenge(challengeId,{...body,characterId:action.characterId},action.key);
   if(!response.data)throw new Error(t("unconfirmed"));
   queryClient.setQueryData(queryKey,{view:response.data,receivedAt:Date.now()});setSelected(null);setPending(null);
  }catch(error){
   setFailure(describeFailure(error));
   if(error instanceof ApiError&&[400,401,403,404,409,422,429].includes(error.status)){
    setPending(null);
    // A definite refusal can mean the other tab already moved forward. One
    // explicit state read recovers it; no polling and no automatic resubmission.
    if(error.status===409)await result.refetch();
   }
  }finally{setBusy(false);inFlight.current=false;}
 }
 function guess(){if(view&&selected&&active&&!pending)void submit({key:createIdempotencyKey(),revision:view.revision,ticket:view.ticket,characterId:selected.characterId});}
 async function finish(){if(view&&active&&!pending&&!inFlight.current&&await confirmAction({ns:"challenge",key:"finishConfirm"},"finish"))await submit({key:createIdempotencyKey(),revision:view.revision,ticket:view.ticket,characterId:null});}
 if(sessionLoading)return <LoadingBlock label={t("loading")} rows={3}/>;
 if(!session)return <div className="uma-stack"><Alert tone="warning" title={t("missing")}>{t("missingDetail")}</Alert><Button asChild variant="secondary"><Link to="/play/classic/verified">{t("back")}</Link></Button></div>;
 if(result.isError)return <ErrorView failure={localizeFailure(describeFailure(result.error))} onRetry={()=>void result.refetch()}/>;
 if(!view)return <LoadingBlock label={t("preparingGame")} rows={3}/>;
 const failureText=failure?localizeFailure(failure):null;
 const ended=view.status!=="active";
 const knownAnswer=charactersQuery.data?.items.find(c=>c.characterId===view.answer?.characterId);
 return <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
  <header className="flex flex-wrap items-center gap-3 border-b border-[var(--color-line)] pb-3"><div className="min-w-0 flex-1"><Link className="text-sm font-semibold text-[var(--color-blue-deep)] underline underline-offset-4" to="/play/classic/verified">{t("name")}</Link><h1 className="mt-1 text-xl font-extrabold">{tc("title")}</h1></div><div className="text-right"><p className="text-lg font-bold tabular-nums text-[var(--color-action-green)]">{ended?tc(view.status==="solved"?"solved":"ended"):tc("remaining",{count:view.remainingWrong})}</p><p className="text-sm tabular-nums text-[var(--color-muted)]">{t("attempts",{count:view.attempts})}{!ended?` · ${t("time",{time:`${Math.floor(remainingMs/60000)}:${String(Math.floor(remainingMs/1000)%60).padStart(2,"0")}`})}`:""}</p></div></header>
  {pending&&!busy?<Alert tone="warning" title={t("pending")} action={<Button variant="secondary" onClick={()=>void submit(pending)}>{t("confirm")}</Button>}>{t("pendingDetail")}</Alert>:null}
  {failureText?<Alert tone="warning" title={failureText.title}>{failureText.detail}{failureText.diagnosticId?<p className="mt-1">{t("diagnostic",{id:failureText.diagnosticId})}</p>:null}</Alert>:null}
  {view.status==="active"&&!active?<Alert tone="info" title={t("expired")} action={<Button variant="secondary" onClick={()=>void result.refetch()}>{t("result")}</Button>}>{t("expiredDetail")}</Alert>:null}
  {ended?<section className="uma-panel uma-reveal-row grid grid-cols-[auto_minmax(0,1fr)] items-center gap-4 p-5 sm:flex">{view.answer?<AvatarImage characterId={view.answer.characterId} src={view.answer.avatarPath} alt="" size="lg"/>:null}<div className="min-w-0 flex-1"><h2 className="text-lg font-extrabold">{tc(view.status==="solved"?"resultSolved":"resultAnswer")}</h2><p className="mt-1 text-xl font-bold text-[var(--color-action-green)]">{knownAnswer?characterName(knownAnswer):view.answer?.displayName??tc("ended")}</p><p className="mt-1 text-sm">{t("score",{score:Math.round(view.scoreUnits/100),count:view.attempts})}</p></div><Button className="col-span-2 w-full sm:w-auto" asChild variant="primary"><Link to="/play/classic/verified">{t("again")}</Link></Button></section>:<section className="uma-panel space-y-3 p-4 sm:p-5"><h2 className="font-bold">{tc("who")}</h2><SearchInput items={choices} onSelect={setSelected} excludeIds={excluded} disabled={busy||!!pending||!active} loading={charactersQuery.isPending} loadError={charactersQuery.isError?t("charactersFailed"):null} hideLabel/>{selected?<div className="flex items-center gap-3 rounded-[var(--radius-control)] bg-[var(--color-panel-soft)] p-2"><AvatarImage characterId={selected.characterId} alt="" size="sm"/><span className="min-w-0 flex-1 font-semibold">{characterName(selected)}</span><Button variant="primary" onClick={guess} disabled={busy||!!pending||!active}>{busy?t("confirming"):tc("guess")}</Button></div>:<p className="text-sm text-[var(--color-muted)]">{tc("chooseHint")}</p>}</section>}
  <section ref={measure} className="min-w-0 space-y-4" aria-label={t("records")}><div className="flex flex-wrap items-center gap-2"><h2 className="mr-auto text-lg font-bold">{tc("guesses")} <span className="text-sm font-normal text-[var(--color-muted)]">{tc("count",{count:rows.length})}</span></h2><Tabs value={manual??"auto"} onValueChange={v=>setManual(v==="auto"?null:v==="table"?"table":"cards")}><TabsList aria-label={tc("display")}><TabsTrigger value="auto">{tc("auto")}</TabsTrigger><TabsTrigger value="table">{tc("table")}</TabsTrigger><TabsTrigger value="cards">{tc("cards")}</TabsTrigger></TabsList></Tabs></div>{rows.length?(manual??(wide?"table":"cards"))==="table"?<GuessTable rows={rows}/>:<div className="space-y-3">{[...rows].reverse().map(row=><GuessCard key={row.characterId} row={row} latest={row.attempt===rows.length}/>)}</div>:<p className="rounded-[var(--radius-card)] border border-dashed border-[var(--color-line-strong)] p-6 text-center text-sm text-[var(--color-muted)]">{tc("empty")}</p>}</section>
  <div className="flex flex-wrap gap-2">{!ended?<Button variant="ghost" onClick={()=>void finish()} disabled={busy||!!pending||!active}><Flag className="size-4" aria-hidden="true"/>{tc("finish")}</Button>:null}<Button variant="ghost" onClick={()=>void result.refetch()} disabled={busy||!!pending||result.isFetching}><RefreshCw className="size-4" aria-hidden="true"/>{t("refresh")}</Button></div>
 </div>;
}
