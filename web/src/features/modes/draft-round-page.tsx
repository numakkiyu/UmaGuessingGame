import { roomDisplayName } from "@/lib/room-display-name";
import { GameStage } from "@/components/uma/game-stage";
import "@/i18n/draft-online";
import {useTranslation} from "@/i18n";
import {usePlayerText} from "@/i18n/player-text";
import {cardMatches,cardName} from "@/i18n/entities";
import {taskConditions} from "@/i18n/aptitude-values";
import {draftOnlineCopy} from "@/i18n/draft-online";
import {useMemo,useRef,useState} from "react";
import {Link,useParams} from "react-router";
import {Check,Loader2,RotateCcw} from "lucide-react";
import {TeamRoundControl} from "@/features/rooms/team-round-control";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView,ErrorView,ForbiddenView,LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Sheet,SheetContent,SheetDescription,SheetHeader,SheetTitle} from "@/components/ui/sheet";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {useApi} from "@/lib/api";
import {ApiError,describeFailure,localizeFailure,type Failure} from "@/lib/api/client";
import {clearLineupDraft,loadLineupDraft,normalizeDraftRound,saveLineupDraft,spentOf,taskPointsFor,validateLineup,type DraftRound,type LineupDraft} from "@/lib/api/draft";
import {validDraftLineup,type DraftLineupCommand} from "@/lib/api/game-commands";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {useRoom} from "@/lib/api/room-queries";
import {useCharacters} from "@/lib/api/queries";
import {readTeamMembers,readTeams} from "@/lib/api/teams";
import {isTerminalPhase,phaseLabel,publicStateOf,resultsOf,useRoundSnapshot} from "@/lib/api/round-snapshot";
import type {components} from "@/lib/api/types";
import {useRealtime} from "@/lib/realtime/use-realtime";
import {cn} from "@/lib/utils";
import {RejoinPrompt,isCredentialRefreshFailure} from "./rejoin-prompt";
import {GradeBadge,TaskCard} from "./draft-task-card";

type Score={subjectId:string;scoreUnits:number;rank:number|null};
export function draftScores(items:readonly unknown[]):Score[]{
 return items.flatMap(item=>{if(!item||typeof item!=="object")return [];const value=item as Record<string,unknown>;
  return typeof value.subjectId==="string"&&typeof value.scoreUnits==="number"&&Number.isSafeInteger(value.scoreUnits)&&value.scoreUnits>=0?[{subjectId:value.subjectId,scoreUnits:value.scoreUnits,rank:typeof value.rank==="number"&&Number.isSafeInteger(value.rank)&&value.rank>0?value.rank:null}]:[];
 });
}

