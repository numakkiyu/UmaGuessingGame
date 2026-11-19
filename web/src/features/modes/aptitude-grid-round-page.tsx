import { roomDisplayName } from "@/lib/room-display-name";
import { GameStage } from "@/components/uma/game-stage";
import "@/i18n/grid-online";
import {useTranslation} from "@/i18n";
import {axisName,aptitudeValue} from "@/i18n/aptitude-values";
import {cardMatches,cardName} from "@/i18n/entities";
import {gridCondition,gridFailure} from "@/i18n/grid-online";
import {usePlayerText} from "@/i18n/player-text";
import {useMemo,useState} from "react";
import {Link,useParams} from "react-router";
import {Check,Loader2,RotateCcw,Search} from "lucide-react";
import {TeamRoundControl} from "@/features/rooms/team-round-control";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView,ErrorView,ForbiddenView,LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {useApi} from "@/lib/api";
import {RejoinPrompt,isCredentialRefreshFailure} from "./rejoin-prompt";
import {boardAxes,normalizeGridRound,type GridCell,type GridPoolCard,type GridRound} from "@/lib/api/aptitude-grid";
import {ApiError,describeFailure,localizeFailure,type Failure} from "@/lib/api/client";
import {validGridClaim,type GridClaimCommand} from "@/lib/api/game-commands";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {useCharacters} from "@/lib/api/queries";
import {useRoom} from "@/lib/api/room-queries";
import {readTeams,readTeamMembers} from "@/lib/api/teams";
import {isTerminalPhase,phaseLabel,publicStateOf,resultsOf,useRoundSnapshot} from "@/lib/api/round-snapshot";
import type {components,RoomSnapshot} from "@/lib/api/types";
import {useRealtime} from "@/lib/realtime/use-realtime";
import {cn} from "@/lib/utils";
import {BoardCell} from "./grid-board-cell";

type PoolCard = GridPoolCard & {aliases?:readonly string[]};
function PoolPicker({pool,selectedCell,selectedCardId,disabled,used,onPick}:{pool:readonly PoolCard[];selectedCell:GridCell|null;selectedCardId:string|null;disabled:boolean;used:ReadonlySet<string>;onPick:(id:string)=>void}) {
 const {t}=useTranslation("gridOnline");
 const [query,setQuery]=useState("");
 const filtered=useMemo(()=>pool.filter(card=>cardMatches(card,query)||(query.trim()&&card.aliases?.some(alias=>cardMatches({name:alias},query)))),[pool,query]);
 return <section className="uma-panel flex min-w-0 flex-col p-4" aria-labelledby="pool-heading">
  <div className="uma-section-head"><h2 id="pool-heading" className="text-base font-bold">{t("candidates")}</h2><p className="text-sm text-[var(--color-muted)]">{t("count",{shown:filtered.length,total:pool.length})}</p></div>
  <label htmlFor="pool-search" className="mt-2 block text-sm font-semibold">{t("search")}</label>
  <div className="relative mt-1"><Search aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-[var(--color-muted)]"/><Input type="search" id="pool-search" value={query} onChange={event=>setQuery(event.target.value)} placeholder={t("searchPlaceholder")} className="pl-9" disabled={disabled} autoComplete="off" inputMode="search"/></div>
  {!selectedCell ? <p className="mt-2 text-sm text-[var(--color-muted)]">{t("chooseFirst")}</p> : null}
  {!filtered.length ? <p className="mt-2 text-sm text-[var(--color-muted)]">{t("noMatch",{query})}</p> : <ul className="mt-2 flex min-w-0 flex-col gap-1 overflow-y-auto pr-1 lg:max-h-[28rem]">{filtered.map(card=><li key={card.id} className="min-w-0"><button type="button" disabled={disabled||used.has(card.id)} aria-pressed={card.id===selectedCardId} onClick={()=>onPick(card.id)} className={cn("flex min-h-11 w-full min-w-0 items-center gap-2 rounded-[var(--radius-control)] border px-2 py-1.5 text-left transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",card.id===selectedCardId ? "border-[var(--color-blue-deep)] bg-[var(--color-page-tint)] ring-1 ring-[var(--color-blue-deep)]" : "border-[var(--color-line)] bg-[var(--color-panel)] hover:bg-[var(--color-panel-soft)]")}>
   {card.imagePath ? <img src={card.imagePath} alt="" className="size-9 shrink-0 object-contain" loading="lazy" decoding="async"/> : <span aria-hidden="true" className="size-9 shrink-0 rounded-full bg-[var(--color-panel-soft)]"/>}
   <span className="min-w-0 flex-1 break-words text-sm font-semibold">{card.name ? cardName(card) : t("unknownCard")}{used.has(card.id) ? <span className="block text-xs text-[var(--color-muted)]">{t("used")}</span> : null}</span>{card.id===selectedCardId ? <Check aria-hidden="true" className="size-4 shrink-0"/> : null}
  </button></li>)}</ul>}
 </section>;
}

