
import * as TabsPrimitive from "@radix-ui/react-tabs";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

export const Tabs = TabsPrimitive.Root;

export function TabsList({ className, ...props }: ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      className={cn(
        "flex flex-wrap items-center gap-1 rounded-[var(--radius-control-lg)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-1",
        className,
      )}
      {...props}
    />
  );
}

export function TabsTrigger({ className, ...props }: ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn(
        "inline-flex min-h-11 flex-auto items-center justify-center gap-2 whitespace-nowrap rounded-[var(--radius-control)] px-3 py-2",
        "text-sm font-semibold text-[var(--color-muted)]",
        "transition-colors duration-[var(--duration-quick)] ease-[var(--ease-primary)]",
        "hover:text-[var(--color-ink)]",
        // 选中：深字 + 白底 + 草绿下划线（形状+文字，不只靠颜色）
        "relative",
        "data-[state=active]:bg-[var(--color-panel)] data-[state=active]:text-[var(--color-ink)]",
        "data-[state=active]:shadow-[var(--shadow-soft)]",
        "data-[state=active]:after:absolute data-[state=active]:after:inset-x-3 data-[state=active]:after:bottom-1",
        "data-[state=active]:after:h-0.5 data-[state=active]:after:rounded-full",
        "data-[state=active]:after:bg-[var(--color-brand-green)] data-[state=active]:after:content-['']",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",
        "disabled:pointer-events-none disabled:opacity-60",
        className,
      )}
      {...props}
    />
  );
}

export function TabsContent({ className, ...props }: ComponentProps<typeof TabsPrimitive.Content>) {
  return (
    <TabsPrimitive.Content
      className={cn("mt-4 focus-visible:outline-2 focus-visible:outline-offset-2", className)}
      {...props}
    />
  );
}
