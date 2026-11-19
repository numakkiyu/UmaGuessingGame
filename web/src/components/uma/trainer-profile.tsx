import { lazy, Suspense, useState, useContext } from "react";
import { ChevronRight } from "lucide-react";
import { useTranslation } from "@/i18n";
import { usePlayerName } from "@/lib/player-name";
import { TrainerAvatar } from "./trainer-avatar";
import { useTrainerProfile } from "@/lib/trainer-profile";

import { AccountConfigContext } from "@/features/account/account-context";
const AccountStatus = lazy(() => import("@/features/account/account-ui").then(m => ({default:m.AccountStatus})));
const ProfileEditor = lazy(() => import("./trainer-profile-editor"));

export function TrainerProfile({ compact = false }: { compact?: boolean }) {
  const { t } = useTranslation("theme");
  const displayedName = usePlayerName();
  const profile = useTrainerProfile();
  const [open, setOpen] = useState(false);
  const accounts = useContext(AccountConfigContext);
  return <>
    <button data-trainer-settings type="button" className={compact ? "uma-trainer-trigger" : "uma-trainer-card"} onClick={() => setOpen(true)} aria-label={t("profile.open")}>
      <TrainerAvatar id={profile.avatarId} loading="eager" />
      <span><strong>{displayedName}</strong><small>{accounts?.enabled ? <Suspense fallback="…"><AccountStatus /></Suspense> : t("profile.greeting")}</small></span><ChevronRight aria-hidden="true" />
    </button>
    {open ? <Suspense fallback={<p className="uma-profile-loading" role="status">{t("profile.opening")}</p>}><ProfileEditor onClose={() => setOpen(false)} /></Suspense> : null}
  </>;
}
