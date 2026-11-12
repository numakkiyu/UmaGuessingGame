import { useState } from "react";
import { Link } from "react-router";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useApi } from "@/lib/api";
import { describeFailure } from "@/lib/api/client";
import { phaseLabel, type RoundSnapshot } from "@/lib/api/round-snapshot";
import { useCountdown } from "@/lib/hooks/use-countdown";
import { useGameCommand } from "@/lib/hooks/use-game-command";
import { useTranslation } from "@/i18n";
import "@/i18n/event-controls";
import { manualRoundLock, validRoundControl, type RoundControlCommand } from "./round-control-command";

export function EventRoundControls({snapshot,closed,onChanged,roundId,roomId,disabled=false}:{snapshot:RoundSnapshot;closed:boolean;onChanged:()=>Promise<unknown>;roundId?:string|null;roomId?:string;disabled?:boolean}) {
 const {t}=useTranslation("eventControls");
 const {api,session}=useApi();
 const id=roundId??snapshot.meta?.roundId;
 const meta=snapshot.meta?.roundId===id?snapshot.meta:null;
 const scope=session&&id?`${session.sessionId}:${id}:host-transition`:null;
 const command=useGameCommand(scope,validRoundControl,(body,key)=>api.createTransition(id!,body,undefined,key));
 const [confirm,setConfirm]=useState<"locked"|"cancel"|null>(null),[reason,setReason]=useState("");
 const [notice,setNotice]=useState<"confirmed"|"readFailed"|"reasonInvalid"|null>(null),[refreshing,setRefreshing]=useState(false);
 const timer=useCountdown(meta?.deadlineAt,meta?.paused!==true);
 const info=command.error?describeFailure(command.error):null;
 const ended=meta?.phase==="settled"||meta?.phase==="cancelled";
 const unavailable=disabled||!meta||closed||command.busy||!!command.pending||command.expired;
 const reviewMode=meta?.modeId==="creative"||meta?.modeId==="bluff";
 const raw=snapshot.raw as {publicState?:{subphase?:unknown}}|null;
 const reviewClosed=raw?.publicState?.subphase==="closed";
 const windowMode=!!meta&&["strategy-race","rapport","trade"].includes(meta.modeId);
 const preview=meta?.modeId==="memory"&&meta.phase==="briefing";
 async function refresh(acknowledge=false) {
  if(refreshing)return;
  setRefreshing(true);
  try {const result=await onChanged();if(result!==true)throw Error("read failed");if(acknowledge)command.acknowledgeExpired();setNotice(null);}
  catch {setNotice("readFailed");}finally{setRefreshing(false);}
 }
 async function transition(target:RoundControlCommand["target"],extensionSeconds?:number){
  if(!meta||unavailable)return;
  if(target==="cancel"&&([...reason.trim()].length<5||[...reason.trim()].length>240)){setNotice("reasonInvalid");return;}
  const body:RoundControlCommand={target,expectedPhaseVersion:meta.phaseVersion,...(extensionSeconds?{extensionSeconds}:{}),...(target==="cancel"?{reason:reason.trim()}: {})};
  setConfirm(null);setNotice(null);
  const result=await command.run(body);
  if(result?.ok){setNotice("confirmed");await refresh();}else await refresh();
 }
 async function confirmOriginal(){setNotice(null);const result=await command.run();if(result?.ok)setNotice("confirmed");await refresh();}
 if(!id)return null;
 return <section className="uma-panel order-first min-w-0 space-y-3 p-4 lg:order-none" aria-labelledby="event-round-heading" data-testid="event-round-controls">
  <div className="flex flex-wrap items-center justify-between gap-2"><h2 id="event-round-heading" className="font-bold">{t("title")}</h2><span className="text-sm font-semibold">{meta?(meta.paused?t("paused"):phaseLabel(meta.phase)):t("missing")}{meta?.deadlineAt&&!meta.paused&&!ended?` · ${timer.text}`:""}</span></div>
  {command.pending?<Alert tone="warning" title={t("pendingTitle")}><p>{t("pendingDetail")}</p><Button variant="primary" className="mt-2 h-auto min-h-11 whitespace-normal" disabled={command.busy} onClick={()=>void confirmOriginal()}>{t("confirm")}</Button></Alert>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" className="mt-2 h-auto min-h-11 whitespace-normal" disabled={refreshing} onClick={()=>void refresh(true)}>{t("refresh")}</Button></Alert>:null}
  {command.storageLimited?<Alert tone="warning" title={t("storage")}><p>{t("storageDetail")}</p></Alert>:null}
  {info&&!command.pending?<Alert tone="warning" title={info.title}><p>{info.detail}</p><Button variant="secondary" className="mt-2 h-auto min-h-11 whitespace-normal" disabled={refreshing} onClick={()=>void refresh()}>{t("refresh")}</Button></Alert>:null}
  {notice?<p role="status" className="text-sm leading-6">{t(notice)}</p>:null}
  {!meta?<p className="text-sm">{t("missing")}</p>:closed?<p className="text-sm text-[var(--color-muted)]">{t("closed")}</p>:ended?<p className="text-sm text-[var(--color-muted)]">{t(meta.phase==="settled"?"settled":"cancelled")}</p>:<>
   <div className="flex flex-wrap gap-2">
    {meta.phase==="prepared"?<Button variant="primary" onClick={()=>void transition("briefing")} disabled={unavailable}>{t("briefing")}</Button>:null}
    {meta.phase==="briefing"&&!preview&&!meta.paused?<Button variant="primary" onClick={()=>void transition("collecting")} disabled={unavailable}>{t("start")}</Button>:null}
    {(meta.paused||["briefing","collecting"].includes(meta.phase))?<Button variant="secondary" onClick={()=>void transition(meta.paused?"resume":"pause")} disabled={unavailable}>{t(meta.paused?"resume":"pause")}</Button>:null}
    {meta.phase==="collecting"&&!meta.paused&&!reviewMode?<>
     <Button variant="secondary" onClick={()=>void transition("collecting",30)} disabled={unavailable||!meta.deadlineAt}>{t("extend")}</Button>
     {manualRoundLock(meta.modeId)?<Button variant="primary" onClick={()=>setConfirm("locked")} disabled={unavailable}>{t("lock")}</Button>:null}
    </>:null}
    {meta.phase==="locked"&&!meta.paused&&(!reviewMode||reviewClosed)?<Button variant="primary" onClick={()=>void transition("revealed")} disabled={unavailable}>{t("reveal")}</Button>:null}
    {meta.phase==="revealed"&&!meta.paused?<Button variant="primary" onClick={()=>void transition("settled")} disabled={unavailable}>{t("settle")}</Button>:null}
   </div>
   {preview?<p className="text-sm leading-6">{t("previewHint")}</p>:null}
   {windowMode&&meta.phase==="collecting"?<p className="text-sm leading-6">{t("windowHint")}</p>:null}
   {reviewMode&&["collecting","locked"].includes(meta.phase)&&!reviewClosed?<><p className="text-sm leading-6">{t("reviewHint")}</p>{roomId?<Button asChild variant="secondary"><Link to={`/rooms/${encodeURIComponent(roomId)}`}>{t("modePage")}</Link></Button>:null}</>:null}
   {meta.phase==="collecting"&&!meta.paused&&timer.expired?<p role="status" className="text-sm leading-6">{t("deadline")}</p>:null}
   <details className="uma-disclosure border-t border-[var(--color-line)] pt-2"><summary className="min-h-11 cursor-pointer content-center text-sm font-semibold">{t("issue")}</summary><p className="mb-2 text-sm leading-6 text-[var(--color-muted)]">{t("issueHint")}</p><Button variant="danger" disabled={unavailable} onClick={()=>{setReason("");setConfirm("cancel");}}>{t("cancel")}</Button></details>
  </>}
  <Dialog open={confirm!==null} onOpenChange={open=>{if(!open&&!command.busy)setConfirm(null);}}><DialogContent><DialogHeader><DialogTitle>{t(confirm==="locked"?"lockTitle":"cancelTitle")}</DialogTitle><DialogDescription>{t(confirm==="locked"?"lockDetail":"cancelDetail")}</DialogDescription></DialogHeader>{confirm==="cancel"?<div className="space-y-2"><Label htmlFor="cancel-round-reason">{t("reason")}</Label><Input id="cancel-round-reason" value={reason} onChange={e=>setReason(e.target.value)} aria-describedby="cancel-round-hint"/><p id="cancel-round-hint" className="text-sm leading-6 text-[var(--color-muted)]">{t("reasonHint")}</p></div>:null}<DialogFooter><Button variant="secondary" onClick={()=>setConfirm(null)} disabled={command.busy}>{t("back")}</Button><Button variant={confirm==="cancel"?"danger":"primary"} disabled={unavailable||(confirm==="cancel"&&([...reason.trim()].length<5||[...reason.trim()].length>240))} onClick={()=>{if(confirm)void transition(confirm);}}>{t(confirm==="locked"?"confirmLock":"confirmCancel")}</Button></DialogFooter></DialogContent></Dialog>
 </section>;
}
