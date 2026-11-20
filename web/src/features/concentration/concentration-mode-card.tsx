import { Clock3, Sparkles, Users } from "lucide-react";
import { Link } from "react-router";
import { lazy, Suspense } from "react";
const MenuCharacterArt = lazy(() => import("@/components/uma/menu-character-art").then(module => ({ default: module.MenuCharacterArt })));
import { useTranslation } from "@/i18n";
import "@/i18n/concentration-entry";

/** Local-only gameplay is listed independently of room-engine capabilities. */
export function ConcentrationModeCard() {
  const { t } = useTranslation("concentrationEntry"), { t: lobby } = useTranslation("lobby");
  return <Link to="/play/concentration" title={t("available")} className="uma-mode-tile group">
    <Suspense fallback={null}><MenuCharacterArt mode="concentration" className="uma-mode-art" /></Suspense>
    <h3 className="font-bold text-[var(--color-ink)] group-hover:underline">{t("title")}<span className="sr-only">{lobby("availableLabel")}</span></h3>
    <p className="uma-mode-description">{t("description")}</p>
    <dl className="uma-mode-meta"><div className="inline-flex items-center gap-1"><Users className="size-3.5" aria-hidden="true" /><dt className="sr-only">{lobby("players")}</dt><dd>{lobby("playersExact", { count: 1 })}</dd></div><div className="inline-flex items-center gap-1"><Clock3 className="size-3.5" aria-hidden="true" /><dt className="sr-only">{lobby("time")}</dt><dd>{t("untimed")}</dd></div></dl>
    <p className="uma-mode-advice"><span className="font-semibold">{lobby("difficulty")}</span>{t("difficulty")}</p>
    <span className="uma-mode-category"><Sparkles aria-hidden="true" />{lobby("categories.knowledge")}</span>
  </Link>;
}
