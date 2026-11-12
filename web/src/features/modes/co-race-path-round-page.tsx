import "@/i18n/path-online";
import {useMemo, useState} from "react";
import {Link, useParams} from "react-router";
import {ArrowLeft, Loader2, Send, Undo2} from "lucide-react";
import {useTranslation} from "@/i18n";
import {cardMatches, cardName} from "@/i18n/entities";
import {raceDate, raceName} from "@/i18n/race-names";
import {usePlayerText} from "@/i18n/player-text";
import {TeamRoundControl} from "@/features/rooms/team-round-control";
import {AvatarImage} from "@/components/uma/avatar-image";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView, ErrorView, ForbiddenView, LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {useApi} from "@/lib/api";
import {ApiError, describeFailure, localizeFailure, type Failure} from "@/lib/api/client";
import {useCharacters} from "@/lib/api/queries";
import {useRoom} from "@/lib/api/room-queries";
import {edgeCount, explainScore, normalizePathRound, pathDraftValid, type PathEdge, type PathNode, type PathRound} from "@/lib/api/co-race-path";
import {validPathRoute, type PathRouteCommand} from "@/lib/api/game-commands";
import {isTerminalPhase, phaseLabel, publicStateOf, resultsOf, useRoundSnapshot} from "@/lib/api/round-snapshot";
import type {components} from "@/lib/api/types";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {useRealtime} from "@/lib/realtime/use-realtime";
import {RejoinPrompt, isCredentialRefreshFailure} from "./rejoin-prompt";

const PREFIX = "umaguess:path-draft:";
/** Identity/round/subject scoped drafts contain public choices, never credentials. */
export function loadPathDraft(scope: string): string[] | null {
  try {
    const raw = localStorage.getItem(PREFIX + scope);
    if (!raw || raw.length > 8192) return null;
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== "object") return null;
    const {path, savedAt} = value as {path?: unknown; savedAt?: unknown};
    return Array.isArray(path) && path.length >= 1 && path.length <= 7 && path.every(id => typeof id === "string" && id.length > 0 && id.length <= 128) && new Set(path).size === path.length && typeof savedAt === "number" && Number.isSafeInteger(savedAt) && savedAt <= Date.now() + 60000 && savedAt > Date.now() - 86400000 ? path as string[] : null;
  } catch { return null; }
}
export function savePathDraft(scope: string, path: readonly string[]): boolean {
  try { localStorage.setItem(PREFIX + scope, JSON.stringify({path, savedAt: Date.now()})); return true; } catch { return false; }
}
function clearPathDraft(scope: string) { try { localStorage.removeItem(PREFIX + scope); } catch { /* No false persistence claim. */ } }
export function missingPathStep(error: unknown): number | null {
  if (!(error instanceof ApiError) || error.code !== "missing_edge") return null;
  for (const issue of error.fieldErrors) {
    const match = /^modeAction\.characterIds\[([1-6])\]$/.exec(issue.field);
    if (match && issue.message === "missing_edge") return Number(match[1]);
  }
  return null;
}
function edgeMatches(edge: PathEdge, from: string, to: string) { return edge.from === from && edge.to === to || edge.from === to && edge.to === from; }
function publicEvidence(edges: readonly PathEdge[], from: string, to: string) {
  const seen = new Set<string>();
  return edges.filter(edge => { const key = JSON.stringify([edge.raceId, edge.raceName, edge.date]); if (!edgeMatches(edge, from, to) || seen.has(key)) return false; seen.add(key); return true; });
}
function NodeName({node}: {node: PathNode}) {
  const {t} = useTranslation("pathOnline");
  return <span className="inline-flex min-w-0 items-center gap-2"><AvatarImage characterId={node.characterId} src={node.imagePath} alt="" size="sm"/><span className="break-words font-semibold">{node.name ? cardName(node) : t("unknownCard")}</span></span>;
}
function EvidenceLines({evidence}: {evidence: readonly PathEdge[]}) {
  const {t} = useTranslation("pathOnline");
  return evidence.length ? <ul className="space-y-1">{evidence.slice(0, 3).map((edge, i) => <li key={i} className="break-words">{edge.raceName ? raceName(edge.raceName,undefined,{raceId:edge.raceId,date:edge.date}) : t("evidenceRace")}{edge.date ? ` · ${raceDate(edge.date)}` : ""}</li>)}{evidence.length > 3 ? <li>{t("recordCount", {count: evidence.length})}</li> : null}</ul> : <p>{t("evidenceMissing")}</p>;
}
function RouteTrail({path, nameOf}: {path: readonly string[]; nameOf: (id: string) => string}) {
  const {t} = useTranslation("pathOnline");
  return <ol className="flex flex-wrap gap-2" aria-label={t("routeLabel")}>{path.map((id, i) => <li key={id} className="min-w-0 max-w-full break-words rounded-lg border border-[var(--color-line)] bg-[var(--color-panel-soft)] px-3 py-2 text-sm"><span className="mb-1 block text-xs text-[var(--color-muted)]">{i ? t("step", {step: i}) : t("start")}</span><span className="font-semibold">{nameOf(id)}</span></li>)}</ol>;
}

