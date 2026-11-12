
import { useTranslation } from "@/i18n";
import { Check, Loader2 } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { STEP_ORDER, type StartStep, type StepState } from "@/features/modes/use-start-round";
import { localizeFailure, type Failure } from "@/lib/api/client";
import { cn } from "@/lib/utils";

export type StartProgressProps = {
  steps: Record<StartStep, StepState>;
  running: boolean;
  failure: Failure | null;
  onRetry: () => void;
};

export function StartProgress({ steps, running, failure: storedFailure, onRetry }: StartProgressProps) {
  const { t } = useTranslation(["entry", "common"]);
  const failure = storedFailure ? localizeFailure(storedFailure) : null;
  const visible = running || failure !== null;
  if (!visible) return null;

  return (
    <div className="flex flex-col gap-3">
      <ol className="flex flex-col gap-1.5 rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3">
        {STEP_ORDER.map((step) => {
          const state = steps[step];
          return (
            <li key={step} className="flex items-center gap-2 text-sm">
              {state === "done" ? (
                <Check aria-hidden="true" className="size-4 shrink-0 text-[var(--color-action-green)]" />
              ) : state === "active" ? (
                <Loader2
                  aria-hidden="true"
                  className="size-4 shrink-0 text-[var(--color-blue-deep)] motion-safe:animate-spin"
                />
              ) : state === "failed" ? (
                <span aria-hidden="true" className="inline-block size-4 shrink-0 rounded-full bg-[var(--color-danger)]" />
              ) : (
                <span
                  aria-hidden="true"
                  className="inline-block size-4 shrink-0 rounded-full border border-[var(--color-line-strong)]"
                />
              )}
              <span
                className={cn(
                  "min-w-0",
                  state === "pending" ? "text-[var(--color-muted)]" : "font-semibold text-[var(--color-ink)]",
                )}
              >
                {t(`start.steps.${step}`)}
              </span>
              <span className="sr-only">{t(`start.state.${state}`)}</span>
            </li>
          );
        })}
      </ol>

      {failure ? (
        <Alert
          tone={failure.kind === "timeout" || failure.kind === "offline" ? "warning" : "danger"}
          title={failure.title}
          action={
            <Button variant="secondary" onClick={onRetry}>
              {t("actions.retry", { ns: "common" })}
            </Button>
          }
        >
          <p>{failure.detail}</p>
          {failure.diagnosticId ? (
            <p className="mt-1 text-[length:var(--text-aux)]">
              {t("state.problemId", { ns: "common" })}：<span className="uma-code">{failure.diagnosticId}</span>
            </p>
          ) : null}
        </Alert>
      ) : null}
    </div>
  );
}
