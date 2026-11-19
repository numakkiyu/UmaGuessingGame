import { useMatchCurtain } from "@/components/uma/match-curtain-provider";
import { usePlayerName } from "@/lib/player-name";
import { MenuCharacterArt } from "@/components/uma/menu-character-art";

import "@/i18n/matchmaking";
import { useTranslation } from "@/i18n";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { Check, ChevronDown, CircleAlert, Loader2, Radio, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { SelectField } from "@/components/ui/select";
import { ErrorView, LoadingBlock } from "@/components/uma/page-state";
import { useApi } from "@/lib/api";
import { ApiError, createIdempotencyKey, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import type { components } from "@/lib/api/types";
import { prepareMatchWork } from "./matchmaking-work";
import { cn } from "@/lib/utils";

type Ticket = components["schemas"]["Ticket"];
type Lobby = components["schemas"]["MatchLobby"];
type CreateBody = components["schemas"]["TicketCreate"];
const terminal = (t:Ticket) => ["cancelled","expired","finished"].includes(t.status);
const knownPools = ["classic-duel","classic-four","sort-four","memory-four"];

export function MatchmakingPage() {
  const { t } = useTranslation("matchmaking");
  const { api,session,ensureSession,restoreSession } = useApi();
  const navigate = useNavigate();
  const curtain = useMatchCurtain();
  const [params] = useSearchParams();
  const [lobby,setLobby] = useState<Lobby|null>(null);
  const [lobbyFailure,setLobbyFailure] = useState<Failure|null>(null);
  const [poolId,setPoolId] = useState(params.get("pool") ?? (params.get("mode")==="sort" ? "sort-four" : params.get("mode")==="memory" ? "memory-four" : "classic-duel"));
  const displayName = usePlayerName();
  const [ticket,setTicket] = useState<Ticket|null>(null);
  const [busy,setBusy] = useState<"create"|"accept"|"decline"|"cancel"|null>(null);
  const [createLocked,setCreateLocked] = useState(false);
  const [retryAction,setRetryAction] = useState<"accept"|"decline"|"cancel"|null>(null);
  const [restoring,setRestoring] = useState(true);
  const [failure,setFailure] = useState<Failure|null>(null);
  const [now,setNow] = useState(()=>Date.now());
  const [clockOffset,setClockOffset] = useState(0);
  const [disconnected,setDisconnected] = useState(false);
  const mounted = useRef(true);
  const request = useRef<AbortController|null>(null);
  const [queueStarted,setQueueStarted] = useState<{id:string;at:number}|null>(null);
  const pendingCreate = useRef<{body:CreateBody;key:string}|null>(null);
  const pendingAction = useRef<{kind:"accept"|"decline"|"cancel";key:string;ticketId:string}|null>(null);
  const busyRef=useRef(false);
  const activeId = ticket && !terminal(ticket) ? ticket.ticketId : null;
  const pending = ticket && !terminal(ticket) && !ticket.roomId;

  const receive = useCallback((next:Ticket) => {
    if(!mounted.current)return;
    setQueueStarted(previous => previous?.id===next.ticketId ? previous : {id:next.ticketId,at:Date.now()});
    setTicket(next);setDisconnected(false);
    setClockOffset(Date.parse(next.serverTime)-Date.now());
    if(!curtain && next.roomId && (next.status==="ready" || next.status==="playing")) navigate(`/rooms/${encodeURIComponent(next.roomId)}`,{replace:true});
  },[navigate,curtain]);

  useEffect(()=>{
    mounted.current=true;
    return ()=>{mounted.current=false;request.current?.abort();};
  },[]);

  useEffect(()=>{
    const controller=new AbortController();let reading=false;
    const read=async()=>{
      if(reading || controller.signal.aborted)return;
      reading=true;
      try{
        const response=await api.getMatchmakingLobby(controller.signal);
        if(controller.signal.aborted)return;
        if(response.data){setLobby(response.data);setLobbyFailure(null);}
      }catch(error){if(!controller.signal.aborted)setLobbyFailure(describeFailure(error));}
      finally{reading=false;}
    };
    void read();
    const timer=window.setInterval(()=>{if(document.visibilityState==="visible")void read();},5000);
    return ()=>{controller.abort();window.clearInterval(timer);};
  },[api]);

  useEffect(()=>{
    let disposed=false;
    void(async()=>{
      try{
        const restored=session??await restoreSession?.();
        if(!restored || disposed)return;
        const response=await api.getActiveMatchmakingTicket();
        if(!disposed && response.data?.ticket)receive(response.data.ticket);
      }catch(error){if(!disposed){setFailure(describeFailure(error));setDisconnected(true);}}
      finally{if(!disposed)setRestoring(false);}
    })();
    return ()=>{disposed=true;};
    // Restoration belongs to this visit; creating a session must not restore over a new ticket.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  },[api,restoreSession,receive]);

  useEffect(()=>{
    if(!activeId || ticket?.roomId)return;
    const controller=new AbortController();let reading=false;
    const read=async()=>{
      if(reading || controller.signal.aborted)return;
      reading=true;
      try{
        const response=await api.heartbeatMatchmakingTicket(activeId,controller.signal);
        if(response.data && !controller.signal.aborted)receive(response.data);
      }catch(error){
        if(!controller.signal.aborted){setDisconnected(true);if(error instanceof ApiError && error.status===404){setFailure(describeFailure(error));setTicket(previous => previous ? {...previous,status:"expired",roomId:null} : null);}}
      }finally{reading=false;}
    };
    const timer=window.setInterval(()=>{if(document.visibilityState==="visible")void read();},3000);
    const visible=()=>{if(document.visibilityState==="visible")void read();};
    document.addEventListener("visibilitychange",visible);
    return ()=>{controller.abort();window.clearInterval(timer);document.removeEventListener("visibilitychange",visible);};
  },[activeId,ticket?.roomId,api,receive]);

  useEffect(()=>{
    if(!pending)return;
    const timer=window.setInterval(()=>setNow(Date.now()),1000);
    return ()=>window.clearInterval(timer);
  },[pending]);

  async function startQueue(){
    if(busyRef.current)return;
    busyRef.current=true;setBusy("create");setFailure(null);
    const controller=new AbortController();request.current=controller;
    try{
      await ensureSession(displayName.trim()||t("player"));
      // Recover an uncertain creation before spending another admission challenge.
      if(!pendingCreate.current){
        const active=await api.getActiveMatchmakingTicket(controller.signal);
        if(active.data?.ticket){receive(active.data.ticket);return;}
        const proof=await api.createMatchmakingProof({poolId},controller.signal);
        if(!proof.data)throw new Error("Missing admission challenge");
        const nonce=await prepareMatchWork(proof.data.challengeId,proof.data.bits,controller.signal);
        setCreateLocked(true);
        pendingCreate.current={body:{poolId,proof:{challengeId:proof.data.challengeId,nonce}},key:createIdempotencyKey()};
      }
      const command=pendingCreate.current;
      const response=await api.createMatchmakingTicket(command.body,command.key,controller.signal);
      if(!response.data)throw new Error("Missing matchmaking ticket");
      pendingCreate.current=null;setCreateLocked(false);pendingAction.current=null;setRetryAction(null);receive(response.data);
    }catch(error){
      if(controller.signal.aborted)return;
      // A definitive refusal may obtain new admission. Uncertain results reuse the exact body and key.
      if(error instanceof ApiError && error.status>=400 && error.status<500 && error.status!==408){pendingCreate.current=null;setCreateLocked(false);}
      if(mounted.current)setFailure(describeFailure(error));
    }finally{busyRef.current=false;if(mounted.current)setBusy(null);}
  }

  async function actOnMatch(kind:"accept"|"decline"|"cancel"){
    if(!ticket || busyRef.current)return;
    busyRef.current=true;setBusy(kind);setFailure(null);
    const action=pendingAction.current??{kind,key:createIdempotencyKey(),ticketId:ticket.ticketId};
    pendingAction.current=action;setRetryAction(action.kind);
    try{
      if(action.kind==="cancel"){
        await api.cancelMatchmakingTicket(action.ticketId,undefined,action.key);
        if(mounted.current)setTicket({...ticket,status:"cancelled",roomId:null});
      }else{
        const response=await api.respondToMatch(action.ticketId,{accept:action.kind==="accept"},undefined,action.key);
        if(!response.data)throw new Error("Missing match response");
        receive(response.data);
      }
      pendingAction.current=null;setRetryAction(null);
    }catch(error){
      if(error instanceof ApiError && error.status>=400 && error.status<500 && error.status!==408){pendingAction.current=null;setRetryAction(null);}
      if(mounted.current)setFailure(describeFailure(error));
      try{const response=await api.getMatchmakingTicket(action.ticketId);if(response.data)receive(response.data);}catch{if(mounted.current)setDisconnected(true);}
    }finally{busyRef.current=false;if(mounted.current)setBusy(null);}
  }

  const pools=lobby?.pools.filter(p=>knownPools.includes(p.poolId))??[];
  const selected=pools.find(p=>p.poolId===poolId)??pools[0];
  useEffect(()=>{if(selected && selected.poolId!==poolId && !pendingCreate.current)setPoolId(selected.poolId);},[selected,poolId]);
  const info=failure?localizeFailure(failure):null;
  const closed=lobbyFailure?.kind==="forbidden";
  const confirm=ticket?.status==="matched-pending-accept";
  const seconds=ticket?Math.max(0,Math.ceil((Date.parse(ticket.expiresAt)-now-clockOffset)/1000)):0;
  const waitingSeconds=ticket?Math.max(0,Math.floor((now-(queueStarted?.at??now))/1000)):0;
  const label=ticket?.status==="ready" && !ticket.roomId?"acceptedLabel":`statuses.${ticket?.status??"queueing"}.label`;
  const hint=ticket?.status==="ready" && !ticket.roomId?"acceptedHint":`statuses.${ticket?.status??"queueing"}.hint`;

  const ticketContent = ticket ? <section className={cn("uma-panel p-5 sm:p-7",confirm && "border-[var(--color-gold)] ring-2 ring-[var(--color-gold)]")} aria-label={t(label)}>
      <div className="mc-status-heading flex items-start gap-3">
        {confirm?<CircleAlert aria-hidden="true" className="mt-1 size-6 shrink-0 text-[var(--color-near-edge)]"/>:<Radio aria-hidden="true" className={cn("mt-1 size-6 shrink-0 text-[var(--color-action-green)]",pending && "motion-safe:animate-pulse")}/>}
        <div className="min-w-0"><h2 className="text-xl font-bold">{t(label)}</h2><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t(hint)}</p><p className="mt-2 font-semibold">{t(`pools.${ticket.poolId}.name`)}</p></div>
      </div>
      {pending?<div className="mc-timing mt-5 flex flex-wrap items-center gap-3 text-sm font-semibold tabular-nums" role="status">
        <span>{ticket.status==="queueing"?t("queueTime",{count:waitingSeconds}):t("acceptedCount",{accepted:ticket.accepted,total:ticket.players})}</span>
        {ticket.status!=="queueing"?<span className="text-[var(--color-near-edge)]">{t("confirmCountdown",{count:seconds})}</span>:null}
      </div>:null}
      <div className="mc-actions mt-5 grid gap-3 sm:grid-cols-2">
        {retryAction && failure?<Button variant="primary" size="primary" disabled={!!busy} onClick={()=>void actOnMatch(retryAction)}>{t("retry")}</Button>:<>
          {confirm?<Button variant="primary" size="primary" disabled={!!busy || seconds===0} onClick={()=>void actOnMatch("accept")}><Check aria-hidden="true" className="size-4"/>{t("accept")}</Button>:null}
          {pending?<Button variant="secondary" size="primary" disabled={!!busy} onClick={()=>void actOnMatch(confirm?"decline":"cancel")}>{busy?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:null}{t(busy==="cancel"?"cancelling":confirm?"decline":"cancel")}</Button>:null}
        </>}
        {terminal(ticket)?<Button variant="primary" size="primary" onClick={()=>{setTicket(null);setFailure(null);pendingAction.current=null;setRetryAction(null);}}>{t("again")}</Button>:null}
      </div>
      <p className="mt-4 text-sm leading-6 text-[var(--color-muted)]">{t(terminal(ticket)?"cooldown":"stay")}</p>
    </section> : null;
  useEffect(() => {
    if (!curtain) return;
    if (!ticket) { curtain.dismiss(); return; }
    curtain.show({
      id: ticket.ticketId,
      title: t(label), description: t(hint),
      content: <>{ticketContent}{disconnected ? <p role="status">{t("connectionHint")}</p> : null}</>,
      failed: terminal(ticket),
      ready: !!ticket.roomId && (ticket.status === "ready" || ticket.status === "playing"),
      onCovered: () => { if (ticket.roomId) navigate(`/rooms/${encodeURIComponent(ticket.roomId)}`, { replace: true }); },
      onCancel: () => { if (!ticket.roomId && !terminal(ticket) && !busyRef.current) void actOnMatch(confirm ? "decline" : "cancel"); },
    });
    // Controls carry the current ticket and retry identity into the shell-owned overlay.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [curtain, ticket, busy, retryAction, failure, disconnected, now, clockOffset, t, label, hint, navigate]);
  useEffect(() => () => curtain?.dismiss(), [curtain]);

  return <div className="uma-stack mx-auto w-full max-w-5xl">
    <header className="uma-match-header uma-panel">
      <div className="uma-match-header-copy"><h1 className="u-fluid-title">{t("title")}</h1><p className="mt-2 text-base leading-7 text-[var(--color-muted)]">{t("intro")}</p></div><MenuCharacterArt pool={selected?.poolId ?? "classic-duel"} className="uma-match-hero-art" />
      {lobby && !closed ? <section className="mt-5 grid grid-cols-2 gap-2" aria-label={t("online")}>
        {(["queued","confirming","playing","rooms"] as const).map(key=><p key={key} className="rounded-xl bg-[var(--color-panel-soft)] p-3 text-sm font-semibold tabular-nums">{t(key==="rooms"?"roomCount":`${key}Count`,{count:lobby[key]})}</p>)}
      </section>:null}
      {lobbyFailure?<p className="mt-3 text-sm text-[var(--color-muted)]">{t("statsUnavailable")}</p>:null}
    </header>
    {restoring?<LoadingBlock label={t("recovering")} rows={1}/>:null}
    {lobbyFailure && !lobby && !closed ? <ErrorView failure={lobbyFailure} onRetry={()=>window.location.reload()} /> : null}
    {closed && !ticket ? <Alert tone="info" title={t("closed")}>{t("closedDetail")}</Alert>:null}
    {info?<Alert fault tone="warning" title={info.title}><p>{info.detail}</p></Alert>:null}
    {disconnected && ticket?<Alert tone="warning" title={t("connectionHint")}/>:null}
    {ticket && (!curtain || terminal(ticket)) ? ticketContent : null}
    {!ticket && !closed && pools.length>0 ? <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1.4fr)_minmax(16rem,1fr)]">
      <section className="hidden lg:block" aria-labelledby="pool-heading"><h2 id="pool-heading" className="mb-3 text-lg font-bold">{t("choose")}</h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-1" role="group" aria-labelledby="pool-heading">
          {pools.map(p=><button key={p.poolId} type="button" aria-pressed={selected?.poolId===p.poolId} disabled={!!busy || createLocked} onClick={()=>setPoolId(p.poolId)} className={cn("uma-match-pool uma-match-art-card min-w-0 rounded-[var(--radius-card)] border text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)] disabled:cursor-wait",selected?.poolId===p.poolId?"border-[var(--color-brand-green)] bg-[var(--color-brand-green-soft)]":"border-[var(--color-line)] bg-[var(--color-panel)]")}>
            <MenuCharacterArt pool={p.poolId} className="uma-match-card-art" />
            <span className="flex items-start justify-between gap-3 font-bold"><span>{t(`pools.${p.poolId}.name`)}</span><Users aria-hidden="true" className="mt-1 size-4 shrink-0"/></span>
            <span className="mt-2 block text-sm leading-6 text-[var(--color-muted)]">{t(`pools.${p.poolId}.rule`)}</span>
            <span className="mt-3 block text-sm font-semibold text-[var(--color-action-green)]">{t("poolQueue",{count:p.queued})}</span>
          </button>)}
        </div>
      </section>
      <section className="uma-panel p-5 sm:p-6" aria-labelledby="queue-start">
        <MenuCharacterArt pool={selected?.poolId ?? "classic-duel"} className="uma-match-selected-art" />
        <div className="mb-5 flex flex-col gap-2 lg:hidden"><Label htmlFor="match-pool">{t("choose")}</Label><SelectField id="match-pool" value={selected?.poolId ?? ""} onValueChange={setPoolId} disabled={!!busy || createLocked} options={pools.map(p=>({value:p.poolId,label:t(`pools.${p.poolId}.name`)}))}/><p className="text-sm leading-6 text-[var(--color-muted)]">{selected?t(`pools.${selected.poolId}.rule`):""}</p><p className="text-sm font-semibold text-[var(--color-action-green)]">{t("poolQueue",{count:selected?.queued??0})}</p></div>
        <details className="uma-disclosure"><summary id="queue-start" className="flex min-h-11 cursor-pointer items-center justify-between gap-3 font-semibold">{t("rules")}<ChevronDown aria-hidden="true" className="uma-disclosure__chevron size-4"/></summary><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("readyRule")}</p>{lobby && (lobby.idleSeconds??0)>0?<p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("idleRule",{count:lobby.idleSeconds})}</p>:null}</details>
        {busy==="create"?<p role="status" className="mt-3 text-sm leading-6 text-[var(--color-muted)]">{t("enteringDetail")}</p>:null}
        <Button className="mt-5" variant="primary" size="primary" disabled={!!busy || restoring || !selected} onClick={()=>void startQueue()} block>{busy?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:<Users aria-hidden="true" className="size-4"/>}{t(busy?"entering":failure?"retry":"start")}</Button>
        <p className="mt-4 text-sm leading-6 text-[var(--color-muted)]">{t("stay")}</p>
      </section>
    </div>:null}
    {!lobby && !lobbyFailure?<LoadingBlock label={t("title")} rows={2}/>:null}
    {!ticket && lobby && pools.length===0?<Alert tone="info" title={t("empty")}>{t("emptyDetail")}</Alert>:null}
    {!ticket && lobbyFailure && !closed?<Button variant="secondary" onClick={()=>window.location.reload()}>{t("retry")}</Button>:null}
    <div className="flex flex-wrap gap-3 border-t border-[var(--color-line)] pt-4"><Button asChild variant="secondary"><Link to="/">{t("home")}</Link></Button><Button asChild variant="secondary"><Link to="/friends">{t("friends")}</Link></Button></div>
  </div>;
}
