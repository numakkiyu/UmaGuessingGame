import { roomDisplayName } from "@/lib/room-display-name";
import { GameStage } from "@/components/uma/game-stage";
/** Personal votes are independent from shared team writing and host moderation. */
import "@/i18n/creative-online";
import {useMemo,useState} from "react";
import {Link,useParams} from "react-router";
import {CheckCircle2,Loader2} from "lucide-react";
import {useTranslation} from "@/i18n";
import {creativeOnlineCopy} from "@/i18n/creative-online";
import {localizeCreativePrompt} from "@/i18n/creative-prompts";
import {characterName} from "@/i18n/entities";
import {usePlayerText} from "@/i18n/player-text";
import {TeamRoundControl} from "@/features/rooms/team-round-control";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView,ErrorView,ForbiddenView,LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {validCreativeCommand,validCreativeReview,type CreativeCommand,type CreativeReviewCommand} from "@/lib/api/game-commands";
import {useApi} from "@/lib/api";
import {RejoinPrompt,isCredentialRefreshFailure} from "./rejoin-prompt";
import {ApiError,describeFailure,localizeFailure,type Failure} from "@/lib/api/client";
import {countCodePoints,normalizeCreativeRound,type CreativePrompt,type CreativeRound} from "@/lib/api/modes-g09-g12";
import {useRoom} from "@/lib/api/room-queries";
import {useCharacters} from "@/lib/api/queries";
import {readTeamMembers,readTeams} from "@/lib/api/teams";
import {isTerminalPhase,phaseLabel,revisionForSlot,useRoundSnapshot} from "@/lib/api/round-snapshot";
import {loadCreativeDraft,saveCreativeDraft,type CreativeDraft} from "@/lib/local/creative-online-draft";
import {useRealtime} from "@/lib/realtime/use-realtime";

