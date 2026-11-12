// Essential rules must remain visible on touchscreens without hovering
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/lib/utils";

export const TooltipProvider = TooltipPrimitive.Provider;
export const Tooltip = TooltipPrimitive.Root;
export const TooltipTrigger = TooltipPrimitive.Trigger;

export function TooltipContent({ className, sideOffset = 6, ...props }: ComponentProps<typeof TooltipPrimitive.Content>) {
  return (
    <TooltipPrimitive.Portal>
      <TooltipPrimitive.Content
        sideOffset={sideOffset}
        className={cn(
          "z-[var(--z-popover)] max-w-64 rounded-[var(--radius-control)] px-3 py-2",
          "bg-[var(--color-ink)] text-sm leading-5 text-[var(--color-panel)]",
          "shadow-[var(--shadow-soft)]",
          className,
        )}
        {...props}
      />
    </TooltipPrimitive.Portal>
  );
}

/** 便捷封装：仅用于补充说明，不承载关键规则 */
export function InfoTip({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
