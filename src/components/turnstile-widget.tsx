"use client";

import { useEffect, useRef } from "react";

type TurnstileInstance = {
  render: (
    container: HTMLElement,
    options: Record<string, unknown>,
  ) => string;
  reset: (widgetId: string) => void;
  remove: (widgetId: string) => void;
};

declare global {
  interface Window {
    turnstile?: TurnstileInstance;
    __umaTurnstileLoader?: Promise<void>;
  }
}

type Props = {
  siteKey: string;
  resetSignal: number;
  onTokenChange: (token: string | null) => void;
};

function ensureTurnstileScript() {
  if (typeof window === "undefined") {
    return Promise.resolve();
  }

  if (window.turnstile) {
    return Promise.resolve();
  }

  if (window.__umaTurnstileLoader) {
    return window.__umaTurnstileLoader;
  }

  window.__umaTurnstileLoader = new Promise<void>((resolve, reject) => {
    const existing = document.getElementById("cf-turnstile-script");
    if (existing) {
      existing.addEventListener("load", () => resolve(), { once: true });
      existing.addEventListener("error", () => reject(new Error("Turnstile 脚本加载失败。")), {
        once: true,
      });
      return;
    }

    const script = document.createElement("script");
    script.id = "cf-turnstile-script";
    script.src = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
    script.async = true;
    script.defer = true;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error("Turnstile 脚本加载失败。"));
    document.head.appendChild(script);
  });

  return window.__umaTurnstileLoader;
}

export function TurnstileWidget({ siteKey, resetSignal, onTokenChange }: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const widgetIdRef = useRef<string | null>(null);
  const onTokenChangeRef = useRef(onTokenChange);

  useEffect(() => {
    onTokenChangeRef.current = onTokenChange;
  }, [onTokenChange]);

  useEffect(() => {
    if (!siteKey) {
      return;
    }

    let disposed = false;

    ensureTurnstileScript()
      .then(() => {
        if (disposed || !containerRef.current || !window.turnstile) {
          return;
        }

        containerRef.current.innerHTML = "";
        widgetIdRef.current = window.turnstile.render(containerRef.current, {
          sitekey: siteKey,
          callback: (token: string) => onTokenChangeRef.current(token),
          "expired-callback": () => onTokenChangeRef.current(null),
          "error-callback": () => onTokenChangeRef.current(null),
        });
      })
      .catch(() => onTokenChangeRef.current(null));

    return () => {
      disposed = true;
      if (widgetIdRef.current && window.turnstile) {
        window.turnstile.remove(widgetIdRef.current);
        widgetIdRef.current = null;
      }
    };
  }, [siteKey]);

  useEffect(() => {
    if (!widgetIdRef.current || !window.turnstile) {
      return;
    }

    onTokenChangeRef.current(null);
    window.turnstile.reset(widgetIdRef.current);
  }, [resetSignal]);

  return <div ref={containerRef} className="min-h-[66px]" />;
}
