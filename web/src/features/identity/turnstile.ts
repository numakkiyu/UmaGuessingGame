export type Turnstile = {
  render(element: HTMLElement, options: Record<string, unknown>): string;
  reset(widget: string): void;
  remove(widget: string): void;
};
declare global { interface Window { turnstile?: Turnstile } }
let loading: Promise<Turnstile> | null = null;
export function loadTurnstile(): Promise<Turnstile> {
  if (window.turnstile) return Promise.resolve(window.turnstile);
  if (loading) return loading;
  loading = new Promise<Turnstile>((resolve, reject) => {
    const script = document.createElement("script");
    const timeout = setTimeout(failed, 12_000);
    function failed() {
      clearTimeout(timeout); script.remove(); loading = null;
      reject(new Error("入场确认暂时打不开，请检查网络后重试"));
    }
    script.src = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
    script.async = true;
    script.onerror = failed;
    script.onload = () => {
      if (!window.turnstile) { failed(); return; }
      clearTimeout(timeout); resolve(window.turnstile);
    };
    document.head.append(script);
  });
  return loading;
}
