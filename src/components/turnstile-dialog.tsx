"use client";

import { TurnstileWidget } from "@/components/turnstile-widget";

type Props = {
  open: boolean;
  siteKey: string;
  resetSignal: number;
  message: string;
  statusText?: string;
  onTokenChange: (token: string | null) => void;
  onClose: () => void;
};

export function TurnstileDialog({
  open,
  siteKey,
  resetSignal,
  message,
  statusText,
  onTokenChange,
  onClose,
}: Props) {
  if (!open) {
    return null;
  }

  return (
    <div className="fixed inset-0 z-[80] flex items-end justify-center bg-[rgba(18,31,54,0.52)] px-3 py-3 backdrop-blur-[5px] sm:items-center sm:px-4 sm:py-6">
      <div className="w-full max-w-[460px] rounded-[28px] border border-[var(--color-line)] bg-[linear-gradient(180deg,rgba(255,255,255,0.98),rgba(245,249,255,0.96))] p-5 shadow-[0_24px_60px_rgba(28,54,92,0.22)] sm:p-6">
        <p className="text-xs font-semibold uppercase tracking-[0.2em] text-[var(--color-brand-strong)]">
          开始前确认一下
        </p>
        <h2 className="mt-2 font-[var(--font-display)] text-[1.8rem] font-bold leading-tight text-[var(--color-ink)]">
          先完成一下验证
        </h2>
        <p className="mt-3 text-sm leading-7 text-[var(--color-muted)]">{message}</p>

        <div className="mt-5 rounded-[22px] border border-[var(--color-line)] bg-white px-4 py-4 shadow-[var(--shadow-soft)]">
          <TurnstileWidget
            siteKey={siteKey}
            resetSignal={resetSignal}
            onTokenChange={onTokenChange}
          />
          <p className="mt-2 text-sm text-[var(--color-muted)]">
            {statusText ?? "验证通过后会自动继续。"}
          </p>
        </div>

        <div className="mt-5 flex justify-end">
          <button type="button" onClick={onClose} className="uma-ghost-button text-sm">
            稍后再说
          </button>
        </div>
      </div>
    </div>
  );
}
