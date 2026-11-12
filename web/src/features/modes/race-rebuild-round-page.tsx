import "@/i18n/race-online";
import {useTranslation} from "@/i18n";
import {usePlayerText} from "@/i18n/player-text";
import {cardName} from "@/i18n/entities";
import {raceDate,raceName} from "@/i18n/race-names";
import {useMemo,useState} from "react";
import {Link,useParams} from "react-router";
import {Loader2,Send} from "lucide-react";
import {TeamRoundControl} from "@/features/rooms/team-round-control";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView,ErrorView,ForbiddenView,LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {useApi} from "@/lib/api";
import {ApiError,describeFailure,localizeFailure,type Failure} from "@/lib/api/client";
import {initialRaceDraft,normalizeRaceRound,rankGapsOf,type RaceRound} from "@/lib/api/race-rebuild";
import {mergeWithNext,moveDown,moveUp,sortDraftIssues,splitGroup,type SortDraft} from "@/lib/api/sort";
import {validRaceOrder,type RaceOrderCommand} from "@/lib/api/game-commands";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {useCharacters} from "@/lib/api/queries";
import {useRoom} from "@/lib/api/room-queries";
import {isTerminalPhase,phaseLabel,publicStateOf,resultsOf,useRoundSnapshot} from "@/lib/api/round-snapshot";
import type {components} from "@/lib/api/types";
import {useRealtime} from "@/lib/realtime/use-realtime";
import {SortBoard} from "./sort-board";
import {RejoinPrompt,isCredentialRefreshFailure} from "./rejoin-prompt";

const PREFIX="umaguess:race-draft:";
export function loadRaceDraft(scope:string):SortDraft|null{
 try{const raw=localStorage.getItem(PREFIX+scope);if(!raw||raw.length>8192)return null;const value:unknown=JSON.parse(raw);
  if(!Array.isArray(value)||!value.length||value.length>7)return null;
  if(value.every(id=>typeof id==="string"&&id))return value.map(id=>[id as string]);
  return value.every(group=>Array.isArray(group)&&group.length>0&&group.length<=7&&group.every(id=>typeof id==="string"&&id))?value as SortDraft:null;
 }catch{return null;}
}
export function saveRaceDraft(scope:string,groups:SortDraft):boolean{try{localStorage.setItem(PREFIX+scope,JSON.stringify(groups));return true;}catch{return false;}}
function clearRaceDraft(scope:string){try{localStorage.removeItem(PREFIX+scope);}catch{/* No false persistence claim. */}}
export function raceOrderValid(groups:SortDraft,round:RaceRound):boolean{return sortDraftIssues(groups,round.entries).length===0;}