export function DraftRoundPage(){
 const {t}=useTranslation("draftOnline"),{roomId}=useParams<{roomId:string}>(),{api,session}=useApi();
 const roomQuery=useRoom(roomId??null),roundId=roomQuery.data?.room.currentRound?.roundId??null,snapshot=useRoundSnapshot(roundId),characters=useCharacters().data?.items;
 const round=useMemo(()=>{
  if(snapshot.meta?.roundId!==roundId)return null;
  const publicState=publicStateOf(snapshot.raw);if(!publicState)return null;
  const value=normalizeDraftRound({publicState} as unknown as components["schemas"]["RoundView"]);if(!value)return null;
  // Only public identity metadata is supplemented. The round owns prices,
  // published aptitudes, tasks and the accepted lineup.
  return {...value,cards:value.cards.map(card=>{const known=characters?.find(c=>c.characterId===card.id);return known?{...card,name:known.displayName,localizedNames:known.localizedNames,aliases:known.aliases}:card;})};
 },[roundId,snapshot.meta?.roundId,snapshot.raw,characters]);
 const scope=session&&roundId?`${session.sessionId}:${roundId}:draft`:null;
 const draftScope=scope&&round?.self?`${scope}:${round.self.subjectId}`:null;
 const command=useGameCommand(scope,validDraftLineup,(body,key)=>api.createSubmission(roundId!,body,key));
 const [draft,setDraft]=useState<LineupDraft>({assignments:{}}),[initialized,setInitialized]=useState<string|null>(null);
 const [openTaskId,setOpenTaskId]=useState<string|null>(null),[query,setQuery]=useState("");
 const triggerRef=useRef<HTMLElement|null>(null);
 const [failure,setFailure]=useState<Failure|null>(null),[errorCode,setErrorCode]=useState(""),[rejected,setRejected]=useState(false),[needsRejoin,setNeedsRejoin]=useState(false),[refreshing,setRefreshing]=useState(false);
 const [notice,setNotice]=usePlayerText("draftOnline");
 if(round&&draftScope&&initialized!==draftScope){
  setInitialized(draftScope);const stored=loadLineupDraft(draftScope);
  const valid=stored&&Object.entries(stored.assignments).every(([taskId,cardId])=>round.tasks.some(task=>task.taskId===taskId)&&round.cards.some(card=>card.id===cardId));
  setDraft(round.self?.locked?{assignments:{...round.self.assignments}}:valid?stored:{assignments:{...round.self?.assignments}});
 }
 const realtime=useRealtime({roomId:roomId??null,enabled:Boolean(roomId),handlers:{onSnapshotNeeded:()=>{void snapshot.refetch();void roomQuery.refetch();}}});
 const phase=snapshot.meta?.phase??null,ended=isTerminalPhase(phase),cancelled=phase==="cancelled",collecting=phase==="collecting"&&!snapshot.meta?.paused;
 const countdown=useCountdown(snapshot.meta?.deadlineAt,collecting),locked=round?.self?.locked??false;
 const busy=command.busy||refreshing||!!command.pending||command.expired||snapshot.isError;
 const editable=!!round?.self&&collecting&&!locked&&!busy&&snapshot.canSubmit;
 const tasks=round?.tasks??[],cards=round?.cards??[],budget=round?.budget??24;
 // Validation messages depend on the active locale, not on persisted strings.
 const problems=validateLineup(draft,tasks,cards,budget);
 const shownDraft=locked&&round?.self?{assignments:round.self.assignments}:draft;
 const spent=spentOf(shownDraft,cards),remaining=budget-spent;
 const openTask=tasks.find(task=>task.taskId===openTaskId)??null;
 const candidates=cards.filter(card=>cardMatches(card,query)||("aliases" in card&&Array.isArray(card.aliases)&&card.aliases.some((alias:unknown)=>typeof alias==="string"&&cardMatches({name:alias},query))));
 const used=new Set(Object.values(draft.assignments));
 const knownError=Object.hasOwn(draftOnlineCopy["zh-Hans"].errors,errorCode);
 const info=failure?knownError?{...failure,title:t("errorTitle"),detail:t(`errors.${errorCode}`)}:rejected?{...failure,title:t("rejectedTitle"),detail:t("rejectedDetail")}:localizeFailure(failure):null;
 const labelFor=(id:string)=>{
  const room=roomQuery.data?.room,team=readTeams(room).find(team=>team.teamId===id);if(team)return team.label;
  const member=readTeamMembers(room).find(member=>member.membershipId===id);
  return member?member.publicCode&&/^\d{6}$/.test(member.publicCode)?`${member.displayName} · ${member.publicCode}`:member.displayName:t("participant");
 };
 function update(next:LineupDraft){if(!editable)return;setDraft(next);setNotice("");if(draftScope&&!saveLineupDraft(draftScope,next))setNotice("saveFailed");}
 function closePicker(){setOpenTaskId(null);setQuery("");requestAnimationFrame(()=>triggerRef.current?.focus());}
 function pick(cardId:string){if(!editable||!openTask)return;if(used.has(cardId)&&draft.assignments[openTask.taskId]!==cardId)return;update({assignments:{...draft.assignments,[openTask.taskId]:cardId}});closePicker();}
 async function submit(retry=false){
  if(!roundId||command.busy||refreshing)return;
  if(!retry&&(!round||!editable||problems.length))return;
  const body:DraftLineupCommand|undefined=retry?undefined:{phaseVersion:snapshot.meta!.phaseVersion,submissionRevision:snapshot.submissionRevision,controlGeneration:snapshot.controlGeneration,modeAction:{kind:"draft.lineup",assignments:tasks.map(task=>({slotId:task.taskId,characterId:draft.assignments[task.taskId]!}))}};
  setRefreshing(true);setFailure(null);setErrorCode("");setRejected(false);setNotice("");
  try{
   const result=await command.run(body);if(!result)return;
   if(!result.ok){setFailure(describeFailure(result.error));setErrorCode(result.error instanceof ApiError?result.error.code:"");if(!result.uncertain&&result.error instanceof ApiError&&(result.error.isConflict||result.error.isUnprocessable))await snapshot.refetch();if(isCredentialRefreshFailure(result.error))setNeedsRejoin(true);return;}
   if(result.value.data?.outcome!=="accepted"){setRejected(true);setFailure({kind:"error",title:"",detail:"",diagnosticId:result.value.requestId,retryable:true});return;}
   if(draftScope)clearLineupDraft(draftScope);setNotice("accepted");closePicker();await snapshot.refreshAfterSubmit();
  }finally{setRefreshing(false);}
 }
 if(!roomId)return <ErrorView failure={{kind:"error",title:t("missingTitle"),detail:t("missingDetail"),diagnosticId:null,retryable:false}}/>;
 if(roomQuery.isLoading)return <LoadingBlock label={t("loadingRoom")} rows={3}/>;
 if(roomQuery.isError){const info=describeFailure(roomQuery.error);return info.kind==="forbidden"?<ForbiddenView failure={info} onRetry={()=>void roomQuery.refetch()}/>:<ErrorView failure={info} onRetry={()=>void roomQuery.refetch()}/>;}
 const connectionStatus=realtime.status==="online"?"online":realtime.status==="connecting"||realtime.status==="reconnecting"?"reconnecting":realtime.status==="stale"?"stale":"offline";
 return <GameStage mode="draft" className="uma-stack">
  <TeamRoundControl room={roomQuery.data?.room} raw={snapshot.raw} onChanged={snapshot.refetch}/><ConnectionBar status={connectionStatus} detail={roomDisplayName(roomQuery.data?.room, t("room"))}/>
  {round&&!cancelled&&!ended?<div className="sticky z-[var(--z-sticky)] rounded-[var(--radius-card)] border border-[var(--color-line-strong)] bg-[var(--color-panel)] px-3 py-2 shadow-[var(--shadow-soft)]" style={{top:"calc(var(--header-h) + 4px)"}}>
   <dl className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-sm"><div><dt className="inline text-[var(--color-muted)]">{t("budget")} </dt><dd className="uma-code inline font-bold">{spent} / {budget}</dd><dd className={cn("ml-2 inline",remaining<0&&"text-[var(--color-danger)]")}>{t(remaining<0?"over":"remaining",{count:Math.abs(remaining)})}</dd></div><div><dt className="inline">{t("tasks")} </dt><dd className="uma-code inline font-bold">{Object.keys(shownDraft.assignments).length} / {tasks.length}</dd></div>{collecting&&snapshot.meta?.deadlineAt?<div><dt className="inline">{t("time")} </dt><dd className="uma-code inline font-bold" aria-live="off">{countdown.text}</dd></div>:null}</dl>
  </div>:null}
  <section className="uma-panel p-4"><h1 className="u-fluid-task">{t("title")}</h1><p className="mt-1 text-sm text-[var(--color-muted)]">{t("prompt")}</p><p className="mt-1 text-sm">{phaseLabel(phase)}{snapshot.meta?.paused?` · ${t("paused")}`:""}{locked?` · ${t("locked")}`:""}</p></section>
  {command.pending&&!command.busy?<Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button variant="secondary" className="h-auto min-h-12 whitespace-normal" disabled={refreshing} onClick={()=>void submit(true)}>{t("confirmAction")}</Button></Alert>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" onClick={()=>void snapshot.refetch().then(ok=>{if(ok)command.acknowledgeExpired();})}>{t("latest")}</Button></Alert>:null}
  {command.storageLimited?<p className="text-sm text-[var(--color-muted)]">{t("limited")}</p>:null}
  {needsRejoin?<RejoinPrompt roomId={roomId}/>:null}
  {notice?<Alert tone="info" title={t("notice")}>{notice}</Alert>:null}
  {info?<Alert tone={info.kind==="timeout"?"warning":"danger"} title={info.title}><p>{info.detail}</p>{info.diagnosticId?<p>{t("problemId")}：<span className="uma-code">{info.diagnosticId}</span></p>:null}<Button variant="secondary" onClick={()=>void snapshot.refetch()}>{t("refresh")}</Button></Alert>:null}
  {!roundId?<EmptyView title={t("waiting")} detail={t("waitingDetail")}/>:null}
  {roundId&&snapshot.isLoading&&!round?<LoadingBlock label={t("loadingRound")} rows={3}/>:null}
  {roundId&&snapshot.isError?<ErrorView failure={describeFailure(snapshot.error)} onRetry={()=>void snapshot.refetch()}/>:null}
  {round&&!cancelled?<div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,22rem)] lg:items-start">
   <div className="uma-stack uma-stack--tight min-w-0"><section className="uma-panel p-4" aria-labelledby="tasks-heading"><div className="uma-section-head"><h2 id="tasks-heading" className="text-base font-bold">{t("slots")}</h2><p className="text-sm text-[var(--color-muted)]">{t("unique")}</p></div>
    <ul className="mt-3 flex flex-col gap-3">{tasks.map((task,index)=>{
     const rawCard=cards.find(card=>card.id===shownDraft.assignments[task.taskId]),card=rawCard?{...rawCard,name:rawCard.name||t("unknownCard")}:null;
     const problem=!locked&&!ended?problems.find(problem=>problem.taskId===task.taskId):undefined;
     return <TaskCard key={task.taskId} task={task} index={index} card={card} problem={problem?.message??null} problemSeverity={problem?.severity??null} disabled={!editable} onOpen={()=>{triggerRef.current=document.activeElement instanceof HTMLElement?document.activeElement:null;setOpenTaskId(task.taskId);setQuery("");}} onClear={()=>{const next={...draft.assignments};delete next[task.taskId];update({assignments:next});}}/>;
    })}</ul>
   </section>
   {!ended&&!locked?<section className="uma-panel p-4" aria-labelledby="submit-heading"><h2 id="submit-heading" className="text-base font-bold">{t("submitHeading")}</h2>{problems.length?<Alert tone="warning" title={t("problems",{count:problems.length})} className="mt-3"><ul className="list-disc space-y-1 pl-4">{problems.map((problem,index)=><li key={index}>{problem.message}</li>)}</ul></Alert>:<p className="mt-2 text-sm font-semibold">{t("ready")}</p>}<div className="mt-4 flex flex-col gap-2 sm:flex-row"><Button variant="secondary" size="primary" className="h-auto min-h-12 whitespace-normal" disabled={!editable} onClick={()=>{if(draftScope)setNotice(saveLineupDraft(draftScope,draft)?"saved":"saveFailed");}}>{t("save")}</Button><Button variant="primary" size="primary" className="h-auto min-h-12 whitespace-normal" disabled={!editable||problems.length>0} onClick={()=>void submit()}>{command.busy||refreshing?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:<Check aria-hidden="true" className="size-4"/>}{t(command.busy||refreshing?"submitting":"submit")}</Button></div></section>:null}
   {locked&&!ended&&round.self?<section className="uma-card p-4"><h2 className="text-base font-bold">{t("lockedPoints")}</h2><p className="uma-code mt-2">{round.self.totalPoints} / 36 · {t("spent")} {round.self.spent} / {budget}</p><p className="mt-2 text-sm text-[var(--color-muted)]">{t("rulePreview")}</p></section>:null}
   </div>
   <aside className="uma-stack uma-stack--tight min-w-0"><section className="uma-card p-4"><h2 className="text-base font-bold">{t("rules")}</h2><ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]">{["rulePoints","ruleTotal","ruleBudget","rulePreview","ruleFinal"].map(key=><li key={key}>{t(key,{budget})}</li>)}</ul></section><Button variant="secondary" block disabled={command.busy||refreshing} onClick={()=>void snapshot.refetch()}><RotateCcw aria-hidden="true" className="size-4"/>{t("refresh")}</Button><Button asChild variant="secondary" block><Link to={`/rooms/${roomId}`}>{t("back")}</Link></Button></aside>
  </div>:null}
  {round&&ended?<DraftResultPanel round={round} cancelled={cancelled} scores={draftScores(resultsOf(snapshot.raw))} labelFor={labelFor}/>:null}
  <Sheet open={!!openTask&&editable} onOpenChange={open=>{if(!open)closePicker();}}><SheetContent side="bottom" closeLabel={t("closePicker")} onCloseAutoFocus={event=>{event.preventDefault();triggerRef.current?.focus();}}><SheetHeader><SheetTitle>{t("picker",{count:tasks.findIndex(task=>task.taskId===openTaskId)+1})}</SheetTitle><SheetDescription>{openTask?t("pickerDetail",{conditions:taskConditions(openTask)}):t("unknownTask")}</SheetDescription></SheetHeader>
   <label htmlFor="draft-search" className="text-sm font-semibold">{t("search")}</label><Input type="search" id="draft-search" value={query} onChange={event=>setQuery(event.target.value)} placeholder={t("searchPlaceholder")} autoComplete="off" inputMode="search"/>
   {!candidates.length?<p className="mt-3 text-sm">{t("noMatch",{query})}</p>:<ul className="mt-3 flex flex-col gap-2 pb-4">{candidates.map(card=>{const usedByOther=used.has(card.id)&&draft.assignments[openTaskId??""]!==card.id,points=openTask?taskPointsFor(card,openTask):null;return <li key={card.id}><button type="button" disabled={!editable||usedByOther} onClick={()=>pick(card.id)} className="flex min-h-12 w-full min-w-0 items-center gap-2 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel)] px-3 py-2 text-left hover:bg-[var(--color-panel-soft)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)] disabled:opacity-60">
    {card.imagePath?<img src={card.imagePath} alt="" className="size-10 shrink-0 object-contain" loading="lazy" decoding="async"/>:null}<span className="min-w-0 flex-1"><span className="block break-words text-sm font-semibold">{card.name?cardName(card):t("unknownCard")}</span><span className="mt-1 flex flex-wrap items-center gap-1"><GradeBadge grade={card.aptitudes.surface[openTask?.surface??""]??""}/><GradeBadge grade={card.aptitudes.distance[openTask?.distance??""]??""}/><GradeBadge grade={card.aptitudes.style[openTask?.style??""]??""}/>{points?<span className="text-xs">{t("points",{points:points.total})}</span>:null}</span>{usedByOther?<span className="block text-xs">{t("used")}</span>:null}</span><span className="uma-code shrink-0 text-sm">{t("price",{price:card.price})}</span>
   </button></li>;})}</ul>}
  </SheetContent></Sheet>
 </GameStage>;
}

