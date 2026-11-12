
import { useEffect, useMemo, useState, useSyncExternalStore } from "react";

const STORAGE_KEY = "umaguess:motion-preference";

export type MotionPreference = "system" | "reduced" | "full";

let preference: MotionPreference | undefined;
const subscribers = new Set<() => void>();

function readStored(): MotionPreference {
  if (typeof localStorage === "undefined") return "system";
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw === "reduced" || raw === "full" ? raw : "system";
  } catch {
    return preference ?? "system";
  }
}

function systemPrefersReduced(): boolean {
  if (typeof matchMedia !== "function") return false;
  return matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function applyMotion(next: MotionPreference, systemReduced = systemPrefersReduced()) {
  if (next === "reduced" || (next === "system" && systemReduced)) {
    document.documentElement.setAttribute("data-motion", "reduced");
  } else {
    document.documentElement.removeAttribute("data-motion");
  }
}

function getPreference(): MotionPreference {
  return preference ??= readStored();
}

function onStorage(event: StorageEvent) {
  if (event.key !== null && event.key !== STORAGE_KEY) return;
  preference = readStored();
  applyMotion(preference);
  subscribers.forEach(listener => listener());
}

function subscribe(listener: () => void) {
  if (subscribers.size === 0) {
    preference = readStored();
    window.addEventListener("storage", onStorage);
  }
  subscribers.add(listener);
  return () => {
    subscribers.delete(listener);
    if (subscribers.size === 0) window.removeEventListener("storage", onStorage);
  };
}

function setPreference(next: MotionPreference) {
  preference = next;
  // Keep every consumer in agreement, including the root provider. Otherwise
  // an OS change can overwrite the choice still selected on the help page.
  applyMotion(next);
  try {
    if (next === "system") localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, next);
  } catch { /* The active page keeps the shared in-memory preference. */ }
  subscribers.forEach(listener => listener());
}

export type ReducedMotionState = {
  /** 是否应当降级动画 */
  reduced: boolean;
  /** 用户的显式选择 */
  preference: MotionPreference;
  setPreference: (next: MotionPreference) => void;
  /** 系统偏好（用于在界面上解释当前为何降级） */
  systemReduced: boolean;
};

export function useReducedMotion(): ReducedMotionState {
  const preference = useSyncExternalStore(subscribe, getPreference, () => "system" as const);
  const [systemReduced, setSystemReduced] = useState<boolean>(() => systemPrefersReduced());

  useEffect(() => {
    if (typeof matchMedia !== "function") return;
    const query = matchMedia("(prefers-reduced-motion: reduce)");
    const onChange = () => setSystemReduced(query.matches);
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  const reduced = preference === "reduced" || (preference === "system" && systemReduced);

  // 立即生效：改属性而不是等 React 重渲染（「选择立即生效」）
  useEffect(() => {
    applyMotion(preference, systemReduced);
  }, [preference, systemReduced]);

  return useMemo(
    () => ({ reduced, preference, setPreference, systemReduced }),
    [reduced, preference, systemReduced],
  );
}
