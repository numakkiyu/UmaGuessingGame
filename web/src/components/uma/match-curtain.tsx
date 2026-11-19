import type { MatchCurtainModel } from "./match-curtain-provider";
import { useCallback, useEffect, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { CenterWave, ClosingWave, EdgeField, smooth } from "./match-curtain-motion";
import { useReducedMotion } from "@/lib/hooks/use-reduced-motion";
import "@/styles/match-curtain.css";

const artwork = ["season1", "season2", "season3", "movie-new-scene", "rttt", "cinderella-school"];

export function MatchCurtain({ model, onDone }: { model: MatchCurtainModel; onDone: () => void }) {
  const root = useRef<HTMLDivElement|null>(null);
  const [mounted,setMounted] = useState(false);
  const attach = useCallback((element: HTMLDivElement|null) => { root.current=element;setMounted(element!==null); }, []);
  const latest = useRef({ model, onDone });
  useEffect(() => { latest.current = { model, onDone }; }, [model, onDone]);
  const { reduced } = useReducedMotion();
  const reducedRef = useRef(reduced);
  useEffect(() => { reducedRef.current = reduced; }, [reduced]);
  const [picture] = useState(() => `/assets/ui/matchmaking/${artwork[crypto.getRandomValues(new Uint32Array(1))[0]! % artwork.length]}.webp`);

  useEffect(() => {
    const el = root.current;
    if (!el) return;
    const controller = new AbortController();
    const { signal } = controller;
    const panels = Array.from(el.querySelectorAll<HTMLElement>(".mc-panel"));
    const canvas = el.querySelector<HTMLCanvasElement>("canvas")!;
    const film = el.querySelector<SVGSVGElement>("svg")!;
    const content = el.querySelector<HTMLElement>(".mc-content")!;
    const underlay = el.querySelector<HTMLElement>(".mc-underlay")!;
    let field: EdgeField | undefined;
    let frame = 0, ambient = 0, watchdog = 0, disposed = false, covered = false, opening = false;
    const center = new CenterWave(panels, Array.from(film.querySelectorAll("path")));
    const closing = new ClosingWave(panels);
    const resize = () => {
      const w = el.clientWidth, h = el.clientHeight;
      el.style.setProperty("--mc-width", `${w}px`); el.style.setProperty("--mc-height", `${h}px`);
      film.setAttribute("viewBox", `0 0 ${w} ${h}`);
      field?.resize(w, h); center.resize(w, h); closing.resize(w, h);
      if (el.dataset.phase === "closing") closing.paint(closing.progress);
    };
    const cover = () => { if (!covered && !disposed) { covered = true; latest.current.model.onCovered(); } };
    const finish = () => { if (!disposed) { cover(); latest.current.onDone(); } };
    const tween = (ms: number, paint: (p: number) => void, stop: () => boolean = () => false) => new Promise<void>(resolve => {
      const began = performance.now();
      const end = () => { signal.removeEventListener("abort", end); cancelAnimationFrame(frame); resolve(); };
      const step = (now: number) => {
        if (signal.aborted) { end(); return; }
        const p = reducedRef.current || document.hidden ? 1 : Math.min(1, (now - began) / ms);
        paint(p);
        if (p < 1 && !stop()) frame = requestAnimationFrame(step); else end();
      };
      signal.addEventListener("abort", end, { once: true }); frame = requestAnimationFrame(step);
    });
    const release = async () => {
      if (opening || signal.aborted) return;
      opening = true; cancelAnimationFrame(ambient);
      window.clearTimeout(watchdog);
      watchdog = window.setTimeout(finish, 5000);
      if (latest.current.model.failed && field) {
        el.dataset.outcome = "failure";
        field.closing = -1; field.forming = -1; field.alpha = .95;
        await tween(650, p => { field!.failure = smooth(p); field!.render(performance.now(), reducedRef.current); });
        if (signal.aborted) return;
      }
      cover();
      field?.capturePhase(); field?.pause();
      el.dataset.phase = "opening";
      content.style.pointerEvents = "none"; content.inert = true;
      await tween(3400, p => {
        content.style.opacity = String(1 - smooth(p / .12));
        content.style.transform = `translateY(${-8 * smooth(p / .12)}px)`;
        if (field) {
          field.forming = -1;
          field.returning = p < .6 ? p / .6 : -1;
          field.progress = p < .6 ? -1 : (p - .6) / .4;
          field.alpha = .95;
          if (p >= .6) center.paint(field.progress);
          field.render(performance.now(), reducedRef.current);
        } else center.paint(smooth(p));
        underlay.style.opacity = String(1 - smooth((p - .59) / .10));
      });
      if (!signal.aborted) finish();
    };
    const waitForMatch = () => {
      if (signal.aborted) return;
      if ((latest.current.model.ready || latest.current.model.failed)) { void release(); return; }
      if (!document.hidden && !reducedRef.current) field?.render(performance.now());
      ambient = requestAnimationFrame(waitForMatch);
    };
    const begin = async () => {
      try {
        field = new EdgeField(canvas);
        const syncRenderer = () => { film.dataset.renderer = field!.canvas.dataset.renderer; };
        syncRenderer(); field.canvas.addEventListener("rendererchange", syncRenderer, { signal });
        resize();
        el.dataset.phase = "closing"; content.style.opacity="0";
        await tween(980, p => { closing.paint(p); content.style.opacity=String(smooth((p-.6)/.4)); field!.closing = p; field!.render(performance.now()); });
        if (signal.aborted) return;
        underlay.style.opacity = "1";
        field.closing = -1;
        if ((latest.current.model.ready || latest.current.model.failed)) { void release(); return; }
        el.dataset.phase = "forming";
        await tween(3800, p => {
          field!.forming = p; field!.alpha = .95 * smooth((p - .35) / .65);
          field!.render(performance.now(), reducedRef.current);
        }, () => !!(latest.current.model.ready || latest.current.model.failed));
        if ((latest.current.model.ready || latest.current.model.failed)) { void release(); return; }
        if (signal.aborted || opening) return;
        field.forming = -1; field.alpha = .95; field.alignFlow();
        el.dataset.phase = "searching";
        waitForMatch();
      } catch {
        panels.forEach(panel => { panel.style.transform = "none"; panel.style.clipPath = "none"; });
        content.style.opacity = "1"; underlay.style.opacity = "1";
        waitForMatch();
      }
    };
    // Form controls are usable immediately, independently of the decorative first circuit.
    void begin();
    const observer = new ResizeObserver(resize); observer.observe(el);
    const visible = () => { if (document.hidden && (latest.current.model.ready || latest.current.model.failed)) finish(); };
    document.addEventListener("visibilitychange", visible);
    return () => {
      disposed = true; controller.abort(); cancelAnimationFrame(frame); cancelAnimationFrame(ambient);
      window.clearTimeout(watchdog); observer.disconnect(); document.removeEventListener("visibilitychange", visible); field?.destroy();
    };
  }, [mounted]);

  return <Dialog.Root open onOpenChange={open => { if (!open && !model.ready && !model.failed) model.onCancel(); }}>
    <Dialog.Portal><Dialog.Content ref={attach} className="uma-match-curtain" aria-describedby="mc-description"
      onPointerDownOutside={e => e.preventDefault()} onInteractOutside={e => e.preventDefault()}
      onEscapeKeyDown={e => { e.preventDefault(); if (!model.ready && !model.failed) model.onCancel(); }}
      onCloseAutoFocus={e => { if (model.ready) e.preventDefault(); }}>
      <Dialog.Title className="sr-only">{model.title}</Dialog.Title><Dialog.Description id="mc-description" className="sr-only">{model.description}</Dialog.Description>
      <div className="mc-underlay" aria-hidden="true" />
      <div className="mc-quadrants" aria-hidden="true">{["tl", "tr", "bl", "br"].map(corner => <div key={corner} className={`mc-panel mc-${corner}`}><div className="mc-panorama"><img src={picture} alt="" decoding="async" onError={e => { e.currentTarget.style.visibility = "hidden"; }} /><div className="mc-veil" /></div><span className="mc-join" /></div>)}</div>
      <canvas className="mc-field" aria-hidden="true" /><svg className="mc-film" aria-hidden="true"><path /><path /><path /><path /></svg>
      <div className="mc-content">{model.content}</div>
    </Dialog.Content></Dialog.Portal>
  </Dialog.Root>;
}
