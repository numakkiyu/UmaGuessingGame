/** G09: clues belong to individual members; the confirmed arrangement belongs to their team. */
import "@/i18n/clue-online";
import {useMemo,useRef,useState} from "react";
import {Link,useParams} from "react-router";
import {Eye,EyeOff,Lock} from "lucide-react";
import {useTranslation} from "@/i18n";
import {usePlayerText} from "@/i18n/player-text";
import {clueCardLabel,clueText} from "@/i18n/clue-content";
import {TeamRoundControl} from "@/features/rooms/team-round-control";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView,ErrorView,ForbiddenView,LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {validClueAssignment,type ClueAssignmentCommand} from "@/lib/api/game-commands";
import {useApi} from "@/lib/api";
import {RejoinPrompt,isCredentialRefreshFailure} from "./rejoin-prompt";
import {describeFailure,localizeFailure} from "@/lib/api/client";
import {normalizeCluePuzzleRound} from "@/lib/api/modes-g09-g12";
import {useRoom} from "@/lib/api/room-queries";
import {phaseLabel,useRoundSnapshot} from "@/lib/api/round-snapshot";
import {loadClueDraft,saveClueDraft,type ClueDraft} from "@/lib/local/clue-online-draft";
import {useRealtime} from "@/lib/realtime/use-realtime";
import {cn} from "@/lib/utils";

