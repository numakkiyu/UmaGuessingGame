import { useCallback, useEffect, useRef, useState } from "react";
import { useLocation } from "react-router";
import { useTranslation } from "@/i18n";
import type { RouteCurtainProps } from "./deferred-route-curtain";
import { canAnimateRoute } from "./route-curtain-policy";
import { pageScene, pickDifferent } from "./theme-art";
import runners from "./runner-art.json";
import characters from "./stage-art.json";
import "@/i18n/stage";

const artRoot = "/assets/ui/transition/";
const stories = [
  ["story-anime-1.webp", "story-game-live-1.webp"],
  ["story-anime-2-fixed.webp", "story-game-live-2.webp"],
  ["story-anime-3.webp", "story-game-3.webp"],
  ["story-anime-4.webp", "story-game-4.webp"],
  ["story-anime-5.webp", "story-game-5.webp"],
  ["story-anime-6.webp", "story-game-6.webp"],
  ["story-anime-7.webp", "story-game-7.webp"],
] as const;
type Visual = { left: string; right: string; runner: string; quote: typeof characters[number] };
const sleep = (ms: number, signal: AbortSignal) => new Promise<void>(resolve => {
  if (signal.aborted) { resolve(); return; }
  const done = () => { clearTimeout(timer); signal.removeEventListener("abort", done); resolve(); };
  const timer = setTimeout(done, ms); signal.addEventListener("abort", done, { once: true });
});
const decode = (url: string) => { const image = new Image(); image.src = url; return image.decode().catch(() => undefined); };

