import { SelectField } from "@/components/ui/select";
import "@/i18n/teams";
import { useTranslation } from "@/i18n";
import { useRef, useState } from "react";
import { Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useApi } from "@/lib/api";
import { ApiError, createIdempotencyKey, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import { readTeamMembers, readTeams } from "@/lib/api/teams";
import type { components } from "@/lib/api/types";

type Room = components["schemas"]["RoomSnapshot"];
type Controller = components["schemas"]["TeamController"];
type Handover = { teamId: string; body: components["schemas"]["ControlLeaseRequest"]; key: string };
type Selection = { membershipId: string; roundId: string; generation: number };

export function TeamRoundControl({ room, raw, onChanged }: { room: Room | undefined; raw: unknown; onChanged: () => Promise<unknown> }) {
  const { t } = useTranslation("teams");
  const { api } = useApi();
  const [selected, setSelected] = useState<Record<string, Selection>>({});
  const [busy, setBusy] = useState(false), [failure, setFailure] = useState<Failure | null>(null);
  const [pendingTeam, setPendingTeam] = useState<string | null>(null);
  const pending = useRef<Handover | null>(null), inFlight = useRef(false);
  const info = failure ? localizeFailure(failure) : null;
  if (!room || !raw || typeof raw !== "object") return null;
  const view = raw as { teamControllers?: Controller[]; round?: { roundId?: string; phase?: string; modeId?: string }; canSubmit?: boolean };
  const roundId = view.round?.roundId;
  if (!roundId || !view.teamControllers?.length || !["prepared", "briefing", "collecting"].includes(view.round?.phase ?? "")) return null;
  const teams = readTeams(room), members = readTeamMembers(room);
  const canManage = room.viewer.capabilities.some(c => c === "host" || c === "manage-room");
  const own = members.find(m => m.membershipId === room.viewer.membershipId);
  const ownController = view.teamControllers.find(c => c.teamId === own?.teamId);
  if (!canManage && !ownController) return null;
  const hasPending = pendingTeam !== null;
  const controllerName = (controller: Controller) => members.find(m => m.membershipId === controller.membershipId)?.displayName || controller.displayName;
  function selectedMember(c: Controller) {
    const choice = selected[c.teamId];
    return choice && choice.roundId === roundId && choice.generation === c.generation ? choice.membershipId : c.membershipId;
  }
  async function handover(c: Controller) {
    if (inFlight.current || !room || !roundId || (pending.current && pending.current.teamId !== c.teamId)) return;
    inFlight.current = true; setBusy(true); setFailure(null);
    pending.current ??= { teamId: c.teamId, body: { roundId, membershipId: selectedMember(c), expectedGeneration: c.generation }, key: createIdempotencyKey() };
    setPendingTeam(c.teamId);
    try {
      await api.issueControlLease(room.roomId, pending.current.body, pending.current.key);
      pending.current = null; setPendingTeam(null); await onChanged();
    } catch (error) {
      setFailure(describeFailure(error));
      if (error instanceof ApiError && [400, 401, 403, 404, 409, 422, 429].includes(error.status)) {
        pending.current = null; setPendingTeam(null); await onChanged();
      }
    } finally { inFlight.current = false; setBusy(false); }
  }
  const ownTeamSize = members.filter(m => m.teamId === own?.teamId && (m.role === "player" || m.isCaptain)).length;
  const rotation = ownTeamSize > 1 ? view.round?.modeId === "memory" ? t("rotateMemory") : view.round?.modeId === "strategy-race" ? t("rotateStrategy") : view.round?.modeId === "clue-puzzle" ? t("rotateClue") : null : null;
  return <section className="uma-card min-w-0 space-y-3 p-3 sm:p-4" aria-label={t("control")}>
    {ownController ? <div className="flex items-start gap-3">
      <Users className="mt-0.5 size-5 shrink-0 text-[var(--color-brand-green)]" aria-hidden="true" />
      <div className="min-w-0" role="status" aria-live="polite" aria-atomic="true">
        <p className="break-words text-sm font-bold leading-6">{view.canSubmit ? t("yourTurn") : t("theirTurn", { name: controllerName(ownController) || t("teammate") })}</p>
        <p className="break-words text-sm leading-6 text-[var(--color-muted)]">{rotation ?? t("shared")}</p>
      </div>
    </div> : null}
    {canManage ? <details className="min-w-0" open={hasPending || undefined}>
      <summary className="min-h-11 cursor-pointer content-center text-sm font-semibold text-[var(--color-ink)]">{t("arrange")}</summary>
      <div className="space-y-4 border-t border-[var(--color-line)] pt-3">
        <p className="text-sm leading-6 text-[var(--color-muted)]">{t("arrangeHint")}</p>
        <div className="grid min-w-0 gap-4 lg:grid-cols-2">
          {view.teamControllers.map(c => {
            const group = members.filter(m => m.teamId === c.teamId && (m.role === "player" || m.isCaptain));
            const title = teams.find(t => t.teamId === c.teamId)?.label ?? t("unknownTeam");
            const retrying = pendingTeam === c.teamId;
            return <div key={c.teamId} className="min-w-0 space-y-2">
              <p className="break-words text-sm leading-6">{t("current", { team: title, name: controllerName(c) || t("member") })}</p>
              {group.length > 1 ? <div className="flex flex-wrap items-end gap-2">
                <label className="min-w-0 flex-1 space-y-1 text-sm font-semibold"><span>{t("representative", { team: title })}</span>
                  <SelectField value={selectedMember(c)} onValueChange={value => setSelected(old => ({ ...old, [c.teamId]: { membershipId: value, roundId, generation: c.generation } }))} disabled={busy || hasPending} options={[...group.map((m, i) => ({value:m.membershipId,label:m.publicCode ? t("numberedMember", { name: m.displayName, code: m.publicCode }) : group.filter(p => p.displayName === m.displayName).length > 1 ? t("duplicateMember", { name: m.displayName, index: i + 1 }) : m.displayName}))]} />
                </label>
                <Button className="h-auto min-h-11 whitespace-normal" variant="secondary" aria-label={t(retrying ? "retryLabel" : "handoverLabel", { team: title })} disabled={busy || (hasPending && !retrying) || (!hasPending && selectedMember(c) === c.membershipId)} onClick={() => void handover(c)}>{t(busy && retrying ? "confirming" : retrying ? "retry" : "handover")}</Button>
              </div> : null}
            </div>;
          })}
        </div>
      </div>
    </details> : null}
    {info ? <Alert tone="warning" title={info.title}>{info.detail}</Alert> : null}
    {hasPending && !busy ? <p role="status" className="text-sm leading-6">{t("pending")}</p> : null}
  </section>;
}
