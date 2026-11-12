import type { components } from "./types";

type Schemas = components["schemas"];
export type MemoryFlipCommand = Schemas["SubmissionCreate"];
export type ClassicGuessCommand = Schemas["SubmissionCreate"];
export type TradeCommand =
  | { kind: "propose"; body: Schemas["TradeCreate"] }
  | { kind: "respond"; tradeId: string; body: Schemas["TradeResponse"] };

function record(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0 && value.length <= 160; }
function integer(value: unknown, minimum: number): boolean { return typeof value === "number" && Number.isSafeInteger(value) && value >= minimum; }
export function validMemoryFlip(value: unknown): value is MemoryFlipCommand {
  const body = record(value), action = record(body?.modeAction);
  return !!body && integer(body.phaseVersion, 1) && integer(body.submissionRevision, 0) && integer(body.controlGeneration, 0) && action?.kind === "memory.flip" && text(action.cardId);
}
export function validClassicGuess(value: unknown): value is ClassicGuessCommand {
  const body = record(value), action = record(body?.modeAction);
  return !!body && integer(body.phaseVersion, 1) && integer(body.submissionRevision, 0) && integer(body.controlGeneration, 0) && action?.kind === "classic.guess" && text(action.characterId);
}
export function validTradeCommand(value: unknown): value is TradeCommand {
  const command = record(value), body = record(command?.body);
  if (!body) return false;
  if (command?.kind === "respond") return text(command.tradeId) && typeof body.accept === "boolean";
  return command?.kind === "propose" && integer(body.phaseVersion, 1) && text(body.recipientMembershipId) && text(body.offeredInstanceId) && text(body.requestedInstanceId);
}

export type ClueAssignmentCommand = Schemas["SubmissionCreate"];
export function validClueAssignment(value: unknown): value is ClueAssignmentCommand {
  const body = record(value), action = record(body?.modeAction);
  if (!body || !integer(body.phaseVersion, 1) || !integer(body.submissionRevision, 0) || !integer(body.controlGeneration, 0) || action?.kind !== "clue.assignment" || !Array.isArray(action.assignments) || action.assignments.length < 4 || action.assignments.length > 6) return false;
  const slots = new Set<string>(), cards = new Set<string>();
  for (const value of action.assignments) {
    const item = record(value);
    if (!item || typeof item.slotId !== "string" || !/^[1-6]$/.test(item.slotId) || !text(item.characterId) || slots.has(item.slotId) || cards.has(item.characterId)) return false;
    slots.add(item.slotId); cards.add(item.characterId);
  }
  return true;
}

export type RapportAnswerCommand = Schemas["SubmissionCreate"];
export function validRapportAnswer(value: unknown): value is RapportAnswerCommand {
  const body = record(value), action = record(body?.modeAction);
  return !!body && integer(body.phaseVersion, 1) && integer(body.submissionRevision, 0) && integer(body.controlGeneration, 0) && action?.kind === "rapport.answer" && text(action.optionId);
}

export type SortOrderCommand = Schemas["SubmissionCreate"];
export function validSortOrder(value: unknown): value is SortOrderCommand {
  const body = record(value), action = record(body?.modeAction);
  if (!body || !integer(body.phaseVersion, 1) || !integer(body.submissionRevision, 0) || !integer(body.controlGeneration, 0) || action?.kind !== "sort.order" || !Array.isArray(action.groups) || !action.groups.length || action.groups.length > 6) return false;
  const seen = new Set<string>();
  for (const group of action.groups) {
    if (!Array.isArray(group) || !group.length || group.length > 6) return false;
    for (const id of group) { if (!text(id) || seen.has(id)) return false; seen.add(id); }
  }
  return seen.size >= 2 && seen.size <= 6;
}

export type GridClaimCommand = Schemas["SubmissionCreate"];
export function validGridClaim(value: unknown): value is GridClaimCommand {
  const body = record(value), action = record(body?.modeAction);
  return !!body && integer(body.phaseVersion, 1) && integer(body.submissionRevision, 0) && integer(body.controlGeneration, 0) && action?.kind === "grid.claim" && text(action.cellId) && text(action.characterId);
}

export type DraftLineupCommand = Schemas["SubmissionCreate"];
export function validDraftLineup(value: unknown): value is DraftLineupCommand {
  const body = record(value), action = record(body?.modeAction);
  if (!body || !integer(body.phaseVersion, 1) || !integer(body.submissionRevision, 0) || !integer(body.controlGeneration, 0) || action?.kind !== "draft.lineup" || !Array.isArray(action.assignments) || action.assignments.length !== 4) return false;
  const slots = new Set<string>(), cards = new Set<string>();
  for (const value of action.assignments) {
    const item = record(value);
    if (!item || !text(item.slotId) || !text(item.characterId) || slots.has(item.slotId) || cards.has(item.characterId)) return false;
    slots.add(item.slotId); cards.add(item.characterId);
  }
  return true;
}

export type RaceOrderCommand = Schemas["SubmissionCreate"];
export function validRaceOrder(value: unknown): value is RaceOrderCommand {
  const body = record(value), action = record(body?.modeAction);
  if (!body || !integer(body.phaseVersion, 1) || !integer(body.submissionRevision, 0) || !integer(body.controlGeneration, 0) || action?.kind !== "race.order" || !Array.isArray(action.groups) || !action.groups.length || action.groups.length > 7) return false;
  const ids = new Set<string>();
  for (const group of action.groups) {
    if (!Array.isArray(group) || !group.length || group.length > 7) return false;
    for (const id of group) { if (!text(id) || ids.has(id)) return false; ids.add(id); }
  }
  return ids.size >= 4 && ids.size <= 7;
}

