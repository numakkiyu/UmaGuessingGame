import { roomDisplayName } from "@/lib/room-display-name";
import { GameStage } from "@/components/uma/game-stage";
import "@/i18n/strategy-online";
import {useMemo, useState} from "react";
import {Link, useParams} from "react-router";
import {ChevronDown, Loader2} from "lucide-react";
import {useTranslation} from "@/i18n";
import {strategyAction} from "@/i18n/strategy";
import {strategyOnlineCopy} from "@/i18n/strategy-online";
import {usePlayerText} from "@/i18n/player-text";
import {TeamRoundControl} from "@/features/rooms/team-round-control";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView, ErrorView, ForbiddenView, LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {RaceTrack} from "./race-track";
import {useApi} from "@/lib/api";
import {ApiError, describeFailure, localizeFailure, type Failure} from "@/lib/api/client";
import {isActionAvailable, normalizeStrategyRaceRound, type RaceActionSpec, type StrategyRaceRound} from "@/lib/api/modes-g09-g12";
import {validStrategyCommand, type StrategyCommand} from "@/lib/api/game-commands";
import {useRoom} from "@/lib/api/room-queries";
import {readTeamMembers, readTeams} from "@/lib/api/teams";
import {phaseLabel, resultsOf, useRoundSnapshot} from "@/lib/api/round-snapshot";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {clearStrategyDraft, isStrategyChoice, loadStrategyDraft, saveStrategyDraft, type StrategyChoice} from "@/lib/local/strategy-online-draft";
import {useRealtime} from "@/lib/realtime/use-realtime";
import {cn} from "@/lib/utils";
import {RejoinPrompt, isCredentialRefreshFailure} from "./rejoin-prompt";

