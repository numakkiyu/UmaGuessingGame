import "@/i18n/sort-online";
import { useTranslation } from "@/i18n";
import { cardName } from "@/i18n/entities";
import { onlineSortValue, sortCriterion } from "@/i18n/sort-online";
import { sortPosition } from "@/i18n/sort";
import { usePlayerText } from "@/i18n/player-text";
import { CheckCircle2, Loader2, Send } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams } from "react-router";
import { TeamRoundControl } from "@/features/rooms/team-round-control";
import { ConnectionBar } from "@/components/uma/connection-bar";
import { EmptyView, ErrorView, ForbiddenView, LoadingBlock } from "@/components/uma/page-state";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { SortBoard } from "@/features/modes/sort-board";
import { useApi } from "@/lib/api";
import { initialDraft, mergeWithNext, moveDown, moveUp, normalizeSortRound, sortDraftIssues, splitGroup, type SortDraft, type SortRound } from "@/lib/api/sort";
import { ApiError, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import { validSortOrder, type SortOrderCommand } from "@/lib/api/game-commands";
import { useGameCommand } from "@/lib/hooks/use-game-command";
import { useCharacters } from "@/lib/api/queries";
import { useRoom } from "@/lib/api/room-queries";
import { useRealtime } from "@/lib/realtime/use-realtime";

const DRAFT_STORAGE_PREFIX = "umaguess:sort-draft:";
function loadDraft(roundId: string): SortDraft | null {
  try {
    const raw = localStorage.getItem(DRAFT_STORAGE_PREFIX + roundId);
    if (!raw || raw.length > 8192) return null;
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed) || !parsed.length || !parsed.every(group => Array.isArray(group) && group.length > 0 && group.every(id => typeof id === "string"))) return null;
    return parsed as SortDraft;
  } catch { return null; }
}
function saveDraft(roundId: string, draft: SortDraft): boolean {
  try { localStorage.setItem(DRAFT_STORAGE_PREFIX + roundId, JSON.stringify(draft)); return true; }
  catch { return false; }
}
function clearDraft(roundId: string) {
  try { localStorage.removeItem(DRAFT_STORAGE_PREFIX + roundId); } catch { /* No false persistence claim. */ }
}

