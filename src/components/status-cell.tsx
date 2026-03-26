type Props = {
  value: string;
  status: "correct" | "near" | "wrong";
  emphasized?: boolean;
};

const statusStyles = {
  correct:
    "border-[rgba(171,39,29,0.22)] bg-[rgba(216,79,66,0.88)] text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.16)]",
  near:
    "border-[rgba(176,125,21,0.22)] bg-[rgba(216,166,66,0.88)] text-[#3f2e10] shadow-[inset_0_1px_0_rgba(255,255,255,0.18)]",
  wrong:
    "border-[rgba(82,89,102,0.2)] bg-[rgba(139,143,152,0.88)] text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.12)]",
} as const;

export function StatusCell({ value, status, emphasized = false }: Props) {
  return (
    <div
      className={[
        "min-h-[54px] min-w-[94px] rounded-[18px] border px-3 py-2 text-sm font-medium leading-5 break-words",
        statusStyles[status],
        emphasized ? "ring-2 ring-[rgba(200,108,53,0.22)]" : "",
      ].join(" ")}
    >
      {value}
    </div>
  );
}
