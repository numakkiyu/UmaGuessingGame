import { useTranslation } from "@/i18n";
import "@/i18n/memory";
import "@/i18n/entry";
import "@/i18n/mode-rules";
import { usePlayerName } from "@/lib/player-name";

import { useCallback } from "react";
import { Link } from "react-router";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { StartProgress } from "@/features/modes/start-progress";
import { useStartRound } from "@/features/modes/use-start-round";
import { useModes } from "@/lib/api/queries";
import { describeUnavailable } from "@/lib/copy/unavailable";

const MODE_ID = "memory" as const;

export function MemoryPlayPage() {
  const { t } = useTranslation("memory");
  const { t: entry } = useTranslation("entry");
  const { t: common } = useTranslation("common");
  const { t: rules } = useTranslation("modeRules");
  const modesQuery = useModes();
  const displayName = usePlayerName();

  const controller = useStartRound({
    modeId: MODE_ID,
    modeName: t("title"),
    displayName,
  });

  const mode = (modesQuery.data ?? []).find((item) => item.modeId === MODE_ID);
  // 准入诊断翻译成玩家说法（内部拒绝码不直出）。
  const unavailableCopy = mode && mode.availability !== "available"
    ? describeUnavailable(mode.modeId, mode.unavailableReason)
    : null;

  const onStart = useCallback(() => {
    void controller.start();
  }, [controller]);

  return (
    <div className="uma-prose mx-auto flex flex-col gap-5">
      <header>
        <h1 className="u-fluid-title text-[var(--color-ink)]">{t("title")}</h1>
        <p className="mt-2 text-base leading-7 text-[var(--color-ink)]">
          {t("summary")}
        </p>
      </header>

      {unavailableCopy ? (
        <Alert tone="warning" title={unavailableCopy.title}>
          <p>{unavailableCopy.detail}</p>
        </Alert>
      ) : null}

      <section className="uma-panel flex flex-col gap-4 p-5" aria-labelledby="memory-start">
        <h2 id="memory-start" className="text-base font-bold text-[var(--color-ink)]">
          {entry("start.settings")}
        </h2>

        <Button
          variant="primary"
          size="primary"
          onClick={onStart}
          disabled={controller.running || Boolean(unavailableCopy)}
          block
        >
          {entry(controller.running ? "start.busy" : "start.play")}
        </Button>

        <StartProgress
          steps={controller.steps}
          running={controller.running}
          failure={controller.failure}
          onRetry={onStart}
        />
      </section>

      <details className="uma-card uma-disclosure p-4">
        <summary className="cursor-pointer text-base font-bold text-[var(--color-ink)]">
          {t("rulesTitle")}
        </summary>
        <ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]">
          {(rules("memory.rules", { returnObjects: true }) as string[]).map(rule => <li key={rule}>{rule}</li>)}
        </ul>
      </details>

      <div className="flex flex-col gap-2 border-t border-[var(--color-line)] pt-4 sm:flex-row">
        <Button asChild variant="secondary" size="primary">
          <Link to="/help">{common("actions.rules")}</Link>
        </Button>
        <Button asChild variant="secondary" size="primary">
          <Link to="/">{common("actions.home")}</Link>
        </Button>
      </div>
    </div>
  );
}