export function SortRoundPage() {
  const { t } = useTranslation("sortOnline");
  const { roomId } = useParams<{ roomId: string }>();
  const { api, session } = useApi();
  const roomQuery = useRoom(roomId ?? null);
  const roundId = roomQuery.data?.room.currentRound?.roundId ?? null;
  const roundQuery = useRoundSnapshot(roundId);
  const charactersQuery = useCharacters();
  const round = useMemo(() => {
    const value = normalizeSortRound(roundQuery.data);
    if (!value || value.roundId !== roundId) return null;
    const characters = charactersQuery.data?.items ?? [];
    return { ...value, cards: value.cards.map(card => {
      const known = characters.find(character => character.characterId === card.characterId);
      // Public identity metadata only; never fill raw/display from the index.
      return known ? { ...card, name: known.displayName, localizedNames: known.localizedNames } : card;
    }) };
  }, [roundQuery.data, roundId, charactersQuery.data]);
  const [draft, setDraft] = useState<SortDraft>([]);
  const [initializedFor, setInitializedFor] = useState<string | null>(null);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [rejected, setRejected] = useState(false);
  const [notice, setNotice] = usePlayerText("sortOnline");
  const [draftSaved, setDraftSaved] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const command = useGameCommand(session && roundId ? `${session.sessionId}:${roundId}:sort` : null, validSortOrder,
    (body, key) => api.createSubmission(roundId!, body, key));
  const realtime = useRealtime({roomId:roomId ?? null, enabled:Boolean(roomId), handlers:{onSnapshotNeeded:() => { void roundQuery.refetch(); void roomQuery.refetch(); }}});

  if (round && roundId && initializedFor !== roundId) {
    setInitializedFor(roundId);
    const stored = loadDraft(roundId);
    const valid = stored !== null && sortDraftIssues(stored, round.cards).length === 0;
    setDraft(valid ? stored : round.myOrder && !sortDraftIssues(round.myOrder, round.cards).length ? round.myOrder : initialDraft(round.cards));
    setDraftSaved(valid);
  }
  const ended = !!round && ["revealed", "settled", "cancelled"].includes(round.phase);
  const locked = !round || round.phase !== "collecting" || round.paused || !round.canSubmit;
  const busy = command.busy || refreshing || !!command.pending || command.expired || roundQuery.isError;
  const problems = useMemo(() => round ? sortDraftIssues(draft, round.cards) : [], [draft, round]);
  const cards = round?.cards.map(card => ({...card, name:card.name || t("unknownCard"), display:card.raw !== null ? onlineSortValue(round.criterionKey,card.raw) : null})) ?? [];
  const info = failure ? rejected ? {...failure,title:t("rejectedTitle"),detail:t("rejectedDetail")} : localizeFailure(failure) : null;

  function updateDraft(next: SortDraft) {
    if (locked || busy) return;
    setDraft(next); setNotice("");
    const saved = !!roundId && saveDraft(roundId,next);
    setDraftSaved(saved);
    if (!saved) setNotice("saveFailed");
  }
  function handleSaveDraft() {
    if (!roundId || locked || busy) return;
    const saved = saveDraft(roundId,draft);
    setDraftSaved(saved); setNotice(saved ? "saved" : "saveFailed");
  }
  async function handleSubmit(retry = false) {
    if (!roundId || !round || refreshing || command.busy) return;
    if (!retry && (locked || busy || problems.length)) return;
    const body: SortOrderCommand | undefined = retry ? undefined : {
      phaseVersion:round.phaseVersion, submissionRevision:round.submissionRevision, controlGeneration:round.controlGeneration,
      modeAction:{kind:"sort.order",groups:draft.map(group => [...group])},
    };
    setRefreshing(true); setFailure(null); setRejected(false); setNotice("");
    try {
      const result = await command.run(body);
      if (!result) return;
      if (!result.ok) {
        setFailure(describeFailure(result.error));
        if (!result.uncertain && result.error instanceof ApiError && result.error.isConflict) await roundQuery.refetch();
        return;
      }
      if (result.value.data?.outcome !== "accepted") {
        setRejected(true); setFailure({kind:"error",title:"",detail:"",diagnosticId:result.value.requestId,retryable:true}); return;
      }
      clearDraft(roundId); setDraftSaved(false); setNotice("acceptedHint");
      if (await roundQuery.refetch()) setNotice("");
    } finally { setRefreshing(false); }
  }
  if (!roomId) return <ErrorView failure={{kind:"error",title:t("missingTitle"),detail:t("missingDetail"),diagnosticId:null,retryable:false}}/>;
  if (roomQuery.isLoading) return <LoadingBlock label={t("loadingRoom")} rows={3}/>;
  if (roomQuery.isError) {
    const error = describeFailure(roomQuery.error);
    return error.kind === "forbidden" ? <ForbiddenView failure={error} onRetry={() => void roomQuery.refetch()}/> : <ErrorView failure={error} onRetry={() => void roomQuery.refetch()}/>;
  }
  const connectionStatus = realtime.status === "online" ? "online" : realtime.status === "connecting" || realtime.status === "reconnecting" ? "reconnecting" : realtime.status === "stale" ? "stale" : "offline";
  const criterion = sortCriterion(round?.criterionKey ?? null);
  return <div className="uma-stack">
    <TeamRoundControl room={roomQuery.data?.room} raw={roundQuery.data} onChanged={roundQuery.refetch}/>
    <ConnectionBar status={connectionStatus} detail={roomQuery.data?.room.name ?? t("room")}/>
    <section className="uma-panel p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0"><h1 className="u-fluid-task text-[var(--color-ink)]">{t("title",{criterion})}</h1>
          <p className="mt-1 text-sm text-[var(--color-muted)]">{round ? t("summary",{count:round.cards.length,pairs:round.totalPairs,state:t(round.paused ? "paused" : locked ? "locked" : "adjustable")}) : t("notStarted")}</p></div>
        {round?.submitted ? <span className="inline-flex items-center gap-1.5 rounded-full bg-[var(--color-brand-green-soft)] px-3 py-1 text-sm font-bold"><CheckCircle2 aria-hidden="true" className="size-4"/>{t("submitted")}</span> : null}
      </div>
    </section>
    {/* Recovery controls remain available after phase changes and refresh. */}
    {command.pending && !command.busy ? <Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button variant="secondary" disabled={refreshing} className="h-auto min-h-12 whitespace-normal" onClick={() => void handleSubmit(true)}>{t("confirmAction")}</Button></Alert> : null}
    {command.expired ? <Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" onClick={() => { void roundQuery.refetch().then(ok => { if (ok) command.acknowledgeExpired(); }); }}>{t("latest")}</Button></Alert> : null}
    {round?.submitted && round.myOrderGroupingKnown === false ? <Alert tone="info" title={t("notice")}>{t("legacyOrder")}</Alert> : null}
    {command.storageLimited ? <p className="text-sm text-[var(--color-muted)]">{t("limited")}</p> : null}
    {info ? <Alert tone={info.kind === "timeout" ? "warning" : "danger"} title={info.title} action={<Button variant="secondary" onClick={() => void roundQuery.refetch()}>{t("refresh")}</Button>}><p>{info.detail}</p>{info.diagnosticId ? <p>{t("problemId")}：<span className="uma-code">{info.diagnosticId}</span></p> : null}</Alert> : null}
    {!roundId ? <EmptyView title={t("waitingTitle")} detail={t("waitingDetail")}/> : null}
    {roundId && roundQuery.isLoading && !round ? <LoadingBlock label={t("loadingRound")} rows={2}/> : null}
    {roundId && roundQuery.isError ? <ErrorView failure={describeFailure(roundQuery.error)} onRetry={() => void roundQuery.refetch()}/> : null}
    {round ? ended ? <SortResultPanel round={{...round,cards}}/> : <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,20rem)] lg:items-start">
      <div className="uma-stack uma-stack--tight min-w-0">
        {round.submitted ? <Alert tone="info" title={t("submitted")}>{t(locked ? "lockedHint" : "acceptedHint")}</Alert> : locked && !round.paused ? <Alert tone="info" title={t("locked")}>{t("lockedHint")}</Alert> : null}
        <section className="uma-panel p-4" aria-labelledby="board-heading"><div className="uma-section-head"><h2 id="board-heading" className="text-base font-bold">{t("boardTitle")}</h2><p className="text-sm text-[var(--color-muted)]">{t("direction",{criterion})}</p></div>
          <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t("instructions",{criterion})}</p>
          <div className="mt-3">{!draft.length ? <LoadingBlock label={t("preparing")} rows={3}/> : <SortBoard draft={draft} cards={cards} locked={locked || busy} onChange={updateDraft} onMoveUp={index => updateDraft(moveUp(draft,index))} onMoveDown={index => updateDraft(moveDown(draft,index))} onMerge={index => updateDraft(mergeWithNext(draft,index))} onSplit={index => updateDraft(splitGroup(draft,index))}/>}</div>
        </section>
        <section className="uma-panel p-4"><h2 className="text-base font-bold">{t("submitTitle")}</h2>
          {problems.length ? <Alert tone="warning" title={t("problems",{count:problems.length})} className="mt-3"><ul className="list-disc space-y-1 pl-4">{[...new Set(problems)].map(problem => <li key={problem}>{t(`issues.${problem}`)}</li>)}</ul></Alert> : null}
          {notice ? <Alert tone="info" title={t("notice")} className="mt-3">{notice}</Alert> : null}
          <div className="mt-4 flex flex-col gap-2 sm:flex-row sm:flex-wrap">
            <Button variant="secondary" size="primary" onClick={handleSaveDraft} disabled={busy || locked} className="h-auto min-h-12 whitespace-normal">{t("save")}</Button>
            <Button variant="primary" size="primary" onClick={() => void handleSubmit()} disabled={busy || locked || problems.length > 0} className="h-auto min-h-12 whitespace-normal">{command.busy || refreshing ? <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/> : <Send aria-hidden="true" className="size-4"/>}{t(command.busy || refreshing ? "submitting" : command.pending ? "confirming" : round.submitted ? "resubmit" : "submit")}</Button>
          </div>{draftSaved ? <p className="mt-2 text-sm text-[var(--color-muted)]">{t("savedHint")}</p> : null}
        </section>
      </div>
      <aside className="uma-stack uma-stack--tight min-w-0"><section className="uma-card p-4"><h2 className="text-base font-bold">{t("rules")}</h2><ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]"><li>{t("pairRule")}</li><li>{t("totalRule",{count:round.totalPairs})}</li><li>{t("scoreRule")}</li><li>{t("tieRule",{criterion})}</li><li>{t("revealRule")}</li><li>{t("revisionRule")}</li>{round.criterionKey === "birthday" ? <li>{t("birthdayRule")}</li> : round.criterionKey === "eventDate" || round.criterionKey === "event-date" ? <li>{t("eventRule")}</li> : null}</ul></section><Button asChild variant="secondary" block><Link to={`/rooms/${roomId}`}>{t("back")}</Link></Button></aside>
    </div> : null}
  </div>;
}

