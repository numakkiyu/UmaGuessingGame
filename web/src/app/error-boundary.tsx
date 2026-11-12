
import { isRouteErrorResponse, useRouteError, useNavigate } from "react-router";
import { Button } from "@/components/ui/button";
import { ErrorView, ForbiddenView } from "@/components/uma/page-state";
import { describeFailure } from "@/lib/api/client";
import { useTranslation } from "@/i18n";

export function RouteErrorBoundary() {
  const { t } = useTranslation("common");
  const error = useRouteError();
  const navigate = useNavigate();

  const failure = isRouteErrorResponse(error)
    ? {
        kind: error.status === 403 ? ("forbidden" as const) : ("error" as const),
        title: error.status === 404 ? t("state.notFound") : t("state.loadFailed"),
        detail:
          error.status === 404
            ? t("state.notFoundDetail")
            : t("state.loadFailedDetail"),
        diagnosticId: null,
        retryable: error.status >= 500,
      }
    : describeFailure(error);

  return (
    <div className="mx-auto flex w-full max-w-[40rem] flex-col gap-4 p-6">
      <h1 className="u-fluid-title text-[var(--color-ink)]">{t("state.problem")}</h1>
      {failure.kind === "forbidden" ? (
        <ForbiddenView failure={failure} onRetry={() => navigate(0)} />
      ) : (
        <ErrorView failure={failure} onRetry={() => navigate(0)} retryLabel={t("actions.reload")} />
      )}
      <div className="flex flex-col gap-2 sm:flex-row">
        <Button variant="primary" size="primary" onClick={() => navigate("/")}>
          {t("actions.home")}
        </Button>
        <Button variant="secondary" size="primary" onClick={() => navigate(-1)}>
          {t("actions.back")}
        </Button>
      </div>
    </div>
  );
}
