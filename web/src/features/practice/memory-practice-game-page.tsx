import { useConfirmation } from "@/lib/confirmation";
import "@/i18n/practice-flow";
import { useTranslation } from "@/i18n";
import { MemoryNotice } from "@/i18n/memory";
import { errorPlayerText, resolvePlayerText, usePlayerText, type PlayerText } from "@/i18n/player-text";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { Download, Flag, RotateCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { LoadingBlock } from "@/components/uma/page-state";
import { MemoryCardButton } from "@/features/modes/memory-card";
import { flipMemory, memoryState, VIEW_MS, type MemoryGame } from "@/lib/domain/memory-practice";
import { createMemoryGame, memoryStore, readMemoryPack, volatileMemoryGames, type MemoryArchive } from "@/lib/local/memory-practice-store";
import { ACTIVITY_CHANNEL } from "@/lib/local/activity-store";
import { PracticeConflict, practiceWriterId } from "@/lib/local/practice-store";

export function MemoryPracticeGamePage() {
  const {t:tp}=useTranslation("practice");
  const { gameId = "" } = useParams<{ gameId: string }>();
  const [archive, setArchive] = useState<MemoryArchive | null>(null), [error, setError] = useState<{ gameId: string; message: PlayerText } | null>(null);
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const value = volatileMemoryGames.get(gameId) ?? await memoryStore.load(gameId);
        if (!cancelled) { if (value) setArchive(value); else setError({ gameId, message: errorPlayerText(new MemoryNotice("missing"),"practice","loadFailed") }); }
      } catch (e) { if (!cancelled) setError({ gameId, message: errorPlayerText(e,"practice","loadFailed") }); }
    })();
    return () => { cancelled = true; };
  }, [gameId]);
  if (error?.gameId === gameId) return <div className="uma-stack"><Alert tone="warning" title={tp("missingTitle")}>{resolvePlayerText(error.message)}</Alert><Button asChild variant="secondary"><Link to="/play/memory">{tp("back")}</Link></Button></div>;
  if (!archive || archive.game.gameId !== gameId) return <LoadingBlock label={tp("loadingGame")} rows={3} />;
  return <MemoryPracticeGame key={gameId} initial={archive} />;
}

