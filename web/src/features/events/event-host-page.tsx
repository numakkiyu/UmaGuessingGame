import "@/i18n/start-requirements";
import { startIssue } from "@/features/rooms/start-requirements";
import { SelectField } from "@/components/ui/select";
import { useCallback, useRef, useState } from "react";
import { Link, useParams } from "react-router";
import { AlertTriangle, Monitor, RefreshCw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyView, ErrorView, ForbiddenView, LoadingBlock } from "@/components/uma/page-state";
import { RoomManagement } from "@/features/rooms/room-management";
import { RoomRoster } from "@/features/rooms/room-roster";
import { TeamRoundControl } from "@/features/rooms/team-round-control";
import { useApi } from "@/lib/api";
import { describeFailure } from "@/lib/api/client";
import { useRoom } from "@/lib/api/room-queries";
import { useRoundSnapshot } from "@/lib/api/round-snapshot";
import { useModes, usePublicConfig } from "@/lib/api/queries";
import { canPlay } from "@/lib/deployment/capabilities";
import { useCountdown } from "@/lib/hooks/use-countdown";
import { useGameCommand } from "@/lib/hooks/use-game-command";
import { useRealtime } from "@/lib/realtime/use-realtime";
import { formatScoreUnits } from "@/lib/format-score";
import { useTranslation } from "@/i18n";
import "@/i18n/event-host";
import { useEventData, displayNameOf, type EventMeta, type SegmentRef } from "./use-event-data";
import { EntertainmentResults } from "./entertainment-results";
import { SubmissionProgressPanel } from "./submission-progress-panel";
import { EventRoundControls } from "./event-round-controls";
import { EventSubjectName, EventTieArrangement, eventModeLabel, eventSegmentLabel } from "./event-presentation";
import { entertainmentOnlyMode, newEventModeConfig } from "./event-config";
import { validEventHostCommand, wholeNumber, scoreUnits, type EventHostCommand } from "./event-host-command";

