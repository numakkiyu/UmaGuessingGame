/** Public coarse progress only; a received action is not necessarily a completed round. */
import { useMemo } from "react";
import { CheckCircle2, Clock, CircleSlash, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { classifyPhase, readSubjectProgress, tallySubmissions, type SubjectProgress } from "@/lib/api/submissions-progress";
import { displayNameOf } from "./use-event-data";
import { useTranslation } from "@/i18n";
import "@/i18n/event-results";
import { cn } from "@/lib/utils";

function roomNames(room: unknown): {names:Map<string,string>;codes:Map<string,string>} {
 const names=new Map<string,string>(),codes=new Map<string,string>();
 if(!room||typeof room!=="object")return {names,codes};
 const r=room as Record<string,unknown>;
 for(const value of Array.isArray(r.teams)?r.teams:[]){if(value&&typeof value.teamId==="string"&&typeof value.label==="string")names.set(value.teamId,value.label);}
 for(const value of Array.isArray(r.members)?r.members:[]){if(!value||typeof value.membershipId!=="string")continue;if(typeof value.displayName==="string")names.set(value.membershipId,value.displayName);if(typeof value.publicCode==="string"&&/^[0-9]{6}$/.test(value.publicCode))codes.set(value.membershipId,value.publicCode);}
 return {names,codes};
}
export type SubmissionProgressPanelProps={round:unknown;room?:unknown;phase:string|null|undefined;modeName?:string;paused?:boolean};
function SubjectRow({subject,canNudge,closed,publicCode}:{subject:SubjectProgress;canNudge:boolean;closed:boolean;publicCode?:string}){
 const {t}=useTranslation("eventResults");
 const state=subject.solved?"solved":subject.failed?"failed":subject.submitted?"submitted":subject.known===false?"unreported":canNudge?"outstanding":closed?"missed":"idle";
 const positive=["solved","submitted"].includes(state),Icon=positive?CheckCircle2:state==="failed"?CircleSlash:Clock;
 return <li className="flex flex-wrap items-center justify-between gap-2 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel)] px-3 py-2">
  <span className="flex min-w-0 items-center gap-2"><Users aria-hidden="true" className="size-4 shrink-0 text-[var(--color-muted)]"/><span className="min-w-0 break-words text-sm font-semibold"><span className="block">{displayNameOf(subject)}</span>{publicCode?<span className="block text-xs font-normal text-[var(--color-muted)]">{t("publicCode",{code:publicCode})}</span>:null}</span>{subject.attempts!==null?<span className="text-xs text-[var(--color-muted)]">{t("attempts",{count:subject.attempts})}</span>:null}</span>
  <span className={cn("inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-bold",positive?"bg-[var(--color-correct)] text-[var(--color-ink-strong)]":state==="outstanding"?"bg-[var(--color-near)] text-[var(--color-ink-strong)]":state==="failed"?"bg-[var(--color-wrong)] text-[var(--color-ink-strong)]":"bg-[var(--color-panel-soft)] text-[var(--color-muted)]")}><Icon aria-hidden="true" className="size-3.5 shrink-0"/>{t(state)}</span>
 </li>;
}
export function SubmissionProgressPanel({round,room,phase,modeName,paused=false}:SubmissionProgressPanelProps){
 const {t}=useTranslation("eventResults");
 const mapping=useMemo(()=>roomNames(room),[room]);
 const {subjects,known}=useMemo(()=>readSubjectProgress(round,mapping.names),[round,mapping.names]);
 const submissionPhase=classifyPhase(phase),tally=tallySubmissions(subjects,paused?"not-started":submissionPhase);
 const partial=subjects.some(s=>s.known===false);
 return <section className="uma-panel min-w-0 p-4" aria-labelledby="progress-heading">
  <div className="uma-section-head"><h2 id="progress-heading" className="text-base font-bold">{t("progress")}</h2>{known&&subjects.length>0?<p className="text-sm font-bold">{t("progressRatio",{done:tally.submitted.length,total:tally.total,percent:tally.percent})}</p>:null}</div>
  {!known?<p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t(modeName?"progressUnknownMode":"progressUnknown",{mode:modeName})}</p>:!subjects.length?<p className="mt-2 text-sm">{t("noEntrants")}</p>:<>
   <div className="mt-2 h-2.5 w-full overflow-hidden rounded-full bg-[var(--color-line)]" role="progressbar" aria-valuenow={tally.submitted.length} aria-valuemin={0} aria-valuemax={tally.total} aria-label={t("progress")}><div className="h-full rounded-full bg-[var(--color-action-green)] motion-safe:transition-[width] motion-safe:duration-[var(--duration-standard)]" style={{width:`${tally.percent}%`}}/></div>
   {tally.outstanding.length>0&&!paused?<Alert tone="warning" title={t("waiting",{count:tally.outstanding.length})} className="mt-3"><p>{t("waitHint")}</p></Alert>:null}
   {submissionPhase==="collecting"&&!paused&&tally.outstanding.length===0&&!partial?<Alert tone="info" title={t("allDone")} className="mt-3"><p>{t("allDoneHint")}</p></Alert>:null}
   {partial?<p className="mt-2 text-sm">{t("partialProgress")}</p>:null}
   {paused?<p className="mt-2 text-sm">{t("pausedProgress")}</p>:submissionPhase==="not-started"?<p className="mt-2 text-sm">{t("notStarted")}</p>:submissionPhase==="closed"?<p className="mt-2 text-sm">{t("closedProgress")}</p>:null}
   <ul className="mt-3 flex flex-col gap-1.5">{subjects.map(subject=><SubjectRow key={subject.subjectId} subject={subject} publicCode={mapping.codes.get(subject.subjectId)} canNudge={submissionPhase==="collecting"&&!paused} closed={submissionPhase==="closed"}/>)}</ul>
  </>}
 </section>;
}
