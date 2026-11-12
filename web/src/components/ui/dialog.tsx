// Portal rendering keeps the modal outside transformed or clipped page containers
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;
export const DialogPortal = DialogPrimitive.Portal;

export function DialogOverlay({ className, ...props }: ComponentProps<typeof DialogPrimitive.Overlay>) {
  return (
    <DialogPrimitive.Overlay
      className={cn("uma-dialog-overlay fixed inset-0 z-[var(--z-sheet)]", className)}
      {...props}
    />
  );
}

export function DialogContent({ className, children, ...props }: ComponentProps<typeof DialogPrimitive.Content>) {
  const { t } = useTranslation("common");
  return (
    <DialogPortal>
      <DialogOverlay />
      <DialogPrimitive.Content
        className={cn(
          "fixed left-1/2 top-1/2 z-[var(--z-sheet)] w-[min(100%-2rem,32rem)] -translate-x-1/2 -translate-y-1/2",
          "max-h-[calc(100dvh-2rem)] overflow-y-auto overscroll-contain",
          "rounded-[var(--radius-panel)] border border-[var(--color-line)] bg-[var(--color-panel)] p-5",
          "shadow-[var(--shadow-panel)]",
          "pb-[max(1.25rem,env(safe-area-inset-bottom,0px))]",
          className,
        )}
        {...props}
      >
        {children}
        <DialogPrimitive.Close
          className={cn(
            "absolute right-2 top-2 inline-flex size-11 items-center justify-center rounded-[var(--radius-control)]",
            "text-[var(--color-muted)] hover:bg-[var(--color-panel-soft)] hover:text-[var(--color-ink)]",
            "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",
          )}
        >
          <X aria-hidden="true" className="size-5" />
          <span className="sr-only">{t("actions.close")}</span>
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPortal>
  );
}

export function DialogHeader({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-1.5 pr-10", className)} {...props} />;
}

export function DialogFooter({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn("mt-5 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end", className)}
      {...props}
    />
  );
}

export function DialogTitle({ className, ...props }: ComponentProps<typeof DialogPrimitive.Title>) {
  return (
    <DialogPrimitive.Title
      className={cn("text-lg font-bold text-[var(--color-ink)]", className)}
      {...props}
    />
  );
}

export function DialogDescription({ className, ...props }: ComponentProps<typeof DialogPrimitive.Description>) {
  return (
    <DialogPrimitive.Description
      className={cn("text-sm leading-6 text-[var(--color-muted)]", className)}
      {...props}
    />
  );
}