export type PathRouteCommand = Schemas["SubmissionCreate"];
export function validPathRoute(value:unknown): value is PathRouteCommand {
  const body=record(value),action=record(body?.modeAction);
  return !!body && integer(body.phaseVersion,1) && integer(body.submissionRevision,0) && integer(body.controlGeneration,0) && action?.kind==="path.route" && Array.isArray(action.characterIds) && action.characterIds.length>=2 && action.characterIds.length<=7 && action.characterIds.every(text) && new Set(action.characterIds).size===action.characterIds.length;
}

export type PedigreeAssignmentCommand = Schemas["SubmissionCreate"];
export function validPedigreeAssignment(value:unknown):value is PedigreeAssignmentCommand {
 const body=record(value),action=record(body?.modeAction);
 if(!body||!integer(body.phaseVersion,1)||!integer(body.submissionRevision,0)||!integer(body.controlGeneration,0)||action?.kind!=="pedigree.assignment"||!Array.isArray(action.assignments)||action.assignments.length<1||action.assignments.length>6)return false;
 const slots=new Set<string>(),horses=new Set<string>();
 for(const raw of action.assignments){const a=record(raw);if(!a||!text(a.slotId)||!text(a.horseId)||slots.has(a.slotId)||horses.has(a.horseId))return false;slots.add(a.slotId);horses.add(a.horseId);}
 return true;
}

export type AudioCommand = Schemas["SubmissionCreate"];
export function validAudioCommand(value:unknown):value is AudioCommand {
 const body=record(value),a=record(body?.modeAction);
 if(!body||!integer(body.phaseVersion,1)||!integer(body.submissionRevision,0)||!integer(body.controlGeneration,0)||!a||!Array.isArray(a.playbackFailures)||a.playbackFailures.length>6||!a.playbackFailures.every(text)||new Set(a.playbackFailures).size!==a.playbackFailures.length)return false;
 const clips=new Set<string>(a.playbackFailures as string[]),characters=new Set<string>();
 if(a.kind==="audio.ready"){
  if(!Array.isArray(a.readyClipIds)||a.readyClipIds.length>6||!a.readyClipIds.every(text))return false;
  for(const id of a.readyClipIds){if(clips.has(id))return false;clips.add(id);}
 }else if(a.kind==="audio.matches"){
  if(!Array.isArray(a.matches)||a.matches.length>6)return false;
  for(const raw of a.matches){const m=record(raw);if(!m||!text(m.clipId)||!text(m.characterId)||clips.has(m.clipId)||characters.has(m.characterId))return false;clips.add(m.clipId);characters.add(m.characterId);}
 }else return false;
 return clips.size>=2&&clips.size<=6;
}

export type BluffCommand=Schemas["SubmissionCreate"];
export function validBluffCommand(value:unknown):value is BluffCommand{
 const b=record(value),a=record(b?.modeAction);
 if(!b||!integer(b.phaseVersion,1)||!integer(b.submissionRevision,0)||!integer(b.controlGeneration,0)||!a)return false;
 if(a.kind==="bluff.vote")return text(a.optionId);
 if(a.kind!=="bluff.decoy"||typeof a.text!=="string"||a.text!==a.text.normalize("NFC").trim())return false;
 const count=Array.from(a.text).length;return count>=2&&count<=40;
}
export type BluffReviewCommand=Schemas["TransitionCreate"];
export function validBluffReview(value:unknown):value is BluffReviewCommand{
 const b=record(value);return !!b&&b.target==="bluff-voting"&&integer(b.expectedPhaseVersion,1)&&Array.isArray(b.excludedOptionIds)&&b.excludedOptionIds.length<=32&&b.excludedOptionIds.every(text)&&new Set(b.excludedOptionIds).size===b.excludedOptionIds.length;
}

export type StrategyCommand=Schemas["SubmissionCreate"];
export function validStrategyCommand(value:unknown):value is StrategyCommand {
 const b=record(value),a=record(b?.modeAction);
 return !!b&&integer(b.phaseVersion,1)&&integer(b.submissionRevision,0)&&integer(b.controlGeneration,0)&&!!a&&a.kind==="strategy.action"&&integer(a.turn,1)&&Number(a.turn)<=32&&["steady","sprint","recover","follow"].includes(String(a.action));
}

export type CreativeCommand=Schemas["SubmissionCreate"];
export function validCreativeCommand(value:unknown):value is CreativeCommand {
 const b=record(value),a=record(b?.modeAction);
 if(!b||!integer(b.phaseVersion,1)||!integer(b.submissionRevision,0)||!integer(b.controlGeneration,0)||!a)return false;
 if(a.kind==="creative.vote")return text(a.optionId)&&b.controlGeneration===0;
 if(a.kind!=="creative.work"||typeof a.text!=="string"||a.text!==a.text.normalize("NFC").trim())return false;
 const count=Array.from(a.text).length;return count>=1&&count<=80;
}
export type CreativeReviewCommand=Schemas["TransitionCreate"];
export function validCreativeReview(value:unknown):value is CreativeReviewCommand {
 const b=record(value);return !!b&&b.target==="creative-voting"&&integer(b.expectedPhaseVersion,1)&&Array.isArray(b.excludedOptionIds)&&b.excludedOptionIds.length<=8&&b.excludedOptionIds.every(text)&&new Set(b.excludedOptionIds).size===b.excludedOptionIds.length;
}