export function CluePuzzleRoundPage(){
 const {t,i18n}=useTranslation("clueOnline"),{roomId}=useParams<{roomId:string}>(),{api,session}=useApi();
 const roomQuery=useRoom(roomId??null),room=roomQuery.data?.room,roundId=room?.currentRound?.roundId??null,snapshot=useRoundSnapshot(roundId);
 const round=useMemo(()=>snapshot.raw&&snapshot.meta?.roundId===roundId?normalizeCluePuzzleRound(snapshot.raw):null,[snapshot.raw,snapshot.meta?.roundId,roundId]);
 const commandScope=session&&roundId?`${session.sessionId}:${roundId}:clue-puzzle`:null;
 const command=useGameCommand(commandScope,validClueAssignment,(body:ClueAssignmentCommand,key)=>api.createSubmission(roundId!,body,key));
 // A new controller or confirmed revision starts from the team's authoritative mapping.
 // Pause/locale changes never alter this scope; an uncertain command has its own original scope.
 const draftScope=commandScope&&round?.selfSubjectId?`${commandScope}:${round.selfSubjectId}:${snapshot.controlGeneration}:${snapshot.submissionRevision}`:null;
 const [draft,setDraft]=useState<ClueDraft|null>(null),[initialized,setInitialized]=useState<string|null>(null);
 if(initialized!==draftScope){setInitialized(draftScope);setDraft(draftScope&&round?loadClueDraft(draftScope,round.slots.map(s=>s.slotIndex),round.cards.map(c=>c.cardId)):null);}
 const frozen=["locked","revealed","settled","cancelled"].includes(snapshot.meta?.phase??"");
 const effective=(frozen?round?.assignment:draft?.assignment??round?.assignment)??new Map<number,string>(),activeSlot=frozen?null:draft?.activeSlot??null;
 const [showClues,setShowClues]=useState(true),[refreshing,setRefreshing]=useState(false),[failure,setFailure]=useState<unknown>(null),[notice,setNotice]=usePlayerText("clueOnline");
 const candidates=useRef<HTMLElement>(null),phase=snapshot.meta?.phase??null,published=phase==="revealed"||phase==="settled",cancelled=phase==="cancelled";
 const collecting=phase==="collecting"&&!snapshot.meta?.paused,countdown=useCountdown(snapshot.meta?.deadlineAt,collecting);
 const busy=refreshing||command.busy||!!command.pending||command.expired||snapshot.isError||roomQuery.isError;
 const editable=!!round?.selfIsController&&snapshot.canSubmit&&collecting&&!countdown.expired&&!busy;
 const realtime=useRealtime({roomId:roomId??null,enabled:!!roomId,handlers:{onSnapshotNeeded:()=>{void snapshot.refetch();void roomQuery.refetch();}}});
 const info=failure?localizeFailure(describeFailure(failure)):null;
 function slotName(position:number){return t("slot",{position});}
 function labelOf(id:string){const card=round?.cards.find(c=>c.cardId===id);return card?.label?clueCardLabel(card,i18n.language).text:t("missingCard");}
 function choose(next:ClueDraft){if(!editable)return;setDraft(next);setNotice("");if(draftScope&&!saveClueDraft(draftScope,next))setNotice("saveFailed");}
 function selectSlot(slot:number){choose({assignment:new Map(effective),activeSlot:slot});if(editable&&window.matchMedia?.("(max-width:767px)").matches)candidates.current?.scrollIntoView?.({behavior:window.matchMedia("(prefers-reduced-motion: reduce)").matches?"instant":"smooth",block:"start"});}
 function assign(cardId:string){if(!editable||activeSlot===null)return;const assignment=new Map(effective);for(const[slot,value]of assignment)if(value===cardId)assignment.delete(slot);assignment.set(activeSlot,cardId);choose({assignment,activeSlot:round?.slots.find(s=>!assignment.has(s.slotIndex))?.slotIndex??null});}
 async function submit(confirm=false){if(!roundId||command.busy||refreshing||!confirm&&(!editable||!round||effective.size!==round.slots.length))return;setRefreshing(true);setFailure(null);setNotice("");try{const result=await command.run(confirm?undefined:{phaseVersion:snapshot.meta!.phaseVersion,submissionRevision:snapshot.submissionRevision,controlGeneration:snapshot.controlGeneration,modeAction:{kind:"clue.assignment",assignments:[...effective].sort((a,b)=>a[0]-b[0]).map(([slot,card])=>({slotId:String(slot),characterId:card}))}});if(result?.ok){setNotice(result.value.data?.outcome==="accepted"?"saved":"notAccepted");await snapshot.refetch();}else if(result){setFailure(result.error);if(!result.uncertain)await snapshot.refetch();}}finally{setRefreshing(false);}}
 function readError(error:unknown){const f=localizeFailure(describeFailure(error));return f.kind==="forbidden"?<ForbiddenView failure={f} onRetry={()=>void snapshot.refetch()}/>:<ErrorView failure={f} onRetry={()=>void snapshot.refetch()}/>;}
 const candidateButton=(id:string)=><Button variant="secondary" className="h-auto min-h-11 shrink-0 whitespace-normal" disabled={!editable||activeSlot===null} aria-label={t("chooseCard",{label:labelOf(id)})} onClick={()=>assign(id)}>{t("chooseThis")}</Button>;
 const clues=round?[...round.sharedClues,...round.privateClues,...round.answer?.constraints??[]]:[];
 return <div className="uma-stack">
  <ConnectionBar status={realtime.status} detail={room?.name??t("room")}/>
  {isCredentialRefreshFailure(snapshot.error)||isCredentialRefreshFailure(failure)?<RejoinPrompt roomId={roomId??""}/>:null}
  {snapshot.isLoading||roomQuery.isLoading?<LoadingBlock label={t("loading")} rows={4}/>:null}
  {snapshot.isError?readError(snapshot.error):null}{roomQuery.isError?readError(roomQuery.error):null}
  {command.pending?<Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button className="mt-3 h-auto min-h-11 whitespace-normal" disabled={refreshing||command.busy} onClick={()=>void submit(true)}>{t("confirmAction")}</Button></Alert>:null}
  {command.busy?<p role="status" className="text-sm">{t("busy")}</p>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button className="mt-3 h-auto min-h-11 whitespace-normal" variant="secondary" disabled={refreshing} onClick={()=>void(async()=>{setRefreshing(true);try{if(await snapshot.refetch())command.acknowledgeExpired();}finally{setRefreshing(false);}})()}>{t("refresh")}</Button></Alert>:null}
  {command.storageLimited?<Alert tone="warning" title={t("storageTitle")}>{t("storage")}</Alert>:null}
  {info&&!command.pending?<Alert tone="danger" title={info.title}>{info.detail}</Alert>:null}{notice?<p role="status" className="text-sm leading-6">{notice}</p>:null}
  {round?<>
   <header className="border-b border-[var(--color-line)] pb-4"><h1 className="u-fluid-task">{t("title")}</h1><p className="mt-2 max-w-prose text-sm leading-6 text-[var(--color-muted)]">{t("intro")}</p><p className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm"><span>{phaseLabel(phase)}</span>{round.selfSubjectId?<span>{t("placed",{placed:effective.size,total:round.slots.length})}</span>:null}<span>{t("conditions",{count:round.constraintCount})}</span>{collecting&&countdown.text?<span className="font-bold">{countdown.text}</span>:null}</p></header>
   {cancelled?<Alert tone="info" title={t("cancelled")}>{t("cancelledDetail")}</Alert>:null}
   {!cancelled&&!published&&snapshot.meta?.paused?<p role="status" className="text-sm">{t("paused")}</p>:null}
   {collecting&&countdown.expired?<p role="status" className="text-sm">{t("deadline")}</p>:null}
   {!published&&!cancelled&&round.privateClues.length?<section className="uma-panel border-l-4 border-l-[var(--color-brand-green)] p-4" aria-labelledby="clue-mine"><div className="uma-section-head"><h2 id="clue-mine" className="flex items-center gap-2 font-bold"><Lock aria-hidden="true" className="size-4"/>{t("private")}</h2><Button variant="ghost" className="h-auto min-h-11 whitespace-normal" onClick={()=>setShowClues(v=>!v)}>{showClues?<EyeOff aria-hidden="true" className="size-4"/>:<Eye aria-hidden="true" className="size-4"/>}{t(showClues?"hideClues":"showClues")}</Button></div><p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("privateHint")}</p>{showClues?<ul className="mt-3 space-y-2">{round.privateClues.map(c=><li key={c.constraintId} className="break-words rounded bg-[var(--color-brand-green-soft)] p-3 text-sm leading-6">{clueText(c,round.cards,i18n.language).text}</li>)}</ul>:null}</section>:null}
   {round.sharedClues.length?<section className="uma-panel p-4" aria-labelledby="clue-shared"><h2 id="clue-shared" className="font-bold">{t("shared")}</h2><ul className="mt-2 space-y-2">{round.sharedClues.map(c=><li key={c.constraintId} className="break-words text-sm leading-6">{clueText(c,round.cards,i18n.language).text}</li>)}</ul></section>:!published&&!cancelled?<p className="text-sm leading-6 text-[var(--color-muted)]">{t("noShared")}</p>:null}
   {!published&&!cancelled?<p className="text-sm leading-6 text-[var(--color-muted)]">{!round.selfSubjectId?t("observer"):!round.selfIsController?t("notController"):null}</p>:null}
   <div className={cn("grid min-w-0 items-start gap-4",round.selfSubjectId?"lg:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)]":"")}>
    {round.selfSubjectId?<section className="uma-panel min-w-0 p-4" aria-labelledby="clue-slots"><h2 id="clue-slots" className="text-lg font-bold">{t("arrangement")}</h2>{!frozen?<p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("arrangementHint")}</p>:null}<ol className="mt-3 space-y-2">{round.slots.map(slot=><li key={slot.slotIndex}><button type="button" aria-label={t("selectSlot",{slot:slotName(slot.slotIndex)})} aria-pressed={activeSlot===slot.slotIndex} disabled={!editable} onClick={()=>selectSlot(slot.slotIndex)} className={cn("flex min-h-14 w-full flex-col items-start gap-1 rounded-[var(--radius-control)] border-2 px-3 py-2 text-left disabled:opacity-100",activeSlot===slot.slotIndex?"border-[var(--color-blue-deep)] bg-[var(--color-page-tint)]":"border-[var(--color-line)] bg-[var(--color-panel)]")}><span className="text-sm font-bold">{slotName(slot.slotIndex)}</span><span className="break-words text-sm">{effective.has(slot.slotIndex)?labelOf(effective.get(slot.slotIndex)!):t("emptySlot")}</span></button></li>)}</ol>{activeSlot!==null?<Button className="mt-3 h-auto min-h-11 whitespace-normal" variant="ghost" disabled={!editable} onClick={()=>{const assignment=new Map(effective);assignment.delete(activeSlot);choose({assignment,activeSlot});}}>{t("clearSlot")}</Button>:null}</section>:null}
    <section ref={candidates} className="uma-panel min-w-0 scroll-mt-4 p-4" aria-labelledby="clue-cards"><h2 id="clue-cards" className="text-lg font-bold">{t("candidates")}</h2>{round.selfSubjectId&&!frozen?<p className="mt-1 text-sm leading-6 text-[var(--color-muted)]" aria-live="polite">{activeSlot!==null?t("activeSlot",{slot:slotName(activeSlot)}):t("selectFirst")}</p>:null}
     <table className="mt-3 hidden w-full table-fixed text-left text-sm md:table"><caption className="sr-only">{t("candidates")}</caption><thead><tr><th className="py-2">{t("card")}</th>{round.selfSubjectId?<th className="w-24 py-2">{t("currentSlot")}</th>:null}{!published&&!cancelled&&round.selfIsController?<th className="w-24 py-2">{t("choose")}</th>:null}</tr></thead><tbody>{round.cards.map(card=>{const slot=[...effective].find(([,v])=>v===card.cardId)?.[0];return <tr key={card.cardId} className="border-t border-[var(--color-line)]"><td className="break-words py-3 pr-2 leading-6">{labelOf(card.cardId)}</td>{round.selfSubjectId?<td className="py-3">{slot?slotName(slot):t("unplaced")}</td>:null}{!published&&!cancelled&&round.selfIsController?<td className="py-3">{candidateButton(card.cardId)}</td>:null}</tr>;})}</tbody></table>
     <ul className="mt-3 space-y-2 md:hidden">{round.cards.map(card=>{const slot=[...effective].find(([,v])=>v===card.cardId)?.[0];return <li key={card.cardId} className="flex min-w-0 items-center gap-3 rounded border border-[var(--color-line)] p-3"><div className="min-w-0 flex-1"><p className="break-words text-sm font-semibold leading-6">{labelOf(card.cardId)}</p>{round.selfSubjectId?<p className="text-sm">{slot?slotName(slot):t("unplaced")}</p>:null}</div>{!published&&!cancelled&&round.selfIsController?candidateButton(card.cardId):null}</li>;})}</ul>
     {!published&&!cancelled&&round.selfIsController?<div className="mt-4 space-y-2 border-t border-[var(--color-line)] pt-4"><Button variant="primary" size="primary" block className="h-auto min-h-11 whitespace-normal" disabled={!editable||effective.size!==round.slots.length} onClick={()=>void submit()}>{t("submit")}</Button><p className="text-sm leading-6 text-[var(--color-muted)]">{effective.size===round.slots.length?t("editableHint"):t("remaining",{count:round.slots.length-effective.size})}</p></div>:null}
    </section>
   </div>
   <TeamRoundControl room={room} raw={snapshot.raw} onChanged={async()=>{await snapshot.refetch();await roomQuery.refetch();}}/>
   {published?<section className="uma-panel min-w-0 p-4" aria-labelledby="clue-scores"><h2 id="clue-scores" className="text-lg font-bold">{t("scores")}</h2>{round.scores.length?<><table className="mt-3 hidden w-full table-fixed text-left text-sm md:table"><caption className="sr-only">{t("scores")}</caption><thead><tr><th className="w-16 py-2">{t("rank")}</th><th className="py-2">{t("team")}</th><th className="w-20 py-2">{t("pointsHeading")}</th></tr></thead><tbody>{round.scores.map(s=><tr key={s.subjectId} className="border-t border-[var(--color-line)]"><td className="py-3">{s.rank??"—"}</td><td className="break-words py-3 pr-2">{s.label||t("team")}</td><td className="py-3 font-bold">{t("points",s)}</td></tr>)}</tbody></table><ol className="mt-3 space-y-2 md:hidden">{round.scores.map(s=><li key={s.subjectId} className="flex min-w-0 items-center gap-3 border-t border-[var(--color-line)] py-3"><span className="text-sm">{s.rank?t("rankValue",s):"—"}</span><strong className="min-w-0 flex-1 break-words text-sm">{s.label||t("team")}</strong><span className="shrink-0 font-bold">{t("points",s)}</span></li>)}</ol></>:<p role="status" className="mt-3 text-sm">{t("waitingScore")}</p>}</section>:null}
   {published&&round.answer?<section className="uma-panel min-w-0 p-4" aria-labelledby="reveal-heading"><h2 id="reveal-heading" className="text-lg font-bold">{t(round.answer.solutionCount>1?"alternative":"solution")}</h2><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{round.answer.solutionCount>1?t("alternativeHint",{count:round.answer.solutionCount}):t("solutionHint")}</p><ol className="mt-3 space-y-2">{round.answer.slots.map(slot=><li key={slot.slotIndex} className="flex min-w-0 flex-wrap gap-2 border-t border-[var(--color-line)] py-3 text-sm leading-6"><strong>{slotName(slot.slotIndex)}</strong><span className="break-words">{labelOf(slot.cardId)}</span>{round.assignment.has(slot.slotIndex)?<span className="break-words text-[var(--color-muted)]">{t("ourAnswer",{label:labelOf(round.assignment.get(slot.slotIndex)!)})}</span>:null}</li>)}</ol><details className="mt-3"><summary className="min-h-11 cursor-pointer content-center font-semibold">{t("allClues")}</summary><ul className="space-y-2">{round.answer.constraints.map(c=><li key={c.constraintId} className="break-words text-sm leading-6">{clueText(c,round.cards,i18n.language).text}</li>)}</ul></details></section>:null}
   <details className="uma-panel p-4"><summary className="min-h-11 cursor-pointer content-center font-semibold">{t("rules")}</summary><div className="mt-2 max-w-prose space-y-2 text-sm leading-6 text-[var(--color-muted)]"><p>{t("window",{seconds:round.answerSeconds})}</p><p>{t("scoreRule")}</p></div></details>
   {round.cards.some(c=>clueCardLabel(c,i18n.language).original)||clues.some(c=>clueText(c,round.cards,i18n.language).original)?<p className="text-xs leading-5 text-[var(--color-muted)]">{t("original")}</p>:null}
  </>:!snapshot.isLoading&&!roomQuery.isLoading&&!snapshot.isError&&!roomQuery.isError?<EmptyView title={t("emptyTitle")} detail={t("emptyDetail")}/>:null}
  <Button asChild variant="secondary" className="h-auto min-h-11 whitespace-normal"><Link to="/">{t("back")}</Link></Button>
 </div>;
}
