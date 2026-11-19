import { roomDisplayName } from "@/lib/room-display-name";
import { GameStage } from "@/components/uma/game-stage";
import "@/i18n/pedigree-online";
import {useMemo, useState} from "react";
import {Link, useParams} from "react-router";
import {Loader2, Save, X} from "lucide-react";
import {useTranslation} from "@/i18n";
import {characterName} from "@/i18n/entities";
import {pedigreeSlot} from "@/i18n/pedigree";
import {pedigreeOnlineCopy} from "@/i18n/pedigree-online";
import {usePlayerText} from "@/i18n/player-text";
import type {PedigreeSlotId} from "@/lib/domain/pedigree-practice";
import {TeamRoundControl} from "@/features/rooms/team-round-control";
import {AvatarImage} from "@/components/uma/avatar-image";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView, ErrorView, ForbiddenView, LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {useApi} from "@/lib/api";
import {ApiError, describeFailure, localizeFailure, type Failure} from "@/lib/api/client";
import {useCharacters} from "@/lib/api/queries";
import {useRoom} from "@/lib/api/room-queries";
import {readTeamMembers, readTeams} from "@/lib/api/teams";
import {normalizePedigreeRound, onlinePedigreeDraftValid, type OnlinePedigreeAssignment, type PedigreeRound, type PedigreeSlot} from "@/lib/api/pedigree";
import {validPedigreeAssignment, type PedigreeAssignmentCommand} from "@/lib/api/game-commands";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {isTerminalPhase, phaseLabel, resultsOf, useRoundSnapshot} from "@/lib/api/round-snapshot";
import {useRealtime} from "@/lib/realtime/use-realtime";
import {cn} from "@/lib/utils";
import {RejoinPrompt, isCredentialRefreshFailure} from "./rejoin-prompt";