export function RaceRebuildRoundPage(){
 const {t}=useTranslation("raceOnline"),{t:tr}=useTranslation("race"),{roomId}=useParams<{roomId:string}>(),{api,session}=useApi();
 const roomQuery=useRoom(roomId??null),roundId=roomQuery.data?.room.currentRound?.roundId??null,snapshot=useRoundSnapshot(roundId),characters=useCharacters().data?.items;
 const round=useMemo(()=>{
  if(snapshot.meta?.roundId!==roundId)return null;const publicState=publicStateOf(snapshot.raw);if(!publicState)return null;
  const value=normalizeRaceRound({publicState} as unknown as components["schemas"]["RoundView"]);if(!value)return null;
  const localize=<T extends {characterId:string;name:string}>(entry:T)=>{const known=characters?.find(c=>c.characterId===entry.characterId);return known?{...entry,name:known.displayName,localizedNames:known.localizedNames}:entry;};
  // Names are public. Never fill finishRank or clues from an external library.
  return {...value,entries:value.entries.map(localize),answer:value.answer?{...value.answer,ranked:value.answer.ranked.map(localize)}:null};
 },[snapshot.raw,snapshot.meta?.roundId,roundId,characters]);
 const scope=session&&roundId?`${session.sessionId}:${roundId}:race`:null,draftScope=scope&&round?.self?`${scope}:${round.self.subjectId}`:null;
 const command=useGameCommand(scope,validRaceOrder,(body,key)=>api.createSubmission(roundId!,body,key));
 const [order,setOrder]=useState<SortDraft>([]),[initialized,setInitialized]=useState<string|null>(null);
 const [failure,setFailure]=useState<Failure|null>(null),[rejected,setRejected]=useState(false),[refreshing,setRefreshing]=useState(false),[needsRejoin,setNeedsRejoin]=useState(false);
 const [notice,setNotice]=usePlayerText("raceOnline");
 if(round&&draftScope&&initialized!==draftScope){setInitialized(draftScope);const stored=loadRaceDraft(draftScope),server=round.self?.groups??[];setOrder(stored&&raceOrderValid(stored,round)?stored:raceOrderValid(server,round)?server:initialRaceDraft(round.entries,roundId!).map(id=>[id]));}
 const realtime=useRealtime({roomId:roomId??null,enabled:Boolean(roomId),handlers:{onSnapshotNeeded:()=>{void snapshot.refetch();void roomQuery.refetch();}}});
 const phase=snapshot.meta?.phase??null,ended=isTerminalPhase(phase),cancelled=phase==="cancelled",collecting=phase==="collecting"&&!snapshot.meta?.paused;
 const countdown=useCountdown(snapshot.meta?.deadlineAt,collecting),busy=command.busy||refreshing||!!command.pending||command.expired||snapshot.isError;
 const editable=!!round?.self&&collecting&&snapshot.canSubmit&&!busy;
 const cards=round?.entries.map(entry=>({characterId:entry.characterId,name:entry.name||t("unknownCard"),...("localizedNames" in entry?{localizedNames:entry.localizedNames}:{}),imagePath:`/assets/avatars/${entry.characterId}.webp`,remoteImageUrl:null,raw:null,display:null}))??[];
 const problems=round?sortDraftIssues(order,round.entries):[];
 const info=failure?rejected?{...failure,title:t("rejectedTitle"),detail:t("rejectedDetail")}:localizeFailure(failure):null;
 const changed=!!round?.self?.submitted&&JSON.stringify(order)!==JSON.stringify(round.self.groups);
 const clues=round?.clueRelations.map(clue=>{const first=round.entries.find(e=>e.characterId===clue.firstId),second=round.entries.find(e=>e.characterId===clue.secondId);return tr(clue.kind==="tied"?"clueTied":"clueBefore",{first:first?.name?cardName(first):t("unknownCard"),second:second?.name?cardName(second):t("unknownCard")});})??[];
 function update(next:SortDraft){if(!editable)return;setOrder(next);setNotice("");if(draftScope&&!saveRaceDraft(draftScope,next))setNotice("saveFailed");}
 async function submit(retry=false){
  if(!roundId||!round||command.busy||refreshing)return;if(!retry&&(!editable||problems.length))return;
  const body:RaceOrderCommand|undefined=retry?undefined:{phaseVersion:snapshot.meta!.phaseVersion,submissionRevision:snapshot.submissionRevision,controlGeneration:snapshot.controlGeneration,modeAction:{kind:"race.order",groups:order.map(group=>[...group])}};
  setRefreshing(true);setFailure(null);setRejected(false);setNotice("");
  try{const result=await command.run(body);if(!result)return;if(!result.ok){setFailure(describeFailure(result.error));if(!result.uncertain&&result.error instanceof ApiError&&(result.error.isConflict||result.error.isUnprocessable))await snapshot.refetch();if(isCredentialRefreshFailure(result.error))setNeedsRejoin(true);return;}
   if(result.value.data?.outcome!=="accepted"){setRejected(true);setFailure({kind:"error",title:"",detail:"",diagnosticId:result.value.requestId,retryable:true});return;}
   if(draftScope)clearRaceDraft(draftScope);setNotice("acceptedHint");if(await snapshot.refreshAfterSubmit())setNotice("");
  }finally{setRefreshing(false);}
 }
 if(!roomId)return <ErrorView failure={{kind:"error",title:t("missingTitle"),detail:t("missingDetail"),diagnosticId:null,retryable:false}}/>;
 if(roomQuery.isLoading)return <LoadingBlock label={t("loadingRoom")} rows={3}/>;
 if(roomQuery.isError){const failure=describeFailure(roomQuery.error);return failure.kind==="forbidden"?<ForbiddenView failure={failure} onRetry={()=>void roomQuery.refetch()}/>:<ErrorView failure={failure} onRetry={()=>void roomQuery.refetch()}/>;}
 const connectionStatus=realtime.status==="online"?"online":realtime.status==="connecting"||realtime.status==="reconnecting"?"reconnecting":realtime.status==="stale"?"stale":"offline";
 const ownScore=resultsOf(snapshot.raw).flatMap(value=>{if(!value||typeof value!=="object")return [];const result=value as Record<string,unknown>;return result.subjectId===round?.self?.subjectId&&typeof result.scoreUnits==="number"&&Number.isSafeInteger(result.scoreUnits)&&result.scoreUnits>=0?[result.scoreUnits]:[];})[0];
 return <div className="uma-stack">
  <TeamRoundControl room={roomQuery.data?.room} raw={snapshot.raw} onChanged={snapshot.refetch}/><ConnectionBar status={connectionStatus} detail={roomQuery.data?.room.name??t("room")}/>
  <section className="uma-panel p-4"><div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><h1 className="u-fluid-task">{round?.raceName?raceName(round.raceName,undefined,{raceId:round.raceId,date:round.raceDate}):t("title")}</h1><p className="mt-1 text-sm text-[var(--color-muted)]">{phaseLabel(phase)}{round?.raceDate?` · ${raceDate(round.raceDate)}`:""}{round?` · ${t("entrants",{count:round.entries.length})}`:""}{snapshot.meta?.paused?` · ${t("paused")}`:""}</p>{round?<p className="mt-1 text-sm">{t(round.cooperative?"cooperative":"closed")}{round.openBook?` · ${t("openBook")}`:""}</p>:null}</div>{collecting&&snapshot.meta?.deadlineAt?<div className="shrink-0"><p className="text-xs text-[var(--color-muted)]">{t("time")}</p><p className="uma-code text-2xl font-bold" aria-live="off">{countdown.text}</p></div>:null}</div></section>
  {command.pending&&!command.busy?<Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button variant="secondary" className="h-auto min-h-12 whitespace-normal" disabled={refreshing} onClick={()=>void submit(true)}>{t("confirmAction")}</Button></Alert>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" onClick={()=>void snapshot.refetch().then(ok=>{if(ok)command.acknowledgeExpired();})}>{t("latest")}</Button></Alert>:null}
  {command.storageLimited?<p className="text-sm">{t("limited")}</p>:null}{needsRejoin?<RejoinPrompt roomId={roomId}/>:null}
  {info?<Alert tone={info.kind==="timeout"?"warning":"danger"} title={info.title}><p>{info.detail}</p>{info.diagnosticId?<p>{t("problemId")}：<span className="uma-code">{info.diagnosticId}</span></p>:null}<Button variant="secondary" onClick={()=>void snapshot.refetch()}>{t("refresh")}</Button></Alert>:null}
  {notice?<Alert tone="info" title={t("notice")}>{notice}</Alert>:null}
  {!roundId?<EmptyView title={t("waitingTitle")} detail={t("waitingDetail")}/>:null}{roundId&&snapshot.isLoading&&!round?<LoadingBlock label={t("loadingRound")} rows={3}/>:null}{roundId&&snapshot.isError?<ErrorView failure={describeFailure(snapshot.error)} onRetry={()=>void snapshot.refetch()}/>:null}
  {round&&!ended?<div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,20rem)] lg:items-start"><div className="uma-stack uma-stack--tight min-w-0">
   <section className="uma-panel p-4" aria-labelledby="race-order-heading"><h2 id="race-order-heading" className="text-base font-bold">{t("boardTitle")}</h2><p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("instructions")}</p><p role="status" className="mt-2 text-sm font-semibold">{t(round.self?.submitted?changed?"draftChanged":"draftUnchanged":"unsaved")}</p>{round.self?.submitted&&!round.self.groupingKnown?<p className="mt-2 text-sm">{t("legacy")}</p>:null}<div className="mt-3"><SortBoard draft={order} cards={cards} locked={!editable} positionKind="position" onChange={update} onMoveUp={index=>update(moveUp(order,index))} onMoveDown={index=>update(moveDown(order,index))} onMerge={index=>update(mergeWithNext(order,index))} onSplit={index=>update(splitGroup(order,index))}/></div></section>
   <section className="uma-panel p-4"><h2 className="text-base font-bold">{t("submitTitle")}</h2>{problems.length?<Alert tone="warning" title={t("problems",{count:problems.length})} className="mt-3"><ul className="list-disc pl-4">{[...new Set(problems)].map(problem=><li key={problem}>{t(`issues.${problem}`)}</li>)}</ul></Alert>:null}<div className="mt-4 flex flex-col gap-2 sm:flex-row sm:flex-wrap"><Button variant="secondary" size="primary" className="h-auto min-h-12 whitespace-normal" disabled={!editable} onClick={()=>{if(draftScope)setNotice(saveRaceDraft(draftScope,order)?"saved":"saveFailed");}}>{t("save")}</Button><Button variant="primary" size="primary" className="h-auto min-h-12 whitespace-normal" disabled={!editable||problems.length>0} onClick={()=>void submit()}>{command.busy||refreshing?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:<Send aria-hidden="true" className="size-4"/>}{t(command.busy||refreshing?"submitting":"submit")}</Button></div></section>
   </div><aside className="uma-stack uma-stack--tight min-w-0">{clues.length||round.clues.length?<section className="uma-card p-4" aria-labelledby="race-clues-heading"><h2 id="race-clues-heading" className="text-base font-bold">{t("cooperative")}</h2><ul className="mt-2 list-disc space-y-2 pl-5 text-sm leading-6">{clues.map((clue,index)=><li key={index}>{clue}</li>)}{round.clues.length>clues.length?<li>{t("clueUnavailable")}</li>:null}</ul></section>:null}<section className="uma-card p-4"><h2 className="text-base font-bold">{t("rules")}</h2><ul className="mt-2 space-y-2 text-sm leading-6 text-[var(--color-muted)]">{(t("playRules",{returnObjects:true,count:round.entries.length,seconds:round.questionSeconds??180}) as string[]).map(rule=><li key={rule}>{rule}</li>)}</ul></section><Button asChild variant="secondary" block><Link to={`/rooms/${roomId}`}>{t("back")}</Link></Button></aside></div>:null}
  {round&&ended?<RaceRevealPanel round={round} cancelled={cancelled} scoreUnits={ownScore}/>:null}
 </div>;
}

