import { PedigreeNotice } from "@/i18n/pedigree";
import type { LocalizedNames } from "@/i18n/entities";
import { practiceFinished, type PracticeBase } from "./aptitude-practice";
export const PEDIGREE_DURATION = 180000;
export const PEDIGREE_DISCLAIMER = "史实原型血统：本题展示的是角色所依据的真实赛马的祖先关系，不是角色之间的家庭关系。";
export const PEDIGREE_SLOTS = [
  { slotId: "sire", label: "父", means: "原型马的父亲", branch: "sire", generation: 1 },
  { slotId: "dam", label: "母", means: "原型马的母亲", branch: "dam", generation: 1 },
  { slotId: "sireSire", label: "父父", means: "原型马父亲的父亲", branch: "sire", generation: 2 },
  { slotId: "sireDam", label: "父母", means: "原型马父亲的母亲", branch: "sire", generation: 2 },
  { slotId: "damSire", label: "母父", means: "原型马母亲的父亲", branch: "dam", generation: 2 },
  { slotId: "damDam", label: "母母", means: "原型马母亲的母亲", branch: "dam", generation: 2 },
] as const;
export type PedigreeSlotId = typeof PEDIGREE_SLOTS[number]["slotId"];
export type PedigreeHorse = { horseId: string; name: string; nkbId: string | null };
export type PedigreeFamily = { subject: { characterId: string; characterName: string; prototypeName: string; nkbId: string; localizedNames?: LocalizedNames }; relations: Record<PedigreeSlotId, string> };
export type PedigreeLibrary = { horses: PedigreeHorse[]; families: PedigreeFamily[] };
export type PedigreeAssignment = { slotId: PedigreeSlotId; horseId: string };
export type PedigreeGame = PracticeBase & { modeId: "pedigree"; characterId: string; slotIds: PedigreeSlotId[]; candidateIds: string[]; draft: PedigreeAssignment[]; submitted: PedigreeAssignment[] | null; submittedAt: number | null; submissions: number };
export function normalizedHorseName(name: string) { return name.trim().replace(/\s*\((?:USA|IRE|CAN|GB|FR|JPN|AUS|NZ|GER|BRA|BRZ|ARG|CHI|SAU|UAE|KOR|IND|PER|URU|SWE|DEN|ITY|SPA|POL|TUR|CZE|HUN|NOR|FIN)\)\s*$/, "").trim().replace(/\s+/g, " "); }
export function pedigreeAssignmentsValid(raw: unknown, slots: readonly PedigreeSlotId[], candidates: readonly string[]): raw is PedigreeAssignment[] {
  if (!Array.isArray(raw) || raw.length > slots.length) return false;
  const usedSlots = new Set(), usedHorses = new Set();
  for (const a of raw) {
    if (!a || typeof a !== "object" || !slots.includes(a.slotId as PedigreeSlotId) || !candidates.includes(a.horseId as string) || usedSlots.has(a.slotId) || usedHorses.has(a.horseId)) return false;
    usedSlots.add(a.slotId); usedHorses.add(a.horseId);
  }
  return true;
}
export function changePedigree(game: PedigreeGame, assignments: PedigreeAssignment[], submit: boolean, now: number): PedigreeGame {
  if (practiceFinished(game, now)) throw new PedigreeNotice("stopped");
  if (!pedigreeAssignmentsValid(assignments, game.slotIds, game.candidateIds)) throw new PedigreeNotice("unique");
  if (submit && !assignments.length) throw new PedigreeNotice("empty");
  const ordered = [...assignments].sort((a, b) => game.slotIds.indexOf(a.slotId) - game.slotIds.indexOf(b.slotId)).map(a => ({ slotId: a.slotId, horseId: a.horseId }));
  return { ...game, draft: ordered, submitted: submit ? ordered.map(a => ({ ...a })) : game.submitted, submittedAt: submit ? now : game.submittedAt, submissions: game.submissions + (submit ? 1 : 0), updatedAt: now, revision: game.revision + 1 };
}
export function pedigreeResult(game: PedigreeGame, family: PedigreeFamily) {
  const rows = game.slotIds.map(slotId => { const selected = game.submitted?.find(a => a.slotId === slotId)?.horseId ?? null, expected = family.relations[slotId]; return { slotId, selected, expected, correct: selected === expected }; });
  const correct = rows.filter(r => r.correct).length;
  return { rows, correct, total: rows.length, scoreUnits: Math.floor(100 * correct / rows.length) * 100 };
}
