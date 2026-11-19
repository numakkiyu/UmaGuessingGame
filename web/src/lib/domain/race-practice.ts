import type { SortCard, SortDraft } from "@/lib/api/sort";
import type { PracticeBase } from "./aptitude-practice";
import { practiceFinished } from "./aptitude-practice";
import i18n from "@/i18n";
import { RaceNotice } from "@/i18n/race";
import { cardName, type LocalizedNames } from "@/i18n/entities";
export const RACE_DURATION = 180000;
export type RecordedRunner = { characterId: string; name: string; localizedNames?: LocalizedNames; finishRank: number; imagePath: string };
export type HistoricalRace = { raceId: string; name: string; date: string; course: string; distance: string; surface: string; entries: RecordedRunner[]; excludedCount: number };
export type RaceGame = PracticeBase & { modeId: "race-rebuild"; raceId: string; style: "closed" | "clues"; cardIds: string[]; draft: SortDraft; submitted: SortDraft | null; submittedAt: number | null; submissions: number };
export function validRaceGroups(raw: unknown, ids: readonly string[]): raw is SortDraft {
  if (!Array.isArray(raw) || raw.length < 1 || raw.length > 7 || raw.some(g => !Array.isArray(g) || !g.length || g.length > 7 || g.some(id => typeof id !== "string"))) return false;
  const flat = (raw as SortDraft).flat(); return flat.length === ids.length && new Set(flat).size === ids.length && flat.every(id => ids.includes(id));
}
export function scoreRace(groups: SortDraft, runners: readonly Pick<RecordedRunner, "characterId" | "finishRank">[]) {
  if (!validRaceGroups(groups, runners.map(e => e.characterId))) throw new RaceNotice("groups");
  const positions = new Map(groups.flatMap((g, i) => g.map(id => [id, i] as const))); let correct = 0, total = 0;
  for (let i = 0; i < runners.length; i++) for (let j = i + 1; j < runners.length; j++) {
    const a = runners[i]!, b = runners[j]!; total++;
    if (Math.sign(positions.get(a.characterId)! - positions.get(b.characterId)!) === Math.sign(a.finishRank - b.finishRank)) correct++;
  }
  return { correct, total, scoreUnits: total ? Math.floor(100 * correct / total) * 100 : 0 };
}
export function changeRace(g: RaceGame, groups: SortDraft, submit: boolean, now: number): RaceGame {
  if (practiceFinished(g, now)) throw new RaceNotice("stopped");
  if (!validRaceGroups(groups, g.cardIds)) throw new RaceNotice("groups");
  const copied = groups.map(group => [...group]);
  return { ...g, draft: copied, submitted: submit ? copied.map(group => [...group]) : g.submitted, submittedAt: submit ? now : g.submittedAt, submissions: g.submissions + (submit ? 1 : 0), updatedAt: now, revision: g.revision + 1 };
}
export function raceCards(game: RaceGame, race: HistoricalRace): SortCard[] { return game.cardIds.map(id => { const e = race.entries.find(e => e.characterId === id)!; return { characterId: id, name: e.name, localizedNames:e.localizedNames, imagePath: e.imagePath, remoteImageUrl: null, raw: null, display: null }; }); }
export function raceClues(race: HistoricalRace, language = i18n.resolvedLanguage): string[] {
  const ordered = [...race.entries].sort((a, b) => a.finishRank - b.finishRank || a.characterId.localeCompare(b.characterId));
  return ordered.slice(0, -1).map((a, i) => { const b = ordered[i + 1]!; return i18n.t(a.finishRank === b.finishRank ? "clueTied" : "clueBefore",{ns:"race",lng:language,first:cardName(a,language),second:cardName(b,language)}); });
}
export function raceResult(game: RaceGame, race: HistoricalRace) {
  const score = game.submitted ? scoreRace(game.submitted, race.entries) : { correct: 0, total: race.entries.length * (race.entries.length - 1) / 2, scoreUnits: 0 };
  return { ...score, ranked: [...race.entries].sort((a, b) => a.finishRank - b.finishRank || a.characterId.localeCompare(b.characterId)) };
}
