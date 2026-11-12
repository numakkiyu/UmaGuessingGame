
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

export function Input({ className, type, ...props }: ComponentProps<"input">) {
  return (
    <input
      type={type}
      className={cn(
        "flex min-h-11 w-full rounded-[var(--radius-control)] border border-[var(--color-line-strong)]",
        "bg-[var(--color-panel)] px-3 py-2",
        // 16px：iOS 不会因 <16px 触发自动放大，且满足手机正文要求
        "text-base text-[var(--color-ink)]",
        "placeholder:text-[var(--color-muted)]",
        "transition-colors duration-[var(--duration-instant)] ease-[var(--ease-primary)]",
        "focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--color-blue-deep)]",
        "disabled:cursor-not-allowed disabled:bg-[var(--color-panel-soft)] disabled:opacity-70",
        "aria-invalid:border-[var(--color-danger)]",
        className,
      )}
      {...props}
    />
  );
}

export function Textarea({ className, ...props }: ComponentProps<"textarea">) {
  return (
    <textarea
      className={cn(
        "flex min-h-24 w-full rounded-[var(--radius-control)] border border-[var(--color-line-strong)]",
        "bg-[var(--color-panel)] px-3 py-2 text-base text-[var(--color-ink)]",
        "placeholder:text-[var(--color-muted)]",
        "focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--color-blue-deep)]",
        "disabled:cursor-not-allowed disabled:bg-[var(--color-panel-soft)] disabled:opacity-70",
        "aria-invalid:border-[var(--color-danger)]",
        className,
      )}
      {...props}
    />
  );
}
