
import { Link, useLocation } from "react-router";
import { Button } from "@/components/ui/button";
import { useTranslation } from "@/i18n";

export function NotFoundPage() {
  const { t } = useTranslation("common");
  const location = useLocation();
  return (
    <div className="uma-prose mx-auto flex flex-col gap-4 py-8">
      <h1 className="u-fluid-title text-[var(--color-ink)]">{t("state.notFound")}</h1>
      <p className="text-base leading-7 text-[var(--color-ink)]">
        {t("state.notFoundDetail")}
        {" "}<code className="uma-code break-all rounded bg-[var(--color-panel-soft)] px-1.5 py-0.5 text-sm">{location.pathname}</code>
      </p>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Button asChild variant="primary" size="primary">
          <Link to="/">{t("actions.home")}</Link>
        </Button>
        <Button asChild variant="secondary" size="primary">
          <Link to="/help">{t("actions.rules")}</Link>
        </Button>
      </div>
    </div>
  );
}
