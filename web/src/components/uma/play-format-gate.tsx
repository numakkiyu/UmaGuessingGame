import "@/i18n/gates";
import { useTranslation } from "@/i18n";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { usePublicConfig } from "@/lib/api/queries";
import { anyCanPlay, canPlay, type PlayForm } from "@/lib/deployment/capabilities";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { LoadingBlock, ErrorView } from "@/components/uma/page-state";
import { describeFailure } from "@/lib/api/client";

export function PlayFormatGate({ form, modeId, children }: { form: PlayForm; modeId?: string; children: ReactNode }) {
  const { t } = useTranslation("gates");
  const config = usePublicConfig();
  if (config.isLoading) return <LoadingBlock label={t("loading")} rows={2} />;
  if (config.isError) return <ErrorView failure={describeFailure(config.error)} onRetry={() => void config.refetch()} />;
  if (!(modeId ? canPlay(config.data, modeId, form) : anyCanPlay(config.data, form))) {
    return <div className="uma-stack"><Alert tone="info" title={t("title")}>{t("detail")}</Alert><Button asChild variant="secondary"><Link to="/">{t("home")}</Link></Button></div>;
  }
  return children;
}