export function SortResultPanel({round}:{round:SortRound}) {
  const {t} = useTranslation("sortOnline");
  const revealed = !!round.answer && round.phase !== "cancelled";
  const names = new Map(round.cards.map(card => [card.characterId,card.name ? cardName(card) : t("unknownCard")]));
  const groups: {ids:string[]; value:number | undefined}[] = [];
  for (const id of round.answer?.order ?? []) {
    const value = round.answer?.values.find(entry => entry.characterId === id)?.value;
    const last = groups.at(-1);
    if (last && value !== undefined && last.value === value) last.ids.push(id);
    else groups.push({ids:[id],value});
  }
  return <section id="sort-result" className="uma-panel p-5" tabIndex={-1}>
    <h2 className="u-fluid-task text-[var(--color-ink)]">{t(round.phase === "cancelled" ? "cancelled" : !revealed ? "waitingResult" : round.submitted && round.correctPairs === round.totalPairs ? "complete" : "result")}</h2>
    {round.phase === "cancelled" ? <p className="mt-2">{t("cancelledDetail")}</p> : revealed ? <dl className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3">{[
      [t("score"),Math.floor(round.scoreUnits/100)], [t("correctPairs"),`${round.correctPairs}/${round.totalPairs}`], [t("accuracy"),`${round.totalPairs > 0 ? Math.floor(100*round.correctPairs/round.totalPairs) : 0}%`],
    ].map(([label,value]) => <div key={label} className="rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3"><dt className="text-sm text-[var(--color-muted)]">{label}</dt><dd className="uma-code text-xl font-bold">{value}</dd></div>)}</dl> : null}
    {round.myOrderGroupingKnown === false && round.myOrder ? <p className="mt-3 text-sm text-[var(--color-muted)]">{t("legacyOrder")}</p> : null}
    <div className="mt-4"><h3 className="text-sm font-bold">{t("myOrder")}</h3>{round.myOrder ? <ol className="mt-2 space-y-2 text-sm">{round.myOrder.map((group,index) => { const rank = 1 + round.myOrder!.slice(0,index).reduce((sum,item) => sum + item.length,0); const label = sortPosition(rank,group.length > 1); return <li key={group.join("+")} className="flex flex-wrap gap-x-2"><span className="shrink-0 text-[var(--color-muted)]">{label}</span><span className="min-w-0 break-words">{group.map(id => names.get(id) ?? t("unknownCard")).join(" = ")}</span></li>; })}</ol> : <p className="mt-2 text-sm text-[var(--color-muted)]">{t("noAnswer")}</p>}</div>
    {revealed ? <section className="mt-4" aria-labelledby="sort-reveal-heading"><div className="uma-section-head"><h3 id="sort-reveal-heading" className="text-base font-bold">{t("answer")}</h3><span className="text-sm">{t("revealed")}</span></div><ol className="mt-3 space-y-2">{groups.map((group,index) => {const rank = 1 + groups.slice(0,index).reduce((sum,item) => sum + item.ids.length,0); const label = sortPosition(rank,group.ids.length > 1); return <li key={group.ids.join("+")} className="uma-reveal-row flex flex-wrap items-center gap-2 rounded-[var(--radius-control)] border px-3 py-2"><span className="shrink-0 text-sm text-[var(--color-muted)]">{label}</span><span className="min-w-0 flex-1 break-words text-sm font-semibold">{group.ids.map(id => names.get(id) ?? t("unknownCard")).join(" = ")}</span>{group.value !== undefined ? <span className="text-sm font-bold">{onlineSortValue(round.criterionKey,group.value)}</span> : null}</li>;})}</ol></section> : null}
    <div className="mt-4 flex flex-col gap-2 sm:flex-row sm:flex-wrap"><Button asChild variant="primary" size="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/">{t("home")}</Link></Button><Button asChild variant="secondary" size="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/help">{t("seeRules")}</Link></Button></div>
  </section>;
}

