import { useCallback, useEffect, useRef, useState, type ComponentType } from "react";
import { useBlocker, type Blocker } from "react-router";
import { useReducedMotion } from "@/lib/hooks/use-reduced-motion";
import { canAnimateRoute } from "./route-curtain-policy";

export type RouteCurtainProps = { blocker: Blocker; allowBlocking: (allowed: boolean) => void; reduced: boolean };

/** Decoration loads after first paint; a failed download must not block navigation. */
export function DeferredRouteCurtain() {
  const { reduced } = useReducedMotion();
  const canBlock = useRef(true);
  const allowBlocking = useCallback((allowed: boolean) => { canBlock.current = allowed; }, []);
  const [Curtain, setCurtain] = useState<ComponentType<RouteCurtainProps> | null>(null);
  const [failed, setFailed] = useState(false);
  const blocker = useBlocker(({ currentLocation, nextLocation }) => !nextLocation.state?.umaMatchCurtain && !failed && !reduced && !document.hidden && canBlock.current && typeof Element.prototype.animate === "function" && canAnimateRoute(currentLocation.pathname, nextLocation.pathname));
  useEffect(() => {
    let active = true;
    void import("./route-curtain").then(module => {
      if (active) setCurtain(() => module.RouteCurtain);
    }).catch(() => { if (active) setFailed(true); });
    return () => { active = false; };
  }, []);
  useEffect(() => {
    if (Curtain || blocker.state !== "blocked") return;
    // A failed or stalled decoration download must not trap the first navigation
    const timer = setTimeout(() => blocker.proceed(), failed || reduced ? 0 : 2500);
    return () => clearTimeout(timer);
  }, [Curtain, blocker, failed, reduced]);
  return Curtain ? <Curtain blocker={blocker} allowBlocking={allowBlocking} reduced={reduced} /> : null;
}