export function RouteCurtain({ blocker, allowBlocking, reduced }: RouteCurtainProps) {
  const location = useLocation();
  const { t: stage } = useTranslation("stage");
  const [visual, setVisual] = useState<Visual | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const running = useRef(false), controller = useRef<AbortController | null>(null), animations = useRef<Animation[]>([]);
  const previous = useRef<Visual | null>(null);
  const warmStory = useRef(pickDifferent(stories));
  const warmRunner = useRef(pickDifferent(runners));
  const latest = useRef(blocker);

  const restore = useCallback(() => {
    animations.current.forEach(animation => animation.cancel()); animations.current = [];
    const main = document.getElementById("main-content");
    if (main) { main.inert = false; main.removeAttribute("aria-busy"); }
    running.current = false; allowBlocking(true); setVisual(null);
  }, [allowBlocking]);

  useEffect(() => {
    latest.current = blocker;
    if (blocker.state !== "blocked" || running.current) return;
    running.current = true;
    const abort = new AbortController(); controller.current = abort;
    const pair = warmStory.current;
    warmStory.current = pickDifferent(stories, pair);
    const next: Visual = { left: pair[0], right: pair[1], runner: previous.current ? pickDifferent(runners, previous.current.runner) : warmRunner.current, quote: pickDifferent(characters, previous.current?.quote) };
    previous.current = next; setVisual(next);
    const main = document.getElementById("main-content");
    if (main) { main.inert = true; main.setAttribute("aria-busy", "true"); }
    const motion = (element: Element, frames: Keyframe[], duration: number, delay = 0) => {
      const animation = element.animate(frames, { duration, delay, fill: "forwards", easing: "cubic-bezier(.2,0,0,1)" });
      animations.current.push(animation); return animation.finished.catch(() => undefined);
    };
    async function run() {
      try {
        await sleep(16, abort.signal);
        if (abort.signal.aborted || !root.current) return;
        const left = root.current.querySelector(".uma-curtain-left")!, right = root.current.querySelector(".uma-curtain-right")!, center = root.current.querySelector(".uma-curtain-center")!;
        await Promise.all([
          motion(left, [{ transform: "translateX(-101%)" }, { transform: "translateX(-48%)", offset: .48 }, { transform: "translateX(0)" }], 390),
          motion(right, [{ transform: "translateX(101%)" }, { transform: "translateX(48%)", offset: .48 }, { transform: "translateX(0)" }], 390),
          motion(center, [{ opacity: 0 }, { opacity: 1 }], 160, 300),
        ]);
        if (abort.signal.aborted) return;
        allowBlocking(false);
        if (latest.current.state === "blocked") latest.current.proceed();
        const began = performance.now();
        const ready = async () => {
          await sleep(60, abort.signal);
          while (!abort.signal.aborted && document.querySelector("[data-route-loading]") && performance.now() - began < 7400) await sleep(50, abort.signal);
          if (abort.signal.aborted) return;
          const pictures = [...document.querySelectorAll<HTMLImageElement>("#main-content img:not([loading=lazy])")].map(image => image.decode().catch(() => undefined));
          await Promise.all([...pictures, decode(artRoot + next.left), decode(artRoot + next.right), decode(artRoot + "runners/" + next.runner), decode(pageScene(window.location.pathname))]);
        };
        let path = window.location.pathname;
        await Promise.all([sleep(1000, abort.signal), Promise.race([ready(), sleep(7500, abort.signal)])]);
        while (!abort.signal.aborted && path !== window.location.pathname && performance.now() - began < 7500) { path = window.location.pathname; await Promise.race([ready(), sleep(7500 - (performance.now() - began), abort.signal)]); }
        if (abort.signal.aborted) return;
        await Promise.all([
          motion(center, [{ opacity: 1 }, { opacity: 0 }], 160),
          motion(left, [{ transform: "translateX(0)" }, { transform: "translateX(-48%)", offset: .57 }, { transform: "translateX(-101%)" }], 400, 120),
          motion(right, [{ transform: "translateX(0)" }, { transform: "translateX(48%)", offset: .57 }, { transform: "translateX(101%)" }], 400, 120),
        ]);
        if (!abort.signal.aborted) {
          restore();
          const heading = document.querySelector<HTMLElement>("#main-content h1");
          if (heading) { heading.tabIndex = -1; heading.focus({ preventScroll: true }); }
        }
      } catch {
        if (latest.current.state === "blocked") latest.current.proceed();
        if (!abort.signal.aborted) restore();
      } finally {
        abort.abort();
      }
    }
    void run();
  }, [blocker, allowBlocking, restore]);

  useEffect(() => {
    const dismiss = () => {
      if (!running.current) return;
      controller.current?.abort();
      if (latest.current.state === "blocked") latest.current.proceed();
      restore();
    };
    if (reduced || document.hidden || !canAnimateRoute("", location.pathname)) dismiss();
    const visibility = () => { if (document.hidden) dismiss(); };
    document.addEventListener("visibilitychange", visibility);
    return () => document.removeEventListener("visibilitychange", visibility);
  }, [reduced, location.pathname, restore]);

  useEffect(() => () => {
    controller.current?.abort(); animations.current.forEach(animation => animation.cancel());
    const main = document.getElementById("main-content"); if (main) { main.inert = false; main.removeAttribute("aria-busy"); }
  }, []);

  useEffect(() => {
    if (visual || reduced || (navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData) return;
    const timer = setTimeout(() => { for (const file of [...warmStory.current, "runners/" + warmRunner.current]) void decode(artRoot + file); }, 1800);
    return () => clearTimeout(timer);
  }, [reduced, visual]);

  return <div className="uma-route-curtain" ref={root} data-active={!!visual} data-story={visual?.left} aria-hidden="true">
    <div className="uma-curtain-wing uma-curtain-left"><img src={visual ? artRoot + visual.left : undefined} alt="" /><span /></div>
    <div className="uma-curtain-wing uma-curtain-right"><img src={visual ? artRoot + visual.right : undefined} alt="" /><span /></div>
    <div className="uma-curtain-center"><div className="uma-runner-disc"><img src={visual ? artRoot + "runners/" + visual.runner : undefined} alt="" width={248} height={248} /></div><figure><blockquote>{visual ? stage(`${visual.quote.key}.quote`) : ""}</blockquote><figcaption>{visual ? stage(`${visual.quote.key}.name`) : ""}</figcaption></figure></div>
  </div>;
}
