import { useEffect, useRef, useState } from "react";
import { Check, Loader2, ShieldCheck } from "lucide-react";
import { nextAdmissionPair } from "./admission-art";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import type { AdmissionScene } from "./admission-scene";
type Scene = typeof AdmissionScene;
import { useReducedMotion } from "@/lib/hooks/use-reduced-motion";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { api, setAdmissionHandler } from "@/lib/api";
import { ApiError } from "@/lib/api/client";
import type { components } from "@/lib/api/types";
import i18n, { matchLanguage, useTranslation } from "@/i18n";
import type { Turnstile } from "./turnstile";

type Config = components["schemas"]["AdmissionConfig"];
export function AdmissionController({ prepareSession, installHandler = setAdmissionHandler }: { prepareSession?: () => Promise<unknown>; installHandler?: typeof setAdmissionHandler } = {}) {
  const { reduced } = useReducedMotion();
  const [reason, setReason] = useState<"entry" | "traffic">("entry");
  const [success, setSuccess] = useState(false);
  const [Scene, setScene] = useState<Scene | null>(null);
  const [sceneFailed, setSceneFailed] = useState(false);
  const [pair, setPair] = useState(nextAdmissionPair);
  const successTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const prepare = useRef(prepareSession);
  useEffect(() => { prepare.current = prepareSession; }, [prepareSession]);
  const { t } = useTranslation("admission");
  const { t: common } = useTranslation("common");
  const [messagesReady, setMessagesReady] = useState(() => i18n.hasResourceBundle("zh-Hans", "admission"));
  const [messagesFailed, setMessagesFailed] = useState(false);
  const [open, setOpen] = useState(false), [config, setConfig] = useState<Config | null>(null);
  const [token, setToken] = useState<string | null>(null), [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false), [attempt, setAttempt] = useState(0);
  const [waiting, setWaiting] = useState(false);
  const pending = useRef<{ promise: Promise<boolean>; resolve: (ok: boolean) => void; timer: ReturnType<typeof setTimeout> } | null>(null);
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  const [widgetFit, setWidgetFit] = useState({ scale: 1, minWidth: 0 });
  const widget = useRef<{ api: Turnstile; id: string; minWidth: number } | null>(null);
  const submitting = useRef(false), generation = useRef(0);
  const challengeComplete = useRef(false), verificationStarted = useRef(false);
  const dismissedTraffic = useRef(false);
  function finish(ok: boolean) {
    clearTimeout(successTimer.current);
    if (!ok) dismissedTraffic.current = true;
    generation.current += 1; const entry = pending.current; pending.current = null;
    if (entry) { clearTimeout(entry.timer); entry.resolve(ok); }
    setOpen(false); setSuccess(false); setToken(null); setBusy(false); submitting.current = false;
  }
  useEffect(() => {
    let active = true;
    const requestAdmission = (nextReason: "entry" | "traffic" = "entry") => {
      if (pending.current) { if (nextReason === "traffic") setReason("traffic"); return pending.current.promise; }
      const promise = new Promise<boolean>(resolve => {
        const timer = setTimeout(() => finish(false), 300_000);
        pending.current = { promise: null as unknown as Promise<boolean>, resolve, timer };
      });
      pending.current!.promise = promise;
      challengeComplete.current = false; verificationStarted.current = false;
      setConfig(null); setToken(null); setError(null); setMessagesFailed(false); setWaiting(true); setSuccess(false); setReason(nextReason); setPair(nextAdmissionPair());
      const version = ++generation.current;
      void api.getPublicConfig().then(async response => {
        if (!active || version !== generation.current) return;
        const next = response.data?.admission;
        if (!next?.enabled) { finish(true); return; }
        const status = (await api.getAdmissionStatus()).data;
        if (!active || version !== generation.current) return;
        if (status?.verified && !status.required) { finish(true); return; }
        setConfig(next); setOpen(true);
      }).catch(() => {
        if (active && version === generation.current) { setError("loadFailed"); setOpen(true); }
      }).finally(() => { if (active && version === generation.current) setWaiting(false); });
      return promise;
    };
    installHandler(requestAdmission);
    // Check existing sessions on arrival and on a bounded foreground cadence.
    // This neither creates an identity nor loads the provider for healthy visits.
    let lastCheck = 0;
    let adaptiveEnabled: boolean | undefined;
    const controller = new AbortController();
    async function checkTraffic() {
      if (adaptiveEnabled === false || !active || document.hidden || pending.current || Date.now() - lastCheck < 45_000) return;
      lastCheck = Date.now();
      try {
        if (adaptiveEnabled === undefined) {
          const config = (await api.getPublicConfig(controller.signal)).data?.admission;
          if (!active) return;
          adaptiveEnabled = Boolean(config?.enabled && config.adaptiveEnabled);
        }
        if (!adaptiveEnabled) return;
        const status = (await api.getAdmissionStatus(controller.signal)).data;
        if (!active) return;
        if (!status?.required) dismissedTraffic.current = false;
        if (status?.enabled && status.required && !dismissedTraffic.current) void requestAdmission("traffic");
      } catch { /* Entry operations still enforce verification if this optional read fails. */ }
    }
    void checkTraffic();
    const poll = setInterval(() => void checkTraffic(), 60_000);
    const foreground = () => void checkTraffic();
    document.addEventListener("visibilitychange", foreground);
    window.addEventListener("online", foreground);
    return () => {
      controller.abort(); clearInterval(poll); clearTimeout(successTimer.current);
      document.removeEventListener("visibilitychange", foreground); window.removeEventListener("online", foreground);
      active = false; generation.current += 1; installHandler(null);
      const entry = pending.current; pending.current = null;
      if (entry) { clearTimeout(entry.timer); entry.resolve(false); }
    };
  }, [installHandler]);
  useEffect(() => {
    if (!open || messagesReady) return;
    let active = true;
    void import("@/i18n/admission").then(() => {
      if (active) setMessagesReady(true);
    }).catch(() => { if (active) setMessagesFailed(true); });
    return () => { active = false; };
  }, [open, messagesReady, attempt]);
  useEffect(() => {
    if (!messagesReady || !open || !config?.enabled || !container) return;
    const element = container.firstElementChild as HTMLDivElement;
    if (!element) return;
    const compact = container.clientWidth < 300;
    let disposed = false;
    let watchdog: ReturnType<typeof setTimeout> | undefined;
    const interactive = () => { clearTimeout(watchdog); if (!disposed) setWaiting(false); };
    const widgetFailed = () => {
      clearTimeout(watchdog);
      if (!disposed && !verificationStarted.current) { setToken(null); setWaiting(false); setError("widgetFailed"); }
    };
    void Promise.resolve().then(() => {
      if (disposed) return null;
      setWidgetFit({ scale: 1, minWidth: 0 });
      setWaiting(true);
      return import("./turnstile").then(module => disposed ? null : module.loadTurnstile());
    }).then(turnstile => {
      if (disposed || !turnstile) return;
      watchdog = setTimeout(widgetFailed, 45_000);
      const id = turnstile.render(element, {
        sitekey: config.siteKey, action: "game_entry", theme: "light", size: compact ? "compact" : "flexible",
        // Keep the mounted challenge and its response when the UI language changes.
        // A newly opened or explicitly retried challenge uses the current locale.
        appearance: "always", language: { "zh-Hans": "zh-CN", "zh-Hant": "zh-TW", ja: "ja" }[matchLanguage(i18n.language) ?? "zh-Hans"], retry: "never", "refresh-expired": "manual",
        callback: (value: string) => { clearTimeout(watchdog); if (!disposed && !verificationStarted.current) { challengeComplete.current = true; setToken(value); setError(null); setWaiting(false); } },
        "before-interactive-callback": interactive,
        "unsupported-callback": widgetFailed,
        "expired-callback": () => { clearTimeout(watchdog); if (!disposed && !verificationStarted.current) { setToken(null); setWaiting(false); setError("expired"); } },
        "error-callback": widgetFailed,
        "timeout-callback": () => { clearTimeout(watchdog); if (!disposed && !verificationStarted.current) { setToken(null); setWaiting(false); setError("expired"); } },
      });
      widget.current = { api: turnstile, id, minWidth: compact ? 150 : 300 };
      setWaiting(false);
    }).catch(() => { if (!disposed) { setWaiting(false); setError("loadFailed"); } });
    return () => { disposed = true; clearTimeout(watchdog); if (widget.current) { try { widget.current.api.remove(widget.current.id); } catch { /* The provider may already have removed an expired frame. */ } widget.current = null; } element.replaceChildren(); };
  }, [open, config, attempt, messagesReady, container]);
  useEffect(() => {
    if (!container || typeof ResizeObserver === "undefined") return;
    let compact = container.clientWidth < 300;
    const observer = new ResizeObserver(() => {
      const next = container.clientWidth < 300;
      if (challengeComplete.current || submitting.current) {
        const minWidth = widget.current?.minWidth ?? 300;
        setWidgetFit({ scale: Math.min(1, container.clientWidth / minWidth), minWidth });
        return;
      }
      if (next === compact) return;
      compact = next; setToken(null); setError(null); setAttempt(value => value + 1);
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, [container]);
  useEffect(() => {
    if (!open || Scene) return;
    let active = true;
    const timeout = setTimeout(() => { if (active) setSceneFailed(true); }, 12_000);
    void import("./admission-scene").then(module => {
      if (active) { setScene(() => module.AdmissionScene); setSceneFailed(false); }
    }).catch(() => { if (active) setSceneFailed(true); }).finally(() => clearTimeout(timeout));
    return () => { active = false; clearTimeout(timeout); };
  }, [open, Scene, attempt]);
  async function confirm() {
    if (!token || submitting.current) return;
    verificationStarted.current = true;
    submitting.current = true; setBusy(true); setError(null);
    const version = generation.current;
    try {
      const restored = prepare.current ? await prepare.current() : true;
      if (version !== generation.current || !pending.current) return;
      if (!restored) { setError("sessionLost"); return; }
      const signal = config?.browserSignalsEnabled ? await import("./browser-signal").then(module => module.browserSignal()) : undefined;
      if (version !== generation.current || !pending.current) return;
      const response = await api.verifyAdmission({ token, ...(signal ? { browserSignal: signal } : {}) });
      if (version !== generation.current || !pending.current) return;
      if (response.data?.verified === true) {
        setSuccess(true);
        successTimer.current = setTimeout(() => finish(true), reduced ? 0 : 650);
      }
      else setError("incomplete");
    } catch (failure) {
      if (version !== generation.current) return;
      if (failure instanceof ApiError && failure.code === "admission-invalid") {
        verificationStarted.current = false; setToken(null); setError("invalid");
      } else {
        setError("uncertain");
      }
    } finally {
      if (version === generation.current) { submitting.current = false; setBusy(false); }
    }
  }
  function retry() {
    challengeComplete.current = false; verificationStarted.current = false;
    setToken(null); setError(null);
    if (!config) {
      const entry = pending.current;
      if (!entry) return;
      setWaiting(true); const version = generation.current;
      void api.getPublicConfig().then(response => {
        if (version !== generation.current) return;
        if (!response.data?.admission?.enabled) finish(true);
        else setConfig(response.data.admission);
      }).catch(() => { if (version === generation.current) setError("loadFailed"); })
        .finally(() => { if (version === generation.current) setWaiting(false); });
    } else setAttempt(value => value + 1);
  }
  const title = !messagesReady ? common(messagesFailed ? "state.loadFailed" : "state.loading") : t(success ? "successTitle" : reason === "traffic" ? "trafficTitle" : "title");
  const description = !messagesReady ? common(messagesFailed ? "state.loadFailedDetail" : "state.loading") : t(success ? "successIntro" : reason === "traffic" ? "trafficIntro" : "intro");
  if (!open) return null;
  if (!Scene) return sceneFailed ? <Dialog open onOpenChange={value => { if (!value) finish(false); }}>
    <DialogContent><DialogHeader><DialogTitle>{common("state.loadFailed")}</DialogTitle><DialogDescription>{common("state.loadFailedDetail")}</DialogDescription></DialogHeader>
      <div className="mt-5 flex flex-wrap gap-2"><Button variant="ghost" onClick={() => finish(false)}>{common("actions.back")}</Button><Button onClick={() => { setSceneFailed(false); setAttempt(value => value + 1); }}>{common("actions.retry")}</Button></div>
    </DialogContent>
  </Dialog> : <span role="status" className="sr-only">{common("state.loading")}</span>;
  return <Scene open={open} onCancel={() => { if (!success) finish(false); }} traffic={reason === "traffic"} success={success} busy={busy || success} pair={pair} title={title} description={description}>
    <div className="uma-admission-body">
      {!messagesReady ? <div className="uma-admission-actions">
        <Button variant="ghost" onClick={() => finish(false)}>{common("actions.back")}</Button>
        {messagesFailed ? <Button onClick={() => { setMessagesFailed(false); setAttempt(value => value + 1); }}>{common("actions.retry")}</Button> : <Loader2 aria-hidden="true" className="size-5 motion-safe:animate-spin" />}
      </div> : <>
        <p className="uma-admission-purpose"><ShieldCheck aria-hidden="true" />{t("purpose")}</p>
        <div className="uma-admission-widget">
          {waiting ? <p role="status" className="uma-admission-status"><Loader2 aria-hidden="true" className="motion-safe:animate-spin" />{t("waiting")}</p> : null}
          <div ref={setContainer} className="uma-admission-widget-host">
            <div style={{ width: "100%", minWidth: widgetFit.minWidth || undefined, zoom: widgetFit.scale }} />
          </div>
          {token && !busy ? <p role="status" className="uma-admission-status is-ready"><Check aria-hidden="true" />{t(success ? "successTitle" : "ready")}</p> : null}
        </div>
        {error ? <Alert tone="warning" className="uma-error-fallback" title={t("errorTitle")}>{t(`errors.${error}`)}</Alert> : null}
        <p className="uma-admission-note">{t("privacy")}{config?.browserSignalsEnabled ? <span>{t("signals")}</span> : null}</p>
        <div className="uma-admission-actions">
          <Button variant="ghost" onClick={() => finish(false)} disabled={success}>{t("back")}</Button>
          {error && !token ? <Button variant="secondary" onClick={retry} disabled={waiting || busy || success}>{t("retry")}</Button> : null}
          <Button variant="primary" onClick={() => void confirm()} disabled={!token || busy || success}>{success ? <><Check aria-hidden="true" className="size-4" />{t("successAction")}</> : busy ? t("busy") : t("confirm")}</Button>
        </div>
      </>}
    </div>
  </Scene>;
}
