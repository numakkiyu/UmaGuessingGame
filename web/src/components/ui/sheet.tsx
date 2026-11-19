
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";

export const Sheet = DialogPrimitive.Root;
export const SheetTrigger = DialogPrimitive.Trigger;
export const SheetClose = DialogPrimitive.Close;

type Side = "bottom" | "right" | "left";

const SIDE_CLASS: Record<Side, string> = {
  bottom:
    "inset-x-0 bottom-0 max-h-[min(85dvh,44rem)] w-full rounded-t-[var(--radius-panel-lg)] border-t",
  right: "inset-y-0 right-0 h-full w-[min(100%,26rem)] rounded-l-[var(--radius-panel)] border-l",
  left: "inset-y-0 left-0 h-full w-[min(100%,26rem)] rounded-r-[var(--radius-panel)] border-r",
};

export type SheetContentProps = ComponentProps<typeof DialogPrimitive.Content> & {
  side?: Side;
  /** 屏幕阅读器使用的关闭按钮名称 */
  closeLabel?: string;
  /** 始终可见的主操作，与候选列表的滚动区域分开 */
  footer?: ReactNode;
};

export function SheetContent({
  className,
  children,
  side = "bottom",
  closeLabel,
  footer,
  ...props
}: SheetContentProps) {
  const { t } = useTranslation("common");
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="uma-dialog-overlay fixed inset-0 z-[var(--z-sheet)]" />
      <DialogPrimitive.Content
        className={cn(
          "fixed z-[var(--z-sheet)] flex flex-col overflow-hidden",
          "border-[var(--color-line)] bg-[var(--color-panel)] shadow-[var(--shadow-panel)]",
          SIDE_CLASS[side],
          className,
        )}
        {...props}
      >
        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-5">{children}</div>
        {footer ? <div className="shrink-0 border-t border-[var(--color-line)] bg-[var(--color-panel)] px-5 pt-3 pb-[max(1rem,env(safe-area-inset-bottom,0px))]">{footer}</div> : null}
        <DialogPrimitive.Close
          className={cn(
            "absolute right-2 top-2 inline-flex size-11 items-center justify-center rounded-[var(--radius-control)]",
            "text-[var(--color-muted)] hover:bg-[var(--color-panel-soft)] hover:text-[var(--color-ink)]",
            "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",
          )}
        >
          <X aria-hidden="true" className="size-5" />
          <span className="sr-only">{closeLabel ?? t("actions.close")}</span>
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export function SheetHeader({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("mb-4 flex flex-col gap-1.5 pr-10", className)} {...props} />;
}

export function SheetTitle({ className, ...props }: ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title className={cn("text-lg font-bold text-[var(--color-ink)]", className)} {...props} />
  );
}

export function SheetDescription({ className, ...props }: ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      className={cn("text-sm leading-6 text-[var(--color-muted)]", className)}
      {...props}
    />
  );
}

export function SheetFooter({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "mt-4 flex flex-col-reverse gap-2 border-t border-[var(--color-line)] pt-4 sm:flex-row sm:justify-end",
        className,
      )}
      {...props}
    />
  );
}