function signed(n:number) { return n>0?`+${n}`:String(n); }
export function strategyScores(items:readonly unknown[]):{subjectId:string;scoreUnits:number;rank:number|null;shared:boolean}[] {
 return items.flatMap(item=>{
  if(!item||typeof item!=="object")return [];
  const r=item as Record<string,unknown>;
  return typeof r.subjectId==="string"&&typeof r.scoreUnits==="number"&&Number.isSafeInteger(r.scoreUnits)&&r.scoreUnits>=0?[{subjectId:r.subjectId,scoreUnits:r.scoreUnits,rank:typeof r.rank==="number"&&Number.isSafeInteger(r.rank)&&r.rank>0?r.rank:null,shared:r.sharedRank===true}]:[];
 });
}
function ActionButton({spec,round,selected,disabled,onSelect}:{spec:RaceActionSpec;round:StrategyRaceRound;selected:boolean;disabled:boolean;onSelect:()=>void}) {
 const {t}=useTranslation("strategyOnline"), available=isActionAvailable(spec,round.self.stamina);
 return <button type="button" onClick={onSelect} disabled={disabled||!available} aria-pressed={selected} aria-label={strategyAction(spec.action)} className={cn("flex min-h-11 min-w-0 flex-col gap-1 rounded-[var(--radius-control)] border-2 p-3 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",selected?"border-[var(--color-blue-deep)] bg-[var(--color-page-tint)]":"border-[var(--color-line-strong)] bg-[var(--color-panel)]",!available&&"opacity-60")}>
  <span className="font-bold">{strategyAction(spec.action)}</span>
  <span className="text-sm">{t("positionGain",{gain:spec.positionGain})} · {spec.staminaDelta===0?t("staminaUnchanged"):t("staminaChange",{delta:signed(spec.staminaDelta)})}</span>
  {spec.minStamina!==null?<span className="text-xs leading-5 text-[var(--color-muted)]">{available?t("minimum",{minimum:spec.minStamina}):t("shortage",{stamina:round.self.stamina})}</span>:null}
  {spec.leaderGap?<span className="text-xs leading-5 text-[var(--color-muted)]">{t("follow",{min:round.followMinGap,max:round.followMaxGap,gain:spec.positionGain,alternate:spec.alternateGain??0})}</span>:null}
  {spec.action==="recover"?<span className="text-xs text-[var(--color-muted)]">{t("cap",{cap:round.staminaCap})}</span>:null}
 </button>;
}
export function StrategyResultPanel({round,results,labelFor,phase}:{round:StrategyRaceRound;results:readonly unknown[];labelFor:(id:string,index:number)=>string;phase:string|null}) {
 const {t}=useTranslation("strategyOnline");
 if(phase==="cancelled")return <Alert tone="info" title={t("cancelled")}><p>{t("cancelledDetail")}</p></Alert>;
 if(phase!=="revealed"&&phase!=="settled")return null;
 if(!round.final.length)return null;
 const scores=strategyScores(results), rows=round.final.map((p,i)=>{const score=scores.find(s=>s.subjectId===p.subjectId);return {...p,label:labelFor(p.subjectId,i),score:score?String(score.scoreUnits/100):"—",rank:score?.rank?t(scores.filter(s=>s.rank===score.rank).length>1?"shared":"rankValue",{rank:score.rank}):"—"};});
 return <section className="uma-panel min-w-0 p-4" aria-labelledby="strategy-result"><h2 id="strategy-result" className="text-lg font-bold">{t("results")}</h2><p className="mt-1 text-sm text-[var(--color-muted)]">{t(round.competitive?"resultHint":"practice")}</p>
  {round.competitive&&scores.length<rows.length?<p className="mt-2 text-sm" role="status">{t("scoreWaiting")}</p>:null}
  <div className="mt-3 hidden md:block"><table className="w-full table-fixed text-left text-sm"><caption className="sr-only">{t("results")}</caption><thead><tr>{["participant","position","stamina",...(round.competitive?["rank","score"]:[])].map(k=><th key={k} className="p-2">{t(k)}</th>)}</tr></thead><tbody>{rows.map(r=><tr key={r.subjectId} className="border-t border-[var(--color-line)]"><th scope="row" className="break-words p-2 font-medium">{r.label}</th><td className="p-2">{r.position}</td><td className="p-2">{r.stamina}</td>{round.competitive?<><td className="p-2">{r.rank}</td><td className="p-2">{r.score}</td></>:null}</tr>)}</tbody></table></div>
  <ul className="mt-3 space-y-2 md:hidden">{rows.map(r=><li key={r.subjectId} className="rounded-lg border border-[var(--color-line)] p-3"><p className="break-words font-bold">{r.label}</p><dl className="mt-2 grid grid-cols-2 gap-2 text-sm">{[["position",r.position],["stamina",r.stamina],...(round.competitive?[["rank",r.rank],["score",r.score]]:[])].map(([k,v])=><div key={k}><dt className="text-[var(--color-muted)]">{t(String(k))}</dt><dd>{v}</dd></div>)}</dl></li>)}</ul>
 </section>;
}
export function StrategyHistory({round,labelFor}:{round:StrategyRaceRound;labelFor:(id:string,index:number)=>string}) {
 const {t}=useTranslation("strategyOnline");
 return <section className="uma-panel min-w-0 p-4" aria-labelledby="strategy-history"><h2 id="strategy-history" className="font-bold">{t("history")}</h2><p className="mt-1 text-sm text-[var(--color-muted)]">{t("historyHint")}</p>
  {!round.history.length?<p className="mt-3 text-sm">{t("noHistory")}</p>:null}
  {[...round.history].reverse().map(h=><details key={h.turn} className="uma-disclosure mt-3 rounded-lg border border-[var(--color-line)] p-3" open={h.turn===round.resolvedTurns}><summary className="flex min-h-11 cursor-pointer items-center justify-between gap-2 font-semibold">{t("historyTurn",{turn:h.turn})}<ChevronDown aria-hidden="true" className="uma-disclosure__chevron size-4"/></summary>
   <div className="hidden md:block"><table className="w-full table-fixed text-left text-sm"><caption className="sr-only">{t("historyTurn",{turn:h.turn})}</caption><thead><tr>{["participant","action","position","stamina"].map(k=><th key={k} className="p-2">{t(k)}</th>)}</tr></thead><tbody>{h.applied.map((p,i)=>{const before=h.start.find(s=>s.subjectId===p.subjectId);return <tr key={p.subjectId} className="border-t border-[var(--color-line)]"><th scope="row" className="break-words p-2 font-medium">{labelFor(p.subjectId,i)}</th><td className="break-words p-2">{strategyAction(p.action)}{p.defaulted?<span className="block text-xs text-[var(--color-muted)]">{t("automatic")}</span>:null}{p.leaderGap?<span className="block text-xs">{t("followBonus")}</span>:null}</td><td className="p-2">{before?.position??"—"} → {p.position}</td><td className="p-2">{before?.stamina??"—"} → {p.stamina}</td></tr>;})}</tbody></table></div>
   <ul className="space-y-2 md:hidden">{h.applied.map((p,i)=>{const before=h.start.find(s=>s.subjectId===p.subjectId);return <li key={p.subjectId} className="border-t border-[var(--color-line)] py-2 text-sm"><p className="break-words font-semibold">{labelFor(p.subjectId,i)}</p><p>{strategyAction(p.action)}{p.defaulted?` · ${t("automatic")}`:""}{p.leaderGap?` · ${t("followBonus")}`:""}</p><p className="text-[var(--color-muted)]">{t("historyPoint",{before:before?.position??"—",after:p.position})} · {t("historyStamina",{before:before?.stamina??"—",after:p.stamina})}</p></li>;})}</ul>
  </details>)}
 </section>;
}

