import { useConfirmation } from "@/lib/confirmation";
import "@/i18n/practice-flow";
import { useTranslation } from "@/i18n";
import { formatDateTime } from "@/i18n/entities";
import { errorPlayerText, resolvePlayerText, usePlayerText, type PlayerText } from "@/i18n/player-text";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { Download, Flag, Play, Upload } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { LoadingBlock } from "@/components/uma/page-state";
import { LocalizedNotice } from "@/lib/player-notice";
import type { PracticeBase } from "@/lib/domain/aptitude-practice";
import { ACTIVITY_CHANNEL, type activityStore } from "@/lib/local/activity-store";
import { prepareOfflineShell } from "@/lib/local/offline-shell";
import { newPracticeId, PracticeConflict, practiceWriterId } from "@/lib/local/practice-store";

type Archive<P, G> = { schemaVersion: 1; pack: P; game: G };
export type PracticeAdapter<P, G extends PracticeBase> = {
  modeId: G["modeId"]; title: string; summary: string; hint: string; prepareLabel: string; startLabel: string; filePrefix: string; rules: readonly string[];
  fetch: (signal: AbortSignal) => Promise<{ data: P }>;
  read: (raw: unknown) => { pack: P; media: readonly { imagePath: string }[] };
  validate: (raw: unknown) => Archive<P, G>; create: (pack: P) => G;
  prepare?: (pack: P, signal: AbortSignal, progress: (done: number, total: number) => void) => Promise<boolean>;
  beforeStart?: (pack: P) => Promise<void>;
  store: ReturnType<typeof activityStore<P, G>>;
  volatile: Map<string, Archive<P, G> & { savedRevision?: number | null }>;
};

export function ActivityPracticeStart<P, G extends PracticeBase>({ adapter: a, createGame, options }: { adapter: PracticeAdapter<P, G>; createGame?: (pack: P) => G; options?: ReactNode }) {
  const {t}=useTranslation("practice");
  const confirmAction = useConfirmation();
  const [warning,setWarning]=usePlayerText("practice"), [message,setMessage,setMessageError]=usePlayerText("practice");
  const navigate = useNavigate(), file = useRef<HTMLInputElement>(null), abort = useRef<AbortController | null>(null);
  const [pack, setPack] = useState<P | null>(null), [games, setGames] = useState<G[]>([]), [limit, setLimit] = useState(20);
  const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false);
  useEffect(() => {
    let cancelled = false;
    void Promise.all([a.store.latestPack(), a.store.list()]).then(([p, g]) => { if (!cancelled) { setPack(p ? a.read(p).pack : null); setGames(g); } }).catch(() => { if (!cancelled) setWarning("storageWarning"); }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; abort.current?.abort(); };
  }, [a,setWarning]);
  async function prepare() {
    if (busy) return; setBusy(true); const c = new AbortController(); abort.current = c; setMessage("preparing");
    try {
      const response = await a.fetch(c.signal), { pack: next, media } = a.read(response.data);
      const ready = a.prepare ? await a.prepare(next, c.signal, (done, total) => setMessage("preparingAudio",{done,total})) : await prepareOfflineShell(media, c.signal, (done, total) => setMessage("preparingAvatars",{done,total}));
      try { await a.store.install(next); setWarning(""); } catch { setWarning("packWarning"); }
      setPack(next); setMessage(ready ? "ready" : "readyKeepOpen");
    } catch (e) { if (!c.signal.aborted) setMessageError(e,"prepareFailed"); }
    finally { setBusy(false); }
  }
  async function start() {
    if (!pack || busy) return; setBusy(true);
    try {
      await a.beforeStart?.(pack);
      const game = (createGame ?? a.create)(pack);
      try { await a.store.save(game, pack, null); } catch { a.volatile.set(game.gameId, { schemaVersion: 1, game, pack, savedRevision: null }); }
      navigate(`/practice/${a.modeId}/${game.gameId}`);
    } catch (e) { setMessageError(e,"startFailed"); setBusy(false); }
  }
  async function importRecord(selected: File | undefined) {
    if (!selected || busy) return; setBusy(true);
    try {
      if (selected.size > 2_000_000) { setMessage("largeArchive"); return; }
      const archive = a.validate(JSON.parse(await selected.text())), game = { ...archive.game, gameId: newPracticeId() };
      await a.store.save(game, archive.pack, null); navigate(`/practice/${a.modeId}/${game.gameId}`);
    } catch (e) { setMessageError(e,"importFailed"); }
    finally { setBusy(false); if (file.current) file.current.value = ""; }
  }
  async function remove(id: string) {
    if (!(await confirmAction({ns:"practice",key:"removeConfirm"},"remove"))) return;
    try { await a.store.remove(id); setGames(items => items.filter(g => g.gameId !== id)); } catch { setMessage("removeFailed"); }
  }
  return <div className="uma-prose mx-auto space-y-5">
    <header className="uma-panel relative overflow-hidden p-5 sm:p-7"><div className="pointer-events-none absolute inset-0 bg-[url('/assets/ui/scenes/lobby-classroom.jpg')] bg-cover bg-center opacity-10" aria-hidden="true" /><div className="relative"><h1 className="u-fluid-title">{a.title}</h1><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{a.summary}</p></div></header>
    <section className="uma-panel space-y-4 p-5" aria-labelledby="activity-start-heading"><h2 id="activity-start-heading" className="text-lg font-bold">{t("practice")}</h2><p className="text-sm leading-6 text-[var(--color-muted)]">{a.hint}</p>
      {warning ? <Alert tone="warning" title={t("warning")}>{warning}</Alert> : null}
      {options}
      {loading ? <p role="status">{t("loading")}</p> : <Button variant="primary" size="primary" block disabled={busy} onClick={() => void (pack ? start() : prepare())}>{pack ? <Play className="size-4" aria-hidden="true" /> : <Download className="size-4" aria-hidden="true" />}{busy ? t("busy") : pack ? a.startLabel : a.prepareLabel}</Button>}
      {message ? <p role="status" className="text-sm leading-6">{message}</p> : null}
      <div className="flex flex-wrap gap-2 border-t border-[var(--color-line)] pt-3">{pack ? <Button variant="ghost" disabled={busy} onClick={() => void prepare()}>{t("refresh")}</Button> : null}<Button variant="ghost" disabled={busy} onClick={() => file.current?.click()}><Upload className="size-4" aria-hidden="true" />{t("import")}</Button><Button asChild variant="secondary"><Link to="/?form=multiplayer">{t("friends")}</Link></Button><input ref={file} type="file" accept="application/json,.json" className="sr-only" aria-label={t("file")} onChange={e => void importRecord(e.target.files?.[0])} /></div>
    </section>
    {games.length ? <section className="uma-panel p-5"><h2 className="text-lg font-bold">{t("history")}</h2><ul className="mt-3 divide-y divide-[var(--color-line)]">{games.slice(0, limit).map(g => <li key={g.gameId} className="flex flex-wrap items-center gap-2 py-3"><p className="min-w-0 flex-1 text-sm">{formatDateTime(g.updatedAt)}</p><Button asChild variant="secondary"><Link to={`/practice/${a.modeId}/${g.gameId}`}>{t("open")}</Link></Button><Button variant="ghost" onClick={() => void remove(g.gameId)}>{t("remove")}</Button></li>)}</ul>{games.length > limit ? <Button variant="ghost" onClick={() => setLimit(n => n + 20)}>{t("more")}</Button> : null}</section> : null}
    <PracticeInstructions rules={a.rules} />
  </div>;
}

