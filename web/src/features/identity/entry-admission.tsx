import { useCallback, useEffect, useRef, useState } from "react";
import { api, setAdmissionHandler, type AdmissionReason } from "@/lib/api";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useTranslation } from "@/i18n";

import type { AdmissionController } from "./admission-controller";
type Controller = typeof AdmissionController;
type Handler = NonNullable<Parameters<typeof setAdmissionHandler>[0]>;
type Pending = { reason: AdmissionReason; promise: Promise<boolean>; resolve: (ok: boolean) => void; timer: ReturnType<typeof setTimeout> };

/** Healthy visits check status without downloading the verification scene. */
export function EntryAdmission({ prepareSession }: { prepareSession?: () => Promise<unknown> } = {}) {
  const { t } = useTranslation("common");
  const [Controller, setController] = useState<Controller | null>(null);
  const [failed, setFailed] = useState(false);
  const loaded = useRef(false), active = useRef(true), loading = useRef(false);
  const loadTimeout = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const handler = useRef<Handler | null>(null), pending = useRef<Pending | null>(null);
  const load = useCallback(() => {
    if (loading.current) return;
    loading.current = true; setFailed(false);
    clearTimeout(loadTimeout.current);
    loadTimeout.current = setTimeout(() => { if (active.current && pending.current) setFailed(true); }, 12_000);
    void import("./admission-controller").then(module => {
      if (active.current) { loaded.current = true; setFailed(false); setController(() => module.AdmissionController); }
    }).catch(() => { if (active.current) setFailed(true); }).finally(() => { loading.current = false; clearTimeout(loadTimeout.current); });
  }, []);
  const installHandler = useCallback((next: Handler | null) => {
    handler.current = next;
    // Wait for StrictMode's mount rehearsal before handing over a live action.
    queueMicrotask(() => {
      const request = pending.current;
      if (!next || handler.current !== next || !active.current || !request) return;
      pending.current = null; clearTimeout(request.timer);
      void next(request.reason).then(request.resolve, () => request.resolve(false));
    });
  }, []);
  const cancel = () => {
    const request = pending.current; pending.current = null;
    if (request) { clearTimeout(request.timer); request.resolve(false); }
    setFailed(false);
  };
  useEffect(() => {
    active.current = true;
    const request: Handler = (reason = "entry") => {
      if (handler.current) return handler.current(reason);
      if (pending.current) { if (reason === "traffic") pending.current.reason = reason; return pending.current.promise; }
      let resolve!: (ok: boolean) => void;
      const promise = new Promise<boolean>(done => { resolve = done; });
      const timer = setTimeout(() => { if (pending.current?.promise === promise) cancel(); }, 300_000);
      pending.current = { reason, promise, resolve, timer }; load(); return promise;
    };
    setAdmissionHandler(request);
    const controller = new AbortController(); let last = 0, enabled: boolean | undefined;
    async function check() {
      if (loaded.current || !active.current || document.hidden || pending.current || enabled === false || Date.now() - last < 45_000) return;
      last = Date.now();
      try {
        if (enabled === undefined) { const config = (await api.getPublicConfig(controller.signal)).data?.admission; enabled = Boolean(config?.enabled && config.adaptiveEnabled); }
        if (!enabled || !active.current) return;
        const status = (await api.getAdmissionStatus(controller.signal)).data;
        if (active.current && status?.enabled && status.required) void request("traffic");
      } catch { /* Protected operations still enforce confirmation if the status read is unavailable. */ }
    }
    void check(); const poll = setInterval(() => void check(), 60_000); const foreground = () => void check();
    document.addEventListener("visibilitychange", foreground); window.addEventListener("online", foreground);
    return () => {
      active.current = false; clearTimeout(loadTimeout.current); controller.abort(); clearInterval(poll); setAdmissionHandler(null);
      document.removeEventListener("visibilitychange", foreground); window.removeEventListener("online", foreground);
      const entry = pending.current; pending.current = null;
      if (entry) { clearTimeout(entry.timer); entry.resolve(false); }
    };
  }, [load]);
  return <>
    {Controller ? <Controller prepareSession={prepareSession} installHandler={installHandler} /> : null}
    <Dialog open={failed} onOpenChange={value => { if (!value) cancel(); }}><DialogContent>
      <DialogHeader><DialogTitle>{t("state.loadFailed")}</DialogTitle><DialogDescription>{t("state.loadFailedDetail")}</DialogDescription></DialogHeader>
      <div className="mt-5 flex flex-wrap gap-2"><Button variant="ghost" onClick={cancel}>{t("actions.back")}</Button><Button onClick={load}>{t("actions.retry")}</Button></div>
    </DialogContent></Dialog>
  </>;
}