function useDraft(scope:string|null,fallback:CreativeDraft){
 const restored=useMemo(()=>scope?loadCreativeDraft(scope):null,[scope]);
 const [state,setState]=useState<{scope:string|null;value:CreativeDraft}|null>(null);
 const value=state?.scope===scope?state.value:restored??fallback;
 function save(next:CreativeDraft){setState({scope,value:next});return !scope||saveCreativeDraft(scope,next);}
 return [value,save] as const;
}
function Prompt({prompt}:{prompt:CreativePrompt}){
 const {t,i18n}=useTranslation("creativeOnline"),characters=useCharacters().data?.items;
 const value=localizeCreativePrompt(prompt,i18n.language),known=characters?.find(c=>c.characterId===prompt.characterId);
 const name=known?characterName(known,i18n.language):prompt.characterName||t("unknownCharacter");
 return <><dl className="space-y-1.5 text-sm leading-6"><div><dt className="inline text-[var(--color-muted)]">{t("character")}：</dt><dd className="inline break-words font-bold">{name}</dd></div><div><dt className="inline text-[var(--color-muted)]">{t("scene")}：</dt><dd className="inline break-words">{value.scene}</dd></div><div><dt className="inline text-[var(--color-muted)]">{t("constraint")}：</dt><dd className="inline break-words">{value.constraint}</dd></div></dl>{value.original?<p className="mt-2 text-xs leading-5 text-[var(--color-muted)]">{t("original")}</p>:null}</>;
}
function Results({round,name}:{round:CreativeRound;name:(id:string,index:number)=>string}){
 const {t}=useTranslation("creativeOnline"),result=round.result;
 if(!result)return null;
 const works=result.works.map(w=>({...w,entry:round.ballot.find(e=>e.optionId===w.workId)}));
 function award(id:string){return result?.award?.workIds.includes(id)?t(result.award.shared?"sharedAward":"awardBadge"):null;}
 function score(id:string){const author=round.authors[id],score=round.scores.find(s=>s.subjectId===author);return score?t("points",{points:score.points}):"—";}
 function author(id:string,index:number){const author=round.authors[id];return author?name(author,index):t("unknownAuthor");}
 return <section className="uma-panel min-w-0 p-4 sm:p-6" aria-labelledby="creative-result"><h2 id="creative-result" className="text-xl font-bold">{t("results")}</h2><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("awardRule")}</p>{!result.award?<p className="mt-2 text-sm">{t("noAward")}</p>:null}
  <p className="mt-2 text-sm leading-6">{round.config.entertainmentBudget?t("bonusRule",{points:round.config.entertainmentBudget}):t("defaultScore")}</p>
  {!round.scores.length?<p className="mt-2 text-sm" role="status">— {t("waitingScore")}</p>:null}
  <table className="mt-4 hidden w-full table-fixed text-left text-sm md:table"><caption className="sr-only">{t("results")}</caption><thead><tr><th className="w-[44%] p-2">{t("work")}</th><th className="w-[20%] p-2">{t("author")}</th><th className="p-2">{t("teamVotes")}</th><th className="p-2">{t("audienceVotes")}</th><th className="p-2">{t("formal")}</th></tr></thead><tbody>{works.map((w,i)=><tr key={w.workId} className="border-t border-[var(--color-line)]"><th scope="row" className="break-words p-2 align-top font-normal"><h3 className="font-bold">{t("workNumber",{number:i+1})}{award(w.workId)?<span className="ml-2 rounded bg-[var(--color-gold)] px-1.5 py-0.5 text-xs text-[var(--color-ink)]">{award(w.workId)}</span>:null}</h3><p className="mt-2 whitespace-pre-wrap leading-7">{w.entry?.text}</p>{w.entry?.prompt?<details className="mt-2"><summary className="min-h-11 cursor-pointer content-center text-[var(--color-muted)]">{t("constraint")}</summary><Prompt prompt={w.entry.prompt}/></details>:null}</th><td className="break-words p-2 align-top">{author(w.workId,i)}</td><td className="p-2 align-top">{t("votes",{count:w.teamVotes})}</td><td className="p-2 align-top">{t("votes",{count:w.audienceVotes})}</td><td className="p-2 align-top font-bold">{score(w.workId)}</td></tr>)}</tbody></table>
  <ol className="mt-4 space-y-4 md:hidden">{works.map((w,i)=><li key={w.workId} className="min-w-0 border-t border-[var(--color-line)] pt-4"><h3 className="flex flex-wrap gap-2 font-bold">{t("workNumber",{number:i+1})}{award(w.workId)?<span className="rounded bg-[var(--color-gold)] px-2 py-0.5 text-xs text-[var(--color-ink)]">{award(w.workId)}</span>:null}</h3><p className="mt-2 whitespace-pre-wrap break-words leading-7">{w.entry?.text}</p><p className="mt-2 break-words text-sm text-[var(--color-muted)]">{t("author")}：{author(w.workId,i)}</p><dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-sm"><dt>{t("teamVotes")}</dt><dd>{t("votes",{count:w.teamVotes})}</dd><dt>{t("audienceVotes")}</dt><dd>{t("votes",{count:w.audienceVotes})}</dd><dt>{t("formal")}</dt><dd className="font-bold">{score(w.workId)}</dd></dl>{w.entry?.prompt?<details className="mt-2"><summary className="min-h-11 cursor-pointer content-center text-sm text-[var(--color-muted)]">{t("constraint")}</summary><Prompt prompt={w.entry.prompt}/></details>:null}</li>)}</ol>
 </section>;
}

export function CreativeRoundPage(){
 const {t}=useTranslation("creativeOnline"),{roomId}=useParams<{roomId:string}>(),{api,session}=useApi();
 const roomQuery=useRoom(roomId??null),room=roomQuery.data?.room,roundId=room?.currentRound?.roundId??null,snapshot=useRoundSnapshot(roundId);
 const round=useMemo(()=>snapshot.raw&&snapshot.meta?.roundId===roundId?normalizeCreativeRound(snapshot.raw):null,[snapshot.raw,snapshot.meta?.roundId,roundId]);
 const scope=session?.sessionId&&roundId?`${session.sessionId}:${roundId}:creative`:null;
 const command=useGameCommand(scope,validCreativeCommand,(body:CreativeCommand,key)=>api.createSubmission(roundId!,body,key));
 const reviewCommand=useGameCommand(scope?`${scope}:review`:null,validCreativeReview,(body:CreativeReviewCommand,key)=>api.createTransition(roundId!,body,undefined,key));
 const workScope=scope&&round?.self.subjectId?`${scope}:work:${round.self.subjectId}:${snapshot.controlGeneration}`:null;
 const [workDraft,saveWork]=useDraft(workScope,{text:round?.self.workText??"",selected:null,excluded:[]});
 const [voteDraft,saveVote]=useDraft(scope?`${scope}:vote`:null,{text:"",selected:null,excluded:[]});
 const [reviewDraft,saveReview]=useDraft(scope?`${scope}:review-draft`:null,{text:"",selected:null,excluded:[]});
 const [failure,setFailure]=useState<Failure|null>(null),[code,setCode]=useState(""),[rejected,setRejected]=useState(false),[refreshing,setRefreshing]=useState(false),[needsRejoin,setNeedsRejoin]=useState(false),[notice,setNotice]=usePlayerText("creativeOnline");
 const phase=snapshot.meta?.phase??null,cancelled=phase==="cancelled",reveal=phase==="revealed"||phase==="settled",terminal=isTerminalPhase(phase),writing=round?.subphase==="writing",reviewing=round?.subphase==="review",voting=round?.subphase==="voting";
 const collecting=phase==="collecting"&&!snapshot.meta?.paused&&!terminal,countdown=useCountdown(snapshot.meta?.deadlineAt,collecting);
 const host=!!room?.viewer?.capabilities.some(c=>c==="host"||c==="manage-room");
 const busy=command.busy||reviewCommand.busy||refreshing||!!command.pending||!!reviewCommand.pending||command.expired||reviewCommand.expired||snapshot.isError||roomQuery.isError;
 const editable=collecting&&snapshot.canSubmit&&!busy&&!countdown.expired;
 const canWrite=!!round?.self.subjectId&&writing&&editable,canVote=!!round?.vote?.canVote&&voting&&editable;
 const canReview=host&&reviewing&&phase==="locked"&&!snapshot.meta?.paused&&!busy;
 const normalized=workDraft.text.normalize("NFC").trim(),used=countCodePoints(normalized),limit=round?.maxCodePoints??80;
 const selectedId=round?.vote?.voteWorkId??voteDraft.selected,selected=round?.ballot.find(w=>w.optionId===selectedId&&w.optionId!==round.vote?.ownWorkId);
 const excluded=reviewDraft.excluded.filter(id=>round?.review.some(w=>w.optionId===id));
 const realtime=useRealtime({roomId:roomId??null,enabled:!!roomId,handlers:{onSnapshotNeeded:()=>{void snapshot.refetch();void roomQuery.refetch();}}});
 const info=failure?Object.hasOwn(creativeOnlineCopy["zh-Hans"].errors,code)?{...localizeFailure(failure),title:t("rejectedTitle"),detail:t(`errors.${code}`)}:rejected?{...failure,title:t("rejectedTitle"),detail:t("rejectedDetail")}:localizeFailure(failure):null;
 const members=readTeamMembers(room),teams=readTeams(room);
 function name(id:string,index:number){const team=teams.find(g=>g.teamId===id);if(team)return team.label;const member=members.find(m=>m.membershipId===id);return member?/^\d{6}$/.test(member.publicCode??"")?`${member.displayName} · ${member.publicCode}`:member.displayName:t("participantNumber",{number:index+1});}
 function edit(kind:"work"|"vote"|"review",next:CreativeDraft){setNotice("");if(!(kind==="work"?saveWork(next):kind==="vote"?saveVote(next):saveReview(next)))setNotice("saveFailed");}
 function resetFailure(){setFailure(null);setCode("");setRejected(false);setNotice("");}
 async function submit(kind:"work"|"vote",retry=false){
  if(!roundId||command.busy||refreshing)return;
  if(!retry){if(!round||kind==="work"&&!canWrite||kind==="vote"&&(!canVote||!selected))return;if(kind==="work"&&!used){setNotice("emptyWork");return;}if(kind==="work"&&used>limit){setNotice("tooLong",{limit,used});return;}}
  const body:CreativeCommand|undefined=retry?undefined:{phaseVersion:snapshot.meta!.phaseVersion,submissionRevision:revisionForSlot(snapshot,kind),controlGeneration:kind==="vote"?0:snapshot.controlGeneration,modeAction:kind==="work"?{kind:"creative.work",text:normalized}:{kind:"creative.vote",optionId:selected!.optionId}};
  setRefreshing(true);resetFailure();
  try{const result=await command.run(body);if(!result)return;if(!result.ok){setFailure(describeFailure(result.error));setCode(result.error instanceof ApiError?result.error.code:"");if(isCredentialRefreshFailure(result.error))setNeedsRejoin(true);if(!result.uncertain)await snapshot.refetch();return;}if(result.value.data?.outcome!=="accepted"){setRejected(true);setFailure({kind:"error",title:"",detail:"",diagnosticId:result.value.requestId,retryable:true});return;}setNotice("accepted");await snapshot.refreshAfterSubmit();}finally{setRefreshing(false);}
 }
 async function openVoting(retry=false){
  if(!roundId||reviewCommand.busy||refreshing||!retry&&!canReview)return;
  setRefreshing(true);resetFailure();
  try{const result=await reviewCommand.run(retry?undefined:{target:"creative-voting",expectedPhaseVersion:snapshot.meta!.phaseVersion,excludedOptionIds:[...excluded]});if(!result)return;if(!result.ok){setFailure(describeFailure(result.error));setCode(result.error instanceof ApiError?result.error.code:"");if(isCredentialRefreshFailure(result.error))setNeedsRejoin(true);if(!result.uncertain)await snapshot.refetch();return;}setNotice("accepted");await snapshot.refetch();}finally{setRefreshing(false);}
 }
 const action=(id:string,index:number)=><Button variant="secondary" className="h-auto min-h-11 whitespace-normal" disabled={!canVote||id===round?.vote?.ownWorkId} aria-pressed={selectedId===id} aria-label={`${t("workNumber",{number:index+1})}：${t(selectedId===id?"selected":"choose")}`} onClick={()=>edit("vote",{...voteDraft,selected:id})}>{selectedId===id?<CheckCircle2 aria-hidden="true" className="size-4 shrink-0"/>:null}{t(id===round?.vote?.ownWorkId?"ownWork":selectedId===id?"selected":"choose")}</Button>;
 if(!roomId)return <ErrorView failure={{kind:"error",title:t("missingTitle"),detail:t("missingDetail"),diagnosticId:null,retryable:false}}/>;
 return <GameStage mode="creative" className="uma-stack">
  <ConnectionBar status={realtime.status} detail={roomDisplayName(room, t("room"))}/>
  {needsRejoin||isCredentialRefreshFailure(snapshot.error)?<RejoinPrompt roomId={roomId}/>:null}
  {roomQuery.isLoading?<LoadingBlock label={t("loadingRoom")} rows={3}/>:null}
  {roomQuery.isError?(describeFailure(roomQuery.error).kind==="forbidden"?<ForbiddenView failure={describeFailure(roomQuery.error)} onRetry={()=>void roomQuery.refetch()}/>:<ErrorView failure={describeFailure(roomQuery.error)} onRetry={()=>void roomQuery.refetch()}/>):null}
  {snapshot.isLoading?<LoadingBlock label={t("loading")} rows={4}/>:null}
  {snapshot.isError?<ErrorView failure={describeFailure(snapshot.error)} onRetry={()=>void snapshot.refetch()}/>:null}
  {command.pending||reviewCommand.pending?<Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p>{command.pending?<Button className="mt-3 h-auto min-h-11 whitespace-normal" disabled={command.busy||reviewCommand.busy||refreshing} onClick={()=>void submit("work",true)}>{t("confirmAction")}</Button>:null}{reviewCommand.pending?<Button className="mt-3 h-auto min-h-11 whitespace-normal" disabled={command.busy||reviewCommand.busy||refreshing} onClick={()=>void openVoting(true)}>{t("reviewConfirm")}</Button>:null}</Alert>:null}
  {command.busy||reviewCommand.busy?<p role="status" className="flex items-center gap-2 text-sm"><Loader2 className="size-4 motion-safe:animate-spin" aria-hidden="true"/>{t("busy")}</p>:null}
  {command.expired||reviewCommand.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button className="mt-3 h-auto min-h-11 whitespace-normal" disabled={refreshing} onClick={()=>void(async()=>{setRefreshing(true);try{if(await snapshot.refetch()){command.acknowledgeExpired();reviewCommand.acknowledgeExpired();}}finally{setRefreshing(false);}})()}>{t("acknowledge")}</Button></Alert>:null}
  {command.storageLimited||reviewCommand.storageLimited?<Alert tone="warning" title={t("notice")}>{t("storage")}</Alert>:null}
  {info?<Alert tone="danger" title={info.title}>{info.detail}</Alert>:null}{notice?<p role="status" className="text-sm leading-6">{notice}</p>:null}
  {round?<>
   <header className="border-b border-[var(--color-line)] pb-4"><h1 className="u-fluid-task text-[var(--color-ink)]">{t("title")}</h1><p className="mt-2 max-w-prose text-sm leading-6 text-[var(--color-muted)]">{t("intro")}</p><p className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm text-[var(--color-muted)]"><span>{phaseLabel(phase)}</span>{round.subphase?<span>{t(writing?"writing":reviewing?"review":voting?"voting":"closed")}</span>:null}{collecting&&snapshot.meta?.deadlineAt&&countdown.text?<span className="font-bold text-[var(--color-ink)]">{countdown.text}</span>:null}</p></header>
   {cancelled?<Alert tone="info" title={t("cancelled")}>{t("cancelledDetail")}</Alert>:null}
   {!round.subphase&&!cancelled?<Alert tone="warning" title={t("notice")}>{t("legacy")}</Alert>:null}
   {!cancelled&&!reveal&&writing?<div className="grid min-w-0 items-start gap-4 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
    <section className="uma-panel min-w-0 p-4 sm:p-6" aria-labelledby="creative-work"><h2 id="creative-work" className="mb-3 text-xl font-bold">{round.prompt?t("work"):t("writing")}</h2>{round.prompt?<Prompt prompt={round.prompt}/>:<p className="text-sm leading-6">{t("observer")}</p>}
     {round.self.subjectId?<div className="mt-5 border-t border-[var(--color-line)] pt-4"><label htmlFor="creative-text" className="mb-2 block font-semibold">{t("workLabel")}</label><textarea id="creative-text" rows={5} disabled={!canWrite} value={workDraft.text} onChange={e=>edit("work",{...workDraft,text:e.target.value})} aria-describedby="creative-count creative-count-hint" className="w-full min-w-0 resize-y rounded-[var(--radius-control)] border border-[var(--color-line-strong)] bg-[var(--color-panel)] p-3 text-base leading-7 text-[var(--color-ink)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)] disabled:opacity-100"/><p id="creative-count" className="mt-1 text-right text-sm" aria-live="polite">{t("count",{used,limit})}</p><p id="creative-count-hint" className="mt-1 text-xs leading-5 text-[var(--color-muted)]">{t("countHint")}</p><p className="mt-3 text-sm leading-6" role="status">{snapshot.meta?.paused?t("paused"):phase!=="collecting"?t("beforeStart"):countdown.expired?t("deadline"):!snapshot.canSubmit?t("nonController"):round.self.submitted?t("saved"):null}</p><div className="mt-3 flex flex-wrap gap-2"><Button variant="primary" size="primary" className="h-auto min-h-11 flex-1 whitespace-normal" disabled={!canWrite||!used||used>limit} onClick={()=>void submit("work")}>{t(round.self.submitted?"revise":"publish")}</Button><Button variant="secondary" className="h-auto min-h-11 whitespace-normal" disabled={!canWrite||!workDraft.text} onClick={()=>edit("work",{...workDraft,text:""})}>{t("clear")}</Button></div></div>:null}
    </section>
    <aside className="uma-panel min-w-0 p-4" aria-label={t("progress")}><h2 className="font-bold">{t("progress")}</h2><ul className="mt-3 space-y-3">{round.progress.map((p,i)=><li key={p.subjectId} className="flex flex-wrap justify-between gap-2 border-b border-[var(--color-line)] pb-2 text-sm"><span className="min-w-0 break-words font-medium">{name(p.subjectId,i)}</span><span className="text-[var(--color-muted)]">{t(p.submitted?"submitted":"notSubmitted")}</span></li>)}</ul></aside>
   </div>:null}
   {!cancelled&&!reveal&&reviewing?<section className="uma-panel min-w-0 p-4 sm:p-6" aria-labelledby="creative-review"><h2 id="creative-review" className="text-xl font-bold">{t("review")}</h2><p className="mt-2 max-w-prose text-sm leading-6">{t(host?"reviewHint":"reviewPrivate")}</p>{host?<><ul className="mt-4 space-y-4">{round.review.map((w,i)=><li key={w.optionId} className="min-w-0 border-t border-[var(--color-line)] pt-4"><h3 className="font-bold">{t("workNumber",{number:i+1})}</h3>{w.prompt?<div className="mt-2"><Prompt prompt={w.prompt}/></div>:null}<p className="mt-3 whitespace-pre-wrap break-words leading-7">{w.text}</p><label className="mt-3 flex min-h-11 cursor-pointer items-center gap-3 text-sm"><input type="checkbox" className="size-5 shrink-0" checked={excluded.includes(w.optionId)} disabled={!canReview} onChange={e=>edit("review",{...reviewDraft,excluded:e.target.checked?[...excluded,w.optionId]:excluded.filter(id=>id!==w.optionId)})}/>{t("skip")} · {t("workNumber",{number:i+1})}</label></li>)}</ul>{!round.review.length||excluded.length===round.review.length?<p className="mt-3 text-sm">{t(round.review.length?"zeroSelected":"zeroReview")}</p>:null}<Button variant="primary" className="mt-4 h-auto min-h-11 whitespace-normal" disabled={!canReview} onClick={()=>void openVoting()}>{t("openVoting")}</Button></>:null}</section>:null}
   {!cancelled&&!reveal&&(voting||round.subphase==="closed")?<section className="uma-panel min-w-0 p-4 sm:p-6" aria-labelledby="creative-ballot"><h2 id="creative-ballot" className="text-xl font-bold">{t("ballot")}</h2><p className="mt-2 text-sm leading-6">{t("voteHint")}</p>{round.vote?<p className="mt-1 text-sm text-[var(--color-muted)]">{t(round.vote.voteKind==="audience"?"audiencePool":"teamPool")}</p>:null}{round.self.hostRejected?<p className="mt-2 text-sm">{t("excluded")}</p>:null}
    {!round.ballot.length?<p className="mt-3 text-sm">{t("noBallot")}</p>:<><table className="mt-4 hidden w-full table-fixed text-left md:table"><caption className="sr-only">{t("ballot")}</caption><thead><tr><th className="p-2">{t("work")}</th><th className="w-48 p-2">{t("choose")}</th></tr></thead><tbody>{round.ballot.map((w,i)=><tr key={w.optionId} className="border-t border-[var(--color-line)]"><td className="min-w-0 p-3"><h3 className="font-bold">{t("workNumber",{number:i+1})}</h3><p className="my-3 whitespace-pre-wrap break-words leading-7">{w.text}</p>{w.prompt?<Prompt prompt={w.prompt}/>:null}</td><td className="p-3 align-top">{action(w.optionId,i)}</td></tr>)}</tbody></table><ol className="mt-4 space-y-4 md:hidden">{round.ballot.map((w,i)=><li key={w.optionId} className="min-w-0 border-t border-[var(--color-line)] pt-4"><h3 className="font-bold">{t("workNumber",{number:i+1})}</h3><p className="my-3 whitespace-pre-wrap break-words leading-7">{w.text}</p>{w.prompt?<Prompt prompt={w.prompt}/>:null}<div className="mt-3">{action(w.optionId,i)}</div></li>)}</ol></>}
    {round.vote?<div className="mt-4 border-t border-[var(--color-line)] pt-4"><p className="mb-3 text-sm leading-6" role="status">{snapshot.meta?.paused?t("paused"):round.vote.voted?t("voted"):countdown.expired?t("deadline"):!round.vote.canVote?t("cannotVote"):null}</p>{!round.vote.voted?<Button variant="primary" size="primary" className="h-auto min-h-11 whitespace-normal" disabled={!canVote||!selected} onClick={()=>void submit("vote")}>{t("confirmVote")}</Button>:null}</div>:null}
   </section>:null}
   {reveal&&round.subphase==="closed"?<Results round={round} name={name}/>:null}
   <details className="uma-panel min-w-0 p-4"><summary className="min-h-11 cursor-pointer content-center font-semibold">{t("rules")}</summary><div className="mt-2 max-w-prose space-y-2 text-sm leading-6 text-[var(--color-muted)]"><p>{t("timingRule",{write:round.config.writeSeconds,vote:round.config.voteSeconds,limit})}</p><p>{t("voteHint")}</p><p>{t("awardRule")}</p><p>{round.config.entertainmentBudget?t("bonusRule",{points:round.config.entertainmentBudget}):t("defaultScore")}</p><p>{t("privacyRule")}</p></div></details>
  </>:!snapshot.isLoading&&!snapshot.isError&&!roomQuery.isLoading&&!roomQuery.isError?<EmptyView title={t("emptyTitle")} detail={t("emptyDetail")}/>:null}
  <TeamRoundControl room={room} raw={snapshot.raw} onChanged={snapshot.refetch}/>
  <Button asChild variant="secondary" className="h-auto min-h-11 whitespace-normal"><Link to={`/rooms/${roomId}`}>{t("back")}</Link></Button>
 </GameStage>;
}
