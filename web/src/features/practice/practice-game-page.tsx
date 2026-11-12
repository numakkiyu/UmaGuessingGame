import { useConfirmation } from "@/lib/confirmation";
import "@/i18n/classic";
import { useTranslation } from "@/i18n";
import { usePlayerText } from "@/i18n/player-text";
import { characterName } from "@/i18n/entities";
import { LocalizedNotice } from "@/lib/player-notice";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { Download, Flag, RotateCcw, Share2 } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { SearchInput } from "@/components/uma/search-input";
import { AvatarImage } from "@/components/uma/avatar-image";
import { LoadingBlock } from "@/components/uma/page-state";
import { GuessCard, GuessTable } from "@/features/classic/guess-card";
import { classicProgress, guessClassic, type ClassicGame, type PracticeCharacter, type PracticePack } from "@/lib/domain/classic-practice";
import { readPracticePack } from "@/lib/local/practice-pack";
import { createClassicGame, loadPracticeGame, PracticeConflict, practiceWriterId, savePracticeGame } from "@/lib/local/practice-store";
import { volatilePractices } from "./volatile-practices";

type Loaded = {game:ClassicGame;pack:PracticePack;characters:PracticeCharacter[];persisted:boolean};
export function PracticeGamePage() {
  const {t}=useTranslation("classic");
  const {gameId}=useParams<{gameId:string}>();
  const [loaded,setLoaded]=useState<Loaded|null>(null);
  const [error,,setError]=usePlayerText("classic");
  useEffect(()=>{
    let cancelled=false;
    async function load() {
      if(!gameId)return;
      try {
        const volatile=volatilePractices.get(gameId);
        const archive=volatile ?? await loadPracticeGame(gameId);
        if(!archive)throw new LocalizedNotice("missingGame");
        const {characters}=readPracticePack(archive.pack);
        if(!cancelled)setLoaded({...archive,characters,persisted:!volatile});
      }catch(reason){if(!cancelled)setError(reason,"loadFailed");}
    }
    void load();return()=>{cancelled=true;};
  },[gameId,setError]);
  if(error)return <div className="uma-stack"><Alert tone="warning" title={t("gameMissing")}>{error}</Alert><Button asChild variant="secondary"><Link to="/play/classic">{t("back")}</Link></Button></div>;
  if(!loaded||loaded.game.gameId!==gameId)return <LoadingBlock label={t("loadingGame")} rows={3}/>;
  return <PracticeGame key={loaded.game.gameId} initial={loaded}/>;
}
function PracticeGame({initial}:{initial:Loaded}) {
  const {t}=useTranslation("classic");
  const [game,setGame]=useState(initial.game);
  const [persisted,setPersisted]=useState(initial.persisted);
  const [selected,setSelected]=useState<PracticeCharacter|null>(null);
  const [busy,setBusy]=useState(false);
  const [message,setMessage,setMessageError]=usePlayerText("classic");
  const [warning,setWarning]=usePlayerText("classic",initial.persisted?"":"unsaved");
  const [stale,setStale]=useState(false);
  const [manual,setManual]=useState<"table"|"cards"|null>(null);
  const [wide,setWide]=useState(false);
  const historyRef=useRef<HTMLDivElement>(null);
  const inFlight=useRef(false);
  const gameRef=useRef(game);
  useEffect(()=>{gameRef.current=game;},[game]);
  const navigate=useNavigate();
  const {pack,characters}=initial;
  const progress=useMemo(()=>classicProgress(game,characters),[game,characters]);
  const confirmAction = useConfirmation(`${game.gameId}:${game.revision}:${stale}:${busy}:${progress.finished}`);
  const excluded=useMemo(()=>new Set(game.guesses),[game.guesses]);
  useEffect(()=>{
    const element=historyRef.current;if(!element)return;
    const observer=new ResizeObserver(entries=>setWide((entries[0]?.contentRect.width??0)>=720));
    observer.observe(element);return()=>observer.disconnect();
  },[]);
  useEffect(()=>{
    if(typeof BroadcastChannel==="undefined")return;
    const channel=new BroadcastChannel("umaguess-practice-changes");
    channel.onmessage=(event:MessageEvent<unknown>)=>{
      const change=event.data as {gameId?:string;writerId?:string}|null;
      if(change?.gameId!==initial.game.gameId||change.writerId===practiceWriterId)return;
      void loadPracticeGame(initial.game.gameId).then(latest=>{
        if(!latest||latest.game.revision>gameRef.current.revision)setStale(true);
      }).catch(()=>undefined);
    };
    return()=>channel.close();
  },[initial.game.gameId]);
  async function commit(next:ClassicGame) {
    try {
      await savePracticeGame(next,persisted?game.revision:null);
      setPersisted(true);setWarning("");volatilePractices.delete(next.gameId);
    }catch(reason){
      if(reason instanceof PracticeConflict){setStale(true);throw reason;}
      setPersisted(false);setWarning("saveFailed");
      volatilePractices.set(next.gameId,{game:next,pack});
    }
    setGame(next);
  }
  async function act(character:PracticeCharacter) {
    if(inFlight.current||stale)return;inFlight.current=true;setBusy(true);setMessage("");
    try{await commit(guessClassic(game,characters,character.characterId,Date.now()));setSelected(null);}catch(reason){setMessageError(reason,"guessFailed");}
    finally{inFlight.current=false;setBusy(false);}
  }
  async function finish() {
    if(inFlight.current||stale||progress.finished||!(await confirmAction({ns:"classic",key:"finishConfirm"},"finish")))return;
    inFlight.current=true;setBusy(true);
    try{await commit({...game,ended:true,updatedAt:Date.now(),revision:game.revision+1});}catch(reason){setMessageError(reason,"finishFailed");}
    finally{inFlight.current=false;setBusy(false);}
  }
  function exportRecord() {
    const blob=new Blob([JSON.stringify({schemaVersion:1,game,pack})],{type:"application/json"});
    const url=URL.createObjectURL(blob),link=document.createElement("a");link.href=url;link.download=`umaguess-practice-${game.gameId}.json`;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
    setMessage("downloadReady");
  }
  async function again() {
    if(inFlight.current)return;inFlight.current=true;setBusy(true);
    try {
      const next=createClassicGame(pack,game.wrongBudget);
      try{await savePracticeGame(next,null);}catch{volatilePractices.set(next.gameId,{game:next,pack});}
      navigate(`/practice/${next.gameId}`);
    }catch(reason){setMessageError(reason,"startFailed");}finally{inFlight.current=false;setBusy(false);}
  }
  const shareText=useCallback(()=>{
    const colors={correct:"🟩",near:"🟨",wrong:"⬜",unknown:"▫️"};
    return `${t("shareTitle")}\n${t(progress.solved?"shareSolved":"shareTryAgain",{count:game.guesses.length})}\n`+progress.rows.map(row=>row.cells.map(cell=>colors[cell.status]).join("")).join("\n");
  },[game.guesses.length,progress.rows,progress.solved,t]);
  async function share() {
    try{await navigator.clipboard.writeText(shareText());setMessage("shareCopied");}catch{setMessage("shareFailed");}
  }
  const view=manual??(wide?"table":"cards");
  return <div className="flex w-full flex-col gap-4">
    <header className="flex flex-wrap items-center gap-3 border-b border-[var(--color-line)] pb-3">
      <div className="min-w-0 basis-full sm:flex-1"><Link className="text-sm font-semibold text-[var(--color-blue-deep)] underline underline-offset-4" to="/play/classic">{t("practice")}</Link><h1 className="mt-1 text-xl font-extrabold text-[var(--color-ink)]">{t("title")}</h1></div>
      <div className="text-right"><p className="text-lg font-bold tabular-nums text-[var(--color-action-green)]">{t(progress.finished?progress.solved?"solved":"ended":"remaining",{count:progress.remainingWrong})}</p><p className="text-sm text-[var(--color-muted)]">{t("attempts",{count:game.guesses.length})}</p></div>
    </header>
    {warning?<Alert tone="warning" title={t("warning")}>{warning}</Alert>:null}
    {stale?<Alert tone="warning" title={t("stale")} action={<Button variant="secondary" onClick={()=>window.location.reload()}>{t("reload")}</Button>}>{t("staleDetail")}</Alert>:null}
    <div className="grid min-w-0 gap-5 xl:grid-cols-[minmax(0,1fr)_18rem]">
      <section ref={historyRef} className="min-w-0 space-y-4" aria-label={t("answerArea")}>
        {!progress.finished?<div className="uma-panel space-y-3 p-4 sm:p-5"><h2 className="font-bold">{t("who")}</h2><SearchInput items={characters} onSelect={setSelected} excludeIds={excluded} disabled={busy||stale} hideLabel />
          {selected?<div className="flex items-center gap-3 rounded-[var(--radius-control)] bg-[var(--color-panel-soft)] p-2"><AvatarImage characterId={selected.characterId} src={selected.imagePath} alt="" size="sm"/><span className="min-w-0 flex-1 font-semibold">{characterName(selected)}</span><Button variant="primary" onClick={()=>void act(selected)} disabled={busy||stale}>{t(busy?"saving":"guess")}</Button></div>:<p className="text-sm text-[var(--color-muted)]">{t("chooseHint")}</p>}</div>:<section className="uma-panel uma-reveal-row grid grid-cols-[auto_minmax(0,1fr)] items-center gap-4 border-[var(--color-brand-green)] p-5 sm:flex" aria-labelledby="practice-result"><AvatarImage characterId={progress.answer.characterId} src={progress.answer.imagePath} alt="" size="lg"/><div className="min-w-0 flex-1"><h2 id="practice-result" className="text-lg font-extrabold">{t(progress.solved?"resultSolved":"resultAnswer")}</h2><p className="mt-1 text-xl font-bold text-[var(--color-action-green)]">{characterName(progress.answer)}</p><p className="mt-1 text-sm text-[var(--color-muted)]">{t(progress.solved?"resultCount":"resultTryAgain",{count:game.guesses.length})}</p></div><Button className="col-span-2 w-full sm:w-auto" variant="primary" onClick={()=>void again()} disabled={busy}><RotateCcw className="size-4" aria-hidden="true"/>{t("again")}</Button></section>}
        {message?<p role="status" className="text-sm leading-6 text-[var(--color-ink)]">{message}</p>:null}
        <div className="flex flex-wrap items-center gap-2"><h2 className="mr-auto w-full text-lg font-bold sm:w-auto">{t("guesses")} <span className="text-sm font-normal text-[var(--color-muted)]">{t("count",{count:progress.rows.length})}</span></h2><Tabs value={manual??"auto"} onValueChange={v=>setManual(v==="auto"?null:v==="table"?"table":"cards")}><TabsList aria-label={t("display")}><TabsTrigger className="whitespace-nowrap" value="auto">{t("auto")}</TabsTrigger><TabsTrigger className="whitespace-nowrap" value="table">{t("table")}</TabsTrigger><TabsTrigger className="whitespace-nowrap" value="cards">{t("cards")}</TabsTrigger></TabsList></Tabs></div>
        {progress.rows.length?view==="table"?<GuessTable rows={progress.rows}/>:<div className="space-y-3">{[...progress.rows].reverse().map(row=><GuessCard key={row.characterId} row={row} latest={row.attempt===progress.rows.length}/>)}</div>:<div className="rounded-[var(--radius-card)] border border-dashed border-[var(--color-line-strong)] p-6 text-center text-sm leading-6 text-[var(--color-muted)]">{t("empty")}<br/>{t("colors")}</div>}
      </section>
      <aside className="min-w-0 space-y-4">
        <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{t("gameRules")}</summary><div className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]"><p>{t("gameRule1")}</p><p>{t("gameRule2")}</p><p>{t("gameRule3")}</p><p>{t(persisted?"saved":"unsavedShort")}</p></div></details>
        <div className="flex flex-wrap gap-2"><Button variant="secondary" onClick={exportRecord}><Download className="size-4" aria-hidden="true"/>{t("export")}</Button>{!progress.finished?<Button variant="ghost" onClick={()=>void finish()} disabled={busy||stale}><Flag className="size-4" aria-hidden="true"/>{t("finish")}</Button>:<Button variant="secondary" onClick={()=>void share()}><Share2 className="size-4" aria-hidden="true"/>{t("share")}</Button>}</div>
        {progress.finished?<details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer text-sm font-semibold">{t("showShare")}</summary><pre className="mt-2 whitespace-pre-wrap break-words text-sm leading-6">{shareText()}</pre></details>:null}
      </aside>
    </div>
  </div>;
}
