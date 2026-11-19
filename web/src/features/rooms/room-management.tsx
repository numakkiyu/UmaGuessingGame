import { useMatchCurtain } from "@/components/uma/match-curtain-provider";
import { SelectField } from "@/components/ui/select";
import "@/i18n/room";
import { useTranslation } from "@/i18n";
import { useRef, useState } from "react";
import { useNavigate } from "react-router";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useApi } from "@/lib/api";
import { ApiError, createIdempotencyKey, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import type { RoomSnapshot } from "@/lib/api/types";

type Pending = { key: string; body: { targetMembershipId: string; expectedRoomVersion: number } };
export function RoomManagement({ room, onChanged }: { room: RoomSnapshot; onChanged: () => unknown }) {
  const { t } = useTranslation("room");
  const curtain = useMatchCurtain();
  const { api } = useApi(), navigate = useNavigate();
  const [target, setTarget] = useState("");
  const [confirm, setConfirm] = useState<"transfer" | "leave" | null>(null);
  const [busy, setBusy] = useState(false), [uncertain, setUncertain] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null), [notice, setNotice] = useState("");
  const pending = useRef<Pending | null>(null), leaveKey = useRef<string | null>(null), inFlight = useRef(false);
  const manager = room.viewer.capabilities.includes("manage-room");
  const label = t(room.kind === "event" ? "host" : "owner");
  const info = failure ? localizeFailure(failure) : null;
  const candidates = room.members.filter(member => member.membershipId !== room.viewer.membershipId && member.role !== "display");
  const selected = candidates.find(member => member.membershipId === target);
  async function act(action: "transfer" | "leave") {
    if (inFlight.current) return;
    if (action === "transfer" && !pending.current && !selected) return;
    inFlight.current = true; setBusy(true); setFailure(null); setNotice("");
    try {
      if (action === "transfer") {
        pending.current ??= { key: createIdempotencyKey(), body: { targetMembershipId: target, expectedRoomVersion: room.roomVersion } };
        await api.transferRoomHost(room.roomId, pending.current.body, pending.current.key);
        pending.current = null; setTarget(""); setNotice("transferred");
      } else {
        leaveKey.current ??= createIdempotencyKey();
        await api.leaveOrRemoveMembership(room.roomId, room.viewer.membershipId, undefined, leaveKey.current);
        const destination = room.kind === "matchmaking" ? "/matchmaking" : "/";
        if (curtain && room.kind === "matchmaking") curtain.show({
          id:`leave-${room.roomId}`,title:t("leave"),description:t("leaveHint"),ready:false,failed:true,
          content:<section><h2>{t("leave")}</h2><p className="mt-3 text-sm">{t("leaveHint")}</p></section>,
          onCancel:()=>{},onCovered:()=>navigate(destination,{replace:true,state:{umaMatchCurtain:true}}),
        });
        else navigate(destination, { replace: true });
      }
      setUncertain(false); setConfirm(null); void onChanged();
    } catch (error) {
      setFailure(describeFailure(error));
      // Keep the original target, version and command after an unknown outcome.
      // A definite refusal allows choosing again after loading current members.
      const unknown = !(error instanceof ApiError) || error.status >= 500;
      setUncertain(unknown);
      if (!unknown) { pending.current = null; leaveKey.current = null; void onChanged(); }
    } finally { inFlight.current = false; setBusy(false); }
  }
  return <details className="uma-card uma-disclosure min-w-0 p-4">
    <summary className="min-h-11 cursor-pointer content-center font-semibold">{t("management")}</summary>
    <div className="mt-3 space-y-4">
      {manager ? <div className="space-y-2"><label className="block text-sm font-semibold" htmlFor={`host-target-${room.roomId}`}>{t("transfer", { role: label })}</label><SelectField id={`host-target-${room.roomId}`} value={target} disabled={busy || uncertain} onValueChange={value => setTarget(value)} className="max-w-lg" options={[({value:"",label:t("chooseSuccessor")}),...candidates.map(member => ({value:member.membershipId,label:t("memberOption", { name: member.nickname ?? member.displayName, code: member.publicCode })}))]} /><p className="text-sm leading-6 text-[var(--color-muted)]">{t("transferHint")}</p><Button variant="secondary" disabled={!selected || busy || uncertain} onClick={() => { setFailure(null); setConfirm("transfer"); }}>{t("transfer", { role: label })}</Button></div> : null}
      <div className="border-t border-[var(--color-line)] pt-3"><p className="mb-2 text-sm leading-6 text-[var(--color-muted)]">{t(manager ? "leaveManager" : "leaveHint", { role: label })}</p><Button variant="secondary" disabled={busy || uncertain} onClick={() => { setFailure(null); setConfirm("leave"); }}>{t("leave")}</Button></div>
      {notice ? <p role="status" className="text-sm text-[var(--color-action-green)]">{t(notice, { role: label })}</p> : null}
    </div>
    <Dialog open={confirm !== null} onOpenChange={open => { if (!open && !busy && !uncertain) { setConfirm(null); setFailure(null); } }}>
      <DialogContent><DialogHeader><DialogTitle>{t(confirm === "transfer" ? "transferConfirm" : "leaveConfirm", { role: label })}</DialogTitle><DialogDescription>{t(confirm === "transfer" ? "transferDetail" : manager ? "leaveManagerDetail" : "leaveDetail", { name: selected?.nickname ?? selected?.displayName ?? t("selectedPartner"), code: selected?.publicCode ?? "" })}</DialogDescription></DialogHeader>
      {info ? <Alert tone="warning" title={uncertain ? t("confirmingPrevious") : info.title}><p>{uncertain ? t("unknown") : info.detail}</p></Alert> : null}
      <DialogFooter><Button variant="secondary" disabled={busy || uncertain} onClick={() => setConfirm(null)}>{t("stay")}</Button><Button disabled={busy || (confirm === "transfer" && !selected && !uncertain)} onClick={() => { if (confirm) void act(confirm); }}>{t(busy ? "confirming" : uncertain ? "confirmPrevious" : confirm === "transfer" ? "confirmTransfer" : "confirmLeave")}</Button></DialogFooter>
      {uncertain && !busy ? <Button variant="ghost" onClick={() => { navigate("/"); }}>{t("homeFirst")}</Button> : null}
      </DialogContent>
    </Dialog>
  </details>;
}
