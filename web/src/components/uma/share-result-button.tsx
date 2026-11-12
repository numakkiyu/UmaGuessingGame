/** Copies text already filtered by the caller for reveal permissions. Clipboard failures leave a manual copy fallback. */
import { useCallback, useEffect, useId, useRef, useState } from "react";
import "@/i18n/sharing";
import { useTranslation } from "@/i18n";
import { Check, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";

export type ShareResultButtonProps = {
  /** 已经按揭晓状态裁剪好的文本。未揭晓时调用方不得把答案拼进来。 */
  text: string;
  /** 按钮文案，默认「复制战绩」。 */
  label?: string;
  className?: string;
};

type State = "idle" | "copied" | "failed";

export function ShareResultButton({ text, label, className }: ShareResultButtonProps) {
  const { t } = useTranslation("sharing");
  const statusId = useId();
  const textId = useId();
  const [state, setState] = useState<State>("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);

  const onCopy = useCallback(async () => {
    if (timer.current) clearTimeout(timer.current);
    try {
      // clipboard 在非安全上下文（http 非 localhost）或旧浏览器上可能不存在。
      if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(text);
      setState("copied");
    } catch {
      setState("failed");
    }
    // Keep manual copying available after a failed attempt. Only success fades.
    timer.current = setTimeout(() => setState(previous => previous === "copied" ? "idle" : previous), 3000);
  }, [text]);

  return (
    <div className={className}>
      <Button
        type="button"
        variant="secondary"
        size="primary"
        onClick={onCopy}
        // 复制是异步的，用 aria-live 让读屏器也听到结果，而不是只有颜色变化。
        aria-describedby={state === "idle" ? undefined : statusId}
        className="h-auto min-h-12 max-w-full whitespace-normal"
      >
        {state === "copied" ? (
          <Check aria-hidden="true" className="size-4" />
        ) : (
          <Copy aria-hidden="true" className="size-4" />
        )}
        {state === "copied" ? t("copied") : label ?? t("copy")}
      </Button>
      <p
        id={statusId}
        role="status"
        aria-live="polite"
        className="mt-1 text-[var(--text-aux)] text-[var(--color-muted)]"
      >
        {state === "copied"
          ? t("success")
          : state === "failed"
            ? t("failed")
            : ""}
      </p>
      {state === "failed" ? (
        <div className="mt-2 flex min-w-0 flex-col gap-1">
          <label htmlFor={textId} className="text-sm font-semibold">{t("text")}</label>
          <textarea id={textId} readOnly value={text} rows={4} onFocus={event => event.currentTarget.select()} className="w-full min-w-0 rounded-lg border border-[var(--color-line)] bg-[var(--color-panel)] p-3 text-sm text-[var(--color-ink)]" />
        </div>
      ) : null}
    </div>
  );
}
