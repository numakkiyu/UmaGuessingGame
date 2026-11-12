import type { components, Mode } from "@/lib/api/types";
import { packModeConfig } from "@/lib/api/types";

export type EventModeConfig = components["schemas"]["ModeConfig"];

/** Defaults for new items only. Existing items always keep their saved rules. */
export function newEventModeConfig(mode: Mode): EventModeConfig {
  if (mode.modeId === "classic") return {modeId:"classic",variant:"public",maxWrongGuesses:8,winPolicy:"window-attempts",durationSeconds:180,shadowEnabled:false};
  return packModeConfig(mode.modeId, mode.rulesVersion, mode.informationStyle);
}

export function entertainmentOnlyMode(modeId: string | undefined): boolean { return modeId === "creative" || modeId === "trade"; }