type Choice={cellId:string|null;characterId:string|null};
function readChoice(scope:string):Choice|null {try{const raw=sessionStorage.getItem(`uma:grid-choice:${scope}`);if(!raw||raw.length>1024)return null;const value=JSON.parse(raw) as Choice;return value&&(!value.cellId||typeof value.cellId==="string")&&(!value.characterId||typeof value.characterId==="string") ? value : null;}catch{return null;}}
function saveChoice(scope:string|null,choice:Choice) {if(!scope)return;try{sessionStorage.setItem(`uma:grid-choice:${scope}`,JSON.stringify(choice));}catch{/* Selection stays in memory; no persistence claim. */}}
export function gridSubjectName(room:RoomSnapshot|undefined,id:string,fallback:string):string {
 if(!room)return fallback;
 const team=readTeams(room).find(team=>team.teamId===id);if(team)return team.label;
 const member=readTeamMembers(room).find(member=>member.membershipId===id);
 return member ? member.publicCode&&/^\d{6}$/.test(member.publicCode) ? `${member.displayName} · ${member.publicCode}` : member.displayName : fallback;
}

export function AptitudeGridRoundPage() {
 const {t}=useTranslation("gridOnline"),{roomId}=useParams<{roomId:string}>(),{api,session}=useApi();
 const roomQuery=useRoom(roomId??null),roundId=roomQuery.data?.room.currentRound?.roundId??null,snapshot=useRoundSnapshot(roundId),characters=useCharacters().data?.items;
 const round=useMemo(()=>{
  if(snapshot.meta?.roundId!==roundId)return null;
  const publicState=publicStateOf(snapshot.raw);if(!publicState)return null;
  const normalized=normalizeGridRound({publicState} as unknown as components["schemas"]["RoundView"]);if(!normalized)return null;
  const pool=normalized.pool.map(card=>{const known=characters?.find(c=>c.characterId===card.id);return known ? {...card,name:known.displayName,localizedNames:known.localizedNames,aliases:known.aliases} : card;});
  return {...normalized,pool,cells:normalized.cells.map(cell=>{const known=characters?.find(c=>c.characterId===cell.claim?.characterId);return known&&cell.claim ? {...cell,claim:{...cell.claim,name:known.displayName,localizedNames:known.localizedNames}} : cell;})};
 },[snapshot.raw,snapshot.meta?.roundId,roundId,characters]);
 const scope=session&&roundId ? `${session.sessionId}:${roundId}:grid` : null;
 const command=useGameCommand(scope,validGridClaim,(body,key)=>api.createSubmission(roundId!,body,key));
 const [choice,setChoice]=useState<Choice>({cellId:null,characterId:null}),[initialized,setInitialized]=useState<string|null>(null);
 const [failure,setFailure]=useState<Failure|null>(null),[errorCode,setErrorCode]=useState(""),[rejected,setRejected]=useState(false),[needsRejoin,setNeedsRejoin]=useState(false),[refreshing,setRefreshing]=useState(false);
 const [notice,setNotice]=usePlayerText("gridOnline");
 if(scope&&round&&initialized!==scope){setInitialized(scope);const stored=readChoice(scope);setChoice(stored&&round.cells.some(c=>c.cellId===stored.cellId&&!c.claim)&&(!stored.characterId||round.pool.some(c=>c.id===stored.characterId)) ? stored : {cellId:null,characterId:null});}
 const realtime=useRealtime({roomId:roomId??null,enabled:Boolean(roomId),handlers:{onSnapshotNeeded:()=>{void snapshot.refetch();void roomQuery.refetch();}}});
 const currentPhase=snapshot.meta?.phase??null,ended=isTerminalPhase(currentPhase),collecting=currentPhase==="collecting"&&!snapshot.meta?.paused;
 const countdown=useCountdown(snapshot.meta?.deadlineAt,collecting),duel=round?.variant==="duel",ownTurn=!duel||!!round?.self&&round.self.subjectId===round.turnSubjectId;
 const personalDone=!!round?.self?.timedOut||(!duel&&(round?.self?.claims.length??0)>=9);
 const canAct=!!round&&!!round.self&&collecting&&!round.closed&&!personalDone&&ownTurn&&snapshot.canSubmit;
 const busy=command.busy||refreshing||!!command.pending||command.expired||snapshot.isError;
 const selectedCell=round?.cells.find(c=>c.cellId===choice.cellId)??null,selectedCard=round?.pool.find(c=>c.id===choice.characterId)??null;
 const used=new Set(round?.cells.flatMap(cell=>cell.claim ? [cell.claim.characterId] : [])??[]),axes=round?boardAxes(round.cells):null;
 const labelFor=(id:string)=>gridSubjectName(roomQuery.data?.room,id,t("participant"));
 const mappedError=gridFailure(errorCode);
 const info=failure ? mappedError ? {...failure,title:t("errorTitle"),detail:mappedError} : rejected ? {...failure,title:t("rejectedTitle"),detail:t("rejectedDetail")} : localizeFailure(failure) : null;
 function pick(next:Choice){if(!canAct||busy)return;setChoice(next);saveChoice(scope,next);setNotice("");}
 async function claim(retry=false){
  if(!roundId||command.busy||refreshing)return;
  if(!retry&&(!round||!canAct||busy||!selectedCell||selectedCell.claim||!selectedCard||used.has(selectedCard.id)))return;
  const body:GridClaimCommand|undefined=retry?undefined:{phaseVersion:snapshot.meta!.phaseVersion,submissionRevision:snapshot.submissionRevision,controlGeneration:snapshot.controlGeneration,modeAction:{kind:"grid.claim",cellId:selectedCell!.cellId,characterId:selectedCard!.id}};
  const original=body??command.pending?.body,action=original?.modeAction,target=action?.kind==="grid.claim"?round?.cells.find(c=>c.cellId===action.cellId):null;
  setRefreshing(true);setFailure(null);setErrorCode("");setRejected(false);setNotice("");
  try{
   const result=await command.run(body);if(!result)return;
   if(!result.ok){setFailure(describeFailure(result.error));setErrorCode(result.error instanceof ApiError?result.error.code:"");if(!result.uncertain&&result.error instanceof ApiError&&(result.error.isConflict||result.error.isUnprocessable))await snapshot.refetch();if(isCredentialRefreshFailure(result.error))setNeedsRejoin(true);return;}
   if(result.value.data?.outcome!=="accepted"){setRejected(true);setFailure({kind:"error",title:"",detail:"",diagnosticId:result.value.requestId,retryable:true});return;}
   setNotice(target?"claimed":"recorded",target?{row:target.row+1,col:target.col+1}:undefined);setChoice({cellId:null,characterId:null});saveChoice(scope,{cellId:null,characterId:null});await snapshot.refreshAfterSubmit();
  }finally{setRefreshing(false);}
 }
 if(!roomId)return <ErrorView failure={{kind:"error",title:t("missingTitle"),detail:t("missingDetail"),diagnosticId:null,retryable:false}}/>;
 if(roomQuery.isLoading)return <LoadingBlock label={t("loadingRoom")} rows={3}/>;
 if(roomQuery.isError){const failure=describeFailure(roomQuery.error);return failure.kind==="forbidden"?<ForbiddenView failure={failure} onRetry={()=>void roomQuery.refetch()}/>:<ErrorView failure={failure} onRetry={()=>void roomQuery.refetch()}/>;}
 const connectionStatus=realtime.status==="online"?"online":realtime.status==="connecting"||realtime.status==="reconnecting"?"reconnecting":realtime.status==="stale"?"stale":"offline";
 return <GameStage mode="aptitude-grid" className="uma-stack">
  <TeamRoundControl room={roomQuery.data?.room} raw={snapshot.raw} onChanged={snapshot.refetch}/><ConnectionBar status={connectionStatus} detail={roomDisplayName(roomQuery.data?.room, t("room"))}/>
  <section className="uma-panel p-4"><div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><h1 className="u-fluid-task">{t("title")}</h1><p className="mt-1 text-sm text-[var(--color-muted)]">{phaseLabel(currentPhase)} · {t(duel?"duel":"challenge")} · {t("occupied",{count:round?.cells.filter(c=>c.claim).length??0})}</p></div>{collecting&&snapshot.meta?.deadlineAt&&!ended ? <div className="flex shrink-0 flex-col items-end"><span className="text-xs text-[var(--color-muted)]">{t(duel?"turnRemaining":"remaining")}</span><span className={cn("uma-code text-2xl font-bold tabular-nums",countdown.expired?"text-[var(--color-danger)]":"text-[var(--color-ink)]")} aria-live="off">{countdown.text}</span></div>:null}</div></section>
  {command.pending&&!command.busy?<Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button variant="secondary" className="h-auto min-h-12 whitespace-normal" disabled={refreshing} onClick={()=>void claim(true)}>{t("confirmAction")}</Button></Alert>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" onClick={()=>{void snapshot.refetch().then(ok=>{if(ok)command.acknowledgeExpired();});}}>{t("latest")}</Button></Alert>:null}
  {command.storageLimited?<p className="text-sm text-[var(--color-muted)]">{t("limited")}</p>:null}
  {notice?<Alert tone="info" title={t("notice")}>{notice}</Alert>:null}
  {needsRejoin?<RejoinPrompt roomId={roomId}/>:null}
  {info?<Alert tone={info.kind==="timeout"?"warning":"danger"} title={info.title} action={<Button variant="secondary" onClick={()=>void snapshot.refetch()}>{t("refresh")}</Button>}><p>{info.detail}</p>{info.diagnosticId?<p>{t("problemId")}：<span className="uma-code">{info.diagnosticId}</span></p>:null}</Alert>:null}
  {round&&!round.solvable?<Alert tone="warning" title={t("unsolvable")}>{t("unsolvableDetail")}</Alert>:null}
  {!roundId?<EmptyView title={t("waitingTitle")} detail={t("waitingDetail")}/>:null}
  {roundId&&snapshot.isLoading&&!round?<LoadingBlock label={t("loadingRound")} rows={3}/>:null}
  {roundId&&snapshot.isError?<ErrorView failure={describeFailure(snapshot.error)} onRetry={()=>void snapshot.refetch()}/>:null}
  {round&&axes&&currentPhase!=="cancelled"?<div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,22rem)] lg:items-start">
   <div className="uma-stack uma-stack--tight min-w-0">
    {!ended?<p role="status" className="text-sm font-semibold">{t(!round.self?"watching":round.self.timedOut?"dropped":round.closed?"boardClosed":personalDone?"complete":duel?ownTurn?"ownTurn":"otherTurn":!collecting?"phaseClosed":"mine",{name:round.turnSubjectId?labelFor(round.turnSubjectId):t("participant"),phase:phaseLabel(currentPhase),count:round.self?.claims.length??0})}</p>:null}
    <section className="uma-panel min-w-0 p-3 sm:p-4" aria-labelledby="board-heading"><div className="uma-section-head"><h2 id="board-heading" className="text-base font-bold">{t("board")}</h2><p className="text-sm text-[var(--color-muted)]">{t("boardAxes",{row:axisName(axes.rowAxis),col:axisName(axes.colAxis)})}</p></div><p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("prompt")}</p>
     <div className="mt-3"><div className="grid grid-cols-3 gap-2" aria-hidden="true">{axes.colLabels.map(label=><div key={label} className="rounded-[var(--radius-control)] bg-[var(--color-panel-soft)] px-1 py-1 text-center text-sm font-bold">{aptitudeValue(axes.colAxis,label)}</div>)}</div><div className="mt-2 flex flex-col gap-2">{axes.rowLabels.map((rowLabel,index)=><div key={rowLabel} className="flex flex-col gap-1"><div className="text-xs font-bold text-[var(--color-muted)]">{t("rowLabel",{axis:axisName(axes.rowAxis),value:aptitudeValue(axes.rowAxis,rowLabel)})}</div><div className="grid grid-cols-3 gap-2">{round.cells.filter(cell=>cell.row===index).sort((a,b)=>a.col-b.col).map(cell=><BoardCell key={cell.index} cell={{...cell,claim:cell.claim?{...cell.claim,name:cell.claim.name||t("unknownCard")}:null}} selected={cell.cellId===choice.cellId} disabled={busy||!canAct||!!cell.claim} ownerLabel={duel&&cell.claim?labelFor(cell.claim.subjectId):undefined} onSelect={cell=>pick({cellId:cell.cellId,characterId:null})}/>)}</div></div>)}</div></div>
     {ended?<p className="mt-3 text-sm font-semibold">{t("ended")}</p>:null}
    </section>
    {!ended?<section className="uma-panel p-4" aria-labelledby="confirm-heading"><h2 id="confirm-heading" className="text-base font-bold">{t("confirm")}</h2>{selectedCell?<div className="mt-2 space-y-2"><p className="text-sm">{t("selectedCell",{row:selectedCell.row+1,col:selectedCell.col+1})}</p><dl className="grid grid-cols-1 gap-2 sm:grid-cols-2">{[["rowCondition",selectedCell.rowCondition],["colCondition",selectedCell.colCondition]].map(([key,condition])=><div key={key as string} className="rounded-[var(--radius-control)] bg-[var(--color-panel-soft)] px-3 py-2"><dt className="text-xs text-[var(--color-muted)]">{t(key as string)}</dt><dd className="text-sm font-bold">{gridCondition(condition as GridCell["rowCondition"])}</dd></div>)}</dl><p className="break-words text-sm">{selectedCard?t("selectedCard",{name:selectedCard.name?cardName(selectedCard):t("unknownCard")}):t("chooseCard")}</p></div>:<p className="mt-2 text-sm text-[var(--color-muted)]">{t("chooseCell")}</p>}<Button variant="primary" size="primary" block className="mt-3 h-auto min-h-12 whitespace-normal" disabled={busy||!canAct||!selectedCell||!!selectedCell.claim||!selectedCard||used.has(selectedCard.id)} onClick={()=>void claim()}>{command.busy||refreshing?<Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/>:<Check aria-hidden="true" className="size-4"/>}{t(command.busy||refreshing?"submitting":"submit")}</Button></section>:null}
   </div>
   <aside className="uma-stack uma-stack--tight min-w-0">{!ended?<PoolPicker pool={round.pool} selectedCell={selectedCell} selectedCardId={choice.characterId} disabled={busy||!canAct} used={used} onPick={id=>pick({...choice,characterId:id})}/>:null}<section className="uma-card p-4"><h2 className="text-base font-bold">{t("rules")}</h2><ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]"><li>{t(duel?"duelRule":"challengeRule")}</li>{duel?<li>{t("turnRule")}</li>:null}<li>{t("uniqueRule")}</li><li>{t("hiddenRule")}</li>{round.self?<li>{t("mine",{count:round.self.claims.length})}</li>:null}</ul></section><Button variant="secondary" block disabled={command.busy||refreshing} onClick={()=>void snapshot.refetch()}><RotateCcw aria-hidden="true" className="size-4"/>{t("refresh")}</Button><Button asChild variant="secondary" block><Link to={`/rooms/${roomId}`}>{t("back")}</Link></Button></aside>
  </div>:null}
  {ended&&round?<GridResult round={round} cancelled={currentPhase==="cancelled"} labelFor={labelFor} scores={parseGridScores(resultsOf(snapshot.raw))}/>:null}
 </GameStage>;
}

