import "@/i18n/entry";

import { useTranslation } from "@/i18n";
import { useParams } from "react-router";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Link } from "react-router";
import { EmptyView, ErrorView, LoadingBlock } from "@/components/uma/page-state";
import { useModes } from "@/lib/api/queries";
import { toKnownModeId } from "@/lib/api/types";
import { describeFailure } from "@/lib/api/client";
import { describeUnavailable } from "@/lib/copy/unavailable";

export function ModeInfoPage() {
  const { t } = useTranslation(["entry", "modes", "common"]);
  const { modeId } = useParams<{ modeId: string }>();
  const modesQuery = useModes();

  // 未知模式：安全回退，不按未知规则渲染
  const known = modeId ? toKnownModeId(modeId) : null;

  if (!known) {
    return (
      <div className="uma-prose mx-auto flex flex-col gap-4">
        <h1 className="u-fluid-title text-[var(--color-ink)]">{t("info.missing")}</h1>
        <Alert tone="warning" title={t("info.check")}>
          <p>
            {t("info.checkDetail")}
          </p>
        </Alert>
        <Button asChild variant="primary" size="primary">
          <Link to="/">{t("actions.home", { ns: "common" })}</Link>
        </Button>
      </div>
    );
  }

  if (modesQuery.isLoading) {
    return (
      <div className="uma-prose mx-auto">
        <LoadingBlock label={t("info.loading")} rows={3} />
      </div>
    );
  }

  if (modesQuery.isError) {
    return (
      <div className="uma-prose mx-auto">
        <ErrorView failure={describeFailure(modesQuery.error)} onRetry={() => void modesQuery.refetch()} />
      </div>
    );
  }

  const mode = (modesQuery.data ?? []).find((item) => item.modeId === known);

  if (!mode) {
    return (
      <div className="uma-prose mx-auto">
        <EmptyView
          title={t("info.closed", { name: t(`${known}.name`, { ns: "modes" }) })}
          detail={t("info.closedDetail")}
          action={
            <Button asChild variant="secondary">
              <Link to="/">{t("actions.home", { ns: "common" })}</Link>
            </Button>
          }
        />
      </div>
    );
  }

  if (mode.availability !== "available") {
    return (
      <div className="uma-prose mx-auto flex flex-col gap-4">
        <h1 className="u-fluid-title text-[var(--color-ink)]">{t(`${known}.name`, { ns: "modes" })}</h1>
        <p className="text-base leading-7 text-[var(--color-muted)]">{t(`${known}.description`, { ns: "modes" })}</p>
        <Alert tone="warning" title={t("info.unavailable")}>
          {/* 准入诊断要翻译成玩家说法，不能把内部拒绝码直出。 */}
          <p>{describeUnavailable(mode.modeId, mode.unavailableReason)?.detail ?? t("info.unavailableDetail")}</p>
        </Alert>
        <Button asChild variant="secondary" size="primary">
          <Link to="/">{t("actions.home", { ns: "common" })}</Link>
        </Button>
      </div>
    );
  }



  // Fallback for known modes without a matching dedicated route
  return (
    <div className="uma-prose mx-auto flex flex-col gap-4">
      <h1 className="u-fluid-title text-[var(--color-ink)]">{t(`${known}.name`, { ns: "modes" })}</h1>
      <p className="text-base leading-7 text-[var(--color-muted)]">{t(`${known}.description`, { ns: "modes" })}</p>
      <Alert tone="info" title={t("info.fromLobby")}>
        <p>{t("info.fromLobbyDetail")}</p>
      </Alert>
      <Button asChild variant="primary" size="primary">
        <Link to="/">{t("actions.home", { ns: "common" })}</Link>
      </Button>
    </div>
  );
}
