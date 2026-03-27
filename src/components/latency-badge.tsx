import { formatLatencyLabel, resolveLatencyLevel } from "@/lib/realtime/latency";

type Props = {
  latencyMs?: number | null;
  compact?: boolean;
};

const toneStyles = {
  good: {
    text: "text-[var(--color-correct)]",
    bg: "bg-[rgba(88,165,92,0.16)]",
    dim: "bg-[rgba(88,165,92,0.32)]",
    bar: "bg-[var(--color-correct)]",
  },
  ok: {
    text: "text-[var(--color-near)]",
    bg: "bg-[rgba(214,160,58,0.18)]",
    dim: "bg-[rgba(214,160,58,0.34)]",
    bar: "bg-[var(--color-near)]",
  },
  slow: {
    text: "text-[var(--color-near)]",
    bg: "bg-[rgba(214,160,58,0.18)]",
    dim: "bg-[rgba(214,160,58,0.34)]",
    bar: "bg-[var(--color-near)]",
  },
  poor: {
    text: "text-[#c74f42]",
    bg: "bg-[rgba(199,79,66,0.16)]",
    dim: "bg-[rgba(199,79,66,0.34)]",
    bar: "bg-[#c74f42]",
  },
  unknown: {
    text: "text-[var(--color-muted)]",
    bg: "bg-[rgba(104,79,48,0.1)]",
    dim: "bg-[rgba(104,79,48,0.18)]",
    bar: "bg-[rgba(104,79,48,0.42)]",
  },
} as const;

export function LatencyBadge({ latencyMs, compact = false }: Props) {
  const level = resolveLatencyLevel(latencyMs);
  const styles = toneStyles[level];
  const activeBars =
    level === "good" ? 3 : level === "ok" ? 2 : level === "slow" ? 2 : level === "poor" ? 1 : 0;

  return (
    <div
      className={[
        "inline-flex items-center gap-2 rounded-full border border-[rgba(104,79,48,0.12)] px-3 py-2",
        styles.bg,
      ].join(" ")}
    >
      <div className="flex items-end gap-1">
        {[1, 2, 3].map((bar) => (
          <span
            key={bar}
            className={[
              "block w-1.5 rounded-full",
              bar === 1 ? "h-2.5" : bar === 2 ? "h-4" : "h-5.5",
              bar <= activeBars ? styles.bar : styles.dim,
            ].join(" ")}
          />
        ))}
      </div>
      <div className="flex items-baseline gap-1.5">
        {!compact ? (
          <span className={`text-xs font-semibold ${styles.text}`}>{formatLatencyLabel(latencyMs)}</span>
        ) : null}
        <span className={`text-sm font-semibold ${styles.text}`}>
          {latencyMs != null ? `${latencyMs}ms` : "--"}
        </span>
      </div>
    </div>
  );
}
