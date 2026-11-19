import * as Dialog from "@radix-ui/react-dialog";
import { Check, Flag, ShieldCheck, X } from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";
import { useTranslation } from "@/i18n";
import { useReducedMotion } from "@/lib/hooks/use-reduced-motion";
import "@/styles/admission.css";

function AdmissionEdge({ success }: { success: boolean }) {
  const container = useRef<HTMLDivElement>(null);
  const { reduced } = useReducedMotion();
  useEffect(() => {
    if (reduced || !container.current) return;
    const host = container.current;
    const abort = new AbortController();
    let dispose: (() => void) | undefined;
    // Reuse the matching scene's continuous edge field, with a separate clock
    // and canvas so opening a confirmation cannot disturb the matching scene.
    void import("@/components/uma/match-curtain-motion").then(({ EdgeField }) => {
      if (abort.signal.aborted) return;
      const canvas = document.createElement("canvas"); host.append(canvas);
      const field = new EdgeField(canvas, true);
      field.failure = 1; field.alpha = .58;
      const resize = () => field.resize(host.clientWidth, host.clientHeight);
      const observer = new ResizeObserver(resize); observer.observe(host); resize();
      const visibility = () => {
        field.pause();
        if (!document.hidden) field.start(abort.signal);
      };
      document.addEventListener("visibilitychange", visibility); visibility();
      dispose = () => { observer.disconnect(); document.removeEventListener("visibilitychange", visibility); field.destroy(); field.canvas.remove(); };
    }).catch(() => { host.dataset.fallback = "true"; });
    return () => { abort.abort(); dispose?.(); };
  }, [reduced]);
  return <div ref={container} aria-hidden="true" className="uma-admission-edge" data-success={success} data-reduced={reduced} />;
}

export function AdmissionScene({ open, onCancel, traffic, success, pair, title, description, children, busy }: {
  open: boolean; onCancel: () => void; traffic: boolean; success: boolean; busy: boolean;
  pair: readonly [string, string]; title: string; description: string; children: ReactNode;
}) {
  const { t } = useTranslation("common");
  const returnFocus = useRef<HTMLElement | null>(null);
  return <Dialog.Root open={open} onOpenChange={value => { if (!value && !success) onCancel(); }}>
    <Dialog.Portal>
      <Dialog.Overlay className="uma-admission-overlay" data-traffic={traffic} data-success={success}>
        {traffic ? <AdmissionEdge success={success} /> : null}
      </Dialog.Overlay>
      <Dialog.Content className="uma-admission-dialog" data-traffic={traffic} data-success={success} aria-busy={busy}
        onOpenAutoFocus={() => { returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; }}
        onCloseAutoFocus={event => { event.preventDefault(); const target = returnFocus.current; if (target?.isConnected && !target.closest('[aria-hidden="true"]')) target.focus({ preventScroll: true }); }}
        onEscapeKeyDown={event => { if (success) event.preventDefault(); }}
        onPointerDownOutside={event => { event.preventDefault(); }}>
        <div className="uma-admission-scene">
          <img src={`/assets/ui/admission/${pair[0]}.webp`} alt="" aria-hidden="true" className="uma-admission-character is-left" width={360} height={480} onError={event => { event.currentTarget.style.visibility = "hidden"; }} />
          <section className="uma-admission-panel">
            <div className="uma-admission-ribbon" aria-hidden="true" />
            <button type="button" className="uma-admission-close" aria-label={t("actions.close")} onClick={onCancel} disabled={success}><X aria-hidden="true" size={19} /></button>
            <header className="uma-admission-header">
              <span className="uma-admission-emblem" aria-hidden="true">{success ? <Check /> : traffic ? <ShieldCheck /> : <Flag />}</span>
              <Dialog.Title className="uma-admission-title">{title}</Dialog.Title>
              <Dialog.Description className="uma-admission-description">{description}</Dialog.Description>
            </header>
            {children}
          </section>
          <img src={`/assets/ui/admission/${pair[1]}.webp`} alt="" aria-hidden="true" className="uma-admission-character is-right" width={360} height={480} onError={event => { event.currentTarget.style.visibility = "hidden"; }} />
        </div>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}
