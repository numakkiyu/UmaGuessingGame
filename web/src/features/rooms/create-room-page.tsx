import "@/i18n/start-requirements";
import { usePlayerName } from "@/lib/player-name";
import "@/i18n/entry";

import { SelectField } from "@/components/ui/select";
import { useMemo, useRef, useState } from "react";
import { useTranslation } from "@/i18n";
import { Link, useNavigate, useSearchParams } from "react-router";
import { Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ErrorView, LoadingBlock } from "@/components/uma/page-state";
import { useApi } from "@/lib/api";
import { createIdempotencyKey, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import { useModes, usePublicConfig } from "@/lib/api/queries";
import { toKnownModeId, type components, type Mode } from "@/lib/api/types";
import { canPlay } from "@/lib/deployment/capabilities";

import { entryTimeKey, friendSeats } from "./mode-entry-details";

type RoomCreate = components["schemas"]["RoomCreate"];

function friendConfig(mode: Mode): RoomCreate["modeConfig"] {
  if (mode.modeId === "classic") return { modeId: "classic", variant: "friend", maxWrongGuesses: 8, winPolicy: "first-correct", durationSeconds: 180, shadowEnabled: true };
  if (mode.modeId === "pedigree") return { modeId: "pedigree", config: { cooperative: true }, rulesVersion: mode.rulesVersion, contentPackId: "", informationStyle: mode.informationStyle };
  if (mode.modeId === "race-rebuild") return { modeId: "race-rebuild", cooperative: true, rulesVersion: mode.rulesVersion, contentPackId: "", informationStyle: "open" };
  if (mode.modeId === "co-race-path") return { modeId: "co-race-path", openBook: true, rulesVersion: mode.rulesVersion, contentPackId: "", informationStyle: "open" };
  return { modeId: mode.modeId, rulesVersion: mode.rulesVersion, contentPackId: "", informationStyle: mode.informationStyle };
}

export function CreateRoomPage() {
  const { t: requirement } = useTranslation("startRequirements");
  const { t } = useTranslation(["entry", "modes", "common"]);
  const { api, ensureSession } = useApi();
  const navigate = useNavigate(), [params] = useSearchParams();
  const config = usePublicConfig(), modes = useModes();
  const choices = useMemo(() => (modes.data ?? []).filter(m => toKnownModeId(m.modeId) && m.availability === "available" && canPlay(config.data, m.modeId, "friend") && friendSeats(m, config.data?.maxRoomMembers ?? 0).max >= Math.max(2, m.minPlayers)), [config.data, modes.data]);
  const [modeId, setModeId] = useState(params.get("mode") ?? "classic");
  const selected = choices.find(m => m.modeId === modeId) ?? choices[0];
  const displayName = usePlayerName();
  const [name, setName] = useState("");
  const [capacity, setCapacity] = useState(8);
  const [busy, setBusy] = useState(false), [storedFailure, setFailure] = useState<Failure | null>(null);
  const [fieldError, setFieldError] = useState<"name" | "capacity" | null>(null);
  // Keep one exact body and key across an uncertain outcome. Changing the form
  // deliberately creates a different request, instead of changing a retry body.
  const pending = useRef<{ body: RoomCreate; key: string } | null>(null);
  const inFlight = useRef(false);
  const failure = storedFailure ? localizeFailure(storedFailure) : null;
  const { min: minPlayers, max: maxPlayers } = friendSeats(selected ?? { modeId: "classic", minPlayers: 2, maxPlayers: 0 }, config.data?.maxRoomMembers ?? 0);
  const seats = Math.min(maxPlayers, Math.max(minPlayers, capacity));
  async function create() {
    if (!selected || maxPlayers < minPlayers || inFlight.current) return;
    if (!displayName.trim() || Array.from(displayName.trim()).length > 24) { setFieldError("name"); return; }
    if (!Number.isInteger(capacity) || !Number.isInteger(seats)) { setFieldError("capacity"); return; }
    setFieldError(null);
    inFlight.current = true; setBusy(true); setFailure(null);
    try {
      // Capture once, before any await. A language change must not alter the
      // default name or the body of a retry whose outcome is still unknown.
      if (!pending.current) pending.current = { body: { name: name.trim() || t("friend.defaultName", { name: displayName.trim() }), kind: "friend", modeConfig: friendConfig(selected), maxPlayers: seats, maxSpectators: 0, allowSpectators: false, visibility: "invite" }, key: createIdempotencyKey() };
      await ensureSession(displayName.trim());
      const response = await api.createRoom(pending.current.body, pending.current.key);
      if (!response.data?.room?.roomId) throw new Error("Missing room creation");
      navigate(`/rooms/${encodeURIComponent(response.data.room.roomId)}`, { replace: true });
    } catch (error) { setFailure(describeFailure(error)); }
    finally { inFlight.current = false; setBusy(false); }
  }
  if (config.isLoading || modes.isLoading) return <LoadingBlock label={t("friend.loading")} rows={3} />;
  if (config.isError || modes.isError) return <ErrorView failure={describeFailure(config.error ?? modes.error)} onRetry={() => { void config.refetch(); void modes.refetch(); }} />;
  return <div className="mx-auto w-full max-w-4xl space-y-5">
    <header className="border-b border-[var(--color-line)] pb-4"><h1 className="u-fluid-title">{t("friend.title")}</h1><p className="mt-2 max-w-2xl text-sm leading-6 text-[var(--color-muted)]">{t("friend.intro")}</p></header>
    {!selected ? <Alert tone="info" title={t("friend.closed")}>{t("friend.closedDetail")}</Alert> : <div className="grid items-start gap-5 md:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
      <form noValidate className="uma-panel min-w-0 space-y-4 p-5" onSubmit={e => { e.preventDefault(); void create(); }}>
        <div className="space-y-1"><Label htmlFor="friend-mode">{t("friend.mode")}</Label><SelectField id="friend-mode" disabled={busy} value={selected.modeId} onValueChange={value => { setModeId(value); pending.current = null; }} options={[...choices.map(m => ({value:m.modeId,label:t(`${m.modeId}.name`, { ns: "modes" })}))]} /></div>
        <div className="space-y-1"><Label htmlFor="friend-room-name">{t("friend.roomName")}</Label><Input id="friend-room-name" value={name} onChange={e => { setName(e.target.value); pending.current = null; }} maxLength={48} placeholder={t("friend.roomPlaceholder")} disabled={busy} /></div>
        <div className="space-y-1"><Label htmlFor="friend-capacity">{t("friend.capacity")}</Label><Input aria-invalid={fieldError === "capacity" || undefined} aria-describedby={fieldError === "capacity" ? "friend-field-error" : undefined} id="friend-capacity" type="number" className="w-28" min={minPlayers} max={Math.max(minPlayers, maxPlayers)} value={maxPlayers < minPlayers ? minPlayers : seats} onChange={e => { setCapacity(Number(e.target.value)); setFieldError(null); pending.current = null; }} disabled={busy || maxPlayers < minPlayers} /><p className="text-sm leading-6 text-[var(--color-muted)]">{maxPlayers < minPlayers ? t("friend.seatsUnavailable") : minPlayers === maxPlayers ? t("friend.seatsExact", { count: minPlayers }) : t("friend.seatsRange", { min: minPlayers, max: maxPlayers })}</p></div>
        {selected?.startRule && selected.startRule !== "standard" ? <p className="text-sm leading-6 text-[var(--color-muted)]">{requirement(selected.startRule)}</p> : null}
        {fieldError ? <Alert tone="warning" title={t("join.check")}><p id="friend-field-error">{fieldError === "name" ? t("join.invalidName", {max:24}) : t("friend.seatsRange", {min:minPlayers,max:maxPlayers})}</p></Alert> : null}
        {failure ? <Alert fault noticeKey={failure.messageKey} tone="warning" title={failure.title}><p>{failure.detail}</p>{failure.diagnosticId ? <p className="mt-2 text-xs"> {t("state.problemId", { ns: "common" })}：{failure.diagnosticId}</p> : null}</Alert> : null}
        <Button type="submit" variant="primary" size="primary" block disabled={busy || !displayName.trim() || maxPlayers < minPlayers || !Number.isInteger(seats)}><Users className="size-4" aria-hidden="true" />{busy ? t("friend.busy") : t("friend.create")}</Button>
      </form>
      <aside className="min-w-0 space-y-4"><section className="uma-card p-5"><h2 className="text-lg font-bold">{t(`${selected.modeId}.name`, { ns: "modes" })}</h2><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t(`${selected.modeId}.description`, { ns: "modes" })}</p><p className="mt-3 text-sm font-semibold">{t(entryTimeKey(selected.modeId, "multiplayer"), { ns: "lobby" })}</p>{selected.modeId === "clue-puzzle" ? <p className="mt-2 text-sm leading-6">{t("friend.clue")}</p> : selected.modeId === "classic" ? <p className="mt-2 text-sm leading-6">{t("friend.classic")}</p> : <p className="mt-2 text-sm leading-6">{t("friend.other")}</p>}</section><details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-semibold">{t("friend.invite")}</summary><p className="mt-3 text-sm leading-6 text-[var(--color-muted)]">{t("friend.inviteDetail")}</p></details></aside>
    </div>}
    <Button asChild variant="secondary"><Link to="/">{t("actions.home", { ns: "common" })}</Link></Button>
  </div>;
}
