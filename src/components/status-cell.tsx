type Props = {
  value: string;
  status: "correct" | "near" | "wrong";
  emphasized?: boolean;
};

const statusStyles = {
  correct:
    "border-[rgba(66,122,70,0.24)] bg-[rgba(88,165,92,0.94)] text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.16)]",
  near:
    "border-[rgba(176,125,21,0.22)] bg-[rgba(214,160,58,0.94)] text-[#3f2e10] shadow-[inset_0_1px_0_rgba(255,255,255,0.18)]",
  wrong:
    "border-[rgba(90,98,112,0.24)] bg-[rgba(141,147,157,0.94)] text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.12)]",
} as const;

export function StatusCell({ value, status, emphasized = false }: Props) {
  return (
    <div
      className={[
        "flex min-h-[54px] min-w-[88px] items-center justify-center rounded-[16px] border px-3 py-2 text-center text-sm font-medium leading-5 break-words sm:min-w-[96px]",
        statusStyles[status],
        emphasized ? "ring-2 ring-[rgba(200,108,53,0.22)]" : "",
      ].join(" ")}
    >
      {value}
    </div>
  );
}
