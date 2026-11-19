import { useApi } from "@/lib/api";
import { useTranslation } from "@/i18n";
import { useTrainerProfile } from "./trainer-profile";

/** One display name for every entry; local play does not request an online session. */
export function usePlayerName(): string {
  const profile = useTrainerProfile();
  const { session } = useApi();
  const { t } = useTranslation("theme");
  return profile.displayName || session?.displayName || t("profile.defaultName");
}
