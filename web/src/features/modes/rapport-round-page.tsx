import { roomDisplayName } from "@/lib/room-display-name";
import { GameStage } from "@/components/uma/game-stage";
/** Each participant answers independently; group choices stay private. */
import "@/i18n/rapport-online";
import {useMemo, useState} from "react";
import {Link, useParams} from "react-router";
import {CheckCircle2, ChevronDown, Loader2} from "lucide-react";
import {useTranslation} from "@/i18n";
import {rapportOnlineCopy} from "@/i18n/rapport-online";
import {localizeRapportQuestion} from "@/i18n/rapport-questions";
import {usePlayerText} from "@/i18n/player-text";
import {TeamRoundControl} from "@/features/rooms/team-round-control";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView, ErrorView, ForbiddenView, LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {validRapportAnswer, type RapportAnswerCommand} from "@/lib/api/game-commands";
import {useApi} from "@/lib/api";
import {RejoinPrompt, isCredentialRefreshFailure} from "./rejoin-prompt";
import {ApiError, describeFailure, localizeFailure, type Failure} from "@/lib/api/client";
import {normalizeRapportRound, type RapportRound, type RapportQuestion} from "@/lib/api/modes-g09-g12";
import {useRoom} from "@/lib/api/room-queries";
import {readTeamMembers, readTeams} from "@/lib/api/teams";
import {isTerminalPhase, phaseLabel, useRoundSnapshot} from "@/lib/api/round-snapshot";
import {clearRapportDraft, loadRapportDraft, saveRapportDraft} from "@/lib/local/rapport-online-draft";
import {useRealtime} from "@/lib/realtime/use-realtime";
import {cn} from "@/lib/utils";

type Name=(id:string|null,index?:number)=>string;
function RapportHistory({round,name}:{round:RapportRound;name:Name}) {
 const {t,i18n}=useTranslation("rapportOnline");
 const history=round.questions.filter(q=>q.locked).map(q=>localizeRapportQuestion(q,i18n.language));
 if(!history.length)return null;
 function preference(q:RapportQuestion) {return !q.reveal?t("privateHistory"):q.reveal.chosenOptionId?t("chosen",{name:name(q.chooserId),label:q.reveal.chosenLabel}):t("missingChoice");}
 function outcome(q:RapportQuestion) {return !q.answered?t("missed"):q.selfIsChooser?t("ownChoice"):q.reveal?.correctPredictorIds.includes(round.self.subjectId??"")?t("hit"):t("miss");}
 return <details className="uma-panel uma-disclosure min-w-0 p-4" open={isTerminalPhase(round.phase)}><summary className="flex min-h-11 cursor-pointer items-center justify-between gap-2 font-semibold">{t("history",{count:history.length})}<ChevronDown aria-hidden="true" className="uma-disclosure__chevron size-4 shrink-0"/></summary><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("historyHint")}</p>
  <div className="mt-3 hidden md:block"><table className="w-full table-fixed text-left text-sm"><caption className="sr-only">{t("history",{count:history.length})}</caption><thead><tr><th className="w-2/5 p-2">{t("prompt")}</th><th className="p-2">{t("preference")}</th>{round.self.subjectId?<th className="w-1/4 p-2">{t("ownResult")}</th>:null}</tr></thead><tbody>{history.map((q,i)=><tr key={q.questionId} className="border-t border-[var(--color-line)]"><th scope="row" className="break-words p-2 font-medium leading-6">{i+1}. {q.prompt}</th><td className="break-words p-2 leading-6">{preference(q)}</td>{round.self.subjectId?<td className="break-words p-2 leading-6">{outcome(q)}</td>:null}</tr>)}</tbody></table></div>
  <ol className="mt-3 space-y-3 md:hidden">{history.map((q,i)=><li key={q.questionId} className="rounded-[var(--radius-control)] border border-[var(--color-line)] p-3"><h3 className="break-words font-semibold leading-7">{i+1}. {q.prompt}</h3><p className="mt-1 break-words text-sm leading-6">{preference(q)}</p>{round.self.subjectId?<p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{outcome(q)}</p>:null}</li>)}</ol>
 </details>;
}
function RapportResults({round,name,phase}:{round:RapportRound;name:Name;phase:string|null}) {
 const {t}=useTranslation("rapportOnline");
 if(phase!=="revealed"&&phase!=="settled")return null;
 const rank=(s:RapportRound["scores"][number])=>s.rank&&Number.isSafeInteger(s.rank)&&s.rank>0?t(round.scores.filter(other=>other.rank===s.rank).length>1?"sharedRank":"rankValue",{rank:s.rank}):"—";
 return <section className="uma-panel min-w-0 p-4" aria-labelledby="rapport-scores"><h2 id="rapport-scores" className="text-lg font-bold">{t("results")}</h2><p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("resultHint")}</p>{!round.scores.length?<p className="mt-3 text-sm" role="status">— {t("scoreWaiting")}</p>:<>
  <table className="mt-3 hidden w-full table-fixed text-left text-sm md:table"><caption className="sr-only">{t("results")}</caption><thead><tr><th className="w-1/4 p-2">{t("rank")}</th><th className="p-2">{t("participant")}</th><th className="w-24 p-2">{t("score")}</th></tr></thead><tbody>{round.scores.map((s,i)=><tr key={s.subjectId} className="border-t border-[var(--color-line)]"><td className="break-words p-2">{rank(s)}</td><th scope="row" className="break-words p-2 font-medium">{name(s.subjectId,i)}</th><td className="p-2 font-bold">{t("points",{points:s.points})}</td></tr>)}</tbody></table>
  <ol className="mt-3 space-y-2 md:hidden">{round.scores.map((s,i)=><li key={s.subjectId} className="rounded-[var(--radius-control)] border border-[var(--color-line)] p-3"><p className="break-words font-semibold leading-6">{name(s.subjectId,i)}</p><p className="mt-1 flex flex-wrap justify-between gap-2 text-sm"><span>{rank(s)}</span><strong>{t("points",{points:s.points})}</strong></p></li>)}</ol>
 </>}</section>;
}

