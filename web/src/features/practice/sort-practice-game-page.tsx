import { subscribePracticeChanges } from "@/lib/local/practice-channel";
import { PracticeSubmitActions } from "./activity-practice-kit";
import { GameStage } from "@/components/uma/game-stage";
import { useConfirmation } from "@/lib/confirmation";
import { useTranslation } from "@/i18n";
import { SortNotice } from "@/i18n/sort";
import { characterName } from "@/i18n/entities";
import { errorPlayerText, resolvePlayerText, usePlayerText, type PlayerText } from "@/i18n/player-text";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { Download, Flag, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { LoadingBlock } from "@/components/uma/page-state";
import { SortBoard } from "@/features/modes/sort-board";
import { mergeWithNext, moveDown, moveUp, splitGroup, type SortDraft } from "@/lib/api/sort";
import { changeSort, sortCards, sortFinished, sortResult, type SortGame } from "@/lib/domain/sort-practice";
import { createSortGame, loadSortGame, readSortPack, saveSortGame, volatileSortGames, type SortArchive } from "@/lib/local/sort-practice-store";
import { PracticeConflict, practiceWriterId } from "@/lib/local/practice-store";

export function SortPracticeGamePage(){
 const {t:tp}=useTranslation("practice");
 const {gameId=""}=useParams<{gameId:string}>();const [archive,setArchive]=useState<SortArchive|null>(null),[error,setError]=useState<{gameId:string;message:PlayerText}|null>(null);
 useEffect(()=>{let cancelled=false;void (async()=>{try{const value=volatileSortGames.get(gameId)??await loadSortGame(gameId);if(!value)throw new SortNotice("missing");if(!cancelled)setArchive(value);}catch(e){if(!cancelled)setError({gameId,message:errorPlayerText(e,"sort","flow.loadFailed")});}})();return()=>{cancelled=true;};},[gameId]);
 if(error?.gameId===gameId)return <div className="uma-stack"><Alert fault tone="warning" title={tp("missingTitle")}>{resolvePlayerText(error.message)}</Alert><Button asChild variant="secondary"><Link to="/play/sort">{tp("back")}</Link></Button></div>;
 if(!archive||archive.game.gameId!==gameId)return <LoadingBlock label={tp("loadingGame")} rows={3}/>;
 return <SortPracticeGame key={gameId} initial={archive}/>;
}
function SortPracticeGame({initial}:{initial:SortArchive}){
 const {t,i18n}=useTranslation("sort"),{t:tp}=useTranslation("practice");
 const [message,setMessage,setError]=usePlayerText("sort"),[warning,setWarning]=usePlayerText("practice",volatileSortGames.has(initial.game.gameId)?"unsavedDetail":"");
 const navigate=useNavigate(),[game,setGame]=useState(initial.game),[busy,setBusy]=useState(false),[stale,setStale]=useState(false);
 const [now,setNow]=useState(Date.now),inFlight=useRef(false),gameRef=useRef(game),lastSaved=useRef<number|null>(volatileSortGames.has(initial.game.gameId)?volatileSortGames.get(initial.game.gameId)?.savedRevision??null:initial.game.revision);
 const {profiles}=useMemo(()=>readSortPack(initial.pack),[initial.pack]);const ended=sortFinished(game,now),cards=useMemo(()=>sortCards(game,profiles,ended,i18n.resolvedLanguage),[game,profiles,ended,i18n.resolvedLanguage]);const remaining=Math.max(0,Math.ceil((game.deadlineAt-now)/1000));const result=ended?sortResult(game,profiles):null;
 useEffect(()=>{gameRef.current=game;},[game]);
  const confirmAction = useConfirmation(`${game.gameId}:${game.revision}:${stale}:${busy}:${ended}`);
 useEffect(()=>{if(ended)return;const timer=setInterval(()=>setNow(previous=>Math.max(previous,Date.now())),250);return()=>clearInterval(timer);},[ended]);
 useEffect(()=>{
  return subscribePracticeChanges("umaguess-activity-changes",v=>{if(v.gameId!==game.gameId||v.writerId===practiceWriterId)return;void loadSortGame(game.gameId).then(value=>{if(!value||value.game.revision>gameRef.current.revision)setStale(true);}).catch(()=>undefined);});
 },[game.gameId]);
 async function commit(next:SortGame){
  try{await saveSortGame(next,lastSaved.current);lastSaved.current=next.revision;setWarning("");volatileSortGames.delete(game.gameId);}
  catch(error){if(error instanceof PracticeConflict){setStale(true);throw error;}setWarning("unsavedDetail");volatileSortGames.set(game.gameId,{schemaVersion:1,game:next,pack:initial.pack,savedRevision:lastSaved.current});}
  gameRef.current=next;setGame(next);
 }
 async function change(groups:SortDraft,action:"draft"|"submit"){
  if(inFlight.current||stale)return;inFlight.current=true;setBusy(true);setMessage("");
  try{await commit(changeSort(game,groups,action,Math.max(now,Date.now())));if(action==="submit")setMessage("saved");}
  catch(error){setError(error,"flow.actionFailed");}finally{inFlight.current=false;setBusy(false);}
 }
 async function submitAndFinish(){
  if(inFlight.current||stale||ended||!(await confirmAction({ns:"practice",key:"submitFinishConfirm"},"finish")))return;
  inFlight.current=true;setBusy(true);setMessage("");
  try{const at=Math.max(now,Date.now()),next=changeSort(gameRef.current,gameRef.current.draft,"submit",at);await commit({...next,ended:true});setNow(at);}
  catch(error){setError(error,"flow.actionFailed");}finally{inFlight.current=false;setBusy(false);}
 }
 async function finish(){
  if(inFlight.current||stale||ended||!(await confirmAction({ns:"sort",key:game.submitted?"finishSaved":"finishUnsaved"},"finish")))return;
  inFlight.current=true;setBusy(true);try{await commit({...game,ended:true,updatedAt:Math.max(now,Date.now()),revision:game.revision+1});}catch(error){setError(error,"flow.actionFailed");}finally{inFlight.current=false;setBusy(false);}
 }
 async function again(){
  if(inFlight.current)return;inFlight.current=true;setBusy(true);
  try{const next=createSortGame(initial.pack,game.key,game.openBook);try{await saveSortGame(next,null);}catch{volatileSortGames.set(next.gameId,{schemaVersion:1,game:next,pack:initial.pack});}navigate(`/practice/sort/${next.gameId}`);}catch(error){setError(error,"flow.nextFailed");}finally{inFlight.current=false;setBusy(false);}
 }
 function exportRecord(){const url=URL.createObjectURL(new Blob([JSON.stringify({schemaVersion:1,game,pack:initial.pack})],{type:"application/json"}));const a=document.createElement("a");a.href=url;a.download=`umaguess-sort-${game.gameId}.json`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);setMessage("exportReady");}
 const controls={draft:game.draft,cards,locked:busy||stale||ended,onChange:(d:SortDraft)=>void change(d,"draft"),onMoveUp:(i:number)=>void change(moveUp(game.draft,i),"draft"),onMoveDown:(i:number)=>void change(moveDown(game.draft,i),"draft"),onMerge:(i:number)=>void change(mergeWithNext(game.draft,i),"draft"),onSplit:(i:number)=>void change(splitGroup(game.draft,i),"draft")};
 return <GameStage mode="sort" className="mx-auto w-full max-w-5xl space-y-4">
  <header className="flex flex-wrap items-center gap-3 border-b border-[var(--color-line)] pb-3"><div className="min-w-0 flex-1"><Link to="/play/sort" className="text-sm font-semibold text-[var(--color-blue-deep)] underline">{tp("practice")}</Link><h1 className="mt-1 text-xl font-extrabold">{t("title")} · {t(game.key==="height"?"height":"birthday")}</h1><p className="mt-1 text-sm text-[var(--color-muted)]">{t(game.openBook?"openOption":"closedHint")}</p></div><p className="text-lg font-bold tabular-nums text-[var(--color-action-green)]" role="timer" aria-live="off">{ended?tp("ended"):tp("remaining",{time:`${Math.floor(remaining/60)}:${String(remaining%60).padStart(2,"0")}`})}</p></header>
  {warning?<Alert tone="warning" title={tp("warning")}>{warning}</Alert>:null}{stale?<Alert tone="warning" title={tp("stale")} action={<Button variant="secondary" onClick={()=>window.location.reload()}>{tp("reload")}</Button>}>{tp("staleDetail")}</Alert>:null}
  {message?<p role="status" className="text-sm leading-6">{message}</p>:null}
  {result?<section className="uma-panel uma-reveal-row space-y-3 p-5"><h2 className="text-xl font-extrabold">{t(result.scoreUnits===10000?"complete":"result")}</h2><p className="text-lg font-bold text-[var(--color-action-green)]">{t("score",{score:result.scoreUnits/100,correct:result.correct,total:result.total})}</p><p className="text-sm text-[var(--color-muted)]">{t(game.submitted?"scoringSaved":"scoringUnsaved")}</p><Button variant="primary" onClick={()=>void again()} disabled={busy}><RotateCcw className="size-4" aria-hidden="true"/>{t("again")}</Button></section>:null}
  <div className="grid min-w-0 gap-5 lg:grid-cols-[minmax(0,1fr)_18rem]">
   <section className="min-w-0 space-y-3" aria-label={t("controls")}><h2 className="text-lg font-bold">{t(ended?"lastAnswer":"arrange")}</h2>{!ended||game.submitted?<SortBoard {...controls} draft={ended?game.submitted!:game.draft}/>:<p className="uma-panel p-4 text-sm">{t("noAnswer")}</p>}
    {!ended?<div className="uma-panel space-y-3 p-4"><p className="text-sm">{t(game.submitted?JSON.stringify(game.draft)===JSON.stringify(game.submitted)?"savedHint":"changedHint":"unsavedHint")}</p><PracticeSubmitActions saveLabel={t(busy?"saving":"save")} saveDisabled={busy||stale||JSON.stringify(game.draft)===JSON.stringify(game.submitted)} submitDisabled={busy||stale} save={()=>change(game.draft,"submit")} submit={submitAndFinish}/></div>:null}
   </section>
   <aside className="min-w-0 space-y-4">{result?<section className="uma-panel p-4"><h2 className="mb-3 text-lg font-bold">{t("correctOrder")}</h2><ol className="space-y-2">{result.groups.map((group,i)=><li key={group.join("+")} className="border-b border-[var(--color-line)] pb-2 text-sm"><p className="font-semibold">{result.groups.slice(0,i).reduce((rank,g)=>rank+g.length,1)}. {group.map(id=>characterName(profiles.find(p=>p.characterId===id)!)).join("、")}{group.length>1?t("tiedSuffix"):""}</p><p className="mt-1 text-[var(--color-muted)]">{cards.find(c=>c.characterId===group[0])!.display}</p></li>)}</ol></section>:null}
    <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{tp("rules")}</summary><div className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]"><p>{t(game.key==="height"?"heightRule":"birthdayRule")}</p><p>{t("saveRule")}</p><p>{tp("persistence")}</p></div></details>
    <div className="flex flex-wrap gap-2"><Button variant="secondary" onClick={exportRecord}><Download className="size-4" aria-hidden="true"/>{tp("export")}</Button>{!ended?<Button variant="ghost" onClick={()=>void finish()} disabled={busy||stale}><Flag className="size-4" aria-hidden="true"/>{tp("finish")}</Button>:null}</div>
   </aside>
  </div>
 </GameStage>;
}
