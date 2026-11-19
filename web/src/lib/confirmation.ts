import { useCallback, useLayoutEffect, useRef } from "react";
import type { PlayerText } from "@/i18n/player-text";

export type ConfirmationKind = "finish" | "remove" | "submit" | "prepare" | "report";
type Confirmation = { id: number; message: PlayerText; kind: ConfirmationKind; opener: HTMLElement | null };
let current: Confirmation | null = null;
let sequence = 0;
let settle: ((accepted: boolean) => void) | null = null;
const listeners = new Set<() => void>();
export const confirmationStore = {
  subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
  getSnapshot: () => current,
  getServerSnapshot: () => null,
};
export function answerConfirmation(id: number, accepted: boolean) {
  if (current?.id === id) settle?.(accepted);
}
export function cancelConfirmation() { settle?.(false); }

/** One decision at a time. Cancellation never authorizes an operation. */
export function requestConfirmation(message: PlayerText, kind: ConfirmationKind, signal: AbortSignal): Promise<boolean> {
  if (signal.aborted || current) return Promise.resolve(false);
  return new Promise(resolve => {
    const abort = () => finish(false);
    let done = false;
    function finish(accepted: boolean) {
      if (done) return;
      done = true;
      signal.removeEventListener("abort", abort);
      current = null; settle = null;
      listeners.forEach(listener => listener());
      resolve(accepted && !signal.aborted);
    }
    current = { id: ++sequence, message, kind, opener: document.activeElement instanceof HTMLElement ? document.activeElement : null };
    settle = finish;
    signal.addEventListener("abort", abort, { once: true });
    listeners.forEach(listener => listener());
  });
}

/** A changed game revision, deadline, or page invalidates the pending decision. */
export function useConfirmation(scope = "") {
  const owner = useRef<AbortController | null>(null);
  useLayoutEffect(() => {
    const controller = new AbortController(); owner.current = controller;
    return () => { controller.abort(); if (owner.current === controller) owner.current = null; };
  }, [scope]);
  return useCallback(async (message: PlayerText, kind: ConfirmationKind) => {
    const controller = owner.current;
    if (!controller) return false;
    const accepted = await requestConfirmation(message, kind, controller.signal);
    return accepted && owner.current === controller && !controller.signal.aborted;
  }, []);
}