const inputGroup="flex min-w-0 flex-col gap-1.5";
type Notice="readFailed"|"confirmed"|"grantConfirmed"|"grantMissing"|"copied"|"copyFallback"|"segmentInvalid"|"correctionInvalid";
export function EventHostPage(){
 const {eventId}=useParams<{eventId:string}>();
 const {session}=useApi();
 // Identity and event changes cannot carry a host's editable state into another room.
 return <EventHostWorkspace key={`${session?.sessionId??"guest"}:${eventId??""}`} eventId={eventId??null}/>;
}
function EventHostWorkspace({eventId}:{eventId:string|null}){
 const {t,i18n}=useTranslation("eventHost"),{t:rt}=useTranslation("eventResults");
 const {api,session}=useApi();
 const {t:requirement}=useTranslation("startRequirements");
 const data=useEventData(eventId,{pollIntervalMs:5000});
 const {event,standings,segments,refetch}=data;
 const roomQuery=useRoom(event?.roomId??null);
 const roundId=roomQuery.data?.room.currentRound?.roundId??null;
 const round=useRoundSnapshot(roundId);
 const roomRefresh=roomQuery.refetch,roundRefresh=round.refetch;
 const refresh=useCallback(async()=>{
  const [e,r,q]=await Promise.all([refetch(),roomRefresh(),roundRefresh()]);
  return e && !r.isError && !!r.data && (!roundId||q);
 },[refetch,roomRefresh,roundRefresh,roundId]);
 useRealtime({roomId:event?.roomId??null,enabled:!!event?.roomId,handlers:{onSnapshotNeeded:()=>{void refresh();}}});
 const config=usePublicConfig(),modes=useModes();
 const eventModes=(modes.data??[]).filter(m=>m.availability==="available"&&canPlay(config.data,m.modeId,"event"));
 const [label,setLabel]=useState(""),[modeId,setModeId]=useState(""),[budget,setBudget]=useState("100"),[rounds,setRounds]=useState("1"),[competitive,setCompetitive]=useState(true);
 const [correctionSegment,setCorrectionSegment]=useState(""),[subjectId,setSubjectId]=useState(""),[score,setScore]=useState(""),[reason,setReason]=useState("");
 const [grant,setGrant]=useState<{grantCode:string;expiresAt:string}|null>(null);
 const grantTimer=useCountdown(grant?.expiresAt,!!grant);
 const [notice,setNotice]=useState<Notice|null>(null),[refreshing,setRefreshing]=useState(false);
 const writing=useRef(false);
 const [danger,setDanger]=useState<"completed"|"cancelled"|null>(null);
 const scope=session&&eventId?`${session.sessionId}:${eventId}:event-host`:null;
 const command=useGameCommand(scope,validEventHostCommand,async(cmd,key)=>{
  switch(cmd.kind){
   case "transition": await api.createEventTransition(eventId!,cmd.body,undefined,key);return null;
   case "segment": await api.createSegment(eventId!,cmd.body,undefined,key);return null;
   case "round": await api.createRound(cmd.roomId,cmd.body,undefined,key);return null;
   case "correction": await api.createScoreCorrection(eventId!,cmd.body,undefined,key);return null;
   case "grant": {const response=await api.createDisplayGrant(eventId!,undefined,key);return response.data;}
  }
 });
 const host=roomQuery.data?.room.viewer.capabilities.some(cap=>cap==="host"||cap==="manage-room")===true&&!roomQuery.isError;
 const closed=isEventClosed(event);
 const busy=command.busy||!!command.pending||command.expired||refreshing;
 const disabled=busy||closed||!host||data.isError||!session;
 const selected=eventModes.find(m=>m.modeId===modeId);
 const fun=entertainmentOnlyMode(selected?.modeId);
 const canEdit=event?.status==="draft"||event?.status==="lobby";
 const validLabel=[...label.trim()].length>=1&&[...label.trim()].length<=48;
 const validBudget=wholeNumber(budget,0,100),validRounds=wholeNumber(rounds,1,20);
 const items=standings?.items??[];
 const correctionOptions=segments.filter(s=>s.status==="completed"&&s.competitive);
 const chosenSegment=correctionOptions.find(s=>s.segmentId===correctionSegment);
 // Only entrants who have a settled score in this item can be corrected.
 const subjects=items.filter(item=>item.segments.some(s=>s.segmentId===correctionSegment));
 const units=(chosenSegment?.scoreBudget===null||chosenSegment?.scoreBudget===undefined)?null:scoreUnits(score,chosenSegment.scoreBudget);
 const validCorrection=!!chosenSegment&&subjects.some(s=>s.subjectId===subjectId)&&units!==null&&[...reason.trim()].length>=5&&[...reason.trim()].length<=240;
 const info=command.error?describeFailure(command.error):null;
 function time(value:number|string,full=false){const d=new Date(value);return Number.isFinite(d.getTime())?(full?d.toLocaleString(i18n.language):d.toLocaleTimeString(i18n.language)):"—";}
 async function readLatest(ack=false){
  if(refreshing)return;
  setRefreshing(true);
  try{if(await refresh()){if(ack)command.acknowledgeExpired();setNotice(null);}else setNotice("readFailed");}
  catch{setNotice("readFailed");}finally{setRefreshing(false);}
 }
 async function run(cmd?:EventHostCommand){
  if(writing.current||refreshing||cmd&&(disabled||!validEventHostCommand(cmd)))return;
  writing.current=true;setRefreshing(true);
  try {
  setNotice(null);
  const kind=command.pending?.body.kind??cmd?.kind;
  const result=await command.run(cmd);
  if(!result)return;
  if(result.ok){
   if(kind==="grant"){
    const value=result.value;
    if(value&&typeof value.grantCode==="string"&&typeof value.expiresAt==="string"){
     setGrant({grantCode:value.grantCode,expiresAt:value.expiresAt});setNotice("grantConfirmed");
    }else setNotice("grantMissing");
   }else setNotice("confirmed");
   if(kind==="correction")setReason("");
  }
  try{if(!await refresh())setNotice("readFailed");}catch{setNotice("readFailed");}
  } finally {writing.current=false;setRefreshing(false);}
 }
 function transition(target:"lobby"|"running"|"completed"|"cancelled"){
  if(!event)return;
  setDanger(null);
  void run({kind:"transition",body:{target,expectedEventVersion:event.eventVersion,...(target==="completed"||target==="cancelled"?{reason:t(target==="completed"?"finishReason":"cancelReason")}: {})}});
 }
 function add(){
  if(!selected||!canEdit||!validLabel||validBudget===null||validRounds===null){setNotice("segmentInvalid");return;}
  void run({kind:"segment",body:{modeConfig:newEventModeConfig(selected),label:label.trim(),scoreBudget:validBudget,roundCount:validRounds,competitive:!fun&&competitive}});
 }
 function startRound(s:SegmentRef){
  if(!event||!s.modeConfig||event.status!=="running"||startIssue(s.modeConfig.modeId,roomQuery.data?.room.members??[],s.modeConfig))return;
  void run({kind:"round",roomId:event.roomId,body:{segmentId:s.segmentId,modeConfig:s.modeConfig}});
 }
 function correct(){
  if(!validCorrection||units===null||!event){setNotice("correctionInvalid");return;}
  void run({kind:"correction",body:{segmentId:correctionSegment,subjectId,newScoreUnits:units,reason:reason.trim(),expectedEventVersion:event.eventVersion}});
 }
 function copyInvite(){const code=roomQuery.data?.room.inviteCode;if(!code)return;if(!navigator.clipboard){setNotice("copyFallback");return;}void navigator.clipboard.writeText(new URL(`/join?code=${encodeURIComponent(code)}`,window.location.origin).href).then(()=>setNotice("copied")).catch(()=>setNotice("copyFallback"));}
 const recovery=<>
  {command.pending?<Alert tone="warning" title={t("pendingTitle")}><p>{t("pendingDetail")}</p><Button className="mt-2 h-auto min-h-11 whitespace-normal" disabled={command.busy||refreshing} onClick={()=>void run()}>{t("confirm")}</Button></Alert>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" className="mt-2 h-auto min-h-11 whitespace-normal" disabled={refreshing} onClick={()=>void readLatest(true)}>{t("refresh")}</Button></Alert>:null}
  {command.storageLimited?<Alert tone="warning" title={t("storage")}><p>{t("storageDetail")}</p></Alert>:null}
  {info&&!command.pending?<Alert fault tone="warning" title={info.title}><p>{info.detail}</p></Alert>:null}
  {notice?<p role="status" className="rounded-xl bg-[var(--color-panel-soft)] p-3 text-sm leading-6">{t(notice)}</p>:null}
 </>;
 let unavailable;
 if(!eventId)unavailable=<ErrorView failure={{kind:"error",title:rt("missingTitle"),detail:rt("missingDetail"),diagnosticId:null,retryable:false}}/>;
 else if(data.isLoading)unavailable=<LoadingBlock label={t("loading")} rows={4}/>;
 else if(data.isError&&!event){const f=describeFailure(data.error);unavailable=f.kind==="forbidden"?<ForbiddenView failure={f} onRetry={()=>void readLatest()}/>:<ErrorView failure={f} onRetry={()=>void readLatest()}/>;}
 else if(!event)unavailable=<EmptyView title={rt("missingEvent")} detail={rt("missingEventDetail")}/>;
 else if(roomQuery.data&&!host&&!roomQuery.isError)unavailable=<ForbiddenView failure={{kind:"forbidden",title:t("hostOnly"),detail:t("hostOnlyDetail"),diagnosticId:null,retryable:false}}/>;
 if(unavailable||!event)return <div className="uma-stack">{recovery}{unavailable}<EventRoundControls key={roundId} snapshot={round} roundId={roundId} roomId={event?.roomId} closed={closed} disabled onChanged={refresh}/></div>;
 const status=["draft","lobby","running","completed","cancelled"].includes(event.status)?rt(event.status):rt("unknown");
 return <div className="uma-stack">
  <section className="uma-panel min-w-0 p-4" aria-labelledby="host-heading"><div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><h1 id="host-heading" className="u-fluid-title break-words">{event.name||t("title")}</h1><p className="mt-1 flex flex-wrap gap-2 text-sm text-[var(--color-muted)]"><span>{status}</span><span>{t("eventCount",{count:segments.length})}</span><span>{t("scoredCount",{count:items.length})}</span>{data.loadedAt?<span>{t("updated",{time:time(data.loadedAt)})}</span>:null}</p></div><div className="flex flex-wrap gap-2"><Button variant="secondary" disabled={refreshing} onClick={()=>void readLatest()}><RefreshCw aria-hidden="true" className="size-4"/>{t("refresh")}</Button><Button asChild variant="secondary"><Link to={`/events/${encodeURIComponent(eventId!)}/display`}><Monitor aria-hidden="true" className="size-4"/>{t("screen")}</Link></Button></div></div><EventTieArrangement event={event}/></section>
  {recovery}
  {data.isError?<Alert tone="warning" title={t("readFailed")} action={<Button variant="secondary" onClick={()=>void readLatest()}>{t("refresh")}</Button>}/>:null}
  {!host?<Alert tone="warning" title={t("permissionPending")} action={<Button variant="secondary" onClick={()=>void readLatest()}>{t("refresh")}</Button>}/>:null}
  {closed?<Alert tone="warning" title={status}><p>{t("closedDetail")}</p></Alert>:null}
  {roomQuery.data?<>{host?<RoomManagement room={roomQuery.data.room} onChanged={refresh}/>:null}<details className="uma-disclosure"><summary className="min-h-11 cursor-pointer content-center font-semibold">{t("members")}</summary><RoomRoster room={roomQuery.data.room}/></details></>:null}
  <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,22rem)] lg:items-start">
   <section className="uma-panel min-w-0 p-4 lg:col-start-2 lg:row-start-1" aria-labelledby="controls-heading"><h2 id="controls-heading" className="font-bold">{t("controls")}</h2><div className="mt-3 flex flex-col gap-2">
    {roomQuery.data?.room.inviteCode?<div className="rounded-xl border border-[var(--color-line)] p-3"><p className="text-sm font-semibold">{t("invite")}</p><p className="mt-1 text-sm">{t("inviteCode")}：<strong className="uma-code select-all text-lg tracking-widest" data-testid="event-invite-code">{roomQuery.data.room.inviteCode}</strong></p><Button variant="secondary" className="mt-2 h-auto min-h-11 whitespace-normal" onClick={copyInvite}>{t("copyInvite")}</Button></div>:null}
    {event.status==="draft"?<Button variant="secondary" size="primary" disabled={disabled} onClick={()=>transition("lobby")} block>{t("open")}</Button>:null}
    {canEdit?<Button size="primary" disabled={disabled||!segments.length} onClick={()=>transition("running")} block>{t("start")}</Button>:null}
    <Button variant="secondary" size="primary" className="h-auto min-h-11 whitespace-normal" disabled={disabled} onClick={()=>void run({kind:"grant"})} block>{t("grant")}</Button>
   </div>
   {grant?<div className="mt-3 rounded-xl border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3"><h3 className="text-sm font-semibold">{t("grantTitle")}</h3><p className="uma-code mt-1 break-all text-lg font-bold select-all">{grant.grantCode}</p><p className="mt-1 text-sm leading-6">{t("grantHint")}</p><p className="text-sm">{grantTimer.expired?t("grantExpired"):t("grantExpiry",{time:time(grant.expiresAt,true)})}</p></div>:null}
   <p className="mt-3 text-sm leading-6 text-[var(--color-muted)]">{t("roundHint")}</p><Button asChild variant="secondary" className="mt-2 h-auto min-h-11 whitespace-normal" block><Link to={`/rooms/${encodeURIComponent(event.roomId)}`}>{t("playerView")}</Link></Button></section>
   <div className="contents lg:col-start-1 lg:row-start-1 lg:row-span-2 lg:flex lg:min-w-0 lg:flex-col lg:gap-4">
    <TeamRoundControl room={host?roomQuery.data?.room:undefined} raw={round.raw} onChanged={refresh}/>
    <EventRoundControls key={roundId} snapshot={round} roundId={roundId} roomId={event.roomId} closed={closed} disabled={disabled} onChanged={refresh}/>
    <section className="uma-panel min-w-0 p-4" aria-labelledby="programme-heading"><h2 id="programme-heading" className="font-bold">{t("programme")}</h2>{!segments.length?<p className="mt-2 text-sm">{t("noProgramme")}</p>:<ol className="mt-3 space-y-2">{segments.map((s,index)=>{
     const playable=s.hasDetail&&s.modeConfig&&s.status!=="completed"&&s.status!=="voided"&&s.status!=="void"&&s.completedRounds<(s.roundCount??1)&&(!s.roundId||s.currentRoundPhase==="settled");
     const state=s.status==="planned"?"planned":s.status==="running"?"segmentRunning":s.status==="completed"?"segmentCompleted":s.status==="void"||s.status==="voided"?"segmentVoided":null;
     const issue=s.modeConfig?startIssue(s.modeConfig.modeId,roomQuery.data?.room.members??[],s.modeConfig):null;
     return <li key={s.segmentId} className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-[var(--color-line)] p-3"><div className="min-w-0"><h3 className="break-words text-sm font-semibold">{eventSegmentLabel(s,index)}</h3><p className="mt-1 flex flex-wrap gap-2 text-xs text-[var(--color-muted)]">{state?<span>{t(state)}</span>:null}{!s.competitive?<span>{t("funTag")}</span>:null}<span>{t("roundProgress",{done:s.completedRounds,total:s.roundCount??1})}</span></p>{issue?<p className="mt-2 text-sm leading-6 text-[var(--color-near-edge)]">{requirement(issue)}</p>:null}</div>{playable?<Button variant="secondary" disabled={!!issue||disabled||event.status!=="running"||!!roundId&&!['settled','cancelled'].includes(round.meta?.phase??"")} onClick={()=>startRound(s)}>{t(s.startedRounds>0?"nextRound":"newRound")}</Button>:s.roundId?<span className="text-sm text-[var(--color-muted)]">{t(s.status==="completed"?"segmentDone":s.status==="void"||s.status==="voided"?"segmentVoid":"roundStarted")}</span>:null}</li>;
    })}</ol>}</section>
    {roundId?<SubmissionProgressPanel round={round.raw} room={roomQuery.data?.room} phase={round.meta?.phase} paused={round.meta?.paused} modeName={round.meta?eventModeLabel(round.meta.modeId):undefined}/>:<section className="uma-panel p-4"><h2 className="font-bold">{rt("progress")}</h2><p className="mt-2 text-sm leading-6">{t("noRound")}</p></section>}
    <section className="uma-panel min-w-0 p-4" aria-labelledby="add-segment-heading"><h2 id="add-segment-heading" className="font-bold">{t("add")}</h2>{!canEdit?<p className="mt-2 text-sm">{t("programmeLocked")}</p>:<form noValidate onSubmit={e=>{e.preventDefault();add();}}>
     <fieldset disabled={disabled} className="mt-3 grid min-w-0 gap-3 sm:grid-cols-2"><div className={inputGroup}><Label htmlFor="segment-label">{t("segmentName")}</Label><Input id="segment-label" value={label} onChange={e=>setLabel(e.target.value)} placeholder={t("segmentPlaceholder")}/></div><div className={inputGroup}><Label htmlFor="segment-mode">{t("mode")}</Label><SelectField disabled={disabled} id="segment-mode" value={modeId} onValueChange={value =>setModeId(value)} options={[({value:"",label:t("chooseMode")}),modeId&&!selected?({value:modeId,label:eventModeLabel(modeId)}):null,...eventModes.map(m=>({value:m.modeId,label:eventModeLabel(m.modeId)}))]} /></div><div className={inputGroup}><Label htmlFor="segment-budget">{t("budget")}</Label><Input id="segment-budget" type="number" min={0} max={100} step={1} value={budget} onChange={e=>setBudget(e.target.value)}/></div><div className={inputGroup}><Label htmlFor="segment-rounds">{t("rounds")}</Label><Input id="segment-rounds" type="number" min={1} max={20} step={1} value={rounds} onChange={e=>setRounds(e.target.value)}/></div></fieldset>
     {modeId&&!selected?<p className="mt-2 text-sm" role="status">{t("modeUnavailable")}</p>:null}
     <div className="mt-3 rounded-xl bg-[var(--color-panel-soft)] p-3">{!fun?<label className="flex min-h-11 items-center gap-3 text-sm font-semibold"><input type="checkbox" className="size-5 shrink-0" checked={competitive} onChange={e=>setCompetitive(e.target.checked)} disabled={disabled}/>{t("competitive")}</label>:null}{fun||!competitive?<p className="text-sm leading-6">{t("funHint")}</p>:null}</div>
     <Button type="submit" className="mt-3" block disabled={disabled||!selected||config.isPending||config.isError||modes.isPending||modes.isError||!validLabel||validBudget===null||validRounds===null}>{t("add")}</Button>
    </form>}</section>
    <EntertainmentResults segments={segments}/>
    <section className="uma-panel min-w-0 p-4" aria-labelledby="correction-heading"><h2 id="correction-heading" className="font-bold">{t("correction")}</h2><p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("correctionHint")}</p><form noValidate onSubmit={e=>{e.preventDefault();correct();}}><fieldset disabled={disabled} className="mt-3 grid min-w-0 gap-3 sm:grid-cols-2"><div className={inputGroup}><Label htmlFor="correction-segment">{t("correctionSegment")}</Label><SelectField disabled={disabled} id="correction-segment" value={correctionSegment} onValueChange={value =>{setCorrectionSegment(value);setSubjectId("");}} options={[({value:"",label:t("chooseSegment")}),...correctionOptions.map((s,i)=>({value:s.segmentId,label:eventSegmentLabel(s,i)}))]} /></div><div className={inputGroup}><Label htmlFor="correction-subject">{t("subject")}</Label><SelectField disabled={disabled} id="correction-subject" value={subjectId} onValueChange={value =>setSubjectId(value)} options={[({value:"",label:t("chooseSubject")}),...subjects.map(s=>({value:s.subjectId,label:<> {displayNameOf(s)}{s.publicCode?` · ${s.publicCode}`:""} </>}))]} /></div><div className={inputGroup}><Label htmlFor="correction-score">{t("newScore")}</Label><Input id="correction-score" type="number" min={0} max={chosenSegment?.scoreBudget??100} step={0.01} value={score} onChange={e=>setScore(e.target.value)}/></div><div className={`${inputGroup} sm:col-span-2`}><Label htmlFor="correction-reason">{t("reason")}</Label><Input id="correction-reason" value={reason} onChange={e=>setReason(e.target.value)} placeholder={t("reasonPlaceholder")}/></div></fieldset><Button type="submit" variant="secondary" className="mt-3" block disabled={disabled||!validCorrection}>{t("submitCorrection")}</Button></form>{!correctionOptions.length?<p className="mt-2 text-sm">{t("noCorrection")}</p>:null}</section>
   </div>
   <aside className="uma-stack min-w-0 lg:col-start-2 lg:row-start-2"><section className="uma-panel min-w-0 p-4" aria-labelledby="preview-heading"><div className="flex flex-wrap items-center justify-between gap-2"><h2 id="preview-heading" className="font-bold">{t("preview")}</h2><Link to={`/events/${encodeURIComponent(eventId!)}/results`} className="uma-link inline-flex min-h-11 items-center text-sm">{t("fullResults")}</Link></div>{!items.length?<p className="mt-2 text-sm">{t("noResults")}</p>:<ol className="mt-2 max-h-64 space-y-2 overflow-y-auto">{items.map(item=><li key={item.subjectId} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--color-line)] p-2 text-sm"><span className="min-w-0 break-words">{rt("rankValue",{rank:item.rank})}<EventSubjectName subject={item}/></span><strong className="tabular-nums">{formatScoreUnits(item.scoreUnits)}</strong></li>)}</ol>}</section>
    <section className="uma-danger-zone" aria-labelledby="danger-heading"><h2 id="danger-heading" className="flex items-center gap-2 font-bold"><AlertTriangle aria-hidden="true" className="size-4"/>{t("danger")}</h2><p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("dangerHint")}</p><div className="mt-3 flex flex-col gap-2">{event.status==="running"?<Button variant="secondary" disabled={disabled} onClick={()=>setDanger("completed")} block>{t("finish")}</Button>:null}{!closed?<Button variant="danger" disabled={disabled} onClick={()=>setDanger("cancelled")} block>{t("cancel")}</Button>:null}</div></section>
   </aside>
  </div>
  <Dialog open={danger!==null} onOpenChange={open=>{if(!open)setDanger(null);}}><DialogContent><DialogHeader><DialogTitle>{t(danger==="completed"?"finishTitle":"cancelTitle")}</DialogTitle><DialogDescription>{t(danger==="completed"?"finishDetail":"cancelDetail",{name:event.name})}</DialogDescription></DialogHeader><DialogFooter><Button variant="secondary" onClick={()=>setDanger(null)}>{t("back")}</Button><Button variant="danger" disabled={disabled} onClick={()=>{if(danger)transition(danger);}}>{t(danger==="completed"?"finish":"cancel")}</Button></DialogFooter></DialogContent></Dialog>
 </div>;
}
export function isEventClosed(event:EventMeta|null):boolean{return event?.status==="completed"||event?.status==="cancelled";}
