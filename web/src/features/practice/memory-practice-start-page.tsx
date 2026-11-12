import { useConfirmation } from "@/lib/confirmation";
import "@/i18n/memory";
import "@/i18n/practice-flow";
import { useTranslation } from "@/i18n";
import { formatDateTime } from "@/i18n/entities";
import { usePlayerText } from "@/i18n/player-text";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Download, Play, Upload } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { createMemoryGame, memoryStore, readMemoryPack, validateMemoryArchive, volatileMemoryGames, type MemoryPack } from "@/lib/local/memory-practice-store";
import { newPracticeId } from "@/lib/local/practice-store";
import { prepareOfflineShell } from "@/lib/local/offline-shell";
import type { MemoryGame } from "@/lib/domain/memory-practice";

export function MemoryPracticeStartPage() {
  const {t}=useTranslation("memory"),{t:tp}=useTranslation("practice");
  const confirmAction = useConfirmation();
  const [message,setMessage,setError]=usePlayerText("memory"),[warning,setWarning]=usePlayerText("practice");
  const navigate = useNavigate(), abort = useRef<AbortController | null>(null), file = useRef<HTMLInputElement>(null);
  const [pack, setPack] = useState<MemoryPack | null>(null), [games, setGames] = useState<MemoryGame[]>([]), [limit, setLimit] = useState(20);
  const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false);
  useEffect(() => {
    let cancelled = false;
    void Promise.all([memoryStore.latestPack(), memoryStore.list()]).then(([p, g]) => {
      if (!cancelled) { setPack(p ? readMemoryPack(p).pack : null); setGames(g); }
    }).catch(() => { if (!cancelled) setWarning("storageWarning"); }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; abort.current?.abort(); };
  }, [setWarning]);
  async function prepare() {
    if (busy) return; setBusy(true); setMessage("preparing"); const c = new AbortController(); abort.current = c;
    try {
      const response = await api.getMemoryPracticePack(c.signal), { pack: next, faces } = readMemoryPack(response.data);
      const ready = await prepareOfflineShell(faces, c.signal, (done, total) => setMessage("preparingFaces",{done,total}));
      try { await memoryStore.install(next); setWarning(""); } catch { setWarning("packWarning"); }
      setPack(next); setMessage(ready ? "ready" : "readyKeepOpen");
    } catch (error) { if (!c.signal.aborted) setError(error,"prepareFailed"); }
    finally { setBusy(false); }
  }
  async function start() {
    if (!pack || busy) return; setBusy(true);
    try {
      const game = createMemoryGame(pack);
      try { await memoryStore.save(game, pack, null); } catch { volatileMemoryGames.set(game.gameId, { schemaVersion: 1, game, pack }); }
      navigate(`/practice/memory/${game.gameId}`);
    } catch (error) { setError(error,"startFailed"); setBusy(false); }
  }
  async function importRecord(selected: File | undefined) {
    if (!selected || busy) return; setBusy(true);
    try {
      if (selected.size > 2_000_000) { setMessage("largeArchive"); return; }
      const archive = validateMemoryArchive(JSON.parse(await selected.text())), game = { ...archive.game, gameId: newPracticeId() };
      await memoryStore.save(game, archive.pack, null); navigate(`/practice/memory/${game.gameId}`);
    } catch (error) { setError(error,"importFailed"); }
    finally { setBusy(false); if (file.current) file.current.value = ""; }
  }
  async function remove(id: string) {
    if (!(await confirmAction({ns:"practice",key:"removeConfirm"},"remove"))) return;
    try { await memoryStore.remove(id); setGames(items => items.filter(g => g.gameId !== id)); } catch { setMessage("removeFailed"); }
  }
  return <div className="uma-prose mx-auto space-y-5">
    <header className="uma-panel relative overflow-hidden p-5 sm:p-7"><div aria-hidden="true" className="pointer-events-none absolute inset-0 bg-[url('/assets/ui/scenes/lobby-classroom.jpg')] bg-cover bg-center opacity-10" /><div className="relative"><h1 className="u-fluid-title">{t("title")}</h1><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("summary")}</p></div></header>
    <section className="uma-panel space-y-4 p-5" aria-labelledby="memory-practice-heading"><h2 id="memory-practice-heading" className="text-lg font-bold">{tp("practice")}</h2><p className="text-sm leading-6 text-[var(--color-muted)]">{t("hint")}</p>
      {warning ? <Alert tone="warning" title={tp("warning")}>{warning}</Alert> : null}
      {loading ? <p role="status">{tp("loading")}</p> : <Button variant="primary" size="primary" onClick={() => void (pack ? start() : prepare())} disabled={busy} block>{pack ? <Play className="size-4" aria-hidden="true" /> : <Download className="size-4" aria-hidden="true" />}{busy ? tp("busy") : pack ? t("startRound") : t("prepare")}</Button>}
      {message ? <p role="status" className="text-sm leading-6">{message}</p> : null}
      <div className="flex flex-wrap gap-2 border-t border-[var(--color-line)] pt-3">{pack ? <Button variant="ghost" onClick={() => void prepare()} disabled={busy}>{tp("refresh")}</Button> : null}<Button variant="ghost" onClick={() => file.current?.click()} disabled={busy}><Upload className="size-4" aria-hidden="true" />{tp("import")}</Button><Button asChild variant="secondary"><Link to="/?form=multiplayer">{tp("friends")}</Link></Button><input ref={file} type="file" accept="application/json,.json" aria-label={tp("file")} className="sr-only" onChange={e => void importRecord(e.target.files?.[0])} /></div>
    </section>
    {games.length ? <section className="uma-panel p-5"><h2 className="text-lg font-bold">{tp("history")}</h2><ul className="mt-3 divide-y divide-[var(--color-line)]">{games.slice(0, limit).map(g => <li key={g.gameId} className="flex flex-wrap items-center gap-2 py-3"><p className="min-w-0 flex-1 text-sm">{formatDateTime(g.updatedAt)}</p><Button asChild variant="secondary"><Link to={`/practice/memory/${g.gameId}`}>{tp("open")}</Link></Button><Button variant="ghost" onClick={() => void remove(g.gameId)}>{tp("remove")}</Button></li>)}</ul>{games.length > limit ? <Button variant="ghost" onClick={() => setLimit(n => n + 20)}>{tp("more")}</Button> : null}</section> : null}
    <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{t("rulesTitle")}</summary><div className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]">{(t("rules",{returnObjects:true}) as string[]).map((rule,i)=><p key={i}>{rule}</p>)}</div></details>
  </div>;
}