export function ActivityPracticeLoader<P, G extends PracticeBase>({ adapter: a, children }: { adapter: PracticeAdapter<P, G>; children: (archive: Archive<P, G>) => ReactNode }) {
  const {t}=useTranslation("practice");
  const { gameId = "" } = useParams<{ gameId: string }>(), [archive, setArchive] = useState<Archive<P, G> | null>(null), [error, setError] = useState<{ gameId: string; message: PlayerText } | null>(null);
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try { const value = a.volatile.get(gameId) ?? await a.store.load(gameId); if (!cancelled) { if (value) setArchive(a.validate(value)); else throw new LocalizedNotice("missingGame"); } }
      catch (e) { if (!cancelled) setError({ gameId, message: errorPlayerText(e,"practice","loadFailed") }); }
    })(); return () => { cancelled = true; };
  }, [a, gameId]);
  if (error?.gameId === gameId) return <div className="uma-stack"><Alert tone="warning" title={t("missingTitle")}>{resolvePlayerText(error.message)}</Alert><Button asChild variant="secondary"><Link to={`/play/${a.modeId}`}>{t("back")}</Link></Button></div>;
  if (!archive || archive.game.gameId !== gameId) return <LoadingBlock label={t("loadingGame")} rows={3} />;
  return <div key={gameId}>{children(archive)}</div>;
}

