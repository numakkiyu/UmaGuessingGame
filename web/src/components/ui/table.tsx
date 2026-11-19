
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";

export function Table({ className, containerClassName, ...props }: ComponentProps<"table"> & { containerClassName?: string }) {
  const { t } = useTranslation("common");
  return (
    <div
      className={cn("uma-scroll-x w-full rounded-[var(--radius-card)] border border-[var(--color-line)]", containerClassName)}
      tabIndex={0}
      role="region"
      aria-label={t("access.tableScroll")}
    >
      <table className={cn("w-full border-collapse text-left text-sm", className)} {...props} />
    </div>
  );
}

export function TableHeader({ className, ...props }: ComponentProps<"thead">) {
  return <thead className={cn("bg-[var(--color-panel-soft)]", className)} {...props} />;
}

export function TableBody({ className, ...props }: ComponentProps<"tbody">) {
  return <tbody className={cn("divide-y divide-[var(--color-line)]", className)} {...props} />;
}

export function TableRow({ className, ...props }: ComponentProps<"tr">) {
  return <tr className={cn("align-top", className)} {...props} />;
}

export function TableHead({ className, ...props }: ComponentProps<"th">) {
  return (
    <th
      scope="col"
      className={cn(
        "sticky top-0 z-[1] bg-[var(--color-panel-soft)] px-3 py-2 text-[length:var(--text-aux)] font-semibold text-[var(--color-ink)]",
        "whitespace-nowrap",
        className,
      )}
      {...props}
    />
  );
}

export function TableCell({ className, ...props }: ComponentProps<"td">) {
  return <td className={cn("px-3 py-2 text-[var(--color-ink)]", className)} {...props} />;
}

export function TableCaption({ className, ...props }: ComponentProps<"caption">) {
  return <caption className={cn("px-3 py-2 text-[length:var(--text-aux)] text-[var(--color-muted)]", className)} {...props} />;
}
