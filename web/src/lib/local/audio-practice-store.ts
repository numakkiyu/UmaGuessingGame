import { validLocalizedNames } from "@/i18n/entities";
import type { components } from "@/lib/api/schema";
import { AUDIO_DURATION, audioPairsValid, type AudioGame, type AudioLibrary, type AudioPair, type PracticeAudioClip } from "@/lib/domain/audio-practice";
import { activityStore } from "./activity-store";
import { incompletePractice as invalid, practiceId, practiceObject as object, readPracticeBase, readPracticeEnvelope } from "./practice-envelope";
import { newPracticeId } from "./practice-store";
import { shufflePractice } from "./random-draw";
export type AudioPack = components["schemas"]["AudioPracticePack"];
export type AudioArchive = { schemaVersion: 1; pack: AudioPack; game: AudioGame };
export const audioPackKey = (p: AudioPack) => `${p.modeId}:${p.contentPackVersion}:${p.sha256}`;
export function readAudioPack(raw: unknown): { pack: AudioPack; library: AudioLibrary } {
  const { pack, content } = readPracticeEnvelope(raw, "audio");
  if (!Array.isArray(content.clips) || content.clips.length < 2 || content.clips.length > 24 || !Number.isSafeInteger(content.mediaBytes) || Number(content.mediaBytes) > 12_000_000) throw invalid();
  const clips: PracticeAudioClip[] = content.clips.map(c => {
    if (!object(c) || !practiceId(c.clipId) || !practiceId(c.characterId) || typeof c.characterName !== "string" || !c.characterName.trim() || c.characterName.length > 160 || !validLocalizedNames(c.localizedNames) || typeof c.transcript !== "string" || !c.transcript.trim() || c.transcript.length > 2000 || typeof c.audioPath !== "string" || !/^\/assets\/audio\/[A-Za-z0-9_-]+\.webm$/.test(c.audioPath) || typeof c.sha256 !== "string" || !/^[a-f0-9]{64}$/.test(c.sha256) || !Number.isSafeInteger(c.durationMillis) || Number(c.durationMillis) < 1 || Number(c.durationMillis) > 60000 || !Number.isSafeInteger(c.byteLength) || Number(c.byteLength) < 1 || Number(c.byteLength) > 2_000_000) throw invalid();
    return { clipId: c.clipId, characterId: c.characterId, characterName: c.characterName, audioPath: c.audioPath, durationMillis: Number(c.durationMillis), byteLength: Number(c.byteLength), sha256: c.sha256, transcript: c.transcript, ...(c.localizedNames ? { localizedNames: c.localizedNames } : {}) };
  });
  for (const key of ["clipId", "characterId", "audioPath"] as const) if (new Set(clips.map(c => c[key])).size !== clips.length) throw invalid();
  if (clips.reduce((n, c) => n + c.byteLength, 0) !== content.mediaBytes || clips.reduce((n,c)=>n+c.durationMillis,0)>180000 || clips.some(c=>["constructor","prototype"].includes(c.clipId))) throw invalid();
  return { pack, library: { clips, mediaBytes: Number(content.mediaBytes) } };
}
export function createAudioGame(pack: AudioPack, count = 4, now = Date.now()): AudioGame {
  const { library } = readAudioPack(pack);
  if (!Number.isInteger(count) || count < 2 || count > 6 || count > library.clips.length) throw invalid();
  const clips = shufflePractice(library.clips, now).slice(0, count);
  return { schemaVersion: 1, modeId: "audio", gameId: newPracticeId(), packKey: audioPackKey(pack), rulesVersion: pack.rulesVersion, clipIds: clips.map(c => c.clipId), candidateIds: shufflePractice(clips.map(c => c.characterId), now), plays: {}, failedIds: [], draft: [], submitted: null, submittedAt: null, submissions: 0, createdAt: now, updatedAt: now, deadlineAt: now + AUDIO_DURATION, ended: false, revision: 0 };
}
export function validateAudioArchive(raw: unknown): AudioArchive {
  if (!object(raw) || raw.schemaVersion !== 1 || !object(raw.game)) throw invalid();
  const { pack, library } = readAudioPack(raw.pack), g = raw.game, base = readPracticeBase(g, pack, AUDIO_DURATION);
  if (!Array.isArray(g.clipIds) || g.clipIds.length < 2 || g.clipIds.length > 6 || new Set(g.clipIds).size !== g.clipIds.length || !g.clipIds.every(id => typeof id === "string" && library.clips.some(c => c.clipId === id)) || !Array.isArray(g.candidateIds) || g.candidateIds.length !== g.clipIds.length || new Set(g.candidateIds).size !== g.candidateIds.length || !g.candidateIds.every(id => typeof id === "string" && library.clips.some(c => c.characterId === id && (g.clipIds as unknown[]).includes(c.clipId))) || !Array.isArray(g.failedIds) || new Set(g.failedIds).size !== g.failedIds.length || !g.failedIds.every(id => (g.clipIds as unknown[]).includes(id)) || !object(g.plays)) throw invalid();
  const clipIds = g.clipIds as string[], candidates = g.candidateIds as string[], failedIds = g.failedIds as string[], plays: Record<string, number> = {};
  for (const [id, n] of Object.entries(g.plays)) { if (!clipIds.includes(id) || !Number.isInteger(n) || Number(n) < 1 || Number(n) > 2) throw invalid(); plays[id] = Number(n); }
  if (!audioPairsValid(g.draft, clipIds, candidates) || g.draft.some(p => failedIds.includes(p.clipId)) || g.submitted !== null && !audioPairsValid(g.submitted, clipIds, candidates) || !Number.isSafeInteger(g.submissions) || Number(g.submissions) < 0 || Number(g.submissions) > base.revision) throw invalid();
  if (g.submitted !== null) { if (!Number.isSafeInteger(g.submittedAt) || Number(g.submittedAt) < base.createdAt || Number(g.submittedAt) > base.updatedAt || Number(g.submittedAt) >= base.deadlineAt || Number(g.submissions) < 1 || !clipIds.every(id=>(g.submitted as AudioPair[]).some(p=>p.clipId===id)||failedIds.includes(id))) throw invalid(); }
  else if (g.submittedAt !== null || g.submissions !== 0) throw invalid();
  const actionFloor = Object.values(plays).reduce((n, v) => n + v, 0) + failedIds.length + Number(g.submissions);
  if (actionFloor > base.revision || g.draft.length && !base.revision || !base.ended && base.updatedAt >= base.deadlineAt) throw invalid();
  const copy = (pairs: AudioPair[]) => pairs.map(p => ({ clipId: p.clipId, characterId: p.characterId }));
  return { schemaVersion: 1, pack, game: { ...base, modeId: "audio", clipIds: [...clipIds], candidateIds: [...candidates], failedIds: [...failedIds], plays, draft: copy(g.draft), submitted: g.submitted === null ? null : copy(g.submitted), submittedAt: g.submittedAt as number | null, submissions: Number(g.submissions) } };
}
export const audioStore = activityStore<AudioPack, AudioGame>("umaguess-audio-practice", validateAudioArchive, audioPackKey);
export const volatileAudioGames = new Map<string, AudioArchive & { savedRevision?: number | null }>();
