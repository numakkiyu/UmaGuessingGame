
import { useTranslation } from "@/i18n";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps, ReactNode } from "react";
import { AlertTriangle, CheckCircle2, Info, ShieldAlert, WifiOff } from "lucide-react";
import { cn } from "@/lib/utils";

const alertVariants = cva("flex gap-3 rounded-[var(--radius-card)] border p-4 text-sm leading-6", {
  variants: {
    tone: {
      // ink on panel-soft：11.72:1
      info: "border-[var(--color-line-strong)] bg-[var(--color-panel-soft)] text-[var(--color-ink)]",
      // ink on brand-green-soft：浅绿上配深字
      success: "border-[var(--color-correct-edge)] bg-[#e8f8ea] text-[var(--color-ink)]",
      // ink on near 浅黄：浅黄上不默认白字
      warning: "border-[var(--color-near-edge)] bg-[#fdf4dd] text-[var(--color-ink)]",
      // ink on danger-soft：深字保证 ≥4.5:1
      danger: "border-[var(--color-danger)] bg-[var(--color-danger-soft)] text-[var(--color-ink)]",
      offline: "border-[var(--color-line-strong)] bg-[var(--color-panel-soft)] text-[var(--color-ink)]",
    },
  },
  defaultVariants: { tone: "info" },
});

const TONE_ICON = {
  info: Info,
  success: CheckCircle2,
  warning: AlertTriangle,
  danger: ShieldAlert,
  offline: WifiOff,
} as const;

export type AlertProps = Omit<ComponentProps<"div">, "title"> &
  VariantProps<typeof alertVariants> & {
    title?: ReactNode;
    children?: ReactNode;
    /** 追加在标题后的操作区 */
    action?: ReactNode;
  };

export function Alert({ className, tone = "info", title, children, action, ...props }: AlertProps) {
  const { t } = useTranslation("common");
  const resolvedTone = tone ?? "info";
  const Icon = TONE_ICON[resolvedTone];
  return (
    <div role="alert" data-tone={resolvedTone} className={cn(alertVariants({ tone }), className)} {...props}>
      <Icon aria-hidden="true" className="mt-0.5 size-5 shrink-0" />
      <div className="min-w-0 flex-1">
        <p className="font-semibold">
          {/* 文字兜底：屏幕阅读器与色盲用户都能区分状态 */}
          <span className="sr-only">{t(`alert.${resolvedTone}`)}：</span>
          {title}
        </p>
        {children ? <div className="mt-1 text-[var(--color-muted)]">{children}</div> : null}
        {action ? <div className="mt-3">{action}</div> : null}
      </div>
    </div>
  );
}

export { alertVariants };
