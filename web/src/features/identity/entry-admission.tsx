import { useEffect, useRef, useState } from "react";
import { Flag, Loader2, ShieldCheck } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { api, setAdmissionHandler } from "@/lib/api";
import { ApiError } from "@/lib/api/client";
import type { components } from "@/lib/api/types";
import { browserSignal } from "./browser-signal";
import { loadTurnstile, type Turnstile } from "./turnstile";

type Config = components["schemas"]["AdmissionConfig"];
export function EntryAdmission() {
  const [open, setOpen] = useState(false), [config, setConfig] = useState<Config | null>(null);
  const [token, setToken] = useState<string | null>(null), [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false), [attempt, setAttempt] = useState(0);
  const [waiting, setWaiting] = useState(false);
  const pending = useRef<{ promise: Promise<boolean>; resolve: (ok: boolean) => void; timer: ReturnType<typeof setTimeout> } | null>(null);
  const container = useRef<HTMLDivElement>(null), widget = useRef<{ api: Turnstile; id: string } | null>(null);
  const submitting = useRef(false), generation = useRef(0);
  function finish(ok: boolean) {
    generation.current++; const entry = pending.current; pending.current = null;
    if (entry) { clearTimeout(entry.timer); entry.resolve(ok); }
    setOpen(false); setToken(null); setBusy(false); submitting.current = false;
  }
  useEffect(() => {
    let active = true;
    setAdmissionHandler(() => {
      if (pending.current) return pending.current.promise;
      const promise = new Promise<boolean>(resolve => {
        const timer = setTimeout(() => finish(false), 120_000);
        pending.current = { promise: null as unknown as Promise<boolean>, resolve, timer };
      });
      pending.current!.promise = promise;
      setConfig(null); setToken(null); setError(null); setOpen(true); setWaiting(true);
      const version = ++generation.current;
      void api.getPublicConfig().then(response => {
        if (!active || version !== generation.current) return;
        const next = response.data?.admission;
        if (!next?.enabled) { finish(true); return; }
        setConfig(next);
      }).catch(() => {
        if (active && version === generation.current) setError("入场确认暂时打不开，请检查网络后重试");
      }).finally(() => { if (active && version === generation.current) setWaiting(false); });
      return promise;
    });
    return () => {
      active = false; setAdmissionHandler(null);
      const entry = pending.current; pending.current = null;
      if (entry) { clearTimeout(entry.timer); entry.resolve(false); }
    };
  }, []);
  useEffect(() => {
    if (!open || !config?.enabled || !container.current) return;
    const element = container.current;
    let disposed = false;
    setWaiting(true);
    void loadTurnstile().then(turnstile => {
      if (disposed) return;
      const id = turnstile.render(element, {
        sitekey: config.siteKey, action: "game_entry", theme: "light", size: element.clientWidth < 300 ? "compact" : "flexible",
        appearance: "always", language: "zh-CN", retry: "never", "refresh-expired": "manual",
        callback: (value: string) => { if (!disposed) { setToken(value); setError(null); setWaiting(false); } },
        "expired-callback": () => { if (!disposed) { setToken(null); setWaiting(false); setError("等候有些久了，请重新完成入场确认"); } },
        "error-callback": () => { if (!disposed) { setToken(null); setWaiting(false); setError("入场确认暂时没能完成，请重试"); } },
        "timeout-callback": () => { if (!disposed) { setToken(null); setWaiting(false); setError("等候有些久了，请重新完成入场确认"); } },
      });
      widget.current = { api: turnstile, id };
      setWaiting(false);
    }).catch(() => { if (!disposed) { setWaiting(false); setError("入场确认暂时打不开，请检查网络后重试"); } });
    return () => { disposed = true; if (widget.current) { widget.current.api.remove(widget.current.id); widget.current = null; } };
  }, [open, config, attempt]);
  async function confirm() {
    if (!token || submitting.current) return;
    submitting.current = true; setBusy(true); setError(null);
    const version = generation.current;
    try {
      const signal = config?.browserSignalsEnabled ? await browserSignal() : undefined;
      const response = await api.verifyAdmission({ token, ...(signal ? { browserSignal: signal } : {}) });
      if (version !== generation.current || !pending.current) return;
      if (response.data?.verified === true) finish(true);
      else setError("还没完成入场确认，请再试一次");
    } catch (failure) {
      if (version !== generation.current) return;
      if (failure instanceof ApiError && failure.code === "admission-invalid") {
        setToken(null); setError("这次确认已失效，请重新完成入场确认");
      } else {
        setError("暂时没能确认，请稍后再点一次“确认并继续”。你刚才填写的内容还在");
      }
    } finally {
      if (version === generation.current) { submitting.current = false; setBusy(false); }
    }
  }
  function retry() {
    setToken(null); setError(null);
    if (!config) {
      const entry = pending.current;
      if (!entry) return;
      setWaiting(true); const version = generation.current;
      void api.getPublicConfig().then(response => {
        if (version !== generation.current) return;
        if (!response.data?.admission?.enabled) finish(true);
        else setConfig(response.data.admission);
      }).catch(() => { if (version === generation.current) setError("入场确认暂时打不开，请检查网络后重试"); })
        .finally(() => { if (version === generation.current) setWaiting(false); });
    } else setAttempt(value => value + 1);
  }
  return <Dialog open={open} onOpenChange={value => { if (!value) finish(false); }}>
    <DialogContent className="p-0" aria-busy={busy}>
      <div className="relative overflow-hidden rounded-t-[var(--radius-panel)] border-b border-[var(--color-line)] bg-[var(--color-page-tint)] px-5 pb-5 pt-6">
        <div aria-hidden="true" className="pointer-events-none absolute inset-0 bg-[url('/assets/ui/scenes/event-stage.jpg')] bg-cover bg-center opacity-10" />
        <DialogHeader className="relative">
          <Flag aria-hidden="true" className="mb-2 size-7 text-[var(--color-brand-green)]" />
          <DialogTitle className="text-xl">训练员，准备入场</DialogTitle>
          <DialogDescription>完成这一步，就可以和大家一起开跑了</DialogDescription>
        </DialogHeader>
      </div>
      <div className="space-y-4 p-5">
        <p className="flex items-center gap-2 text-sm font-semibold text-[var(--color-ink)]"><ShieldCheck aria-hidden="true" className="size-5 shrink-0 text-[var(--color-brand-green)]" />为大家留出顺畅的赛道</p>
        <div className="min-h-20 min-w-0 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3">
          {waiting ? <p role="status" className="mb-2 flex items-center gap-2 text-sm"><Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin" />正在准备，请稍候……</p> : null}
          <div ref={container} className="min-w-0" />
          {token && !busy ? <p role="status" className="mt-2 text-sm font-semibold text-[var(--color-correct-edge)]">准备好了，确认后继续</p> : null}
        </div>
        {error ? <Alert tone="warning" title="还没完成入场确认">{error}</Alert> : null}
        <p className="text-xs leading-5 text-[var(--color-muted)]">不需要注册。入场确认用于减少干扰游玩的重复操作{config?.browserSignalsEnabled ? "会使用必要的浏览器特征协助保护本次入场" : ""}</p>
        <DialogFooter className="mt-0">
          <Button variant="ghost" onClick={() => finish(false)}>先返回</Button>
          {error && !token ? <Button variant="secondary" onClick={retry} disabled={waiting || busy}>重新确认</Button> : null}
          <Button variant="primary" onClick={() => void confirm()} disabled={!token || busy}>{busy ? "正在确认……" : "确认并继续"}</Button>
        </DialogFooter>
      </div>
    </DialogContent>
  </Dialog>;
}