function useRoundSnapshot(roundId:string | null) {
  const {api} = useApi();
  const requestSeq = useRef(0);
  const [state,setState] = useState<{data:Awaited<ReturnType<typeof api.getRound>>["data"] | null;isLoading:boolean;isError:boolean;error:unknown}>({data:null,isLoading:Boolean(roundId),isError:false,error:null});
  const mounted = useRef(true);
  useEffect(() => {mounted.current = true; return () => {mounted.current = false;};},[]);
  const refetch = useCallback(async () => {
    if (!roundId) return false;
    const seq = ++requestSeq.current;
    setState(prev => ({...prev,isLoading:true}));
    try {const response = await api.getRound(roundId); if (mounted.current && seq===requestSeq.current) {setState({data:response.data,isLoading:false,isError:false,error:null}); return true;}}
    catch (error) {if (mounted.current && seq===requestSeq.current) {const denied=error instanceof ApiError&&(error.isUnauthenticated||error.isForbidden||error.isNotFound);setState(prev => ({...prev,data:denied?null:prev.data,isLoading:false,isError:true,error}));}}
    return false;
  },[api,roundId]);
  useEffect(() => {if (!roundId) {setState({data:null,isLoading:false,isError:false,error:null}); return;} void refetch();},[roundId,refetch]);
  return {...state,refetch};
}
