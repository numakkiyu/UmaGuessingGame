import { SelectField } from "@/components/ui/select";
// Events require an event room first; keep the two creation steps recoverable independently
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { ArrowRight, Loader2, Plus } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useApi } from "@/lib/api";
import { ApiError, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import { toKnownModeId } from "@/lib/api/types";
import { useModes, usePublicConfig } from "@/lib/api/queries";
import { canPlay } from "@/lib/deployment/capabilities";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";
import "@/i18n/event-create";
import { entertainmentOnlyMode, newEventModeConfig } from "./event-config";
import { LoadingBlock, ErrorView } from "@/components/uma/page-state";
import { clearEventForm, emptyEventForm, readEventForm, saveEventForm, type EventForm } from "./event-create-draft";
import { continueEventCreation, eventCreationExpired, EVENT_CREATION_STORAGE, newEventCreation, readEventCreation, type EventCreation, type StoredCreation } from "./event-creation";

function loadAttempt(): StoredCreation {
  try { return readEventCreation(sessionStorage); } catch { return {kind:"unavailable"}; }
}
type Notice = "identity" | "expired" | "uncertain" | "rejected";

export function CreateEventPage() {
  const { t } = useTranslation("eventCreate");
  const { api, ensureSession, restoreSession, session, sessionLoading } = useApi();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const publicConfig = usePublicConfig(), modesQuery = useModes();
  const eventModes = (modesQuery.data ?? []).filter(mode => mode.availability === "available" && canPlay(publicConfig.data, mode.modeId, "event"));
  const [stored] = useState(loadAttempt);
  const initial = stored.kind === "ready" || stored.kind === "expired" ? stored.value : null;
  const [attempt, setAttempt] = useState<EventCreation | null>(initial);
  const attemptRef = useRef(attempt), inFlight = useRef(false);
  const identity = session?.sessionId ?? null;
  const draftOwner = useRef(identity);
  const [form, setForm] = useState<EventForm>(() => initial ? {
    name:initial.event.name, modeId:initial.room.modeConfig.modeId, players:String(initial.room.maxPlayers), spectators:String(initial.room.maxSpectators), tiePolicy:initial.event.tiePolicy,
  } : readEventForm(identity) ?? emptyEventForm());
  const { name, tiePolicy } = form;
  const chosenId = form.modeId ?? toKnownModeId(params.get("mode") ?? "");
  // An unavailable explicit choice must not silently become a different game.
  const baseMode = chosenId ? eventModes.find(mode => mode.modeId === chosenId) : eventModes.find(mode => mode.modeId === "classic") ?? eventModes[0];
  const displayModeId = attempt?.room.modeConfig.modeId ?? chosenId ?? baseMode?.modeId;
  const modeLabel = (id: string) => t(`${id}.name`, {ns:"modes"});
  const [creating, setCreating] = useState(false), [failure, setFailure] = useState<Failure | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [storageLimited, setStorageLimited] = useState(stored.kind === "unavailable");
  const [blocked, setBlocked] = useState(stored.kind === "expired" || stored.kind === "invalid");
  const [canRevise, setCanRevise] = useState(false), [resetOpen, setResetOpen] = useState(false);
  const maxPlayers = publicConfig.data?.maxRoomMembers ?? 0;
  const maxSpectators = publicConfig.data?.maxRoomSpectators ?? 0;
  const seats = form.players === null ? Math.min(16,maxPlayers) : form.players === "" ? NaN : Number(form.players);
  const audience = form.spectators === null ? Math.min(32,maxSpectators) : form.spectators === "" ? NaN : Number(form.spectators);
  const validSeats = Number.isInteger(seats) && seats >= 1 && seats <= maxPlayers && Number.isInteger(audience) && audience >= 0 && audience <= maxSpectators;
  const trimmed = name.trim(), nameLength = [...trimmed].length;
  const locked = creating || attempt !== null || blocked || sessionLoading;
  const canCreate = !creating && !blocked && !sessionLoading && (attempt !== null || (Boolean(baseMode) && nameLength > 0 && nameLength <= 48 && validSeats && !publicConfig.isError && !modesQuery.isError));

  // Restore choices when the initial anonymous session is resolved. Never
  // remount this form during ensureSession or replace an in-flight command.
  useEffect(() => {
    if (sessionLoading || inFlight.current || attemptRef.current || draftOwner.current === identity) return;
    draftOwner.current = identity;
    setForm(readEventForm(identity) ?? emptyEventForm());
  }, [identity, sessionLoading, creating]);
  function edit(patch: Partial<EventForm>) {
    if (locked) return;
    const next = {...form, ...patch, modeId:patch.modeId ?? form.modeId ?? baseMode?.modeId ?? null};
    setForm(next);
    if (!saveEventForm(identity,next)) setStorageLimited(true);
  }
  function remember(next: EventCreation) {
    attemptRef.current = next; setAttempt(next);
    try { sessionStorage.setItem(EVENT_CREATION_STORAGE, JSON.stringify(next)); } catch { setStorageLimited(true); }
  }
  function reset() {
    // If removal fails, keep the old command so refresh cannot resurrect an
    // unknown attempt after a second event was created.
    try { sessionStorage.removeItem(EVENT_CREATION_STORAGE); } catch { setStorageLimited(true); setResetOpen(false); return; }
    attemptRef.current = null; setAttempt(null); setBlocked(false); setCanRevise(false); setFailure(null); setNotice(null); setResetOpen(false);
  }
  async function create() {
    if (!canCreate || inFlight.current) return;
    inFlight.current = true; setCreating(true); setFailure(null); setNotice(null); setCanRevise(false);
    try {
      let pending = attemptRef.current;
      if (pending) {
        const originalIdentity = session ?? await restoreSession?.();
        if (!originalIdentity || originalIdentity.sessionId !== pending.sessionId) {
          setBlocked(true); setNotice("identity"); return;
        }
        if (eventCreationExpired(pending)) {
          setBlocked(true); setNotice("expired"); return;
        }
      } else {
        const originalIdentity = await ensureSession([...trimmed].slice(0,16).join(""));
        pending = newEventCreation(originalIdentity.sessionId, {
          name: [...t("roomName",{name:trimmed})].slice(0,48).join(""), kind:"event", modeConfig:newEventModeConfig(baseMode!),
          maxPlayers:seats, maxSpectators:audience, allowSpectators:audience > 0, visibility:"invite",
        }, {name:trimmed,tiePolicy});
        remember(pending);
      }
      const eventId = await continueEventCreation(api, pending, pending.sessionId, remember);
      try { sessionStorage.removeItem(EVENT_CREATION_STORAGE); } catch { /* A stale attempt still confirms the same event. */ }
      clearEventForm(draftOwner.current); clearEventForm(pending.sessionId);
      navigate(`/events/${encodeURIComponent(eventId)}/host`, {replace:true});
    } catch (error) {
      setFailure(describeFailure(error));
      const rejected = attemptRef.current?.roomId === null && error instanceof ApiError && [400,422].includes(error.status);
      if (attemptRef.current) setNotice(rejected ? "rejected" : "uncertain");
      setCanRevise(rejected);
    } finally { inFlight.current = false; setCreating(false); }
  }
  const localized = failure ? localizeFailure(failure) : null;
  if ((publicConfig.isLoading || modesQuery.isLoading || sessionLoading) && !attempt && !blocked) return <LoadingBlock label={t("loading")} rows={3}/>;
  if (!attempt && !blocked && (publicConfig.isError || modesQuery.isError)) return <ErrorView failure={describeFailure(publicConfig.error ?? modesQuery.error)} onRetry={()=>{void publicConfig.refetch();void modesQuery.refetch();}}/>;

  return <div className="uma-prose mx-auto flex flex-col gap-6">
    <header><h1 className="u-fluid-title text-[var(--color-ink)]">{t("title")}</h1><p className="mt-2 text-base leading-7 text-[var(--color-muted)]">{t("intro")}</p></header>
    {notice || localized ? <Alert tone="danger" title={notice ? t(`${notice}Title`) : localized!.title}>
      <p>{notice ? t(`${notice}Detail`) : localized!.detail}</p>
      {localized?.diagnosticId ? <p className="mt-1 text-[length:var(--text-aux)]">{t("state.problemId",{ns:"common"})}：<span className="uma-code">{localized.diagnosticId}</span></p> : null}
    </Alert> : null}
    {blocked ? <Alert tone="warning" title={t("blocked")}><p>{t("blockedDetail")}</p></Alert> : attempt ? <Alert tone="info" title={t("pending")}><p>{t("pendingDetail")}</p></Alert> : null}
    {storageLimited ? <Alert tone="warning" title={t("storage")}><p>{t("storageDetail")}</p></Alert> : null}
    {!eventModes.length && !attempt ? <Alert tone="info" title={t("unavailable")}><p>{t("unavailableDetail")}</p></Alert> : null}
    <form noValidate onSubmit={e=>{e.preventDefault();void create();}} className="uma-panel flex flex-col gap-5 p-5">
      <div className="flex flex-col gap-2">
        <Label htmlFor="event-name">{t("name")}</Label>
        <Input id="event-name" value={name} onChange={e=>edit({name:e.target.value})} placeholder={t("placeholder")} disabled={locked} autoComplete="off" aria-describedby="event-name-hint event-name-count" aria-invalid={nameLength>48}/>
        <p id="event-name-hint" className="text-sm leading-6 text-[var(--color-muted)]">{t("nameHint")}</p>
        <p id="event-name-count" className="text-sm text-[var(--color-muted)]" aria-live="polite">{nameLength>48 ? t("nameTooLong") : t("nameCount",{count:nameLength})}</p>
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="event-mode">{t("mode")}</Label>
        <SelectField id="event-mode" value={displayModeId ?? ""} onValueChange={value =>edit({modeId:toKnownModeId(value)})} disabled={locked || !eventModes.length} aria-describedby="event-mode-hint" options={[displayModeId && !eventModes.some(m=>m.modeId===displayModeId) ? ({value:displayModeId,label:t("savedMode",{mode:modeLabel(displayModeId)})}) : null,...eventModes.map(mode=>({value:mode.modeId,label:modeLabel(mode.modeId)}))]} />
        <p id="event-mode-hint" className="text-sm leading-6 text-[var(--color-muted)]">{t("modeHint")}</p>
        {!baseMode && displayModeId && !attempt ? <p role="status" className="text-sm">{t("missingMode")}</p> : null}
        {displayModeId=== "classic" ? <p className="text-sm leading-6 text-[var(--color-muted)]">{t("classic")}</p> : null}
        {entertainmentOnlyMode(displayModeId) ? <p className="text-sm leading-6 text-[var(--color-muted)]">{t("entertainment")}</p> : null}
      </div>
      <fieldset className="grid gap-4 sm:grid-cols-2">
        <legend className="mb-2 text-sm font-semibold">{t("seats")}</legend>
        <div className="space-y-2"><Label htmlFor="event-players">{t("players")}</Label><Input id="event-players" type="number" min={1} max={maxPlayers} step={1} value={form.players ?? seats} onChange={e=>edit({players:e.target.value})} disabled={locked} aria-describedby="event-players-hint"/><p id="event-players-hint" className="text-sm leading-6 text-[var(--color-muted)]">{t("playersHint",{count:maxPlayers})}</p></div>
        <div className="space-y-2"><Label htmlFor="event-spectators">{t("spectators")}</Label><Input id="event-spectators" type="number" min={0} max={maxSpectators} step={1} value={form.spectators ?? audience} onChange={e=>edit({spectators:e.target.value})} disabled={locked} aria-describedby="event-spectators-hint"/><p id="event-spectators-hint" className="text-sm leading-6 text-[var(--color-muted)]">{t("spectatorsHint",{count:maxSpectators})}</p></div>
      </fieldset>
      {!validSeats && !attempt && maxPlayers>0 ? <p role="status" className="text-sm">{t("invalidSeats",{players:maxPlayers,spectators:maxSpectators})}</p> : null}
      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-semibold text-[var(--color-ink)]">{t("tie")}</legend>
        {[{value:"shared-rank",label:"shared",detail:"sharedDetail"},{value:"announced-tiebreak",label:"hostTie",detail:"hostTieDetail"}].map(policy=><label key={policy.value} className={cn("flex min-h-11 cursor-pointer items-start gap-3 rounded-[var(--radius-control)] border-2 px-3 py-2.5",tiePolicy===policy.value ? "border-[var(--color-blue-deep)] bg-[var(--color-page-tint)]" : "border-[var(--color-line-strong)] bg-[var(--color-panel)]")}>
          <input type="radio" name="tie-policy" value={policy.value} checked={tiePolicy===policy.value} onChange={()=>edit({tiePolicy:policy.value as EventForm["tiePolicy"]})} disabled={locked} className="mt-1 size-4 shrink-0"/>
          <span className="min-w-0"><span className="block text-sm font-semibold text-[var(--color-ink)]">{t(policy.label)}</span><span className="mt-0.5 block break-words text-sm leading-6 text-[var(--color-muted)]">{t(policy.detail)}</span></span>
        </label>)}
      </fieldset>
      <div className="flex flex-col gap-2 border-t border-[var(--color-line)] pt-4 sm:flex-row">
        <Button type="submit" variant="primary" size="primary" disabled={!canCreate} block>{creating ? <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin"/> : <Plus aria-hidden="true" className="size-4"/>}{t(creating ? "busy" : attempt ? "confirm" : "create")}</Button>
        <Button asChild variant="secondary" size="primary"><Link to="/">{t("back")}</Link></Button>
      </div>
      {blocked || canRevise ? <Button type="button" variant="secondary" disabled={creating} onClick={()=>canRevise ? reset() : setResetOpen(true)}>{t(canRevise ? "revise" : "reset")}</Button> : null}
    </form>
    <details className="uma-panel p-4"><summary className="min-h-11 cursor-pointer py-2 text-base font-bold text-[var(--color-ink)]">{t("next")}</summary><ol className="mt-2 list-decimal space-y-2 pl-5 text-sm leading-6 text-[var(--color-muted)]">{[1,2,3,4].map(n=><li key={n}>{t(`step${n}`)}</li>)}</ol><Button asChild variant="link" className="mt-2 px-0"><Link to="/help">{t("rules")}<ArrowRight aria-hidden="true" className="size-3.5"/></Link></Button></details>
    <Dialog open={resetOpen} onOpenChange={setResetOpen}><DialogContent><DialogHeader><DialogTitle>{t("resetTitle")}</DialogTitle><DialogDescription>{t("resetDetail")}</DialogDescription></DialogHeader><DialogFooter><Button variant="secondary" onClick={()=>setResetOpen(false)}>{t("keep")}</Button><Button variant="primary" onClick={reset}>{t("reset")}</Button></DialogFooter></DialogContent></Dialog>
  </div>;
}
