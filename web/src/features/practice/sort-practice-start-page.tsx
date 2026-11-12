import { useConfirmation } from "@/lib/confirmation";
import { SelectField } from "@/components/ui/select";
import "@/i18n/sort";
import { useTranslation } from "@/i18n";
import { usePlayerText } from "@/i18n/player-text";
import { formatDateTime } from "@/i18n/entities";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Download, Play, Upload } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import type { SortGame, SortKey } from "@/lib/domain/sort-practice";
import { createSortGame, deleteSortGame, installSortPack, latestSortPack, listSortGames, readSortPack, saveSortGame, validateSortArchive, volatileSortGames, type SortPack } from "@/lib/local/sort-practice-store";
import { newPracticeId } from "@/lib/local/practice-store";
import { prepareOfflineShell } from "@/lib/local/offline-shell";

export function SortPracticeStartPage(){
 const {t}=useTranslation("sort"),{t:tp}=useTranslation("practice");
  const confirmAction = useConfirmation();
 const [message,setMessage,setError]=usePlayerText("sort"),[warning,setWarning]=usePlayerText("practice");
 const [listedAt]=useState(Date.now);
 const navigate=useNavigate(),abort=useRef<AbortController|null>(null),file=useRef<HTMLInputElement>(null);
 const [pack,setPack]=useState<SortPack|null>(null),[games,setGames]=useState<SortGame[]>([]),[limit,setLimit]=useState(20);
 const [key,setKey]=useState<SortKey>("height"),[openBook,setOpenBook]=useState(false),[loading,setLoading]=useState(true),[busy,setBusy]=useState(false);
 useEffect(()=>{let cancelled=false;void Promise.all([latestSortPack(),listSortGames()]).then(([p,g])=>{if(!cancelled){setPack(p);setGames(g);}}).catch(()=>{if(!cancelled)setWarning("storageWarning");}).finally(()=>{if(!cancelled)setLoading(false);});return()=>{cancelled=true;abort.current?.abort();};},[setWarning]);
 async function prepare(){
  if(busy)return;setBusy(true);setMessage("preparing");const c=new AbortController();abort.current=c;
  try{
   const response=await api.getSortPracticePack(c.signal),{pack:next,profiles}=readSortPack(response.data);
   const ready=await prepareOfflineShell(profiles,c.signal,(done,total)=>setMessage("flow.preparingAvatars",{done,total}));
   try{await installSortPack(next);setWarning("");}catch{setWarning("packWarning");}
   setPack(next);setMessage(ready?"ready":"flow.readyKeepOpen");
  }catch(error){if(!c.signal.aborted)setError(error,"flow.prepareFailed");}
  finally{setBusy(false);}
 }
 async function start(){
  if(!pack||busy)return;setBusy(true);setMessage("");
  try{const game=createSortGame(pack,key,openBook);try{await saveSortGame(game,null);}catch{volatileSortGames.set(game.gameId,{schemaVersion:1,game,pack});}navigate(`/practice/sort/${game.gameId}`);}
  catch(error){setError(error,"flow.startFailed");setBusy(false);}
 }
 async function importRecord(selected:File|undefined){
  if(!selected||busy)return;setBusy(true);
  try{if(selected.size>2_000_000){setMessage("flow.largeArchive");return;}const archive=validateSortArchive(JSON.parse(await selected.text()));const game={...archive.game,gameId:newPracticeId()};await installSortPack(archive.pack);await saveSortGame(game,null);navigate(`/practice/sort/${game.gameId}`);}
  catch(error){setError(error,"flow.importFailed");setBusy(false);}
  finally{setBusy(false);if(file.current)file.current.value="";}
 }
 async function remove(id:string){if(!(await confirmAction({ns:"practice",key:"removeConfirm"},"remove")))return;try{await deleteSortGame(id);setGames(items=>items.filter(g=>g.gameId!==id));}catch{setMessage("flow.removeFailed");}}
 return <div className="uma-prose mx-auto space-y-5">
  <header className="uma-panel relative overflow-hidden p-5 sm:p-7"><div className="pointer-events-none absolute inset-0 bg-[url('/assets/ui/scenes/lobby-classroom.jpg')] bg-cover bg-center opacity-10" aria-hidden="true"/><div className="relative"><h1 className="u-fluid-title">{t("title")}</h1><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("summary")}</p></div></header>
  <section className="uma-panel space-y-4 p-5" aria-labelledby="sort-practice-heading"><h2 id="sort-practice-heading" className="text-lg font-bold">{tp("practice")}</h2><p className="text-sm text-[var(--color-muted)]">{t("hint")}</p>
   {warning?<Alert tone="warning" title={tp("warning")}>{warning}</Alert>:null}
   {loading?<p role="status">{tp("loading")}</p>:pack?<>
    <div className="grid gap-4 sm:grid-cols-2"><div className="space-y-2"><Label htmlFor="sort-key">{t("keyLabel")}</Label><SelectField id="sort-key" value={key} onValueChange={value =>setKey(value as SortKey)} disabled={busy} options={[({value:"height",label:t("heightOption")}),({value:"birthday",label:t("birthdayOption")})]} /></div><div className="space-y-2"><Label htmlFor="sort-style">{t("styleLabel")}</Label><SelectField id="sort-style" value={openBook?"open":"closed"} onValueChange={value =>setOpenBook(value==="open")} disabled={busy} options={[({value:"closed",label:t("closedOption")}),({value:"open",label:t("openOption")})]} /></div></div>
    <Button variant="primary" size="primary" onClick={()=>void start()} disabled={busy} block><Play className="size-4" aria-hidden="true"/>{t("start")}</Button>
   </>:<Button variant="primary" size="primary" onClick={()=>void prepare()} disabled={busy} block><Download className="size-4" aria-hidden="true"/>{busy?tp("busy"):t("prepare")}</Button>}
   {message?<p role="status" className="text-sm leading-6">{message}</p>:null}
   <div className="flex flex-wrap gap-2 border-t border-[var(--color-line)] pt-3">{pack?<Button variant="ghost" onClick={()=>void prepare()} disabled={busy}>{tp("refresh")}</Button>:null}<Button variant="ghost" onClick={()=>file.current?.click()} disabled={busy}><Upload className="size-4" aria-hidden="true"/>{tp("import")}</Button><Button asChild variant="secondary"><Link to="/?form=multiplayer">{tp("friends")}</Link></Button><input ref={file} type="file" accept="application/json,.json" aria-label={tp("file")} className="sr-only" onChange={e=>void importRecord(e.target.files?.[0])}/></div>
  </section>
  {games.length?<section className="uma-panel p-5"><h2 className="text-lg font-bold">{tp("history")}</h2><ul className="mt-3 divide-y divide-[var(--color-line)]">{games.slice(0,limit).map(g=><li key={g.gameId} className="flex flex-wrap items-center gap-2 py-3"><div className="min-w-0 flex-1"><p className="font-semibold">{t(g.key==="height"?"heightTitle":"birthdayTitle")} · {t(g.openBook?"open":"closed")}</p><p className="text-sm text-[var(--color-muted)]">{formatDateTime(g.updatedAt)} · {t(g.ended||listedAt>=g.deadlineAt?"resultAction":"continueAction")}</p></div><Button asChild variant="secondary"><Link to={`/practice/sort/${g.gameId}`}>{tp("open")}</Link></Button><Button variant="ghost" onClick={()=>void remove(g.gameId)}>{tp("remove")}</Button></li>)}</ul>{games.length>limit?<Button variant="ghost" onClick={()=>setLimit(n=>n+20)}>{tp("more")}</Button>:null}</section>:null}
  <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{t("rulesTitle")}</summary><div className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]">{(t("rules",{returnObjects:true}) as string[]).map((rule,i)=><p key={i}>{rule}</p>)}</div></details>
 </div>;
}
