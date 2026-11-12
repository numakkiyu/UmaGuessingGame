import type { components } from "@/lib/api/types";
import { toKnownModeId } from "@/lib/api/types";
type S = components["schemas"];
export type EventHostCommand =
 | { kind: "transition"; body: S["EventTransitionCreate"] }
 | { kind: "segment"; body: S["SegmentCreate"] }
 | { kind: "round"; roomId: string; body: S["RoundCreate"] }
 | { kind: "correction"; body: S["CorrectionCreate"] }
 | { kind: "grant" };
const record = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const keys = (v: Record<string,unknown>, allowed: string[]) => Object.keys(v).every(k => allowed.includes(k));
const text = (v: unknown, min: number, max: number): v is string => typeof v === "string" && [...v.trim()].length >= min && [...v].length <= max;
const integer = (v: unknown, min: number, max: number): v is number => Number.isSafeInteger(v) && Number(v) >= min && Number(v) <= max;
function mode(v: unknown): boolean {
 if (!record(v) || !toKnownModeId(String(v.modeId))) return false;
 if (v.modeId === "classic") return keys(v,["modeId","variant","maxWrongGuesses","winPolicy","durationSeconds","shadowEnabled"]) && (v.variant === undefined || ["single","public","friend"].includes(String(v.variant))) && ["solo","window-attempts","first-correct"].includes(String(v.winPolicy)) && integer(v.maxWrongGuesses,4,12) && integer(v.durationSeconds,0,Number.MAX_SAFE_INTEGER) && typeof v.shadowEnabled === "boolean";
 return keys(v,["modeId","rulesVersion","contentPackId","informationStyle","config","cooperative","openBook"]) && (v.rulesVersion === undefined || text(v.rulesVersion,1,64)) && (v.contentPackId === undefined || typeof v.contentPackId === "string" && v.contentPackId.length <= 160) && (v.informationStyle === undefined || ["open","closed"].includes(String(v.informationStyle))) && (v.config === undefined || record(v.config)) && (v.cooperative === undefined || typeof v.cooperative === "boolean") && (v.openBook === undefined || typeof v.openBook === "boolean");
}
/** Only saved command fields can be replayed; locale never reconstructs a body. */
export function validEventHostCommand(value: unknown): value is EventHostCommand {
 if (!record(value)) return false;
 if (value.kind === "grant") return keys(value,["kind"]);
 if (!keys(value,value.kind === "round" ? ["kind","roomId","body"] : ["kind","body"]) || !record(value.body)) return false;
 const b = value.body;
 switch(value.kind) {
  case "transition": return keys(b,["target","expectedEventVersion","reason"]) && ["lobby","running","completed","cancelled"].includes(String(b.target)) && integer(b.expectedEventVersion,1,Number.MAX_SAFE_INTEGER) && (b.reason === undefined || text(b.reason,1,240));
  case "segment": return keys(b,["modeConfig","label","scoreBudget","roundCount","competitive"]) && mode(b.modeConfig) && text(b.label,1,48) && integer(b.scoreBudget,0,100) && integer(b.roundCount,1,20) && typeof b.competitive === "boolean";
  case "round": return text(value.roomId,1,160) && keys(b,["segmentId","modeConfig"]) && text(b.segmentId,1,160) && mode(b.modeConfig);
  case "correction": return keys(b,["segmentId","subjectId","newScoreUnits","reason","expectedEventVersion"]) && text(b.segmentId,1,160) && text(b.subjectId,1,160) && integer(b.newScoreUnits,0,10000) && text(b.reason,5,240) && integer(b.expectedEventVersion,1,Number.MAX_SAFE_INTEGER);
  default: return false;
 }
}
export function wholeNumber(value: string, min: number, max: number): number | null {
 if (!/^\d+$/.test(value.trim())) return null;
 const n=Number(value);return integer(n,min,max)?n:null;
}
export function scoreUnits(value: string, maxPoints: number): number | null {
 if (!/^\d+(?:\.\d{1,2})?$/.test(value.trim())) return null;
 const [whole,fraction=""]=value.trim().split(".");
 const units=Number(whole)*100+Number(fraction.padEnd(2,"0"));
 return integer(units,0,maxPoints*100)?units:null;
}
