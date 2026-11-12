import { Root, type Label as LabelPrimitive } from "@radix-ui/react-label";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

export function Label({ className, ...props }: ComponentProps<typeof LabelPrimitive>) {
  return (
    <Root
      className={cn(
        "flex items-center gap-2 text-sm font-semibold text-[var(--color-ink)]",
        "leading-6 select-none",
        className,
      )}
      {...props}
    />
  );
}