export function DraftResultPanel({round,cancelled=false,scores=[],labelFor}:{round:DraftRound;cancelled?:boolean;scores?:Score[];labelFor?:(id:string)=>string}){
 const {t}=useTranslation("draftOnline");
 const own=scores.find(score=>score.subjectId===round.self?.subjectId),name=(id:string)=>labelFor?.(id)??t("participant");
 const content=round.tasks.map((task,index)=>{const card=round.cards.find(card=>card.id===round.self?.assignments[task.taskId]),points=round.self?.taskScores.find(score=>score.taskId===task.taskId);return {task,index,card,points};});
 return <section id="draft-result" className="uma-panel p-5" tabIndex={-1} aria-labelledby="draft-result-heading"><h2 id="draft-result-heading" className="u-fluid-task">{t(cancelled?"cancelled":"result")}</h2>
  {cancelled?<p className="mt-2">{t("cancelledDetail")}</p>:<>
   {round.self?<><p className="mt-2 text-sm">{!round.self.locked?t("noFinal"):own?null:t("waitingScore")}</p><dl className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3">{[[t("score"),own?Math.floor(own.scoreUnits/100):"—"],[t("total"),round.self.locked?`${round.self.totalPoints} / 36`:"—"],[t("spent"),round.self.locked?`${round.self.spent} / ${round.budget}`:"—"]].map(([key,value])=><div key={key} className="rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3"><dt className="text-xs text-[var(--color-muted)]">{key}</dt><dd className="uma-code text-xl font-bold">{value}</dd></div>)}</dl></>:null}
   <section className="mt-4" aria-label={t("standings")}><h3 className="text-base font-bold">{t("standings")}</h3><div className="mt-2 hidden md:block"><table className="w-full table-fixed text-left text-sm"><thead><tr>{["participant","score","rank","status"].map(key=><th key={key} scope="col" className="p-2">{t(key)}</th>)}</tr></thead><tbody>{round.subjects.map(subject=>{const score=scores.find(score=>score.subjectId===subject.subjectId);return <tr key={subject.subjectId}><th scope="row" className="break-words p-2">{name(subject.subjectId)}</th><td className="p-2">{score?Math.floor(score.scoreUnits/100):"—"}</td><td className="p-2">{score?.rank??"—"}</td><td className="p-2">{t(subject.locked?"confirmed":"unsubmitted")}</td></tr>;})}</tbody></table></div><ul className="mt-2 space-y-2 md:hidden">{round.subjects.map(subject=>{const score=scores.find(score=>score.subjectId===subject.subjectId);return <li key={subject.subjectId} className="rounded-lg border border-[var(--color-line)] p-3"><p className="break-words font-semibold">{name(subject.subjectId)}</p><dl className="mt-2 grid grid-cols-2 gap-2 text-sm">{[[t("score"),score?Math.floor(score.scoreUnits/100):"—"],[t("rank"),score?.rank??"—"],[t("status"),t(subject.locked?"confirmed":"unsubmitted")]].map(([label,value])=><div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl></li>;})}</ul></section>
   {round.self?.locked?<section className="mt-4" aria-label={t("breakdown")}><h3 className="text-base font-bold">{t("breakdown")}</h3><div className="uma-scroll-x mt-2 hidden md:block"><table className="w-full min-w-[34rem] text-left text-sm"><thead><tr>{["conditions","character","total"].map(key=><th key={key} scope="col" className="p-2">{t(key)}</th>)}</tr></thead><tbody>{content.map(({task,index,card,points})=><tr key={task.taskId}><th scope="row" className="p-2">{t("task",{count:index+1})}：{taskConditions(task)}</th><td className="break-words p-2">{card?.name?cardName(card):t("unknownCard")}</td><td className="p-2">{points?t("points",{points:points.total}):"—"}</td></tr>)}</tbody></table></div><ul className="mt-2 space-y-2 md:hidden">{content.map(({task,index,card,points})=><li key={task.taskId} className="rounded-lg border border-[var(--color-line)] p-3 text-sm"><h4 className="font-semibold">{t("task",{count:index+1})}：{taskConditions(task)}</h4><p className="mt-1 break-words">{card?.name?cardName(card):t("unknownCard")}</p><p>{points?t("points",{points:points.total}):"—"}</p></li>)}</ul></section>:null}
  </>}
  <div className="mt-4 flex flex-col gap-2 sm:flex-row"><Button asChild variant="secondary" className="h-auto min-h-12 whitespace-normal"><Link to="/play/draft">{t("again")}</Link></Button><Button asChild variant="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/">{t("home")}</Link></Button></div>
 </section>;
}
