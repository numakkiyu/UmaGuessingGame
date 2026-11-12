
import { Command as CommandPrimitive } from "cmdk";
import { Search } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

export function Command({ className, ...props }: ComponentProps<typeof CommandPrimitive>) {
  return (
    <CommandPrimitive
      className={cn("flex w-full flex-col overflow-hidden text-[var(--color-ink)]", className)}
      {...props}
    />
  );
}

export function CommandInput({ className, ...props }: ComponentProps<typeof CommandPrimitive.Input>) {
  return (
    <div className="flex min-h-11 items-center gap-2 border-b border-[var(--color-line)] px-3">
      <Search aria-hidden="true" className="size-4 shrink-0 text-[var(--color-muted)]" />
      <CommandPrimitive.Input
        className={cn(
          "h-11 w-full bg-transparent text-base text-[var(--color-ink)] outline-none",
          "placeholder:text-[var(--color-muted)]",
          className,
        )}
        {...props}
      />
    </div>
  );
}

export function CommandList({ className, ...props }: ComponentProps<typeof CommandPrimitive.List>) {
  return <CommandPrimitive.List className={cn("max-h-72 overflow-y-auto overscroll-contain p-1", className)} {...props} />;
}

export function CommandEmpty({ className, ...props }: ComponentProps<typeof CommandPrimitive.Empty>) {
  return (
    <CommandPrimitive.Empty
      className={cn("px-3 py-4 text-sm text-[var(--color-muted)]", className)}
      {...props}
    />
  );
}

export function CommandGroup({ className, ...props }: ComponentProps<typeof CommandPrimitive.Group>) {
  return (
    <CommandPrimitive.Group
      className={cn(
        "[&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5",
        "[&_[cmdk-group-heading]]:text-[length:var(--text-aux)] [&_[cmdk-group-heading]]:font-semibold",
        "[&_[cmdk-group-heading]]:text-[var(--color-muted)]",
        className,
      )}
      {...props}
    />
  );
}

export function CommandItem({ className, ...props }: ComponentProps<typeof CommandPrimitive.Item>) {
  return (
    <CommandPrimitive.Item
      className={cn(
        // 触摸区 ≥44px
        "flex min-h-11 cursor-pointer select-none items-center gap-2 rounded-[var(--radius-control)] px-3 py-2",
        "text-base text-[var(--color-ink)] outline-none",
        "data-[selected=true]:bg-[var(--color-panel-soft)]",
        "data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-60",
        className,
      )}
      {...props}
    />
  );
}

export function CommandSeparator({ className, ...props }: ComponentProps<typeof CommandPrimitive.Separator>) {
  return <CommandPrimitive.Separator className={cn("my-1 h-px bg-[var(--color-line)]", className)} {...props} />;
}
