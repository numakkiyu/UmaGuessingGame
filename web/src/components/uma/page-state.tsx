// Loading, empty and failed states stay distinct so a failed read is not presented as missing content
import type { ReactNode } from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";

/** 页面级状态机 */
export type PageState<T> =
  | { kind: "loading" }
  | { kind: "ready"; data: T }
  | { kind: "empty"; reason?: string }
  | { kind: "submitting"; data: T }
  | { kind: "stale"; data: T }
  | { kind: "reconnecting"; data: T | null }
  | { kind: "error"; failure: Failure }
  | { kind: "forbidden"; failure: Failure }
  | { kind: "ended"; data: T };

/** 把异常归一化成 error 或 forbidden（403 必须单独走 forbidden，不能显示成空列表） */
export function toErrorState(error: unknown): PageState<never> {
  const failure = describeFailure(error);
  return failure.kind === "forbidden" ? { kind: "forbidden", failure } : { kind: "error", failure };
}

export type StateViewProps = {
  failure: Failure;
  onRetry?: () => void;
  /** 重试按钮文案 */
  retryLabel?: string;
  className?: string;
};

/** forbidden 视图：明确说明权限，不伪装成「暂无数据」 */
export function ForbiddenView({ failure: storedFailure, onRetry, retryLabel, className }: StateViewProps) {
  const { t } = useTranslation("common");
  const failure = localizeFailure(storedFailure);
  return (
    <Alert
      tone="danger"
      diagnosticId={failure.diagnosticId ?? undefined}
      noticeKey={failure.messageKey}
      title={failure.title}
      className={cn("items-start", className)}
      action={
        onRetry ? (
          <Button variant="secondary" onClick={onRetry}>
            {retryLabel ?? t("actions.permissions")}
          </Button>
        ) : null
      }
    >
      <p>{failure.detail}</p>
    </Alert>
  );
}

/** error 视图：区分网络/超时/服务错误，给出可重试动作 */
export function ErrorView({ failure: storedFailure, onRetry, retryLabel, className }: StateViewProps) {
  const { t } = useTranslation("common");
  const failure = localizeFailure(storedFailure);
  const tone = failure.kind === "offline" || failure.kind === "timeout" ? "warning" : "danger";
  return (
    <Alert
      tone={tone}
      fault
      diagnosticId={failure.diagnosticId ?? undefined}
      noticeKey={failure.messageKey}
      title={failure.title}
      className={cn("items-start", className)}
      action={
        onRetry && failure.retryable !== false ? (
          <Button variant="secondary" onClick={onRetry}>
            {retryLabel ?? t("actions.retry")}
          </Button>
        ) : onRetry ? (
          <Button variant="secondary" onClick={onRetry}>
            {retryLabel ?? t("actions.retry")}
          </Button>
        ) : null
      }
    >
      <p>{failure.detail}</p>
    </Alert>
  );
}

/** empty 视图：只有「确认成功但确实没有内容」时才用 */
export function EmptyView({
  title,
  detail,
  action,
  className,
}: {
  title: string;
  detail?: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-start gap-2 rounded-[var(--radius-card)] border border-dashed border-[var(--color-line-strong)] bg-[var(--color-panel-soft)] p-5",
        className,
      )}
    >
      <p className="font-semibold text-[var(--color-ink)]">{title}</p>
      {detail ? <p className="text-sm leading-6 text-[var(--color-muted)]">{detail}</p> : null}
      {action}
    </div>
  );
}

/** loading 骨架：占位结构与最终区域一致，避免按钮跳动 */
export function LoadingBlock({ label, rows = 3, className }: { label?: string; rows?: number; className?: string }) {
  const { t } = useTranslation("common");
  return (
    <div role="status" aria-live="polite" className={cn("flex flex-col gap-3", className)}>
      <span className="sr-only">{label ?? t("state.loading")}</span>
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className="h-12 w-full" />
      ))}
    </div>
  );
}
