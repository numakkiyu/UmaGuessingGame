import { toKnownModeId, type ModeId } from "@/lib/api/types";

const PREFIX = "umaguess:event-form:";
export type EventForm = { name: string; modeId: ModeId | null; players: string | null; spectators: string | null; tiePolicy: "shared-rank" | "announced-tiebreak" };
export const emptyEventForm = (): EventForm => ({ name: "", modeId: null, players: null, spectators: null, tiePolicy: "shared-rank" });
const key = (identity: string | null) => PREFIX + (identity ?? "guest");

/** Unsent choices only: never credentials, keys, requests or an implicit retry. */
export function readEventForm(identity: string | null, now = Date.now()): EventForm | null {
  try {
    const raw = sessionStorage.getItem(key(identity));
    if (!raw || raw.length > 4096) return null;
    const v = JSON.parse(raw);
    const count = (n: unknown) => n === null || typeof n === "string" && n.length <= 16 && /^[0-9eE+.-]*$/.test(n);
    if (!v || !Number.isSafeInteger(v.savedAt) || v.savedAt > now + 60_000 || v.savedAt <= now - 86400000 || typeof v.name !== "string" || [...v.name].length > 256 || !count(v.players) || !count(v.spectators) || !["shared-rank", "announced-tiebreak"].includes(v.tiePolicy) || v.modeId !== null && !toKnownModeId(v.modeId)) return null;
    return { name: v.name, modeId: v.modeId, players: v.players, spectators: v.spectators, tiePolicy: v.tiePolicy };
  } catch { return null; }
}
export function saveEventForm(identity: string | null, form: EventForm): boolean {
  try { sessionStorage.setItem(key(identity), JSON.stringify({ ...form, savedAt: Date.now() })); return true; } catch { return false; }
}
export function clearEventForm(identity: string | null): void {
  try { sessionStorage.removeItem(key(identity)); } catch { /* Never affect the creation receipt. */ }
}