export function useActivityPractice<P, G extends PracticeBase>(a: PracticeAdapter<P, G>, initial: Archive<P, G>, createNext?: (pack: P) => G) {
  const [message,setMessage,setMessageError]=usePlayerText("practice");
  const navigate = useNavigate(), [game, setGame] = useState(initial.game), [now, setNow] = useState(Date.now);
  const [busy, setBusy] = useState(false), [stale, setStale] = useState(false), [saved, setSaved] = useState(() => !a.volatile.has(initial.game.gameId));
  const inFlight = useRef(false), current = useRef(initial.game), lastSaved = useRef<number | null>(a.volatile.has(initial.game.gameId) ? a.volatile.get(initial.game.gameId)?.savedRevision ?? null : initial.game.revision);
  const confirmAction = useConfirmation(`${game.gameId}:${game.revision}:${stale}:${busy}:${game.ended || now >= game.deadlineAt}`);
  useEffect(() => { const id = window.setInterval(() => setNow(Date.now()), 250); return () => clearInterval(id); }, []);
  useEffect(() => {
    if (typeof BroadcastChannel === "undefined") return;
    const channel = new BroadcastChannel(ACTIVITY_CHANNEL); channel.onmessage = event => { const data = event.data as { gameId?: string; writerId?: string }; if (data.gameId === initial.game.gameId && data.writerId !== practiceWriterId) setStale(true); };
    return () => channel.close();
  }, [initial.game.gameId]);
  async function act(update: (g: G, at: number) => G): Promise<boolean> {
    if (inFlight.current || stale) return false; inFlight.current = true; setBusy(true); setMessage("");
    try {
      const next = update(current.current, Date.now());
      try { await a.store.save(next, initial.pack, lastSaved.current); lastSaved.current = next.revision; a.volatile.delete(next.gameId); setSaved(true); }
      catch (e) { if (e instanceof PracticeConflict) { setStale(true); throw e; } a.volatile.set(next.gameId, { ...initial, game: next, savedRevision: lastSaved.current }); setSaved(false); }
      current.current = next; setGame(next); setNow(Date.now()); return true;
    } catch (e) { setMessageError(e,"actionFailed"); return false; }
    finally { inFlight.current = false; setBusy(false); }
  }
  async function again() {
    if (inFlight.current) return; inFlight.current = true; setBusy(true);
    try { await a.beforeStart?.(initial.pack); const next = (createNext ?? a.create)(initial.pack); try { await a.store.save(next, initial.pack, null); } catch { a.volatile.set(next.gameId, { ...initial, game: next, savedRevision: null }); } navigate(`/practice/${a.modeId}/${next.gameId}`); }
    catch (e) { setMessageError(e,"nextFailed"); }
    finally { inFlight.current = false; setBusy(false); }
  }
  function exportRecord() {
    try { const url = URL.createObjectURL(new Blob([JSON.stringify({ schemaVersion: 1, game, pack: initial.pack })], { type: "application/json" })); const link = document.createElement("a"); link.href = url; link.download = `${a.filePrefix}-${game.gameId}.json`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); }
    catch { setMessage("exportFailed"); }
  }
  async function finish() { if (!inFlight.current && !stale && !game.ended && Date.now() < game.deadlineAt && await confirmAction({ns:"practice",key:"finishConfirm"},"finish")) await act((g, at) => ({ ...g, ended: true, updatedAt: at, revision: g.revision + 1 })); }
  return { game, now, busy, stale, saved, message, act, again, exportRecord, finish };
}
export function PracticeInstructions({ rules }: { rules: readonly string[] }) { const {t}=useTranslation("practice"); return <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{t("rules")}</summary><div className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]">{rules.map(rule => <p key={rule}>{rule}</p>)}<p>{t("persistence")}</p></div></details>; }
export function PracticeGameHeading({ title, modeId, deadlineAt, now, finished }: { title: string; modeId: string; deadlineAt: number; now: number; finished: boolean }) { const {t}=useTranslation("practice"); const seconds = Math.max(0, Math.ceil((deadlineAt - now) / 1000)); return <header className="border-b border-[var(--color-line)] pb-4"><Link to={`/play/${modeId}`} className="text-sm font-semibold text-[var(--color-blue-deep)]">{t("practice")}</Link><div className="mt-2 flex flex-wrap items-center justify-between gap-2"><h1 className="u-fluid-task">{title}</h1><p className="font-bold tabular-nums text-[var(--color-action-green)]">{finished ? t("ended") : t("remaining",{time:`${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`})}</p></div></header>; }
export function PracticeNotices({ saved, stale, message }: { saved: boolean; stale: boolean; message: string }) { const {t}=useTranslation("practice"); return <>{!saved ? <Alert tone="warning" title={t("unsaved")}>{t("unsavedDetail")}</Alert> : null}{stale ? <Alert tone="warning" title={t("stale")} action={<Button variant="secondary" onClick={() => window.location.reload()}>{t("reload")}</Button>}>{t("staleDetail")}</Alert> : null}{message ? <p role="status" className="text-sm leading-6">{message}</p> : null}</>; }
export function PracticeUtilityActions({ finished, busy, stale, exportRecord, finish }: { finished: boolean; busy: boolean; stale: boolean; exportRecord: () => void; finish: () => Promise<void> }) { const {t}=useTranslation("practice"); return <div className="flex flex-wrap gap-2"><Button variant="secondary" onClick={exportRecord}><Download className="size-4" aria-hidden="true" />{t("export")}</Button>{!finished ? <Button variant="ghost" disabled={busy || stale} onClick={() => void finish()}><Flag className="size-4" aria-hidden="true" />{t("finish")}</Button> : null}</div>; }