export function RaceRevealPanel({round,cancelled=false,scoreUnits}:{round:RaceRound;cancelled?:boolean;scoreUnits?:number}){
 const {t}=useTranslation("raceOnline"),answer=round.answer;
 const missing=answer?rankGapsOf(answer).missing:[],correct=round.self?.correctPairs,total=round.self?.totalPairs||round.totalPairs;
 const names=new Map(round.entries.map(entry=>[entry.characterId,entry.name?cardName(entry):t("unknownCard")]));
 return <section id="race-result" className="uma-panel p-5" aria-labelledby="race-result-heading"><h2 id="race-result-heading" className="u-fluid-task">{t(cancelled?"cancelled":answer?"result":"answerWaiting")}</h2>
  {cancelled?<p className="mt-2">{t("cancelledDetail")}</p>:answer?<>
   <p className="mt-1 text-sm">{raceName(round.raceName,undefined,{raceId:round.raceId,date:round.raceDate})||t("sourceRace")} · {raceDate(round.raceDate)}</p>
   {round.self?<><dl className="mt-3 grid grid-cols-2 gap-3"><div className="rounded-lg border border-[var(--color-line)] p-3"><dt className="text-xs">{t("score")}</dt><dd className="uma-code text-xl font-bold">{scoreUnits===undefined?"—":Math.floor(scoreUnits/100)}</dd></div><div className="rounded-lg border border-[var(--color-line)] p-3"><dt className="text-xs">{t("correctPairs")}</dt><dd className="uma-code text-xl font-bold">{correct===null?"—":correct} / {total}</dd></div></dl>
   <section className="mt-4" aria-labelledby="race-saved-heading"><h3 id="race-saved-heading" className="text-base font-bold">{t("mySaved")}</h3>{round.self.submitted?<>{!round.self.groupingKnown?<p className="mt-2 text-sm">{t("legacy")}</p>:null}<ol className="mt-2 space-y-2">{round.self.groups.map((group,index)=><li key={index} className="break-words text-sm"><span className="uma-code mr-2">{index+1}</span>{group.map(id=>names.get(id)??t("unknownCard")).join(" = ")}</li>)}</ol></>:<p className="mt-2 text-sm">{t("noScore")}</p>}</section></>:null}
   <section className="mt-4" aria-labelledby="race-ranks-heading"><h3 id="race-ranks-heading" className="text-base font-bold">{t("recordedRanks")}</h3>{missing.length?<p className="mt-2 text-sm text-[var(--color-muted)]">{t("missingRanks",{ranks:missing.join("、")})}</p>:null}
    <div className="mt-3 hidden md:block"><table className="w-full table-fixed text-left text-sm"><thead><tr>{["rankHeading","participant","note"].map(key=><th key={key} scope="col" className="p-2">{t(key)}</th>)}</tr></thead><tbody>{answer.ranked.map(entry=>{const tied=answer.ranked.filter(other=>other.finishRank===entry.finishRank).length;return <tr key={entry.characterId}><th scope="row" className="uma-code p-2 font-bold">{entry.finishRank}</th><td className="break-words p-2">{entry.name?cardName(entry):t("unknownCard")}</td><td className="p-2">{tied>1?t("tiedCount",{count:tied}):""}</td></tr>;})}</tbody></table></div>
    <ol className="mt-3 space-y-2 md:hidden">{answer.ranked.map(entry=>{const tied=answer.ranked.filter(other=>other.finishRank===entry.finishRank).length;return <li key={entry.characterId} className="flex min-w-0 items-start gap-3 rounded-lg border border-[var(--color-line)] p-3 text-sm"><span className="uma-code shrink-0 font-bold">{t("rank",{rank:entry.finishRank})}</span><div className="min-w-0"><p className="break-words font-semibold">{entry.name?cardName(entry):t("unknownCard")}</p>{tied>1?<p>{t("tiedCount",{count:tied})}</p>:null}</div></li>;})}</ol>
   </section>{answer.excluded.length?<p className="mt-3 text-sm text-[var(--color-muted)]">{t("excluded",{count:answer.excluded.length})}</p>:null}
  </>:null}
  <div className="mt-4 flex flex-col gap-2 sm:flex-row"><Button asChild variant="secondary" className="h-auto min-h-12 whitespace-normal"><Link to="/play/race-rebuild">{t("again")}</Link></Button><Button asChild variant="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/">{t("home")}</Link></Button></div>
 </section>;
}
