import { validLocalizedNames } from "@/i18n/entities";
import type { components } from "@/lib/api/schema";
import { RACE_DURATION, validRaceGroups, type HistoricalRace, type RaceGame, type RecordedRunner } from "@/lib/domain/race-practice";
import { activityStore } from "./activity-store";
import { incompletePractice as invalid, practiceId, practiceObject as object, readPracticeBase, readPracticeEnvelope } from "./practice-envelope";
import { newPracticeId } from "./practice-store";
import { shufflePractice } from "./random-draw";
export type RacePack = components["schemas"]["RacePracticePack"];
export type RaceArchive = { schemaVersion: 1; pack: RacePack; game: RaceGame };
export const racePackKey = (p: RacePack) => `${p.modeId}:${p.contentPackVersion}:${p.sha256}`;
const text = (v: unknown, max = 160): v is string => typeof v === "string" && v.length <= max;
function date(v: unknown): v is string { if (typeof v !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(v)) return false; const parsed = new Date(`${v}T00:00:00Z`); return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === v; }
export function readRacePack(raw: unknown) {
  const { pack, content } = readPracticeEnvelope(raw, "race-rebuild");
  if (!Array.isArray(content.races) || !content.races.length || content.races.length > 1000) throw invalid();
  const races: HistoricalRace[] = content.races.map(r => {
    if (!object(r) || !practiceId(r.raceId) || !text(r.name) || !r.name || !date(r.date) || !text(r.course) || !text(r.distance) || !text(r.surface) || !Number.isSafeInteger(r.excludedCount) || Number(r.excludedCount) < 0 || Number(r.excludedCount) > 100 || !Array.isArray(r.entries) || r.entries.length < 4 || r.entries.length > 7) throw invalid();
    const entries: RecordedRunner[] = r.entries.map(e => {
      if (!object(e) || !practiceId(e.characterId) || !text(e.name, 100) || !e.name || !Number.isSafeInteger(e.finishRank) || Number(e.finishRank) < 1 || Number(e.finishRank) > 100) throw invalid();
      if(!validLocalizedNames(e.localizedNames))throw invalid();
      // Derived only from a validated identifier; arbitrary imported asset URLs
      // or extra fields do not become rendering instructions.
      return { characterId: e.characterId, name: e.name, ...(e.localizedNames ? {localizedNames:e.localizedNames} : {}), finishRank: Number(e.finishRank), imagePath: `/assets/avatars/${e.characterId}.webp` };
    });
    if (new Set(entries.map(e => e.characterId)).size !== entries.length) throw invalid();
    return { raceId: r.raceId, name: r.name, date: r.date, course: r.course, distance: r.distance, surface: r.surface, entries, excludedCount: Number(r.excludedCount) };
  });
  if (new Set(races.map(r => r.raceId)).size !== races.length) throw invalid();
  return { pack, races };
}
export function createRaceGame(pack: RacePack, style: RaceGame["style"] = "closed", now = Date.now()): RaceGame {
  const { races } = readRacePack(pack), race = shufflePractice(races, now)[0]!, ids = shufflePractice(race.entries.map(e => e.characterId), now);
  return { schemaVersion: 1, modeId: "race-rebuild", gameId: newPracticeId(), packKey: racePackKey(pack), rulesVersion: pack.rulesVersion, raceId: race.raceId, style, cardIds: ids, draft: ids.map(id => [id]), submitted: null, submittedAt: null, submissions: 0, createdAt: now, updatedAt: now, deadlineAt: now + RACE_DURATION, ended: false, revision: 0 };
}
export function validateRaceArchive(raw: unknown): RaceArchive {
  if (!object(raw) || raw.schemaVersion !== 1 || !object(raw.game)) throw invalid();
  const { pack, races } = readRacePack(raw.pack), g = raw.game, base = readPracticeBase(g, pack, RACE_DURATION), race = races.find(r => r.raceId === g.raceId);
  if (!race || g.style !== "closed" && g.style !== "clues" || !Array.isArray(g.cardIds) || g.cardIds.length !== race.entries.length || new Set(g.cardIds).size !== g.cardIds.length || g.cardIds.some(id => typeof id !== "string" || !race.entries.some(e => e.characterId === id))) throw invalid();
  const ids = g.cardIds as string[];
  if (!validRaceGroups(g.draft, ids) || g.submitted !== null && !validRaceGroups(g.submitted, ids) || !Number.isSafeInteger(g.submissions) || Number(g.submissions) < 0 || Number(g.submissions) > base.revision) throw invalid();
  if (g.submitted) { if (!Number.isSafeInteger(g.submittedAt) || Number(g.submittedAt) < base.createdAt || Number(g.submittedAt) > base.updatedAt || Number(g.submittedAt) >= base.deadlineAt || Number(g.submissions) < 1) throw invalid(); }
  else if (g.submittedAt !== null || g.submissions !== 0) throw invalid();
  const game: RaceGame = { ...base, modeId: "race-rebuild", raceId: race.raceId, style: g.style, cardIds: [...ids], draft: g.draft.map(group => [...group]), submitted: g.submitted ? g.submitted.map(group => [...group]) : null, submittedAt: g.submittedAt as number | null, submissions: Number(g.submissions) };
  return { schemaVersion: 1, game, pack };
}
export const raceStore = activityStore<RacePack, RaceGame>("umaguess-race-practice", validateRaceArchive, racePackKey);
export const volatileRaceGames = new Map<string, RaceArchive & { savedRevision?: number | null }>();
