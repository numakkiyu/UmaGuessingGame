import { useEffect } from "react";
import { Alert } from "@/components/ui/alert";
import { phaseLabel, useRoundSnapshot } from "@/lib/api/round-snapshot";
import { normalizeStrategyRaceRound } from "@/lib/api/modes-g09-g12";
import { readSubjectProgress, tallySubmissions } from "@/lib/api/submissions-progress";
import { useCountdown } from "@/lib/hooks/use-countdown";
import { RaceTrack } from "@/features/modes/race-track";
import { eventModeLabel } from "@/features/events/event-presentation";
import { useTranslation } from "@/i18n";
import "@/i18n/display";

const hints:Record<string,string>={classic:"watchClassic","aptitude-grid":"watchGrid",draft:"watchDraft","race-rebuild":"watchRace","co-race-path":"watchPath",sort:"watchSort",memory:"watchMemory",pedigree:"watchPedigree",audio:"watchAudio","clue-puzzle":"watchClue",rapport:"watchRapport",trade:"watchTrade"};
/** Requests only the restricted display identity. No participant UI or private
 * snapshot is mounted, even when the browser also has an interactive cookie. */
export function PublicRoundStage({roundId,modeId,competitive}:{roundId:string;modeId:string;competitive?:boolean}){
 const {t}=useTranslation("display"),{t:rt}=useTranslation("eventResults");
 const snapshot=useRoundSnapshot(roundId,true),{refetch}=snapshot;
 useEffect(()=>{
  const refresh=()=>{if(!document.hidden)void refetch();};
  const timer=window.setInterval(refresh,2000);
  document.addEventListener("visibilitychange",refresh);
  return()=>{window.clearInterval(timer);document.removeEventListener("visibilitychange",refresh);};
 },[refetch]);
 const meta=snapshot.meta?.roundId===roundId?snapshot.meta:null;
 const cancelled=meta?.phase==="cancelled",ended=cancelled||meta?.phase==="revealed"||meta?.phase==="settled",paused=meta?.paused===true;
 const timer=useCountdown(meta?.deadlineAt,!paused&&!ended&&!snapshot.isError);
 const raw=snapshot.raw as {subjectNames?:Record<string,string>;publicState?:{subphase?:unknown}}|null;
 const race=modeId==="strategy-race"&&snapshot.raw&&meta?normalizeStrategyRaceRound(snapshot.raw):null;
 const progress=readSubjectProgress(snapshot.raw),tally=tallySubmissions(progress.subjects,"closed");
 const completeProgress=progress.known&&!progress.subjects.some(subject=>subject.known===false);
 const reviewMode=modeId==="creative"||modeId==="bluff";
 const hint=cancelled?"roundCancelled":ended?"ended":paused?"pauseHint":meta?.phase==="briefing"||meta?.phase==="prepared"?"briefingHint":reviewMode?(raw?.publicState?.subphase==="review"?"reviewHint":raw?.publicState?.subphase==="voting"?"votingHint":"writingHint"):hints[modeId]??"genericHint";
 const scoring=competitive===false||modeId==="creative"||modeId==="trade"?"funHint":competitive===true?"formalHint":"unknownScoring";
 return <section className="uma-public-round" aria-label={t("progressTitle")}>
  <header className="uma-public-round__heading"><div className="min-w-0"><p className="uma-public-round__eyebrow">{phaseLabel(meta?.phase)}</p><h2 className="break-words">{eventModeLabel(modeId)}</h2></div><div className="uma-public-round__clock" role="timer" aria-label={paused?t("paused"):t("remaining")}>{paused?t("paused"):snapshot.isError||ended||!meta?.deadlineAt?phaseLabel(meta?.phase):timer.text}</div></header>
  {snapshot.isError?<Alert tone="warning" title={t("roundError")}><p>{t("roundErrorHint")}</p></Alert>:null}
  {snapshot.isLoading?<p className="uma-public-round__hint" role="status">{t("loadingRound")}</p>:!meta?<p className="uma-public-round__hint" role="status">{t("roundMissing")}</p>:<>
   {race?<><p className="uma-public-round__hint">{t("roundTurn",{turn:Math.min(race.currentTurn,race.totalTurns),total:race.totalTurns})}</p><RaceTrack teams={race.standings.map((team,i)=>({...team,label:raw?.subjectNames?.[team.subjectId]||t("teamFallback",{number:i+1}),isSelf:false,avatarSrc:null}))} totalTurns={race.totalTurns} staminaCap={race.staminaCap} maxGainPerTurn={Math.max(1,...race.actions.map(action=>action.positionGain))}/></>:<div className="uma-public-round__message"><p>{t(hint)}</p>{meta.phase==="collecting"&&completeProgress&&tally.total>0?<p className="uma-public-round__hint">{rt("progress")}：{rt("progressRatio",{done:tally.submitted.length,total:tally.total,percent:tally.percent})}</p>:null}</div>}
   <p className="uma-public-round__hint">{t(cancelled?"roundCancelled":scoring)}</p>
  </>}
 </section>;
}
