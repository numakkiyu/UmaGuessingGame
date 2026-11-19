import type { components } from "@/lib/api/schema";
import { validLocalizedNames } from "@/i18n/entities";
import { PEDIGREE_DISCLAIMER, PEDIGREE_DURATION, PEDIGREE_SLOTS, normalizedHorseName, pedigreeAssignmentsValid, type PedigreeFamily, type PedigreeGame, type PedigreeHorse, type PedigreeLibrary, type PedigreeSlotId } from "@/lib/domain/pedigree-practice";
import { activityStore } from "./activity-store";
import { incompletePractice as invalid, practiceId, practiceObject as object, readPracticeBase, readPracticeEnvelope } from "./practice-envelope";
import { newPracticeId } from "./practice-store";
import { shufflePractice } from "./random-draw";
export type PedigreePack = components["schemas"]["PedigreePracticePack"];
export type PedigreeArchive = { schemaVersion: 1; pack: PedigreePack; game: PedigreeGame };
export const pedigreePackKey = (p: PedigreePack) => `${p.modeId}:${p.contentPackVersion}:${p.sha256}`;
const text = (v: unknown): v is string => typeof v === "string" && v.trim().length > 0 && v.length <= 160;
const slotId = (v: unknown): v is PedigreeSlotId => PEDIGREE_SLOTS.some(s => s.slotId === v);
export function readPedigreePack(raw: unknown): { pack: PedigreePack; library: PedigreeLibrary } {
  const { pack, content } = readPracticeEnvelope(raw, "pedigree");
  if (content.disclaimer !== PEDIGREE_DISCLAIMER || !Array.isArray(content.slots) || content.slots.length !== 6 || !Array.isArray(content.horses) || content.horses.length < 10 || content.horses.length > 2000 || !Array.isArray(content.families) || !content.families.length || content.families.length > 500) throw invalid();
  content.slots.forEach((s, i) => { const expected = PEDIGREE_SLOTS[i]!; if (!object(s) || s.slotId !== expected.slotId || s.label !== expected.label || s.generation !== expected.generation) throw invalid(); });
  const horses: PedigreeHorse[] = content.horses.map(h => { if (!object(h) || !practiceId(h.horseId) || !text(h.name) || !normalizedHorseName(h.name) || h.nkbId !== undefined && h.nkbId !== null && h.nkbId !== "" && !practiceId(h.nkbId)) throw invalid(); return { horseId: h.horseId, name: h.name, nkbId: typeof h.nkbId === "string" && h.nkbId ? h.nkbId : null }; });
  const ids = new Set(horses.map(h => h.horseId)), horseNames = new Map(horses.map(h => [h.horseId, normalizedHorseName(h.name)]));
  if (ids.size !== horses.length || new Set(horseNames.values()).size !== horses.length) throw invalid();
  const families: PedigreeFamily[] = content.families.map(f => {
    if (!object(f) || !object(f.subject) || !object(f.relations)) throw invalid();
    const s = f.subject, r = f.relations;
    if (!practiceId(s.characterId) || !text(s.characterName) || !text(s.prototypeName) || !practiceId(s.nkbId) || !validLocalizedNames(s.localizedNames) || Object.keys(r).length !== 6) throw invalid();
    const relations = {} as PedigreeFamily["relations"];
    for (const slot of PEDIGREE_SLOTS) { const id = r[slot.slotId]; if (typeof id !== "string" || !ids.has(id) || horseNames.get(id) === normalizedHorseName(s.prototypeName)) throw invalid(); relations[slot.slotId] = id; }
    if (new Set(Object.values(relations)).size !== 6) throw invalid();
    return { subject: { characterId: s.characterId, characterName: s.characterName, prototypeName: s.prototypeName, nkbId: s.nkbId, ...(s.localizedNames ? { localizedNames: s.localizedNames } : {}) }, relations };
  });
  for (const key of ["characterId", "nkbId", "prototypeName"] as const) if (new Set(families.map(f => key === "prototypeName" ? normalizedHorseName(f.subject[key]) : f.subject[key])).size !== families.length) throw invalid();
  return { pack, library: { horses, families } };
}
export function createPedigreeGame(pack: PedigreePack, count = 4, now = Date.now()): PedigreeGame {
  if (![4, 5, 6].includes(count)) throw invalid();
  const { library } = readPedigreePack(pack), family = shufflePractice(library.families, now)[0]!;
  const chosen = new Set(["sire", "dam", ...shufflePractice(PEDIGREE_SLOTS.slice(2), now).slice(0, count - 2).map(s => s.slotId)]), slotIds = PEDIGREE_SLOTS.filter(s => chosen.has(s.slotId)).map(s => s.slotId);
  const correct = slotIds.map(id => family.relations[id]), distractors = shufflePractice(library.horses.filter(h => !correct.includes(h.horseId)), now).slice(0, 10 - count).map(h => h.horseId);
  return { schemaVersion: 1, modeId: "pedigree", gameId: newPracticeId(), packKey: pedigreePackKey(pack), rulesVersion: pack.rulesVersion, characterId: family.subject.characterId, slotIds, candidateIds: shufflePractice([...correct, ...distractors], now), draft: [], submitted: null, submittedAt: null, submissions: 0, createdAt: now, updatedAt: now, deadlineAt: now + PEDIGREE_DURATION, ended: false, revision: 0 };
}
export function validatePedigreeArchive(raw: unknown): PedigreeArchive {
  if (!object(raw) || raw.schemaVersion !== 1 || !object(raw.game)) throw invalid();
  const { pack, library } = readPedigreePack(raw.pack), g = raw.game, base = readPracticeBase(g, pack, PEDIGREE_DURATION), family = library.families.find(f => f.subject.characterId === g.characterId);
  if (!family || !Array.isArray(g.slotIds) || ![4, 5, 6].includes(g.slotIds.length) || !g.slotIds.every(slotId) || new Set(g.slotIds).size !== g.slotIds.length || g.slotIds[0] !== "sire" || g.slotIds[1] !== "dam" || g.slotIds.some((id, i, all) => i > 0 && PEDIGREE_SLOTS.findIndex(s => s.slotId === id) <= PEDIGREE_SLOTS.findIndex(s => s.slotId === all[i - 1]))) throw invalid();
  const slots = g.slotIds as PedigreeSlotId[];
  if (!Array.isArray(g.candidateIds) || g.candidateIds.length !== 10 || new Set(g.candidateIds).size !== 10 || !g.candidateIds.every(id => typeof id === "string" && library.horses.some(h => h.horseId === id)) || !slots.every(id => (g.candidateIds as string[]).includes(family.relations[id]))) throw invalid();
  const candidates = g.candidateIds as string[];
  if (!pedigreeAssignmentsValid(g.draft, slots, candidates) || g.submitted !== null && !pedigreeAssignmentsValid(g.submitted, slots, candidates) || !Number.isSafeInteger(g.submissions) || Number(g.submissions) < 0 || Number(g.submissions) > base.revision) throw invalid();
  if (g.submitted !== null) { if (!g.submitted.length || !Number.isSafeInteger(g.submittedAt) || Number(g.submittedAt) < base.createdAt || Number(g.submittedAt) > base.updatedAt || Number(g.submittedAt) >= base.deadlineAt || Number(g.submissions) < 1) throw invalid(); }
  else if (g.submittedAt !== null || g.submissions !== 0) throw invalid();
  if ((g.draft.length || g.submitted !== null) && base.revision === 0 || !base.ended && base.updatedAt >= base.deadlineAt) throw invalid();
  const copy = (items: typeof g.draft) => items.map(a => ({ slotId: a.slotId, horseId: a.horseId }));
  return { schemaVersion: 1, pack, game: { ...base, modeId: "pedigree", characterId: family.subject.characterId, slotIds: [...slots], candidateIds: [...candidates], draft: copy(g.draft), submitted: g.submitted === null ? null : copy(g.submitted), submittedAt: g.submittedAt as number | null, submissions: Number(g.submissions) } };
}
export const pedigreeStore = activityStore<PedigreePack, PedigreeGame>("umaguess-pedigree-practice", validatePedigreeArchive, pedigreePackKey);
export const volatilePedigreeGames = new Map<string, PedigreeArchive & { savedRevision?: number | null }>();
