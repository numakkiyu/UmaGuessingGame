import { useMemo, useSyncExternalStore } from "react";
import { TRAINER_AVATAR_IDS } from "./trainer-avatar-ids";

export const TRAINER_PROFILE_KEY = "umaguess:trainer-profile";
export const TRAINER_AVATARS = TRAINER_AVATAR_IDS;
export type TrainerProfile = { displayName: string; avatarId: string };
const empty: TrainerProfile = { displayName: "", avatarId: "1001" };
let memory = "";
let memoryOnly = false;

function snapshot(): string {
  if (memoryOnly) return memory;
  try { return localStorage.getItem(TRAINER_PROFILE_KEY) ?? ""; } catch { return memory; }
}
function parse(raw: string): TrainerProfile {
  try {
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== "object") return empty;
    const item = value as Record<string, unknown>;
    return {
      displayName: typeof item.displayName === "string" && [...item.displayName].length <= 24 && ![...item.displayName].some(char => char.codePointAt(0)! < 32 || char.codePointAt(0) === 127) ? item.displayName.trim() : "",
      avatarId: TRAINER_AVATARS.includes(item.avatarId as typeof TRAINER_AVATARS[number]) ? String(item.avatarId) : "1001",
    };
  } catch { return empty; }
}
function subscribe(listener: () => void) {
  const storage = (event: StorageEvent) => { if (event.key === null || event.key === TRAINER_PROFILE_KEY) { memoryOnly = false; listener(); } };
  window.addEventListener("storage", storage);
  window.addEventListener("umaguess:profile", listener);
  return () => { window.removeEventListener("storage", storage); window.removeEventListener("umaguess:profile", listener); };
}
export function readTrainerProfile(): TrainerProfile { return parse(snapshot()); }
export function saveTrainerProfile(value: TrainerProfile): void {
  memory = JSON.stringify(parse(JSON.stringify(value)));
  try { localStorage.setItem(TRAINER_PROFILE_KEY, memory); memoryOnly = false; } catch { memoryOnly = true; }
  window.dispatchEvent(new Event("umaguess:profile"));
}
export function useTrainerProfile(): TrainerProfile {
  const raw = useSyncExternalStore(subscribe, snapshot, () => "");
  return useMemo(() => parse(raw), [raw]);
}
