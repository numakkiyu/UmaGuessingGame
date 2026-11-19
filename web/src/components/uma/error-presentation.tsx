import * as Dialog from "@radix-ui/react-dialog";
import { Children, isValidElement, useEffect, useId, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { AlertTriangle, X } from "lucide-react";
import { useTranslation } from "@/i18n";
import "@/i18n/fault";

export type ErrorPresentationProps = {
  fallback: ReactNode;
  title?: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
  diagnosticId?: string;
  noticeKey?: string;
  tone: string;
};
const pairs = [
  ["specialweek", "teio"],
  ["rice", "mcqueen"],
  ["nicenature", "ikunodictus"],
  ["twinturbo", "matikanetannhauser"],
  ["mejiropalmer", "daitakuhelios"],
] as const;
let lastPair = -1;
// Concurrent failures are presented one at a time without duplicating page content.
let active: string | null = null;
const listeners = new Set<() => void>();
const pending = new Map<string, string>();
function publish(next: string | null) {
  active = next;
  listeners.forEach(listener => listener());
}
function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}
function advance() {
  if (active) return;
  const next = pending.keys().next().value;
  if (next) publish(next);
}
function text(node: ReactNode): string {
  return Children.toArray(node).map(child => isValidElement<{ children?: ReactNode }>(child)
    ? text(child.props.children) : String(child)).join("");
}
function selectPair() {
  const choices = pairs.map((_, index) => index).filter(index => index !== lastPair);
  lastPair = choices[Math.floor(Math.random() * choices.length)]!;
  return pairs[lastPair]!;
}

export default function ErrorPresentation({ title, children, action, diagnosticId, noticeKey, tone }: ErrorPresentationProps) {
  const { t } = useTranslation("fault");
  const { t: common } = useTranslation("common");
  const id = useId();
  const current = useSyncExternalStore(subscribe, () => active, () => null);
  const open = current === id;
  const [pair] = useState(selectPair);
  const [artFailed, setArtFailed] = useState(false);
  const [locked, setLocked] = useState(false);
  const retryTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const returnFocus = useRef<HTMLElement | null>(null);
  const [key] = useState(() => JSON.stringify([window.location.pathname, noticeKey ?? text(title), text(children), diagnosticId]));

  useEffect(() => {
    returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    pending.set(id, key); advance();
    return () => {
      pending.delete(id);
      if (active === id) { publish(null); advance(); }
      clearTimeout(retryTimer.current);
    };
  }, [id, key]);

  const changeOpen = (next: boolean) => {
    if (next) {
      returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      publish(id);
    } else {
      for (const [pendingId, pendingKey] of pending) if (pendingKey === key) pending.delete(pendingId);
      if (active === id) publish(null);
      advance();
    }
  };
  const art = (name: string, position: string) => !artFailed ? <img src={`/assets/ui/errors/${name}.webp`} className={`uma-error-character ${position}`} width={420} height={500} alt="" aria-hidden="true" decoding="async" onError={() => setArtFailed(true)} /> : null;

  return <Dialog.Root open={open} onOpenChange={changeOpen}>
      <Dialog.Portal>
        <Dialog.Overlay className="uma-error-overlay" />
        <Dialog.Content className="uma-error-dialog" role="alertdialog" onPointerDown={event => { if (event.target === event.currentTarget) changeOpen(false); }} onCloseAutoFocus={event => {
          event.preventDefault();
          if (active) return;
          const target = returnFocus.current;
          if (target?.isConnected && target !== document.body && !target.closest('[aria-hidden="true"]')) target.focus({ preventScroll: true });
          else document.getElementById("main-content")?.focus({ preventScroll: true });
        }}>
          <div className="uma-error-scene" data-pair={pair.join(":")}>
            {art(pair[1], "uma-error-character-back")}
            <section className="uma-error-panel" role="alert" data-tone={tone}>
              <div className="uma-error-topline" aria-hidden="true" />
              <Dialog.Close className="uma-error-close" aria-label={common("actions.close")}><X aria-hidden="true" /></Dialog.Close>
              <div className="uma-error-main">
                <div className="uma-error-heading"><span className="uma-error-icon"><AlertTriangle aria-hidden="true" /></span><span>{t("notice")}</span></div>
                <Dialog.Title asChild><h2>{title ?? t("title")}</h2></Dialog.Title>
                <Dialog.Description asChild><div className="uma-error-copy">{children ?? t("detail")}</div></Dialog.Description>
              </div>
              <div className="uma-error-footer">
                <fieldset className="uma-error-actions" disabled={locked} aria-busy={locked} onClickCapture={event => {
                  if (!(event.target instanceof Element) || !event.target.closest("button,a")) return;
                  if (locked) { event.preventDefault(); event.stopPropagation(); return; }
                  setLocked(true);
                  retryTimer.current = setTimeout(() => setLocked(false), 900);
                }}>{action ?? <Dialog.Close className="uma-error-home">{t("back")}</Dialog.Close>}</fieldset>
                <a className="uma-error-home" href="/">{common("actions.home")}</a>
                {diagnosticId ? <p className="uma-error-id">{common("state.problemId")} · <span>{diagnosticId}</span></p> : null}
              </div>
            </section>
            {art(pair[1], "uma-error-character-hands")}
            {art(pair[0], "uma-error-character-front")}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>;
}