/** An optional lookup preserves the old open-book full-graph search. It never
 * fetches a hidden graph or changes the player's route. */
function EdgeSearchPanel({round, nameOf}: {round: PathRound; nameOf: (id: string) => string}) {
  const {t, i18n} = useTranslation("pathOnline"), [query, setQuery] = useState(""), [selected, setSelected] = useState<string | null>(null), [limit, setLimit] = useState(20);
  const matches = round.nodes.filter(node => query.trim() && cardMatches(node, query)).sort((a, b) => cardName(a).localeCompare(cardName(b), i18n.resolvedLanguage)).slice(0, 12);
  const neighbors = selected ? round.nodes.filter(node => publicEvidence(round.edges, selected, node.characterId).length).sort((a, b) => cardName(a).localeCompare(cardName(b), i18n.resolvedLanguage)) : [];
  return <details className="uma-panel min-w-0 p-4"><summary className="cursor-pointer text-base font-bold">{t("lookUp")}</summary><p className="mt-2 text-sm leading-6">{t("lookUpHint")}</p><label htmlFor="path-lookup" className="mt-3 block text-sm font-semibold">{t("lookUpName")}</label><Input type="search" id="path-lookup" className="mt-1" value={query} onChange={e => { setQuery(e.target.value); setSelected(null); setLimit(20); }} placeholder={t("searchHint")}/>{query.trim() && !matches.length ? <p className="mt-2 text-sm">{t("noMatch")}</p> : null}<ul aria-label={t("lookUpResults")} className="mt-2 flex flex-wrap gap-2">{matches.map(node => <li key={node.characterId}><Button variant={selected === node.characterId ? "primary" : "secondary"} className="h-auto min-h-11 whitespace-normal break-words" aria-pressed={selected === node.characterId} onClick={() => { setSelected(node.characterId); setLimit(20); }}>{nameOf(node.characterId)}</Button></li>)}</ul>{selected ? <section className="mt-3" aria-labelledby="path-lookup-neighbors"><h3 id="path-lookup-neighbors" className="text-sm font-bold">{t("opponents", {name: nameOf(selected)})}</h3><ul className="mt-2 space-y-3">{neighbors.slice(0, limit).map(node => <li key={node.characterId} className="text-sm"><p className="break-words font-semibold">{nameOf(node.characterId)}</p><div className="mt-1 text-xs leading-5 text-[var(--color-muted)]"><EvidenceLines evidence={publicEvidence(round.edges, selected, node.characterId)}/></div></li>)}</ul>{!neighbors.length ? <p className="text-sm">{t("noRecords")}</p> : null}{neighbors.length > limit ? <Button variant="ghost" onClick={() => setLimit(n => n + 20)}>{t("showMore")}</Button> : null}</section> : null}</details>;
}

