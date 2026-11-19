import { useConfirmation } from "@/lib/confirmation";
import { LocalizedNotice } from "@/lib/player-notice";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Download, Play, RefreshCw, Upload } from "lucide-react";
import "@/i18n/classic";
import { useTranslation } from "@/i18n";
import { usePlayerText } from "@/i18n/player-text";
import { formatDateTime } from "@/i18n/entities";
import { api } from "@/lib/api";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { readPracticePack } from "@/lib/local/practice-pack";
import { createClassicGame, installPracticePack, latestPracticePack, listPracticeGames, newPracticeId, removePracticeGame, savePracticeGame, validatePracticeArchive } from "@/lib/local/practice-store";
import { prepareOfflineShell } from "@/lib/local/offline-shell";
import type { ClassicGame, PracticePack } from "@/lib/domain/classic-practice";
import { volatilePractices } from "./volatile-practices";

export function PracticeStartPage() {
  const {t}=useTranslation("classic");
  const confirmAction = useConfirmation();
  const navigate=useNavigate();
  const [pack,setPack]=useState<PracticePack|null>(null);
  const [historyLimit,setHistoryLimit]=useState(20);
  const [games,setGames]=useState<ClassicGame[]>([]);
  const [budget,setBudget]=useState(8);
  const [busy,setBusy]=useState(false);
  const [loading,setLoading]=useState(true);
  const [message,setMessage,setMessageError]=usePlayerText("classic");
  const [warning,setWarning]=usePlayerText("classic");
  const [progress,setProgress]=usePlayerText("classic");
  const abortRef=useRef<AbortController|null>(null);
  const fileRef=useRef<HTMLInputElement>(null);
  useEffect(()=>{
    let cancelled=false;
    void Promise.all([latestPracticePack(),listPracticeGames()]).then(([p,g])=>{
      if(!cancelled){setPack(p);setGames(g);}
    }).catch(()=>{if(!cancelled)setWarning("storageWarning");}).finally(()=>{if(!cancelled)setLoading(false);});
    return()=>{cancelled=true;abortRef.current?.abort();};
  },[setWarning]);
  async function prepare() {
    if(busy)return;setBusy(true);setMessage("");setProgress("preparingPack");
    const controller=new AbortController();abortRef.current=controller;
    try {
      const response=await api.getClassicPracticePack(controller.signal);
      const {pack:next,characters}=readPracticePack(response.data);
      // Only activate the new data after the complete shell/media download succeeds.
      const offlineReady=await prepareOfflineShell(characters,controller.signal,(done,total)=>setProgress("preparingAvatars",{done,total}));
      let storageWarning="";
      try{await installPracticePack(next);}catch{storageWarning="packWarning";}
      setPack(next);setWarning(storageWarning);
      setMessage(offlineReady?"ready":"readyKeepOpen");
    } catch(error) {
      if(!controller.signal.aborted)setMessageError(error,"prepareFailed");
    } finally {setBusy(false);setProgress("");}
  }
  async function start() {
    if(!pack||busy)return;setBusy(true);setMessage("");
    try {
      const game=createClassicGame(pack,budget);
      try {await savePracticeGame(game,null);}catch{volatilePractices.set(game.gameId,{game,pack});}
      navigate(`/practice/${game.gameId}`);
    } catch(error){setMessageError(error,"startFailed");setBusy(false);}
  }
  async function importRecord(file:File|undefined) {
    if(!file||busy)return;setBusy(true);setMessage("");
    try {
      if(file.size>2_000_000)throw new LocalizedNotice("largeArchive");
      const archive=validatePracticeArchive(JSON.parse(await file.text()));
      const game={...archive.game,gameId:newPracticeId()};
      await installPracticePack(archive.pack);await savePracticeGame(game,null);
      navigate(`/practice/${game.gameId}`);
    }catch(error){setMessageError(error,"importFailed");setBusy(false);}
    finally{if(fileRef.current)fileRef.current.value="";}
  }
  async function deleteRecord(id:string) {
    if(!(await confirmAction({ns:"classic",key:"removeConfirm"},"remove")))return;
    try{await removePracticeGame(id);setGames(items=>items.filter(g=>g.gameId!==id));}catch{setMessage("removeFailed");}
  }
  return <div className="mx-auto flex w-full max-w-4xl flex-col gap-5">
    <header className="uma-panel relative overflow-hidden p-5 sm:p-7">
      <div className="pointer-events-none absolute inset-0 bg-[url('/assets/ui/scenes/lobby-classroom.jpg')] bg-cover bg-center opacity-10" aria-hidden="true" />
      <div className="relative"><p className="text-sm font-bold text-[var(--color-action-green)]">{t("tag")}</p>
        <h1 className="u-fluid-title mt-1 text-[var(--color-ink)]">{t("title")}</h1>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-[var(--color-muted)]">{t("intro")}</p>
      </div>
    </header>
    <section className="uma-panel space-y-4 p-5" aria-labelledby="practice-ready">
      <div><h2 id="practice-ready" className="text-lg font-bold text-[var(--color-ink)]">{t("practice")}</h2><p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("practiceIntro")}</p></div>
      {warning?<Alert tone="warning" title={t("warning")}>{warning}</Alert>:null}
      {loading?<p role="status">{t("loading")}</p>:pack?<>
        <div className="flex flex-wrap items-end gap-4"><div className="space-y-1"><Label htmlFor="practice-budget">{t("budget")}</Label><Input id="practice-budget" className="w-28" type="number" min={4} max={12} value={budget} onChange={e=>setBudget(Number(e.target.value))} disabled={busy}/></div>
        <Button variant="primary" size="primary" onClick={()=>void start()} disabled={busy||!Number.isInteger(budget)||budget<4||budget>12}><Play className="size-4" aria-hidden="true"/>{t("play")}</Button></div>
        <p className="text-sm text-[var(--color-muted)]">{t("readyCount",{count:readPracticePack(pack).characters.length})}</p>
      </>:<div className="space-y-3"><p className="text-sm leading-6">{t("prepareHint")}</p><Button variant="primary" size="primary" onClick={()=>void prepare()} disabled={busy}><Download className="size-4" aria-hidden="true"/>{t(busy?"preparing":"prepare")}</Button></div>}
      {progress?<p role="status" className="text-sm text-[var(--color-muted)]">{progress}</p>:null}
      {message?<p role="status" className="text-sm leading-6 text-[var(--color-ink)]">{message}</p>:null}
      <div className="flex flex-wrap gap-2 border-t border-[var(--color-line)] pt-3">
        {pack?<Button variant="ghost" onClick={()=>void prepare()} disabled={busy}><RefreshCw className="size-4" aria-hidden="true"/>{t("refresh")}</Button>:null}
        <Button variant="ghost" onClick={()=>fileRef.current?.click()} disabled={busy}><Upload className="size-4" aria-hidden="true"/>{t("import")}</Button>
        <input ref={fileRef} className="sr-only" type="file" accept="application/json,.json" aria-label={t("file")} onChange={e=>void importRecord(e.target.files?.[0])}/>
      </div>
    </section>
    {games.length?<section className="uma-panel p-5" aria-labelledby="practice-history"><h2 id="practice-history" className="text-lg font-bold">{t("history")}</h2><ul className="mt-3 divide-y divide-[var(--color-line)]">{games.slice(0,historyLimit).map(game=><li key={game.gameId} className="flex flex-wrap items-center gap-2 py-3"><div className="min-w-0 flex-1"><p className="font-semibold">{t(game.ended||game.guesses.includes(game.answerId)?"viewGame":"continueGame")}</p><p className="text-sm text-[var(--color-muted)]">{t("historyItem",{date:formatDateTime(game.updatedAt),count:game.guesses.length})}</p></div><Button asChild variant="secondary"><Link to={`/practice/${game.gameId}`}>{t("open")}</Link></Button><Button variant="ghost" onClick={()=>void deleteRecord(game.gameId)}>{t("remove")}</Button></li>)}</ul>{games.length>historyLimit?<Button variant="ghost" onClick={()=>setHistoryLimit(n=>n+20)}>{t("more")}</Button>:null}</section>:null}
    <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{t("rules")}</summary><div className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]"><p>{t("rules1")}</p><p>{t("rules2")}</p><p>{t("rules3")}</p><p>{t("rules4")}</p></div></details>
  </div>;
}