export type GridScore={subjectId:string;scoreUnits:number;rank:number|null};
export function parseGridScores(items:readonly unknown[]):GridScore[]{return items.flatMap(value=>{if(!value||typeof value!=="object")return [];const r=value as Record<string,unknown>;return typeof r.subjectId==="string"&&typeof r.scoreUnits==="number"&&Number.isSafeInteger(r.scoreUnits)&&r.scoreUnits>=0?[{subjectId:r.subjectId,scoreUnits:r.scoreUnits,rank:typeof r.rank==="number"&&Number.isSafeInteger(r.rank)&&r.rank>0?r.rank:null}]:[];});}
export function GridResult({round,cancelled=false,labelFor,scores=[]}:{round:GridRound;cancelled?:boolean;labelFor?:(id:string)=>string;scores?:GridScore[]}){
 const {t}=useTranslation("gridOnline");
 const name=(id:string)=>labelFor?.(id)??t("participant");
 const ready=round.aptitudes.length>0,duel=round.variant==="duel",correct=round.cells.filter(cell=>cell.claim?.satisfied===true).length;
 const ownScore=scores.find(score=>score.subjectId===round.self?.subjectId)?.scoreUnits;
 const judgment=(cell:GridCell)=>t(!cell.claim?"empty":cell.claim.satisfied===true?"satisfied":cell.claim.satisfied===false?"mismatch":"pending");
 return <section id="grid-result" className="uma-panel p-5" aria-labelledby="grid-result-heading"><h2 id="grid-result-heading" className="u-fluid-task">{t(cancelled?"cancelled":ready?"result":"waitingResult")}</h2>
  {cancelled?<p className="mt-2">{t("cancelledDetail")}</p>:!ready?null:<>
   <dl className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-4">{[[t("correct"),`${correct} / 9`],...(!duel&&round.self?[[t("challengeScore"),ownScore===undefined?Math.floor(100*correct/9):Math.floor(ownScore/100)]]:[]),...(round.self?[[t("myCells"),round.self.claims.length],[t("attempts"),round.self.attempts]]:[])].map(([key,value])=><div key={key} className="rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3"><dt className="text-xs text-[var(--color-muted)]">{key}</dt><dd className="uma-code text-xl font-bold">{value}</dd></div>)}</dl>
   <section className="mt-4" aria-label={t("standings")}><h3 className="text-base font-bold">{t("standings")}</h3><div className="mt-2 hidden md:block"><table className="w-full table-fixed text-left text-sm"><thead><tr>{["owner","claims","rank","status"].map(key=><th key={key} scope="col" className="p-2">{t(key)}</th>)}</tr></thead><tbody>{round.subjects.map(subject=><tr key={subject.subjectId}><th scope="row" className="break-words p-2">{name(subject.subjectId)}</th><td className="p-2">{subject.claims}</td><td className="p-2">{scores.find(score=>score.subjectId===subject.subjectId)?.rank??"—"}</td><td className="p-2">{t(subject.timedOut?"stopped":"completed")}</td></tr>)}</tbody></table></div><ul className="mt-2 space-y-2 md:hidden">{round.subjects.map(subject=><li key={subject.subjectId} className="rounded-lg border border-[var(--color-line)] p-3"><p className="break-words font-semibold">{name(subject.subjectId)}</p><dl className="mt-2 grid grid-cols-2 gap-2 text-sm"><div><dt>{t("claims")}</dt><dd>{subject.claims}</dd></div><div><dt>{t("rank")}</dt><dd>{scores.find(score=>score.subjectId===subject.subjectId)?.rank??"—"}</dd></div><div><dt>{t("status")}</dt><dd>{t(subject.timedOut?"stopped":"completed")}</dd></div></dl></li>)}</ul></section>
   <div className="uma-scroll-x mt-4 hidden rounded-lg border border-[var(--color-line)] md:block" tabIndex={0} role="region" aria-label={t("resultsLabel")}><table className="w-full min-w-[40rem] border-collapse text-left text-sm"><caption className="sr-only">{t("resultsCaption")}</caption><thead className="bg-[var(--color-panel-soft)]"><tr>{["cell","rowCondition","colCondition","character","owner","judgement"].map(key=><th key={key} scope="col" className="px-3 py-2">{t(key)}</th>)}</tr></thead><tbody>{round.cells.map(cell=><tr key={cell.index} className="border-t border-[var(--color-line)]"><th scope="row" className="px-3 py-2">{t("cellName",{row:cell.row+1,col:cell.col+1})}</th><td className="px-3 py-2">{gridCondition(cell.rowCondition)}</td><td className="px-3 py-2">{gridCondition(cell.colCondition)}</td><td className="px-3 py-2">{cell.claim?cell.claim.name?cardName(cell.claim):t("unknownCard"):"—"}</td><td className="px-3 py-2">{cell.claim?name(cell.claim.subjectId):"—"}</td><td className="px-3 py-2 font-semibold">{judgment(cell)}</td></tr>)}</tbody></table></div>
   <ul className="mt-4 space-y-2 md:hidden" aria-label={t("resultsLabel")}>{round.cells.map(cell=><li key={cell.index} className="rounded-lg border border-[var(--color-line)] p-3 text-sm"><h3 className="font-semibold">{t("cellName",{row:cell.row+1,col:cell.col+1})}</h3><p className="mt-1">{gridCondition(cell.rowCondition)} × {gridCondition(cell.colCondition)}</p><dl className="mt-2 space-y-1"><div><dt className="inline text-[var(--color-muted)]">{t("character")}：</dt><dd className="inline break-words">{cell.claim?cell.claim.name?cardName(cell.claim):t("unknownCard"):"—"}</dd></div><div><dt className="inline text-[var(--color-muted)]">{t("owner")}：</dt><dd className="inline break-words">{cell.claim?name(cell.claim.subjectId):"—"}</dd></div><div><dt className="inline text-[var(--color-muted)]">{t("judgement")}：</dt><dd className="inline font-semibold">{judgment(cell)}</dd></div></dl></li>)}</ul>
  </>}
  <div className="mt-4 flex flex-col gap-2 sm:flex-row sm:flex-wrap"><Button asChild variant="secondary" size="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/play/aptitude-grid">{t("again")}</Link></Button><Button asChild variant="primary" size="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/">{t("home")}</Link></Button></div>
 </section>;
}
