import { AudioNotice } from "@/i18n/audio";
import type { LocalizedNames } from "@/i18n/entities";
import { practiceFinished, type PracticeBase } from "./aptitude-practice";
export const AUDIO_DURATION = 90000;
export type PracticeAudioClip = { clipId: string; characterId: string; characterName: string; audioPath: string; durationMillis: number; byteLength: number; sha256: string; transcript: string; localizedNames?: LocalizedNames };
export type AudioLibrary = { clips: PracticeAudioClip[]; mediaBytes: number };
export type AudioPair = { clipId: string; characterId: string };
export type AudioGame = PracticeBase & { modeId: "audio"; clipIds: string[]; candidateIds: string[]; plays: Record<string, number>; failedIds: string[]; draft: AudioPair[]; submitted: AudioPair[] | null; submittedAt: number | null; submissions: number };
export function audioPairsValid(raw: unknown, clips: readonly string[], candidates: readonly string[]): raw is AudioPair[] {
  if (!Array.isArray(raw) || raw.length > clips.length) return false;
  const usedClips = new Set(), usedCharacters = new Set();
  for (const p of raw) {
    if (!p || typeof p !== "object" || !clips.includes(p.clipId) || !candidates.includes(p.characterId) || usedClips.has(p.clipId) || usedCharacters.has(p.characterId)) return false;
    usedClips.add(p.clipId); usedCharacters.add(p.characterId);
  }
  return true;
}
function active(g: AudioGame, now: number) { if (practiceFinished(g, now)) throw new AudioNotice("stopped"); }
export function changeAudioPairs(g: AudioGame, pairs: AudioPair[], submit: boolean, now: number): AudioGame {
  active(g, now);
  if (!audioPairsValid(pairs, g.clipIds, g.candidateIds) || pairs.some(p => g.failedIds.includes(p.clipId))) throw new AudioNotice("unique");
  if (submit && pairs.length + g.failedIds.length !== g.clipIds.length) throw new AudioNotice("incomplete");
  const ordered = g.clipIds.flatMap(id => pairs.filter(p => p.clipId === id).map(p => ({ ...p })));
  return { ...g, draft: ordered, submitted: submit ? ordered.map(p => ({ ...p })) : g.submitted, submittedAt: submit ? now : g.submittedAt, submissions: g.submissions + (submit ? 1 : 0), updatedAt: now, revision: g.revision + 1 };
}
export function startAudioPlay(g: AudioGame, clipId: string, now: number): AudioGame {
  active(g, now);
  if (!g.clipIds.includes(clipId) || g.failedIds.includes(clipId)) throw new AudioNotice("unavailable");
  if ((g.plays[clipId] ?? 0) >= 2) throw new AudioNotice("exhausted");
  return { ...g, plays: { ...g.plays, [clipId]: (g.plays[clipId] ?? 0) + 1 }, updatedAt: now, revision: g.revision + 1 };
}
export function reportPracticeAudio(g: AudioGame, clipId: string, now: number): AudioGame {
  active(g, now);
  if (!g.clipIds.includes(clipId) || g.failedIds.includes(clipId)) throw new AudioNotice("reported");
  return { ...g, failedIds: [...g.failedIds, clipId], draft: g.draft.filter(p => p.clipId !== clipId), updatedAt: now, revision: g.revision + 1 };
}
export function audioPracticeResult(g: AudioGame, clips: readonly PracticeAudioClip[]) {
  const rows = g.clipIds.map(id => { const clip = clips.find(c => c.clipId === id)!, selected = g.submitted?.find(p => p.clipId === id)?.characterId ?? null, failed = g.failedIds.includes(id); return { clip, selected, failed, correct: !failed && selected === clip.characterId }; });
  const total = rows.filter(r => !r.failed).length, correct = rows.filter(r => r.correct).length;
  return { rows, total, correct, scoreUnits: total ? Math.floor(100 * correct / total) * 100 : null };
}