const PREFIX = "umaguess:pedigree-draft:";
const SLOT_IDS = ["sire", "dam", "sireSire", "sireDam", "damSire", "damDam"];
export function loadPedigreeDraft(scope: string): OnlinePedigreeAssignment[] | null {
 try {
  const raw = localStorage.getItem(PREFIX + scope); if (!raw || raw.length > 8192) return null;
  const value: unknown = JSON.parse(raw); if (!value || typeof value !== "object") return null;
  const {assignments, savedAt} = value as {assignments?:unknown;savedAt?:unknown};
  if (!Array.isArray(assignments) || assignments.length > 6 || typeof savedAt !== "number" || !Number.isSafeInteger(savedAt) || savedAt > Date.now() + 60000 || savedAt <= Date.now() - 86400000) return null;
  const slots = new Set(), horses = new Set();
  for (const a of assignments) { if (!a || typeof a !== "object" || typeof a.slotId !== "string" || !SLOT_IDS.includes(a.slotId) || typeof a.horseId !== "string" || !a.horseId || a.horseId.length > 128 || slots.has(a.slotId) || horses.has(a.horseId)) return null; slots.add(a.slotId); horses.add(a.horseId); }
  return assignments as OnlinePedigreeAssignment[];
 } catch { return null; }
}
export function savePedigreeDraft(scope:string, assignments: readonly OnlinePedigreeAssignment[]):boolean {
 try { localStorage.setItem(PREFIX + scope, JSON.stringify({assignments, savedAt:Date.now()})); return true; } catch { return false; }
}
function clearDraft(scope:string) { try { localStorage.removeItem(PREFIX + scope); } catch { /* Do not claim persistence. */ } }
export function pedigreeSourceURL(nkbId:string | null | undefined):string | null { return nkbId && /^\d{10}$/.test(nkbId) ? `https://db.netkeiba.com/horse/ped/${nkbId}/` : null; }
function slotText(slot:PedigreeSlot|undefined, language?:string):{label:string;means:string} | null { return slot && SLOT_IDS.includes(slot.slotId) ? pedigreeSlot(slot.slotId as PedigreeSlotId, language) : null; }
function sorted(assignments:readonly OnlinePedigreeAssignment[]) { return [...assignments].sort((a,b)=>SLOT_IDS.indexOf(a.slotId)-SLOT_IDS.indexOf(b.slotId)); }
function same(a:readonly OnlinePedigreeAssignment[], b:readonly OnlinePedigreeAssignment[]) { return JSON.stringify(sorted(a)) === JSON.stringify(sorted(b)); }
export function pedigreeScores(items: readonly unknown[]):{subjectId:string;scoreUnits:number;rank:number|null}[] {
 return items.flatMap(item=>{if(!item||typeof item!=="object")return [];const r=item as Record<string,unknown>;return typeof r.subjectId==="string"&&typeof r.scoreUnits==="number"&&Number.isSafeInteger(r.scoreUnits)&&r.scoreUnits>=0?[{subjectId:r.subjectId,scoreUnits:r.scoreUnits,rank:typeof r.rank==="number"&&Number.isSafeInteger(r.rank)&&r.rank>0?r.rank:null}]:[];});
}
export function PedigreeRoundPage() {
 const {t, i18n} = useTranslation("pedigreeOnline"), {roomId} = useParams<{roomId:string}>(), {api, session} = useApi();
 const roomQuery = useRoom(roomId ?? null), roundId = roomQuery.data?.room.currentRound?.roundId ?? null, snapshot = useRoundSnapshot(roundId), characters = useCharacters().data?.items;
 const round = useMemo(()=>snapshot.raw && snapshot.meta?.roundId === roundId ? normalizePedigreeRound(snapshot.raw) : null,[snapshot.raw,snapshot.meta?.roundId,roundId]);
 const scope = session && roundId ? `${session.sessionId}:${roundId}:pedigree` : null, draftScope = scope && round?.subjectId ? `${scope}:${round.subjectId}` : null;
 const command = useGameCommand(scope, validPedigreeAssignment, (body,key)=>api.createSubmission(roundId!,body,key));
 const [draft,setDraft] = useState<OnlinePedigreeAssignment[]>([]), [initialized,setInitialized] = useState<string|null>(null), [active,setActive] = useState<string|null>(null), [query,setQuery] = useState("");
 const [failure,setFailure] = useState<Failure|null>(null), [errorCode,setErrorCode] = useState(""), [rejected,setRejected] = useState(false), [refreshing,setRefreshing] = useState(false), [needsRejoin,setNeedsRejoin] = useState(false), [notice,setNotice] = usePlayerText("pedigreeOnline");
 if (round && draftScope && initialized !== draftScope) {
  const stored = loadPedigreeDraft(draftScope), confirmed = [...round.assignments].map(([slotId,horseId])=>({slotId,horseId}));
  setInitialized(draftScope);setDraft(onlinePedigreeDraftValid(stored,round)?stored:onlinePedigreeDraftValid(confirmed,round)?confirmed:[]);setActive(round.slots[0]?.slotId ?? null);setQuery("");
 }
 const realtime = useRealtime({roomId:roomId??null,enabled:!!roomId,handlers:{onSnapshotNeeded:()=>{void snapshot.refetch();void roomQuery.refetch();}}});
 const phase = snapshot.meta?.phase ?? null, ended = isTerminalPhase(phase), collecting = phase === "collecting" && !snapshot.meta?.paused, cancelled = phase === "cancelled";
 const countdown = useCountdown(snapshot.meta?.deadlineAt, collecting), busy = command.busy || refreshing || !!command.pending || command.expired || snapshot.isError;
 const editable = !!round?.hasSelf && collecting && snapshot.canSubmit && !busy && !countdown.expired;
 const confirmed = round ? [...round.assignments].map(([slotId,horseId])=>({slotId,horseId})) : [], changed = !same(draft,confirmed), selected = round?.slots.find(s=>s.slotId===active), bySlot = new Map(draft.map(a=>[a.slotId,a.horseId]));
 const knownCharacter = characters?.find(c=>c.characterId===round?.subject?.characterId);
 const subjectName = knownCharacter ? characterName(knownCharacter) : round?.subject?.characterName || t("unknownCard");
 const activeText = selected ? slotText(selected,i18n.resolvedLanguage) : null, visible = round?.candidates.filter(c=>c.name.normalize("NFKC").toLowerCase().includes(query.normalize("NFKC").trim().toLowerCase())) ?? [];
 const knownError = Object.hasOwn(pedigreeOnlineCopy["zh-Hans"].errors,errorCode);
 const info = failure ? knownError ? {...failure,title:t("rejectedTitle"),detail:t(`errors.${errorCode}`)} : rejected ? {...failure,title:t("rejectedTitle"),detail:t("rejectedDetail")} : localizeFailure(failure) : null;
 const source = pedigreeSourceURL(round?.subject?.nkbId);
 function edit(next:OnlinePedigreeAssignment[]) { if (!editable || !round || !onlinePedigreeDraftValid(next,round)) return;setDraft(sorted(next));setNotice("");if(draftScope&&!savePedigreeDraft(draftScope,sorted(next)))setNotice("saveFailed"); }
 function assign(horseId:string) { if(!editable||!selected)return;if(draft.some(a=>a.horseId===horseId)){setNotice("usedCandidate");return;}const next=[...draft.filter(a=>a.slotId!==active),{slotId:active!,horseId}];edit(next);setActive(round?.slots.find(s=>!next.some(a=>a.slotId===s.slotId))?.slotId??active); }
 function labelFor(id:string,index:number) { const team=readTeams(roomQuery.data?.room).find(t=>t.teamId===id);if(team)return team.label;const member=readTeamMembers(roomQuery.data?.room).find(m=>m.membershipId===id);return member ? /^\d{6}$/.test(member.publicCode??"") ? `${member.displayName} · ${member.publicCode}` : member.displayName : t("progressLabel",{number:index+1}); }
 async function submit(retry=false) {
  if(!roundId||command.busy||refreshing)return;if(!retry&&(!round||!editable||!draft.length||!onlinePedigreeDraftValid(draft,round)))return;
  const body:PedigreeAssignmentCommand|undefined=retry?undefined:{phaseVersion:snapshot.meta!.phaseVersion,submissionRevision:snapshot.submissionRevision,controlGeneration:snapshot.controlGeneration,modeAction:{kind:"pedigree.assignment",assignments:sorted(draft)}};
  setRefreshing(true);setFailure(null);setErrorCode("");setRejected(false);setNotice("");
  try { const result=await command.run(body);if(!result)return;
   if(!result.ok){setFailure(describeFailure(result.error));setErrorCode(result.error instanceof ApiError?result.error.code:"");if(!result.uncertain&&result.error instanceof ApiError&&result.error.isConflict)await snapshot.refetch();if(isCredentialRefreshFailure(result.error))setNeedsRejoin(true);return;}
   if(result.value.data?.outcome!=="accepted"){setRejected(true);setFailure({kind:"error",title:"",detail:"",diagnosticId:result.value.requestId,retryable:true});return;}
   if(draftScope)clearDraft(draftScope);setNotice("accepted");await snapshot.refreshAfterSubmit();
  } finally { setRefreshing(false); }
 }
 function slotCard(slot:PedigreeSlot) {
  const text=slotText(slot,i18n.resolvedLanguage),horse=round?.candidates.find(c=>c.horseId===bySlot.get(slot.slotId));
  return <div key={slot.slotId} className="min-w-0" data-pedigree-slot={slot.slotId}><button type="button" className={cn("min-h-24 w-full rounded-lg border-2 p-3 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",active===slot.slotId?"border-[var(--color-blue-deep)] bg-[var(--color-panel-soft)]":"border-[var(--color-line)] bg-[var(--color-panel)]")} disabled={!editable||!text} aria-pressed={active===slot.slotId} aria-label={t("fillSlot",{slot:text?.label??t("unknownSlot"),name:horse?.name||t("unfilled")})} onClick={()=>setActive(slot.slotId)}><span className="flex flex-wrap items-baseline justify-between gap-2"><span className="font-bold">{text?.label??t("unknownSlot")}</span>{active===slot.slotId?<span className="text-xs">{t("activeSlot")}</span>:null}</span><span className="mt-1 block text-xs leading-5 text-[var(--color-muted)]">{text?.means??t("unknownRelation")}</span><span className="mt-2 block break-words text-sm font-semibold">{horse?.name||t("unfilled")}</span></button>{horse?<Button variant="ghost" size="sm" className="h-auto min-h-11 whitespace-normal" disabled={!editable} aria-label={t("clearSlot",{slot:text?.label??t("unknownSlot")})} onClick={()=>{edit(draft.filter(a=>a.slotId!==slot.slotId));setActive(slot.slotId);}}><X className="size-3" aria-hidden="true"/>{t("clear")}</Button>:null}</div>;
 }
 if(!roomId)return <ErrorView failure={{kind:"error",title:t("missingTitle"),detail:t("missingDetail"),diagnosticId:null,retryable:false}}/>;
 if(roomQuery.isLoading)return <LoadingBlock label={t("loadingRoom")} rows={3}/>;
 if(roomQuery.isError){const error=describeFailure(roomQuery.error);return error.kind==="forbidden"?<ForbiddenView failure={error} onRetry={()=>void roomQuery.refetch()}/>:<ErrorView failure={error} onRetry={()=>void roomQuery.refetch()}/>;}
 return <GameStage mode="pedigree" className="uma-stack">
  <TeamRoundControl room={roomQuery.data?.room} raw={snapshot.raw} onChanged={snapshot.refetch}/><ConnectionBar status={realtime.status} detail={roomDisplayName(roomQuery.data?.room, t("room"))}/>
  <section className="uma-panel p-4"><h1 className="u-fluid-task">{t("title")}</h1><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{phaseLabel(phase)}{round?` · ${t(round.cooperative?"cooperativeMode":"soloMode")}`:""}{snapshot.meta?.paused?` · ${t("paused")}`:""}</p>{collecting&&snapshot.meta?.deadlineAt?<p className={`uma-code mt-2 text-xl font-bold ${countdown.expired?"text-[var(--color-danger)]":""}`}><span className="mr-2 text-sm">{t("time")}</span>{countdown.text}</p>:null}</section>
  <section className="uma-card p-4" aria-label={t("prototypeLabel")}><h2 className="text-base font-bold">{t("prototypeLabel")}</h2><p className="mt-2 text-sm leading-6">{t("disclaimer")}</p></section>
  {command.pending&&!command.busy?<Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button variant="secondary" className="h-auto min-h-12 whitespace-normal" disabled={refreshing} onClick={()=>void submit(true)}>{t("confirmAction")}</Button></Alert>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" onClick={()=>void snapshot.refetch().then(ok=>{if(ok)command.acknowledgeExpired();})}>{t("latest")}</Button></Alert>:null}{command.storageLimited?<p className="text-sm">{t("limited")}</p>:null}{needsRejoin?<RejoinPrompt roomId={roomId}/>:null}
  {info?<Alert tone={info.kind==="timeout"?"warning":"danger"} title={info.title}><p>{info.detail}</p>{info.diagnosticId?<p>{t("problemId")}：<span className="uma-code">{info.diagnosticId}</span></p>:null}<Button variant="secondary" onClick={()=>void snapshot.refetch()}>{t("refresh")}</Button></Alert>:null}{notice?<Alert tone="info" title={t("notice")}>{notice}</Alert>:null}
  {!roundId?<EmptyView title={t("waitingTitle")} detail={t("waitingDetail")}/>:null}{roundId&&snapshot.isLoading&&!round?<LoadingBlock label={t("loadingRound")} rows={3}/>:null}{snapshot.isError?<ErrorView failure={describeFailure(snapshot.error)} onRetry={()=>void snapshot.refetch()}/>:null}
  {round&&!ended?<><section className="uma-panel min-w-0 p-4"><div className="flex items-center gap-3"><AvatarImage characterId={round.subject?.characterId??undefined} src={round.subject?.characterId?`/assets/avatars/${round.subject.characterId}.webp`:undefined} alt="" size="lg"/><div className="min-w-0"><h2 className="break-words text-lg font-bold">{subjectName}</h2><p className="mt-1 break-words text-sm">{t("prototypeName",{name:round.subject?.prototypeName||t("unknownHorse")})}</p></div></div></section>
   <div className="grid min-w-0 items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]"><section className="uma-panel min-w-0 p-3 sm:p-4" aria-labelledby="pedigree-tree"><h2 id="pedigree-tree" className="text-lg font-bold">{t("arrange")}</h2><div className="mt-3 grid min-w-0 gap-4 sm:grid-cols-2">{(["sire","dam"] as const).map(branch=><section key={branch} className="min-w-0 border-t-2 border-[var(--color-line)] pt-3" aria-label={t(branch==="sire"?"sireBranch":"damBranch")}><h3 className="mb-2 font-bold">{t(branch==="sire"?"sireBranch":"damBranch")}</h3>{round.slots.filter(s=>s.slotId===branch).map(slotCard)}<div className="ml-2 mt-3 space-y-3 border-l-2 border-[var(--color-line)] pl-3">{round.slots.filter(s=>s.slotId.startsWith(branch)&&s.slotId!==branch).map(slotCard)}</div></section>)}</div></section>
   <section className="uma-panel min-w-0 space-y-3 p-3 sm:p-4" aria-labelledby="pedigree-candidates"><h2 id="pedigree-candidates" className="text-lg font-bold">{activeText?t("pickFor",{slot:activeText.label}):t("chooseFirst")}</h2>{activeText?<p className="text-sm leading-6">{t("pickHint",{means:activeText.means})}</p>:null}<label htmlFor="pedigree-search" className="block text-sm font-semibold">{t("search")}</label><Input type="search" id="pedigree-search" value={query} onChange={e=>setQuery(e.target.value)} disabled={!editable} placeholder={t("search")}/>
    <div className="hidden md:block"><table className="w-full table-fixed text-left text-sm"><caption className="sr-only">{t("candidate")}</caption><thead><tr><th scope="col" className="py-2">{t("candidate")}</th><th scope="col" className="w-24 py-2">{t("assignment")}</th></tr></thead><tbody>{visible.map(c=>{const used=draft.find(a=>a.horseId===c.horseId),label=used?slotText(round.slots.find(s=>s.slotId===used.slotId)!,i18n.resolvedLanguage)?.label??t("unknownSlot"):"";return <tr key={c.horseId} className="border-t border-[var(--color-line)]"><th scope="row" className="break-words py-3 pr-2 font-medium">{c.name||t("unknownHorse")}</th><td className="py-2"><Button variant="secondary" className="h-auto min-h-11 w-full whitespace-normal" disabled={!editable||!activeText||!!used} aria-label={t("selectName",{name:c.name||t("unknownHorse")})} onClick={()=>assign(c.horseId)}>{used?t("used",{slot:label}):t("select")}</Button></td></tr>;})}</tbody></table></div>
    <ul className="space-y-2 md:hidden" aria-label={t("candidate")}>{visible.map(c=>{const used=draft.some(a=>a.horseId===c.horseId);return <li key={c.horseId}><button type="button" className="flex min-h-12 w-full items-center gap-2 rounded-lg border border-[var(--color-line)] p-3 text-left text-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]" disabled={!editable||!activeText||used} aria-label={t("selectName",{name:c.name||t("unknownHorse")})} onClick={()=>assign(c.horseId)}><span className="min-w-0 flex-1 break-words font-semibold">{c.name||t("unknownHorse")}</span><span className="text-xs">{used?t("usedShort"):t("select")}</span></button></li>;})}</ul>{!visible.length?<p className="text-sm">{t("noMatch")}</p>:null}
   </section></div>
   <section className="uma-panel space-y-3 p-4" aria-labelledby="pedigree-save"><h2 id="pedigree-save" className="text-lg font-bold">{t("submitTitle")}</h2><p role="status" className="text-sm leading-6">{t("draftCount",{filled:draft.length,total:round.totalSlots})} {t(round.submissions>0?changed?"changedCount":"savedCount":"unsaved",{saved:round.assignedCount})}</p>{round.submissions>0?<p className="text-sm">{t("revisions",{count:round.submissions})}</p>:null}<div className="flex flex-col gap-2 sm:flex-row"><Button variant="secondary" className="h-auto min-h-11 whitespace-normal" disabled={!editable} onClick={()=>{if(draftScope)setNotice(savePedigreeDraft(draftScope,draft)?"savedDraft":"saveFailed");}}>{t("saveDraft")}</Button><Button variant="primary" size="primary" className="h-auto min-h-12 whitespace-normal" disabled={!editable||!draft.length||!onlinePedigreeDraftValid(draft,round)} onClick={()=>void submit()}>{command.busy||refreshing?<Loader2 className="size-4 motion-safe:animate-spin" aria-hidden="true"/>:<Save className="size-4" aria-hidden="true"/>}{t(command.busy||refreshing?"submitting":"save")}</Button></div><p className="text-xs leading-5">{!draft.length?t("empty"):t("saveHint")}</p>{!collecting?<p className="text-sm">{t(snapshot.meta?.paused?"pausedHint":phase==="locked"?"lockedHint":"beforeStartHint")}</p>:null}</section>
  </>:null}
  {round&&ended?<PedigreeResultPanel round={round} cancelled={cancelled} scores={pedigreeScores(resultsOf(snapshot.raw))}/>:null}
  {round?.cooperative&&!cancelled?<section className="uma-card p-4" aria-label={t("progress")}><h2 className="font-bold">{t("progress")}</h2><ul className="mt-2 space-y-2 text-sm">{round.progress.map((p,i)=><li key={p.subjectId}><p className="break-words font-semibold">{labelFor(p.subjectId,i)}</p><p>{t(p.submissions>0?"progressCount":"noSubmission",{count:p.assignedSlots,total:round.totalSlots})}</p></li>)}</ul></section>:null}
  {round&&!cancelled?<details className="uma-card p-4"><summary className="cursor-pointer font-bold">{t("reference")}</summary><p className="mt-3 text-sm leading-6">{t("referenceHint")}</p><dl className="mt-3 space-y-2 text-sm">{SLOT_IDS.map(id=>{const text=pedigreeSlot(id as PedigreeSlotId,i18n.resolvedLanguage);return <div key={id}><dt className="font-bold">{text.label}</dt><dd>{text.means}</dd></div>;})}</dl>{source?<a className="mt-3 inline-flex min-h-11 items-center text-sm underline" href={source} target="_blank" rel="noopener noreferrer">{t("sourceLink")}</a>:<p className="mt-3 text-sm">{t("sourceUnavailable")}</p>}<p className="mt-2 text-xs leading-5">{t("sourceName")}</p></details>:null}
  {round&&!cancelled?<details className="uma-card p-4"><summary className="cursor-pointer font-bold">{t("rulesHeading")}</summary><ul className="mt-3 space-y-2 text-sm leading-6">{(t("onlineRules",{returnObjects:true,seconds:round.answerSeconds??180,slots:round.totalSlots,candidates:round.candidates.length}) as string[]).map(rule=><li key={rule}>{rule}</li>)}{round.cooperative?<li>{t("cooperativeRule")}</li>:null}</ul></details>:null}
  <Button asChild variant="secondary" className="h-auto min-h-11 whitespace-normal"><Link to={`/rooms/${roomId}`}>{t("back")}</Link></Button>
 </GameStage>;
}
export function PedigreeResultPanel({round,cancelled=false,scores}:{round:PedigreeRound;cancelled?:boolean;scores:ReturnType<typeof pedigreeScores>}) {
 const {t,i18n}=useTranslation("pedigreeOnline"),own=scores.find(s=>s.subjectId===round.subjectId),answer=round.answer;
 const ownCorrect=answer?.slots.filter(s=>round.assignments.get(s.slotId)===s.horseId).length??0;
 const name=(id:string|undefined)=>id?round.candidates.find(c=>c.horseId===id)?.name||t("unknownHorse"):t("unfilled");
 const rows=answer?.slots.map(s=>({slot:s,chosen:round.assignments.get(s.slotId),correct:round.assignments.get(s.slotId)===s.horseId}));
 return <section id="pedigree-result" className="uma-panel min-w-0 p-3 sm:p-5" aria-labelledby="pedigree-result-heading"><h2 id="pedigree-result-heading" className="u-fluid-task">{t(cancelled?"cancelled":answer?"result":"answerWaiting")}</h2>{cancelled?<p className="mt-2">{t("cancelledDetail")}</p>:answer?<>
  {round.hasSelf?<><dl className="mt-3 grid grid-cols-2 gap-3"><div className="rounded-lg border border-[var(--color-line)] p-3"><dt className="text-xs">{t(round.cooperative?"sharedScore":"finalScore")}</dt><dd className="uma-code text-2xl font-bold">{own?Math.floor(own.scoreUnits/100):"—"}</dd></div><div className="rounded-lg border border-[var(--color-line)] p-3"><dt className="text-xs">{t("ownCorrect")}</dt><dd className="uma-code text-2xl font-bold">{ownCorrect} / {round.totalSlots}</dd></div></dl>{!own?<p className="mt-2 text-sm">{t("waitingScore")}</p>:null}{!round.submissions?<p className="mt-3 text-sm">{t("noAnswer")}</p>:<p className="mt-3 text-sm font-bold">{t("finalAnswer")}</p>}</>:null}
  <div className="mt-3 hidden md:block"><table className="w-full table-fixed text-left text-sm"><caption className="sr-only">{t("checkBranches")}</caption><thead><tr>{["relation",...(round.hasSelf?["yourChoice"]:[]),"recordedAnswer",...(round.hasSelf?["check"]:[])].map(key=><th scope="col" key={key} className="p-2">{t(key)}</th>)}</tr></thead><tbody>{rows?.map(({slot,chosen,correct})=><tr key={slot.slotId} className="border-t border-[var(--color-line)]"><th scope="row" className="break-words p-2">{slotText({slotId:slot.slotId,label:"",means:"",generation:null},i18n.resolvedLanguage)?.label??t("unknownSlot")}</th>{round.hasSelf?<td className="break-words p-2">{name(chosen)}</td>:null}<td className="break-words p-2">{slot.name||name(slot.horseId)}</td>{round.hasSelf?<td className="break-words p-2">{t(correct?"correct":chosen?"incorrect":"unanswered")}</td>:null}</tr>)}</tbody></table></div>
  <ol className="mt-3 space-y-2 md:hidden" aria-label={t("checkBranches")}>{rows?.map(({slot,chosen,correct})=><li key={slot.slotId} className="min-w-0 space-y-2 rounded-lg border border-[var(--color-line)] p-3"><h3 className="font-bold">{slotText({slotId:slot.slotId,label:"",means:"",generation:null},i18n.resolvedLanguage)?.label??t("unknownSlot")}</h3>{round.hasSelf?<p className="break-words text-sm">{t("yourChoice")}：{name(chosen)}</p>:null}<p className="break-words text-sm">{t("recordedAnswer")}：{slot.name||name(slot.horseId)}</p>{round.hasSelf?<p className="text-sm font-semibold">{t(correct?"correct":chosen?"incorrect":"unanswered")}</p>:null}</li>)}</ol>
  {round.cooperative?<p className="mt-3 text-sm leading-6">{t("cooperativeRule")}</p>:null}<p className="mt-3 text-xs leading-5">{t("sourceName")}</p>
 </>:null}<div className="mt-4 flex flex-col gap-2 sm:flex-row"><Button asChild variant="secondary" className="h-auto min-h-12 whitespace-normal"><Link to="/play/pedigree">{t("again")}</Link></Button><Button asChild variant="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/">{t("home")}</Link></Button></div></section>;
}
