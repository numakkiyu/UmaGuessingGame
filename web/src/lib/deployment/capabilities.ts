import type { PublicConfig } from "@/lib/api/types";

export type PlayForm = "local" | "verified" | "solo" | "friend" | "matchmaking" | "event";

export function canPlay(config: PublicConfig | undefined, modeId: string, form: PlayForm): boolean {
  const caps = config?.modeCapabilities;
  return caps?.[modeId]?.[form] === true;
}
export function anyCanPlay(config: PublicConfig | undefined, form: PlayForm): boolean {
  const caps = config?.modeCapabilities;
  return caps ? Object.values(caps).some((value) => value[form] === true) : false;
}

/** Choose an existing player entry using advertised capabilities, never a runtime selector. */
export function modeEntry(config: PublicConfig | undefined, modeId: string): string | null {
  if (canPlay(config, modeId, "local")) return `/play/${modeId}`;
  if (canPlay(config, modeId, "verified")) return `/play/${modeId}/verified`;
  if (canPlay(config, modeId, "solo")) return `/play/${modeId}/online`;
  if (canPlay(config, modeId, "friend")) return `/rooms/new?mode=${encodeURIComponent(modeId)}`;
  if (canPlay(config, modeId, "event")) return "/events/new";
  if (canPlay(config, modeId, "matchmaking")) return `/matchmaking?mode=${encodeURIComponent(modeId)}`;
  return null;
}

/** Player-facing forms group implementation paths without exposing deployment. */
export type PlayerForm = "single" | "multiplayer" | "team";
export function formEntry(config: PublicConfig | undefined, modeId: string, form: PlayerForm): string | null {
  if (form === "single") {
    if (canPlay(config, modeId, "local")) return `/play/${modeId}`;
    if (canPlay(config, modeId, "verified")) return `/play/${modeId}/verified`;
    if (canPlay(config, modeId, "solo")) return `/play/${modeId}/online`;
  }
  if (form === "multiplayer") {
    if (canPlay(config, modeId, "friend")) return `/rooms/new?mode=${encodeURIComponent(modeId)}`;
    if (canPlay(config, modeId, "matchmaking")) return `/matchmaking?mode=${encodeURIComponent(modeId)}`;
  }
  if (form === "team" && canPlay(config, modeId, "event")) return `/events/new?mode=${encodeURIComponent(modeId)}`;
  return null;
}