function MemoryPracticeGame({ initial }: { initial: MemoryArchive }) {
  const {t}=useTranslation("memory"),{t:tp}=useTranslation("practice");
  const [message,setMessage,setError]=usePlayerText("memory");
  const navigate = useNavigate(), [game, setGame] = useState(initial.game), [now, setNow] = useState(Date.now);
  const [busy, setBusy] = useState(false), [stale, setStale] = useState(false), [saved, setSaved] = useState(() => !volatileMemoryGames.has(initial.game.gameId));
  const [media, setMedia] = useState<"loading" | "ready" | "error">("loading"), [retry, setRetry] = useState(0);
  const inFlight = useRef(false), current = useRef(game);
  const faces = useMemo(() => readMemoryPack(initial.pack).faces, [initial.pack]);
  const state = memoryState(game, now), beforeStart = game.startedAt === null && !state.finished;
  const confirmAction = useConfirmation(`${game.gameId}:${game.revision}:${stale}:${busy}:${state.finished}`);
  const boardImages = useMemo(() => [...new Set(initial.game.board)].map(id => faces.find(f => f.pairId === id)!.imagePath), [initial.game.board, faces]);
  useEffect(() => { current.current = game; }, [game]);
  useEffect(() => { const id = window.setInterval(() => setNow(Date.now()), 100); return () => clearInterval(id); }, []);
  useEffect(() => {
    let cancelled = false;
    const cleanups: (() => void)[] = [];
    // Decode the eight unique faces before the player starts their one viewing window.
    void Promise.all(boardImages.map(src => new Promise<void>((resolve, reject) => {
      const image = new Image();
      const timer = window.setTimeout(() => { cleanup(); reject(new Error("image preparation timed out")); }, 15000);
      const cleanup = () => { clearTimeout(timer); image.onload = null; image.onerror = null; };
      cleanups.push(cleanup);
      image.onload = () => { void image.decode().then(() => { cleanup(); resolve(); }, error => { cleanup(); reject(error); }); };
      image.onerror = error => { cleanup(); reject(error); }; image.src = src;
    }))).then(() => { if (!cancelled) setMedia("ready"); }).catch(() => { if (!cancelled) setMedia("error"); });
    return () => { cancelled = true; cleanups.forEach(cleanup => cleanup()); };
  }, [boardImages, retry]);
  useEffect(() => {
    if (typeof BroadcastChannel === "undefined") return;
    const channel = new BroadcastChannel(ACTIVITY_CHANNEL);
    channel.onmessage = event => {
      const data = event.data as { gameId?: string; writerId?: string };
      if (data.gameId === initial.game.gameId && data.writerId !== practiceWriterId) setStale(true);
    };
    return () => channel.close();
  }, [initial.game.gameId]);
  async function commit(next: MemoryGame) {
    try { await memoryStore.save(next, initial.pack, current.current.revision); setSaved(true); volatileMemoryGames.delete(next.gameId); }
    catch (error) {
      if (error instanceof PracticeConflict) { setStale(true); throw error; }
      volatileMemoryGames.set(next.gameId, { ...initial, game: next }); setSaved(false);
    }
    current.current = next; setGame(next); setNow(Date.now());
  }
  async function action(kind: "start" | "flip" | "finish", index = 0) {
    if (inFlight.current || stale) return;
    if (kind === "finish" && (state.finished || !(await confirmAction({ns:"memory",key:"finishConfirm"},"finish")))) return;
    if (inFlight.current) return;
    inFlight.current = true; setBusy(true); setMessage("");
    try {
      const at = Date.now(), g = current.current;
      if (kind === "start") {
        if (g.startedAt !== null || media !== "ready") return;
        await commit({ ...g, startedAt: at, updatedAt: at, revision: g.revision + 1 });
      } else if (kind === "flip") await commit(flipMemory(g, index, at));
      else await commit({ ...g, ended: true, updatedAt: at, revision: g.revision + 1 });
    } catch (e) { setError(e,"actionFailed"); }
    finally { inFlight.current = false; setBusy(false); }
  }
  async function again() {
    if (inFlight.current) return; inFlight.current = true; setBusy(true);
    try {
      const next = createMemoryGame(initial.pack);
      try { await memoryStore.save(next, initial.pack, null); } catch { volatileMemoryGames.set(next.gameId, { ...initial, game: next }); }
      navigate(`/practice/memory/${next.gameId}`);
    } catch (e) { setError(e,"nextFailed"); }
    finally { inFlight.current = false; setBusy(false); }
  }
  function exportRecord() {
    try {
      const url = URL.createObjectURL(new Blob([JSON.stringify({ schemaVersion: 1, game, pack: initial.pack })], { type: "application/json" }));
      const a = document.createElement("a"); a.href = url; a.download = `umaguess-memory-${game.gameId}.json`; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch { setMessage("exportFailed"); }
  }
  const remaining = game.startedAt === null ? null : Math.max(0, Math.ceil(((state.viewing ? game.startedAt + VIEW_MS : state.deadlineAt!) - now) / 1000));
  return <div className="mx-auto w-full max-w-5xl space-y-4">
    <header className="border-b border-[var(--color-line)] pb-4"><Link to="/play/memory" className="text-sm font-semibold text-[var(--color-blue-deep)]">{tp("practice")}</Link><div className="mt-2 flex flex-wrap items-center justify-between gap-2"><h1 className="u-fluid-task">{t("title")}</h1><p className="font-bold tabular-nums text-[var(--color-action-green)]">{state.finished ? tp("ended") : beforeStart ? t("beforeStart") : t(state.viewing ? "viewingTime" : "answeringTime",{count:remaining ?? 0})}</p></div></header>
    {!saved ? <Alert tone="warning" title={tp("unsaved")}>{tp("unsavedDetail")}</Alert> : null}
    {stale ? <Alert tone="warning" title={tp("stale")} action={<Button variant="secondary" onClick={() => window.location.reload()}>{tp("reload")}</Button>}>{tp("staleDetail")}</Alert> : null}
    {message ? <p role="status" className="text-sm leading-6">{message}</p> : null}
    <div className="grid min-w-0 gap-5 lg:grid-cols-[minmax(0,1fr)_17rem] lg:items-start">
      <section className="uma-panel min-w-0 p-3 sm:p-5" aria-label={t("board")}>
        <p role="status" className="mb-3 min-h-6 text-sm leading-6 text-[var(--color-muted)]">{t(state.finished ? "revealed" : beforeStart ? media === "ready" ? "facesReady" : media === "error" ? "facesError" : "facesLoading" : state.viewing ? "viewing" : state.cooling ? "cooling" : state.first !== null ? "chooseSecond" : "choosePair")}</p>
        <div className="mx-auto grid w-full max-w-[30rem] grid-cols-4 gap-2" aria-label={t("grid")}>
          {game.board.map((pairId, i) => <MemoryCardButton key={i} index={i} card={{ cardId: String(i), position: i, state: state.matched.has(i) ? "matched" : state.visible.has(i) ? "flipped" : "hidden", imagePath: state.visible.has(i) ? faces.find(f => f.pairId === pairId)!.imagePath : null, label: null }} disabled={busy || stale || beforeStart || state.viewing || state.finished || state.cooling || state.first === i || media !== "ready"} onFlip={id => void action("flip", Number(id))} />)}
        </div>
        {beforeStart ? <Button variant="primary" size="primary" className="mt-4" block disabled={busy || stale || media === "loading"} onClick={() => { if (media === "error") { setMedia("loading"); setRetry(n => n + 1); } else void action("start"); }}>{t(media === "loading" ? "loadingFaces" : media === "error" ? "retryFaces" : "startMemory")}</Button> : null}
        {media === "error" && !beforeStart ? <Button variant="secondary" className="mt-3" onClick={() => { setMedia("loading"); setRetry(n => n + 1); }}>{t("retryFaces")}</Button> : null}
      </section>
      <aside className="space-y-4">
        <section className="uma-panel p-4" aria-labelledby="memory-progress"><h2 id="memory-progress" className="text-lg font-bold">{t(state.finished ? "result" : "progress")}</h2><p className="mt-3 text-3xl font-extrabold text-[var(--color-action-green)]">{state.matched.size / 2}<span className="text-base font-medium text-[var(--color-muted)]">{t("pairs")}</span></p><dl className="mt-3 grid grid-cols-2 gap-3 text-sm"><div><dt className="text-[var(--color-muted)]">{t("attempts")}</dt><dd className="mt-1 font-bold">{state.attempts} / 20</dd></div><div><dt className="text-[var(--color-muted)]">{t("wrong")}</dt><dd className="mt-1 font-bold">{state.wrong}</dd></div></dl>{state.finished ? <><h3 className="mt-4 font-bold">{t(state.matched.size === 16 ? "complete" : "ended")}</h3><p className="mt-1 text-xl font-extrabold">{t("score",{score:state.scoreUnits/100})}</p><Button variant="primary" className="mt-4" block onClick={() => void again()} disabled={busy}><RotateCcw className="size-4" aria-hidden="true" />{t("again")}</Button></> : null}</section>
        <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{tp("rules")}</summary><div className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]"><p>{t("shortRules")}</p><p>{t("scoring")}</p><p>{tp("persistence")}</p></div></details>
        <div className="flex flex-wrap gap-2"><Button variant="secondary" onClick={exportRecord}><Download className="size-4" aria-hidden="true" />{tp("export")}</Button>{!state.finished ? <Button variant="ghost" onClick={() => void action("finish")} disabled={busy || stale}><Flag className="size-4" aria-hidden="true" />{tp("finish")}</Button> : null}</div>
      </aside>
    </div>
  </div>;
}