export function CoRacePathRoundPage() {
  const {t, i18n} = useTranslation("pathOnline"), {roomId} = useParams<{roomId: string}>(), {api, session} = useApi();
  const roomQuery = useRoom(roomId ?? null), roundId = roomQuery.data?.room.currentRound?.roundId ?? null, snapshot = useRoundSnapshot(roundId), characters = useCharacters().data?.items;
  const round = useMemo(() => {
    if (snapshot.meta?.roundId !== roundId) return null;
    const state = publicStateOf(snapshot.raw);
    if (!state) return null;
    const value = normalizePathRound({publicState: state} as unknown as components["schemas"]["RoundView"]);
    if (!value) return null;
    // Identity metadata is public. It never supplies edges, ranks or a solution.
    const localize = (node: PathNode): PathNode => { const known = characters?.find(c => c.characterId === node.characterId); return known ? {...node, name: known.displayName, localizedNames: known.localizedNames} : node; };
    return {...value, nodes: value.nodes.map(localize), start: value.start ? localize(value.start) : null, goal: value.goal ? localize(value.goal) : null};
  }, [snapshot.raw, snapshot.meta?.roundId, roundId, characters]);
  const scope = session && roundId ? `${session.sessionId}:${roundId}:path` : null, draftScope = scope && round?.self?.subjectId ? `${scope}:${round.self.subjectId}` : null;
  const command = useGameCommand(scope, validPathRoute, (body, key) => api.createSubmission(roundId!, body, key));
  const [path, setPath] = useState<string[]>([]), [initialized, setInitialized] = useState<string | null>(null), [query, setQuery] = useState(""), [limit, setLimit] = useState(20);
  const [failure, setFailure] = useState<Failure | null>(null), [rejected, setRejected] = useState(false), [refreshing, setRefreshing] = useState(false), [needsRejoin, setNeedsRejoin] = useState(false), [notice, setNotice] = usePlayerText("pathOnline");
  if (round && draftScope && initialized !== draftScope) {
    const stored = loadPathDraft(draftScope), confirmed = round.self?.path;
    setInitialized(draftScope); setPath(pathDraftValid(stored, round) ? stored : pathDraftValid(confirmed, round) ? confirmed : round.start ? [round.start.characterId] : []); setQuery(""); setLimit(20);
  }
  const realtime = useRealtime({roomId: roomId ?? null, enabled: !!roomId, handlers: {onSnapshotNeeded: () => { void snapshot.refetch(); void roomQuery.refetch(); }}});
  const phase = snapshot.meta?.phase ?? null, ended = isTerminalPhase(phase), cancelled = phase === "cancelled", collecting = phase === "collecting" && !snapshot.meta?.paused;
  const countdown = useCountdown(snapshot.meta?.deadlineAt, collecting), busy = command.busy || refreshing || !!command.pending || command.expired || snapshot.isError;
  const editable = !!round?.self && collecting && snapshot.canSubmit && !busy && !countdown.expired, arrived = path.at(-1) === round?.goal?.characterId, atLimit = edgeCount(path) >= (round?.maxEdges ?? 6);
  const byId = new Map(round?.nodes.map(node => [node.characterId, node]) ?? []);
  const nameOf = (id: string) => { const node = byId.get(id); return node?.name ? cardName(node) : t("unknownCard"); };
  const tail = path.at(-1), changed = !!round?.self?.submitted && JSON.stringify(path) !== JSON.stringify(round.self.path);
  const candidates = (round?.nodes ?? []).filter(node => !path.includes(node.characterId) && (!round?.openBook || round.edges.some(edge => !!tail && edgeMatches(edge, tail, node.characterId))) && cardMatches(node, query)).sort((a, b) => cardName(a).localeCompare(cardName(b), i18n.resolvedLanguage));
  const info = failure ? rejected ? {...failure, title: t("rejectedTitle"), detail: t("rejectedDetail")} : localizeFailure(failure) : null;
  function edit(next: string[]) { if (!editable || !round || !pathDraftValid(next, round)) return; setPath(next); setQuery(""); setLimit(20); setNotice(""); if (draftScope && !savePathDraft(draftScope, next)) setNotice("saveFailed"); }
  async function submit(retry = false) {
    if (!roundId || !round || command.busy || refreshing) return;
    if (!retry && (!editable || !pathDraftValid(path, round) || path.length < 2)) return;
    const body: PathRouteCommand | undefined = retry ? undefined : {phaseVersion: snapshot.meta!.phaseVersion, submissionRevision: snapshot.submissionRevision, controlGeneration: snapshot.controlGeneration, modeAction: {kind: "path.route", characterIds: [...path]}};
    setRefreshing(true); setFailure(null); setRejected(false); setNotice("");
    try {
      const result = await command.run(body); if (!result) return;
      if (!result.ok) {
        const error = result.error;
        if (error instanceof ApiError && error.status === 422 && ["missing_edge", "unknown_character", "bad_start", "repeated_character", "too_long", "invalid_payload"].includes(error.code)) {
          const step = missingPathStep(error); setNotice(step ? "errors.missingStep" : `errors.${error.code}`, step ? {step} : undefined);
        } else setFailure(describeFailure(error));
        if (!result.uncertain && error instanceof ApiError && error.isConflict) await snapshot.refetch();
        if (isCredentialRefreshFailure(error)) setNeedsRejoin(true);
        return;
      }
      if (result.value.data?.outcome !== "accepted") { setRejected(true); setFailure({kind: "error", title: "", detail: "", diagnosticId: result.value.requestId, retryable: true}); return; }
      if (draftScope) clearPathDraft(draftScope); setNotice("accepted"); await snapshot.refreshAfterSubmit();
    } finally { setRefreshing(false); }
  }
  if (!roomId) return <ErrorView failure={{kind: "error", title: t("missingTitle"), detail: t("missingDetail"), diagnosticId: null, retryable: false}}/>;
  if (roomQuery.isLoading) return <LoadingBlock label={t("loadingRoom")} rows={3}/>;
  if (roomQuery.isError) { const error = describeFailure(roomQuery.error); return error.kind === "forbidden" ? <ForbiddenView failure={error} onRetry={() => void roomQuery.refetch()}/> : <ErrorView failure={error} onRetry={() => void roomQuery.refetch()}/>; }
  const connection = realtime.status === "online" ? "online" : realtime.status === "connecting" || realtime.status === "reconnecting" ? "reconnecting" : realtime.status === "stale" ? "stale" : "offline";
  const ownScore = resultsOf(snapshot.raw).flatMap(value => { if (!value || typeof value !== "object") return []; const result = value as Record<string, unknown>; return result.subjectId === round?.self?.subjectId && typeof result.scoreUnits === "number" && Number.isSafeInteger(result.scoreUnits) && result.scoreUnits >= 0 ? [result.scoreUnits] : []; })[0];
  return <div className="uma-stack">
    <TeamRoundControl room={roomQuery.data?.room} raw={snapshot.raw} onChanged={snapshot.refetch}/><ConnectionBar status={connection} detail={roomQuery.data?.room.name ?? t("room")}/>
    <section className="uma-panel p-4"><div className="flex flex-wrap items-start justify-between gap-3"><div className="min-w-0"><h1 className="u-fluid-task">{t("title")}</h1><p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{phaseLabel(phase)}{round ? ` · ${t(round.openBook ? "openMode" : "closedMode")} · ${t("maxSteps", {count: round.maxEdges})}` : ""}{snapshot.meta?.paused ? ` · ${t("paused")}` : ""}</p>{round?.shortestEdges !== null && round?.shortestEdges !== undefined ? <p className="text-sm">{t("shortest", {count: round.shortestEdges})}</p> : null}</div>{collecting && snapshot.meta?.deadlineAt ? <div className="shrink-0"><p className="text-xs">{t("time")}</p><p className={`uma-code text-2xl font-bold ${countdown.expired ? "text-[var(--color-danger)]" : ""}`} aria-live="off">{countdown.text}</p></div> : null}</div></section>
    {command.pending && !command.busy ? <Alert tone="warning" title={t("confirming")}><p>{t("confirmDetail")}</p><Button variant="secondary" className="h-auto min-h-12 whitespace-normal" disabled={refreshing} onClick={() => void submit(true)}>{t("confirmAction")}</Button></Alert> : null}
    {command.expired ? <Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" onClick={() => void snapshot.refetch().then(ok => { if (ok) command.acknowledgeExpired(); })}>{t("latest")}</Button></Alert> : null}
    {command.storageLimited ? <p className="text-sm">{t("limited")}</p> : null}{needsRejoin ? <RejoinPrompt roomId={roomId}/> : null}
    {info ? <Alert tone={info.kind === "timeout" ? "warning" : "danger"} title={info.title}><p>{info.detail}</p>{info.diagnosticId ? <p>{t("problemId")}：<span className="uma-code">{info.diagnosticId}</span></p> : null}<Button variant="secondary" onClick={() => void snapshot.refetch()}>{t("refresh")}</Button></Alert> : null}
    {notice ? <Alert tone="info" title={t("notice")}>{notice}</Alert> : null}
    {!roundId ? <EmptyView title={t("waitingTitle")} detail={t("waitingDetail")}/> : null}{roundId && snapshot.isLoading && !round ? <LoadingBlock label={t("loadingRound")} rows={3}/> : null}{roundId && snapshot.isError ? <ErrorView failure={describeFailure(snapshot.error)} onRetry={() => void snapshot.refetch()}/> : null}
    {round && (!round.start || !round.goal) ? <p>{t("missingEnds")}</p> : null}
    {round?.start && round.goal && !ended ? <>
      <section className="uma-panel p-4" aria-labelledby="path-ends"><h2 id="path-ends" className="text-base font-bold">{t("ends")}</h2><div className="mt-3 grid gap-3 sm:grid-cols-2">{[[round.start, "from"], [round.goal, "to"]].map(([rawNode, key]) => { const node = rawNode as PathNode; return <div key={node.characterId} className="min-w-0 rounded-lg border border-[var(--color-line)] p-3"><p className="mb-2 text-xs font-semibold">{t(key as string)}</p><NodeName node={node}/></div>; })}</div><p className="mt-3 text-sm leading-6 text-[var(--color-muted)]">{t("relation")}</p></section>
      <div className="grid min-w-0 items-start gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]"><div className="min-w-0 space-y-4">
        <section className="uma-panel min-w-0 space-y-3 p-4" aria-labelledby="path-route"><h2 id="path-route" className="text-lg font-bold">{t("route")}</h2><RouteTrail path={path} nameOf={nameOf}/><p className="text-sm">{t("used", {count: edgeCount(path)})}</p><p role="status" className="text-sm leading-6">{t(round.self?.submitted ? changed ? "changed" : "saved" : "noDraft")}</p>{arrived ? <p className="text-sm font-semibold">{t("selectedGoal")}</p> : null}<div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap"><Button variant="secondary" className="h-auto min-h-11 whitespace-normal" disabled={!editable || path.length <= 1} onClick={() => edit(path.slice(0, -1))}><Undo2 aria-hidden="true" className="size-4"/>{t("undoLast")}</Button><Button variant="secondary" className="h-auto min-h-11 whitespace-normal" disabled={!editable} onClick={() => { if (draftScope) setNotice(savePathDraft(draftScope, path) ? "savedDraft" : "saveFailed"); }}>{t("saveDraft")}</Button></div><Button variant="primary" size="primary" className="h-auto min-h-12 whitespace-normal" block disabled={!editable || path.length < 2 || !pathDraftValid(path, round)} onClick={() => void submit()}>{command.busy || refreshing ? <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/> : <Send aria-hidden="true" className="size-4"/>}{t(command.busy || refreshing ? "submitting" : "save")}</Button>{!arrived ? <p className="text-xs leading-5 text-[var(--color-muted)]">{t(path.length < 2 ? "incomplete" : "saveIncomplete")}</p> : null}{!collecting ? <p className="text-sm">{t(snapshot.meta?.paused ? "pausedHint" : phase === "locked" ? "lockedHint" : "beforeStartHint")}</p> : null}</section>
        <section className="uma-panel min-w-0 space-y-3 p-4" aria-labelledby="path-candidates"><h2 id="path-candidates" className="text-lg font-bold">{round.openBook ? t("continueFrom", {name: nameOf(tail ?? "")}) : t("next")}</h2><label htmlFor="path-search" className="block text-sm font-semibold">{t("search")}</label><Input type="search" id="path-search" value={query} onChange={e => { setQuery(e.target.value); setLimit(20); }} placeholder={t("searchHint")} disabled={!editable || arrived || atLimit}/><p className="text-sm leading-6 text-[var(--color-muted)]">{t(arrived ? "goalHint" : round.openBook ? "openHint" : "closedHint")}</p>{atLimit && !arrived ? <p className="text-sm">{t("maxSteps", {count: round.maxEdges})}</p> : null}{!arrived && !atLimit ? <>
          <div className="hidden md:block"><table className="w-full table-fixed text-left text-sm"><caption className="sr-only">{t("candidates")}</caption><thead><tr><th scope="col" className="py-2">{t("character")}</th>{round.openBook ? <th scope="col" className="px-2 py-2">{t("evidence")}</th> : null}<th scope="col" className="w-20 py-2"><span className="sr-only">{t("action")}</span></th></tr></thead><tbody>{candidates.slice(0, limit).map(node => <tr key={node.characterId} className="border-t border-[var(--color-line)]"><td className="py-2"><NodeName node={node}/></td>{round.openBook ? <td className="px-2 py-2 text-xs leading-5"><EvidenceLines evidence={publicEvidence(round.edges, tail ?? "", node.characterId)}/></td> : null}<td className="py-2 text-right"><Button variant="secondary" disabled={!editable} onClick={() => edit([...path, node.characterId])} aria-label={t("connectName", {name: nameOf(node.characterId)})}>{t("connect")}</Button></td></tr>)}</tbody></table></div>
          <ul className="space-y-2 md:hidden" aria-label={t("candidates")}>{candidates.slice(0, limit).map(node => <li key={node.characterId} className="min-w-0 space-y-2 rounded-lg border border-[var(--color-line)] p-3"><NodeName node={node}/><Button variant="secondary" className="h-auto min-h-11 whitespace-normal" block disabled={!editable} onClick={() => edit([...path, node.characterId])} aria-label={t("connectName", {name: nameOf(node.characterId)})}>{t("connect")}</Button>{round.openBook ? <div className="text-xs leading-5 text-[var(--color-muted)]"><EvidenceLines evidence={publicEvidence(round.edges, tail ?? "", node.characterId)}/></div> : null}</li>)}</ul>
          {!candidates.length ? <p role="status" className="text-sm">{t(query ? "noMatch" : "noNext")}</p> : null}{candidates.length > limit ? <Button variant="ghost" disabled={!editable} onClick={() => setLimit(n => n + 20)}>{t("showMore")}</Button> : null}
        </> : null}</section>{round.openBook && round.edges.length ? <EdgeSearchPanel key={roundId} round={round} nameOf={nameOf}/> : null}
      </div><aside className="min-w-0 space-y-4"><section className="uma-card p-4"><h2 className="text-base font-bold">{t("rules")}</h2><ul className="mt-2 space-y-2 text-sm leading-6 text-[var(--color-muted)]">{(t("playRules", {returnObjects: true, seconds: round.questionSeconds ?? 180, max: round.maxEdges}) as string[]).map(rule => <li key={rule}>{rule}</li>)}</ul>{arrived && round.shortestEdges !== null ? <p className="mt-3 text-sm">{t("preview", {score: explainScore(edgeCount(path), round.shortestEdges)})}</p> : null}</section><Button asChild variant="secondary" block><Link to={`/rooms/${roomId}`}>{t("back")}</Link></Button></aside></div>
    </> : null}
    {round && ended ? <PathRevealPanel round={round} cancelled={cancelled} scoreUnits={ownScore}/> : null}
  </div>;
}

export function PathRevealPanel({round, cancelled = false, scoreUnits}: {round: PathRound; cancelled?: boolean; scoreUnits?: number}) {
  const {t} = useTranslation("pathOnline"), answer = round.answer;
  const byId = new Map(round.nodes.map(node => [node.characterId, node])), nameOf = (id: string) => { const node = byId.get(id); return node?.name ? cardName(node) : t("unknownCard"); };
  const actualScore = scoreUnits ?? (round.self?.submitted ? round.self.scoreUnits : 0);
  return <section id="path-result" className="uma-panel min-w-0 p-4 sm:p-5" aria-labelledby="path-result-heading"><h2 id="path-result-heading" className="u-fluid-task">{t(cancelled ? "cancelled" : answer ? "result" : "answerWaiting")}</h2>{cancelled ? <p className="mt-2">{t("cancelledDetail")}</p> : answer ? <>
    {round.self ? <><dl className="mt-3 grid grid-cols-2 gap-3"><div className="rounded-lg border border-[var(--color-line)] p-3"><dt className="text-xs">{t("finalScore")}</dt><dd className="uma-code text-2xl font-bold">{Math.floor(actualScore / 100)}</dd></div><div className="rounded-lg border border-[var(--color-line)] p-3"><dt className="text-xs">{t("used", {count: round.self.edges})}</dt><dd className="text-lg font-bold">{t(round.self.arrived ? "complete" : "notArrived")}</dd></div></dl><section className="mt-4 space-y-3" aria-labelledby="path-confirmed"><h3 id="path-confirmed" className="text-base font-bold">{t("finalRoute")}</h3>{round.self.submitted ? <RouteTrail path={round.self.path} nameOf={nameOf}/> : <p className="text-sm">{t("noScore")}</p>}</section><PathEvidence title={t("yourEvidence")} path={round.self.path} evidence={round.self.evidence} nameOf={nameOf}/></> : null}
    <section className="mt-5 space-y-3" aria-labelledby="path-reference"><h3 id="path-reference" className="text-base font-bold">{t("reference")}</h3><RouteTrail path={answer.shortestPath} nameOf={nameOf}/><PathEvidence title={t("evidence")} path={answer.shortestPath} evidence={answer.evidence} nameOf={nameOf}/></section><p className="mt-3 text-sm leading-6 text-[var(--color-muted)]">{t("scope")}</p><Link to="/sources" className="mt-2 inline-flex min-h-11 items-center text-sm underline">{t("sourceLink")}</Link>
  </> : null}<div className="mt-4 flex flex-col gap-2 sm:flex-row"><Button asChild variant="secondary" className="h-auto min-h-12 whitespace-normal"><Link to="/play/co-race-path">{t("again")}</Link></Button><Button asChild variant="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/">{t("home")}</Link></Button></div></section>;
}
function PathEvidence({title, path, evidence, nameOf}: {title: string; path: readonly string[]; evidence: readonly PathEdge[]; nameOf: (id: string) => string}) {
  const {t} = useTranslation("pathOnline");
  return <section className="mt-4 min-w-0" aria-label={title}><h4 className="text-sm font-bold">{title}</h4>{path.length < 2 ? <p className="mt-2 text-sm">{t("noEvidence")}</p> : <ol className="mt-2 space-y-3">{path.slice(0, -1).map((from, i) => <li key={i} className="min-w-0 rounded-lg border border-[var(--color-line)] p-3"><p className="break-words text-sm font-semibold">{t("evidenceStep", {step: i + 1, from: nameOf(from), to: nameOf(path[i + 1]!)})}</p><div className="mt-1 text-xs leading-5 text-[var(--color-muted)]"><EvidenceLines evidence={publicEvidence(evidence, from, path[i + 1]!)}/></div></li>)}</ol>}</section>;
}
export const PathBackIcon = ArrowLeft;
