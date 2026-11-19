import type { Mode } from "@/lib/api/types";
import type { PlayerForm } from "@/lib/deployment/capabilities";

/** Fixed entry rules have a duration; configurable rounds defer to their setup. */
export function entryTimeKey(modeId: string, form: PlayerForm): string {
  if (form === "team") return "playTime.program";
  if (form === "multiplayer") return modeId === "classic" ? "playTime.threeMinutes" : "playTime.settings";
  switch (modeId) {
    case "classic": return "playTime.ownPace";
    case "memory": return "playTime.memory";
    case "sort":
    case "audio": return "playTime.ninetySeconds";
    case "draft": return "playTime.fourMinutes";
    case "strategy-race": return "playTime.strategy";
    case "aptitude-grid":
    case "race-rebuild":
    case "co-race-path":
    case "pedigree": return "playTime.threeMinutes";
    default: return "playTime.settings";
  }
}

export function friendSeats(mode: Pick<Mode, "modeId" | "minPlayers" | "maxPlayers">, instanceLimit: number) {
  return { min: Math.max(2, mode.minPlayers), max: Math.min(instanceLimit, mode.modeId === "classic" ? 2 : mode.maxPlayers) };
}
