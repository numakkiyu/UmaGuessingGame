type Props = {
  value: string;
  status: "correct" | "near" | "wrong";
  emphasized?: boolean;
  label?: string;
  compact?: boolean;
};

const statusStyles = {
  correct:
    "border-[rgba(61,145,79,0.22)] bg-[rgba(79,184,95,0.94)] text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.16)]",
  near:
    "border-[rgba(193,146,29,0.22)] bg-[rgba(241,197,80,0.95)] text-[#4a3612] shadow-[inset_0_1px_0_rgba(255,255,255,0.2)]",
  wrong:
    "border-[rgba(116,128,147,0.24)] bg-[rgba(141,152,171,0.95)] text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.12)]",
} as const;

export function StatusCell({
  value,
  status,
  emphasized = false,
  label,
  compact = false,
}: Props) {
  return (
    <div
      className={[
        compact
          ? "flex min-h-[68px] flex-col items-center justify-center gap-1 rounded-[14px] border px-2 py-2 text-center"
          : "flex min-h-[54px] min-w-[88px] items-center justify-center rounded-[16px] border px-3 py-2 text-center text-sm font-medium leading-5 break-words sm:min-w-[96px]",
        statusStyles[status],
        emphasized ? "ring-2 ring-[rgba(63,136,247,0.24)]" : "",
      ].join(" ")}
    >
      {label ? (
        <span className={compact ? "text-[11px] font-semibold leading-4 opacity-90" : "sr-only"}>
          {label}
        </span>
      ) : null}
      <span className={compact ? "text-[1.02rem] font-bold leading-5 break-words" : ""}>{value}</span>
    </div>
  );
}