export function RapportRoundPage() {
 const {t,i18n}=useTranslation("rapportOnline"), {roomId}=useParams<{roomId:string}>(), {api,session}=useApi();
 const roomQuery=useRoom(roomId??null), room=roomQuery.data?.room, roundId=room?.currentRound?.roundId??null;
 const snapshot=useRoundSnapshot(roundId);
 const round=useMemo(()=>snapshot.raw&&snapshot.meta?.roundId===roundId?normalizeRapportRound(snapshot.raw):null,[snapshot.raw,snapshot.meta?.roundId,roundId]);
 const phase=snapshot.meta?.phase??null, terminal=isTerminalPhase(phase), cancelled=phase==="cancelled", reveal=phase==="revealed"||phase==="settled";
 const original=round?.questions[round.currentQuestion-1], current=original?localizeRapportQuestion(original,i18n.language):null;
 // Uncertain commands remain scoped to the original round, not a new question.
 const scope=session?.sessionId&&roundId?`${session.sessionId}:${roundId}:rapport`:null;
 const command=useGameCommand(scope,validRapportAnswer,(body:RapportAnswerCommand,key)=>api.createSubmission(roundId!,body,key));
 const draftScope=scope&&round?.self.subjectId&&current?`${scope}:${round.self.subjectId}:${current.questionId}`:null;
 const restored=useMemo(()=>draftScope?loadRapportDraft(draftScope,original?.options.map(o=>o.optionId)??[]):null,[draftScope,original]);
 const [draft,setDraft]=useState<{scope:string;optionId:string|null}|null>(null);
 const selected=current?.selfOptionId??(draft?.scope===draftScope?draft?.optionId:restored?.optionId)??null;
 const [notice,setNotice]=usePlayerText("rapportOnline"), [refreshing,setRefreshing]=useState(false), [needsRejoin,setNeedsRejoin]=useState(false);
 const [failure,setFailure]=useState<Failure|null>(null), [errorCode,setErrorCode]=useState(""), [rejected,setRejected]=useState(false);
 const collecting=phase==="collecting"&&!snapshot.meta?.paused&&!terminal;
 const countdown=useCountdown(snapshot.meta?.deadlineAt,collecting);
 const editable=!!current&&!!round?.self.subjectId&&snapshot.canSubmit&&collecting&&!countdown.expired&&!snapshot.isError&&!roomQuery.isError&&!current.answered&&!current.locked&&!command.busy&&!refreshing&&!command.pending&&!command.expired;
 const realtime=useRealtime({roomId:roomId??null,enabled:Boolean(roomId),handlers:{onSnapshotNeeded:()=>void snapshot.refetch()}});
 const localized=failure?localizeFailure(failure):null;
 const info=localized?{...localized,...(rejected?{title:t("rejectedTitle"),detail:t("rejectedDetail")}:Object.hasOwn(rapportOnlineCopy["zh-Hans"].errors,errorCode)?{detail:t(`errors.${errorCode}`)}:{})}:null;
 const members=readTeamMembers(room),teams=readTeams(room);
 const name:Name=(id,index)=>{
  const team=teams.find(team=>team.teamId===id);if(team)return team.label;
  const member=members.find(member=>member.membershipId===id);
  if(member)return /^\d{6}$/.test(member.publicCode??"")?`${member.displayName} · ${member.publicCode}`:member.displayName;
  const recorded=round?.scores.find(score=>score.subjectId===id)?.label;
  return recorded||t(index===undefined?"partner":"participantNumber",{number:(index??0)+1});
 };
 const groupName=(id:string,index:number)=>teams.find(team=>team.teamId===id)?.label??t("group",{number:index+1});
 function edit(optionId:string|null){if(!editable||!draftScope)return;setDraft({scope:draftScope,optionId});setNotice("");if(!saveRapportDraft(draftScope,optionId))setNotice("saveFailed");}
 async function submit(confirm=false){
  if(!roundId||command.busy||refreshing||(!confirm&&(!editable||!selected)))return;
  setRefreshing(true);setNotice("");setFailure(null);setErrorCode("");setRejected(false);
  try{
   const result=await command.run(confirm?undefined:{phaseVersion:snapshot.meta!.phaseVersion,submissionRevision:snapshot.submissionRevision,controlGeneration:snapshot.controlGeneration,modeAction:{kind:"rapport.answer",optionId:selected!}});
   if(!result)return;
   if(!result.ok){setFailure(describeFailure(result.error));setErrorCode(result.error instanceof ApiError?result.error.code:"");if(isCredentialRefreshFailure(result.error))setNeedsRejoin(true);if(!result.uncertain)await snapshot.refetch();return;}
   if(result.value.data?.outcome!=="accepted"){setRejected(true);setFailure({kind:"error",title:"",detail:"",diagnosticId:result.value.requestId,retryable:true});return;}
   // Never clear a new question's draft while confirming an older command.
   if(!confirm&&draftScope)clearRapportDraft(draftScope);
   setNotice("accepted");await snapshot.refreshAfterSubmit();
  }finally{setRefreshing(false);}
 }
 if(!roomId)return <ErrorView failure={{kind:"error",title:t("missingTitle"),detail:t("missingDetail"),diagnosticId:null,retryable:false}}/>;
 return <GameStage mode="rapport" className="uma-stack">
  <ConnectionBar status={realtime.status} detail={roomDisplayName(room, t("room"))}/>
  {needsRejoin||isCredentialRefreshFailure(snapshot.error)?<RejoinPrompt roomId={roomId}/>:null}
  {roomQuery.isLoading?<LoadingBlock label={t("loadingRoom")} rows={3}/>:null}
  {roomQuery.isError?(describeFailure(roomQuery.error).kind==="forbidden"?<ForbiddenView failure={describeFailure(roomQuery.error)} onRetry={()=>void roomQuery.refetch()}/>:<ErrorView failure={describeFailure(roomQuery.error)} onRetry={()=>void roomQuery.refetch()}/>):null}
  {snapshot.isLoading?<LoadingBlock label={t("loading")} rows={4}/>:null}
  {snapshot.isError?<ErrorView failure={describeFailure(snapshot.error)} onRetry={()=>void snapshot.refetch()}/>:null}
  {command.pending?<Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button className="mt-3 h-auto min-h-11 whitespace-normal" disabled={command.busy||refreshing} onClick={()=>void submit(true)}>{command.busy?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:null}{t("confirmAction")}</Button></Alert>:null}
  {command.busy?<p role="status" className="text-sm">{t("busy")}</p>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button className="mt-3 h-auto min-h-11 whitespace-normal" disabled={refreshing} onClick={()=>void(async()=>{setRefreshing(true);try{if(await snapshot.refetch())command.acknowledgeExpired();}finally{setRefreshing(false);}})()}>{t("acknowledge")}</Button></Alert>:null}
  {command.storageLimited?<Alert tone="warning" title={t("notice")}>{t("storage")}</Alert>:null}
  {info?<Alert tone="danger" title={info.title}>{info.detail}</Alert>:null}
  {notice?<p role="status" className="text-sm leading-6">{notice}</p>:null}
  {round?<>
   <header className="border-b border-[var(--color-line)] pb-4"><h1 className="u-fluid-task text-[var(--color-ink)]">{t("title")}</h1><p className="mt-2 max-w-prose text-sm leading-6 text-[var(--color-muted)]">{t("intro")}</p><p className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm text-[var(--color-muted)]"><span>{phaseLabel(phase)}</span><span>{t("question",{number:Math.min(round.currentQuestion,round.questionCount),total:round.questionCount})}</span>{collecting&&countdown.text?<span className="font-bold text-[var(--color-ink)]">{countdown.text}</span>:null}</p></header>
   {cancelled?<Alert tone="info" title={t("cancelled")}>{t("cancelledDetail")}</Alert>:null}
   {!cancelled&&(phase==="locked"||reveal)&&round.currentQuestion<=round.questionCount?<Alert tone="warning" title={t("notice")}>{t("legacy")}</Alert>:null}
   {round.self.subjectId&&(round.revealedSlots??0)>0?<p className="text-sm leading-6 text-[var(--color-muted)]">{t("tally",{hits:round.revealedHits??0,slots:round.revealedSlots??0})}</p>:null}
   {!cancelled&&!reveal?<div className="grid min-w-0 items-start gap-4 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
    <section className="uma-panel min-w-0 p-4 sm:p-6" aria-labelledby="rapport-question">
     {current?<>
      <h2 id="rapport-question" className="break-words text-xl font-bold leading-8">{current.prompt}</h2>
      {current.original?<p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("original")}</p>:null}
      <p className="mt-3 break-words text-sm leading-6 text-[var(--color-muted)]">{!round.self.subjectId?t("observer"):current.selfIsChooser?t("choose"):t("predict",{name:name(current.chooserId)})}</p>
      <div className="mt-4 grid gap-3 sm:grid-cols-2">{current.options.map(option=><button key={option.optionId} type="button" aria-pressed={selected===option.optionId} disabled={!editable} onClick={()=>edit(option.optionId)} className={cn("flex min-h-14 min-w-0 items-center justify-between gap-3 rounded-[var(--radius-control)] border-2 px-4 py-3 text-left text-base leading-7 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)] disabled:opacity-100",selected===option.optionId?"border-[var(--color-action-green)] bg-[var(--color-brand-green-soft)]":"border-[var(--color-line-strong)] bg-[var(--color-panel)]")}><span className="break-words">{option.label||t("optionMissing")}</span>{selected===option.optionId?<CheckCircle2 className="size-5 shrink-0 text-[var(--color-action-green)]" aria-hidden="true"/>:null}</button>)}</div>
      {round.self.subjectId?<div className="mt-5 border-t border-[var(--color-line)] pt-4"><p role="status" className="mb-3 text-sm leading-6">{snapshot.meta?.paused?t("paused"):current.answered?t("confirmed"):phase!=="collecting"?t(phase==="locked"?"locked":"beforeStart"):countdown.expired?t("deadline"):t("selectHint")}</p>{!current.answered?<div className="flex flex-wrap gap-2"><Button variant="primary" size="primary" className="h-auto min-h-11 flex-1 whitespace-normal" disabled={!editable||!selected} onClick={()=>void submit()}>{command.busy?<Loader2 className="size-4 motion-safe:animate-spin" aria-hidden="true"/>:null}{t(current.selfIsChooser?"confirmChoice":"confirmPrediction")}</Button><Button variant="secondary" className="h-auto min-h-11 whitespace-normal" disabled={!editable||!selected} onClick={()=>edit(null)}>{t("clear")}</Button></div>:null}</div>:null}
     </>:<><h2 id="rapport-question" className="text-xl font-bold">{t("done")}</h2><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("doneHint")}</p></>}
    </section>
    <aside className="min-w-0 space-y-4" aria-label={t("progress")}>
     {round.groups.length?<section className="uma-panel p-4"><h2 className="font-bold">{t("progress")}</h2><ul className="mt-3 space-y-3">{round.groups.map((group,i)=><li key={group.groupId} className="min-w-0 border-b border-[var(--color-line)] pb-3 last:border-0"><p className="break-words font-semibold">{groupName(group.groupId,i)}{group.groupId===round.groupId?` · ${t("ownGroup")}`:""}</p><p className="mt-1 break-words text-sm leading-6 text-[var(--color-muted)]">{t("chooser",{name:name(group.chooserId)})}</p><p className="text-sm leading-6">{t("answered",{answered:group.answered,members:group.members})}</p>{group.totalSlots!==null?<p className="text-sm leading-6 text-[var(--color-muted)]">{t("denominator",{slots:group.totalSlots})}</p>:null}</li>)}</ul></section>:null}
    </aside>
   </div>:null}
   <RapportResults round={round} name={name} phase={phase}/>
   <RapportHistory round={round} name={name}/>
   <details className="uma-panel min-w-0 p-4"><summary className="min-h-11 cursor-pointer content-center font-semibold">{t("rules")}</summary><div className="mt-2 max-w-prose space-y-2 text-sm leading-6 text-[var(--color-muted)]"><p>{t("ruleTiming",{questions:round.questionCount,seconds:round.questionSeconds})}</p><p>{round.groupId?t("denominator",{slots:round.totalSlots}):t("denominatorPublic")}</p><p>{t("ruleScore")}</p><p>{t("rulePrivacy")}</p></div></details>
  </>:!snapshot.isLoading&&!snapshot.isError&&!roomQuery.isLoading&&!roomQuery.isError?<EmptyView title={t("emptyTitle")} detail={t("emptyDetail")}/>:null}
  <TeamRoundControl room={room} raw={snapshot.raw} onChanged={snapshot.refetch}/>
  <Button asChild variant="secondary" className="h-auto min-h-11 whitespace-normal"><Link to={`/rooms/${roomId}`}>{t("back")}</Link></Button>
 </GameStage>;
}
