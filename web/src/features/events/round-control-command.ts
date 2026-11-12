import type { components } from "@/lib/api/types";
export type RoundControlCommand = components["schemas"]["TransitionCreate"];
const targets = ["briefing","collecting","locked","revealed","settled","pause","resume","cancel"];
export function validRoundControl(value: unknown): value is RoundControlCommand {
 if (!value || typeof value !== "object" || Array.isArray(value)) return false;
 const b=value as Record<string,unknown>;
 return Object.keys(b).every(key=>["target","expectedPhaseVersion","extensionSeconds","reason"].includes(key)) && targets.includes(String(b.target)) && Number.isSafeInteger(b.expectedPhaseVersion) && Number(b.expectedPhaseVersion)>0 && (b.extensionSeconds===undefined || b.target==="collecting" && b.extensionSeconds===30) && (b.reason===undefined && b.target!=="cancel" || b.target==="cancel" && typeof b.reason==="string" && [...b.reason].length>=5 && [...b.reason].length<=240);
}
/** Windowed modes cannot skip questions; review/vote modes own their close step. */
export function manualRoundLock(modeId: string): boolean {
 return !["trade","rapport","strategy-race","creative","bluff"].includes(modeId);
}