export function StrategyRaceRoundPage() {
 const {t}=useTranslation("strategyOnline"), {roomId}=useParams<{roomId:string}>(), {api,session}=useApi();
 const roomQuery=useRoom(roomId??null), roundId=roomQuery.data?.room.currentRound?.roundId??null, snapshot=useRoundSnapshot(roundId);
 const round=useMemo(()=>snapshot.raw&&snapshot.meta?.roundId===roundId?normalizeStrategyRaceRound(snapshot.raw):null,[snapshot.raw,snapshot.meta?.roundId,roundId]);
 // Pending command scope stays on the round: a later turn/driver must not rebase it.
 const scope=session&&roundId?`${session.sessionId}:${roundId}:strategy`:null;
 const draftScope=scope&&round?.self.subjectId?`${scope}:${round.self.subjectId}:${round.currentTurn}:${snapshot.controlGeneration}`:null;
 const command=useGameCommand(scope,validStrategyCommand,(body,key)=>api.createSubmission(roundId!,body,key));
 const [selection,setSelection]=useState<StrategyChoice|null>(null),[initialized,setInitialized]=useState<string|null>(null);
 const [refreshing,setRefreshing]=useState(false),[failure,setFailure]=useState<Failure|null>(null),[errorCode,setErrorCode]=useState(""),[rejected,setRejected]=useState(false),[needsRejoin,setNeedsRejoin]=useState(false),[notice,setNotice]=usePlayerText("strategyOnline");
 if(draftScope&&initialized!==draftScope){setInitialized(draftScope);const saved=loadStrategyDraft(draftScope);setSelection(saved?.action??null);}
 const realtime=useRealtime({roomId:roomId??null,enabled:!!roomId,handlers:{onSnapshotNeeded:()=>{void snapshot.refetch();void roomQuery.refetch();}}});
 const phase=snapshot.meta?.phase??null, collecting=phase==="collecting"&&!snapshot.meta?.paused, cancelled=phase==="cancelled", reveal=phase==="revealed"||phase==="settled";
 const countdown=useCountdown(snapshot.meta?.deadlineAt,collecting), own=round?.self.choices.find(c=>c.turn===round.currentTurn);
 const busy=command.busy||refreshing||!!command.pending||command.expired||snapshot.isError;
 const editable=!!round?.hasSelf&&collecting&&snapshot.canSubmit&&!busy&&!countdown.expired&&!own&&round.currentTurn<=round.totalTurns;
 const knownError=Object.hasOwn(strategyOnlineCopy["zh-Hans"].errors,errorCode);
 const info=failure?knownError?{...failure,title:t("rejectedTitle"),detail:t(`errors.${errorCode}`)}:rejected?{...failure,title:t("rejectedTitle"),detail:t("rejectedDetail")}:localizeFailure(failure):null;
 function labelFor(id:string,index:number){const team=readTeams(roomQuery.data?.room).find(x=>x.teamId===id);if(team)return team.label;const member=readTeamMembers(roomQuery.data?.room).find(x=>x.membershipId===id);return member?/^(?:\d){6}$/.test(member.publicCode??"")?`${member.displayName} · ${member.publicCode}`:member.displayName:t("participantNumber",{number:index+1});}
 function edit(action:StrategyChoice|null){if(!editable)return;setSelection(action);setNotice("");if(draftScope&&!saveStrategyDraft(draftScope,action))setNotice("saveFailed");}
 async function submit(retry=false){
  if(!roundId||command.busy||refreshing)return;
  const spec=round?.actions.find(a=>a.action===selection);
  if(!retry&&(!round||!editable||!isStrategyChoice(selection)||!spec||!isActionAvailable(spec,round.self.stamina)))return;
  const body:StrategyCommand|undefined=retry||!round?undefined:{phaseVersion:snapshot.meta!.phaseVersion,submissionRevision:snapshot.submissionRevision,controlGeneration:snapshot.controlGeneration,modeAction:{kind:"strategy.action",turn:round.currentTurn,action:selection!}};
  setRefreshing(true);setFailure(null);setErrorCode("");setRejected(false);setNotice("");
  try{const result=await command.run(body);if(!result)return;
   if(!result.ok){setFailure(describeFailure(result.error));setErrorCode(result.error instanceof ApiError?result.error.code:"");if(!result.uncertain&&result.error instanceof ApiError&&result.error.isConflict)await snapshot.refetch();if(isCredentialRefreshFailure(result.error))setNeedsRejoin(true);return;}
   if(result.value.data?.outcome!=="accepted"){setRejected(true);setFailure({kind:"error",title:"",detail:"",diagnosticId:result.value.requestId,retryable:true});return;}
   // Confirming an older uncertain command must not erase a new turn's draft.
   if(!retry&&draftScope)clearStrategyDraft(draftScope);
   setNotice("accepted");await snapshot.refreshAfterSubmit();
  }finally{setRefreshing(false);}
 }
 if(!roomId)return <ErrorView failure={{kind:"error",title:t("missingTitle"),detail:t("missingDetail"),diagnosticId:null,retryable:false}}/>;
 if(roomQuery.isLoading)return <LoadingBlock label={t("loadingRoom")} rows={3}/>;
 if(roomQuery.isError){const f=describeFailure(roomQuery.error);return f.kind==="forbidden"?<ForbiddenView failure={f} onRetry={()=>void roomQuery.refetch()}/>:<ErrorView failure={f} onRetry={()=>void roomQuery.refetch()}/>;}
 return <GameStage mode="strategy-race" className="uma-stack">
  <TeamRoundControl room={roomQuery.data?.room} raw={snapshot.raw} onChanged={snapshot.refetch}/>
  <ConnectionBar status={realtime.status} detail={roomDisplayName(roomQuery.data?.room, t("room"))}/>
  {needsRejoin||isCredentialRefreshFailure(snapshot.error)?<RejoinPrompt roomId={roomId}/>:null}
  {snapshot.isLoading?<LoadingBlock label={t("loading")} rows={4}/>:null}
  {snapshot.isError?<ErrorView failure={describeFailure(snapshot.error)} onRetry={()=>void snapshot.refetch()}/>:null}
  {command.pending?<Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button className="mt-2 h-auto min-h-11 whitespace-normal" disabled={command.busy||refreshing} onClick={()=>void submit(true)}>{command.busy?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:null}{t("confirmAction")}</Button></Alert>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button className="mt-2 h-auto min-h-11 whitespace-normal" disabled={refreshing} onClick={()=>void (async()=>{setRefreshing(true);try{if(await snapshot.refetch())command.acknowledgeExpired();}finally{setRefreshing(false);}})()}>{t("acknowledge")}</Button></Alert>:null}
  {command.storageLimited?<Alert tone="warning" title={t("notice")}><p>{t("storage")}</p></Alert>:null}
  {info?<Alert tone="danger" title={info.title}><p>{info.detail}</p></Alert>:null}
  {notice?<p role="status" className="text-sm">{notice}</p>:null}
  {round&&round.actions.length?<>
   <section className="uma-panel uma-game-detailed-heading min-w-0 p-4"><div className="uma-game-summary-copy"><h1 className="u-fluid-task">{t("title")}</h1><p className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-sm text-[var(--color-muted)]"><span>{phaseLabel(phase)}</span><span>{t("turn",{turn:Math.min(round.currentTurn,round.totalTurns),total:round.totalTurns})}</span><span>{t("window",{seconds:round.decisionSeconds})}</span>{collecting&&countdown.text?<span>{countdown.text}</span>:null}</p></div>
    <RaceTrack className="mt-3" teams={round.standings.map((p,i)=>({...p,label:labelFor(p.subjectId,i),isSelf:p.subjectId===round.self.subjectId,avatarSrc:null}))} totalTurns={round.totalTurns} staminaCap={round.staminaCap} maxGainPerTurn={Math.max(1,...round.actions.flatMap(a=>[a.positionGain,a.alternateGain??0]))}/>
   </section>
   {!cancelled&&(phase==="locked"||reveal)&&round.currentTurn<=round.totalTurns?<Alert tone="warning" title={t("notice")}><p>{t("legacy")}</p></Alert>:null}
   <StrategyResultPanel round={round} results={resultsOf(snapshot.raw)} labelFor={labelFor} phase={phase}/>
   {!cancelled&&!reveal?<section className="uma-panel min-w-0 p-4" aria-labelledby="race-actions"><h2 id="race-actions" className="font-bold">{t("decide")}</h2><p className="mt-1 text-sm text-[var(--color-muted)]">{t("defaultHint",{action:strategyAction(round.defaultAction)})}</p>
    {!round.hasSelf?<p className="mt-3 text-sm">{t("observer")}</p>:<>
     <p className="mt-2 text-sm" role="status">{snapshot.meta?.paused?t("paused"):phase!=="collecting"?t(phase==="locked"?"waiting":"beforeStart"):round.currentTurn>round.totalTurns?t("finished"):own?t("confirmed",{action:strategyAction(own.action)}):countdown.expired?t("deadlinePassed"):!snapshot.canSubmit?t("notController"):selection?t("selected",{action:strategyAction(selection)}):t("select")}</p>
     <div className="mt-3 grid gap-2 sm:grid-cols-2">{round.actions.map(spec=><ActionButton key={spec.action} spec={spec} round={round} selected={selection===spec.action} disabled={!editable} onSelect={()=>{if(isStrategyChoice(spec.action))edit(spec.action);}}/>)}</div>
     <div className="mt-3 flex flex-wrap gap-2"><Button variant="primary" size="primary" className="h-auto min-h-11 flex-1 whitespace-normal" disabled={!editable||!selection||!isActionAvailable(round.actions.find(a=>a.action===selection)??round.actions[0]!,round.self.stamina)} onClick={()=>void submit()}>{command.busy?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:null}{t("confirm")}</Button><Button variant="secondary" className="h-auto min-h-11 whitespace-normal" disabled={!editable||!selection} onClick={()=>edit(null)}>{t("clear")}</Button></div>
    </>}
   </section>:null}
   <StrategyHistory round={round} labelFor={labelFor}/>
   <details className="uma-card uma-disclosure min-w-0 p-4"><summary className="flex min-h-11 cursor-pointer items-center justify-between gap-2 font-semibold">{t("rules")}<ChevronDown aria-hidden="true" className="uma-disclosure__chevron size-4"/></summary><ul className="mt-2 list-disc space-y-2 pl-5 text-sm leading-6 text-[var(--color-muted)]"><li>{t("ruleLine",{turns:round.totalTurns,seconds:round.decisionSeconds,start:round.startStamina,cap:round.staminaCap})}</li><li>{t("ruleTiming")}</li><li>{t(round.competitive?"ruleScore":"practice")}</li><li>{t("simulation")}</li></ul></details>
  </>:!snapshot.isLoading&&!snapshot.isError?<EmptyView title={t("emptyTitle")} detail={t("emptyDetail")}/>:null}
  <Button asChild variant="secondary" size="primary"><Link to="/">{t("back")}</Link></Button>
 </GameStage>;
}
