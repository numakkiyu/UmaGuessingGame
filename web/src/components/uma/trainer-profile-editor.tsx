import "@/i18n/trainer";
import { lazy, Suspense, useContext, useEffect, useRef, useState } from "react";
import { ChevronDown } from "lucide-react";
import { useTranslation } from "@/i18n";
import { getCsrfToken, getSessionId, useApi } from "@/lib/api";
import { describeFailure } from "@/lib/api/client";
import { readTrainerProfile, saveTrainerProfile } from "@/lib/trainer-profile";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { TrainerAvatarPicker } from "./trainer-avatar-picker";

import { AccountConfigContext } from "@/features/account/account-context";
const AccountButtons = lazy(() => import("@/features/account/account-ui").then(m => ({default:m.AccountButtons})));
const presets = ["suzuka", "teio", "special", "oguri", "gold"] as const;
export default function TrainerProfileEditor({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation("trainer");
  const accounts = useContext(AccountConfigContext);
  const { api, session, setSession } = useApi();
  const latestSession = useRef(session);
  useEffect(() => { latestSession.current = session; }, [session]);
  const [saved] = useState(readTrainerProfile);
  const [name, setName] = useState(saved.displayName || session?.displayName || "");
  const [avatar, setAvatar] = useState(saved.avatarId);
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const inFlight = useRef(false);
  async function save() {
    if (inFlight.current) return;
    const nextName = name.trim();
    if ([...nextName].length < 1 || [...nextName].length > 24 || [...nextName].some(char => char.codePointAt(0)! < 32 || char.codePointAt(0) === 127)) { setError(t("profile.invalid")); return; }
    inFlight.current = true; setBusy(true); setError("");
    try {
      // Appearance changes never create or restore an online identity.
      const current = session;
      if (current && current.displayName !== nextName) {
        const response = await api.updateCurrentSession({ displayName: nextName });
        if (!response.data?.displayName) throw new Error("Missing profile update response");
        const latest = latestSession.current;
        const token = getCsrfToken();
        if (!latest || latest.sessionId !== current.sessionId || getSessionId() !== current.sessionId || !token) throw new Error(t("profile.failed"));
        setSession({ ...latest, csrfToken: token, displayName: response.data.displayName });
      }
      saveTrainerProfile({ displayName: nextName, avatarId: avatar }); onClose();
    } catch (cause) { setError(describeFailure(cause).detail || t("profile.failed")); }
    finally { inFlight.current = false; setBusy(false); }
  }
  return <Dialog open onOpenChange={value => { if (!value && !inFlight.current) onClose(); }}>
      <DialogContent className="uma-profile-dialog md:w-[min(100%-3rem,54rem)]" aria-describedby={undefined} onEscapeKeyDown={event => { if (busy || (event.target instanceof Element && event.target.closest(".uma-avatar-browser"))) event.preventDefault(); }}>
        <DialogHeader><DialogTitle>{t("profile.title")}</DialogTitle></DialogHeader>
        <form noValidate onSubmit={event => { event.preventDefault(); void save(); }} className="mt-5 space-y-5">
          <div className="space-y-2"><Label htmlFor="trainer-name">{t("profile.nickname")}</Label>
            <div className="uma-nickname-field"><Input id="trainer-name" value={name} onChange={event => { setName(event.target.value); setError(""); }} autoComplete="nickname" placeholder={t("profile.placeholder")} maxLength={48} disabled={busy} aria-invalid={!!error} />
              <Select value="" onValueChange={key => { setName(t(`profile.presetNames.${key}`)); setError(""); }} disabled={busy}>
                <SelectTrigger aria-label={t("profile.presets")}><SelectValue placeholder={<ChevronDown aria-hidden="true" />} /></SelectTrigger>
                <SelectContent>{presets.map(key => <SelectItem key={key} value={key}>{t(`profile.presetNames.${key}`)}</SelectItem>)}</SelectContent>
              </Select>
            </div>
          </div>
          <TrainerAvatarPicker value={avatar} onChange={setAvatar} disabled={busy} />
          {error ? <p role="alert" className="text-sm text-[var(--color-danger)]">{error}</p> : null}
          {accounts?.enabled ? <Suspense fallback={null}><AccountButtons config={accounts} /></Suspense> : null}
          <DialogFooter><Button type="button" variant="ghost" disabled={busy} onClick={() => onClose()}>{t("profile.cancel")}</Button><Button type="submit" variant="primary" disabled={busy}>{t(busy ? "profile.saving" : "profile.save")}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>;
}
