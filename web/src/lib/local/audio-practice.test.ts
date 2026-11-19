import { describe, expect, it } from "vitest";
import { audioPracticeResult, changeAudioPairs, reportPracticeAudio, startAudioPlay } from "@/lib/domain/audio-practice";
import { createAudioGame, readAudioPack, validateAudioArchive, type AudioPack } from "./audio-practice-store";
import { sha256 } from "./checksum";
import { audioPlaybackGain } from "@/lib/domain/audio-level";
function facts() { const clips = Array.from({ length: 8 }, (_, i) => ({ clipId: `clip-${i}`, characterId: `char-${i}`, characterName: `合成角色${i}`, transcript: "合成信号测试，不是真实角色台词", audioPath: `/assets/audio/tone-${i}.webm`, durationMillis: 1000, byteLength: 99, sha256: "a".repeat(64) })); return { clips, mediaBytes: 792 }; }
function pack(content = facts()): AudioPack { const contentJSON = JSON.stringify(content); return { schemaVersion: 1, modeId: "audio", rulesVersion: "2.0.0-draft.1", datasetVersion: "test-data", contentPackVersion: "test-pack", sourceUrls: ["https://example.invalid/"], reviewStatus: "source-snapshot", contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length }; }
describe("audio practice rules and recovery", () => {
  it("levels quiet clips within a bounded gain and peak ceiling",()=>{
    expect(audioPlaybackGain([new Float32Array([0,0])])).toBe(1);
    expect(audioPlaybackGain([new Float32Array([0.01,-0.01])])).toBe(3);
    expect(audioPlaybackGain([new Float32Array([0.9,-0.9])])*0.9).toBeLessThanOrEqual(0.85);
    expect(audioPlaybackGain([new Float32Array([NaN])])).toBe(0);
  });
  it("draws matching numbers of distinct speakers, preserves two-play budget and original cutoff", () => {
    const p = pack();
    for (const count of [2, 3, 4, 5, 6]) { let g = createAudioGame(p, count, 1000); expect(g.clipIds).toHaveLength(count); expect(new Set(g.candidateIds).size).toBe(count); expect(g.deadlineAt).toBe(91000); g = startAudioPlay(g, g.clipIds[0]!, 1001); g = startAudioPlay(g, g.clipIds[0]!, 1002); expect(() => startAudioPlay(g, g.clipIds[0]!, 1003)).toThrow(); expect(validateAudioArchive({ schemaVersion: 1, pack: p, game: g }).game).toEqual(g); expect(() => startAudioPlay(g, g.clipIds[1]!, g.deadlineAt)).toThrow(); }
  });
  it("does not submit drafts on timeout; revisions replace only explicitly confirmed pairings", () => {
    const p = pack(), { library } = readAudioPack(p); let g = createAudioGame(p, 4, 1000);
    const correct = g.clipIds.map(id => ({ clipId: id, characterId: library.clips.find(c => c.clipId === id)!.characterId }));
    g = changeAudioPairs(g, correct, false, 1001); expect(audioPracticeResult(g, library.clips).scoreUnits).toBe(0);
    g = changeAudioPairs(g, correct, true, 1002); expect(audioPracticeResult(g, library.clips).scoreUnits).toBe(10000);
    const wrong = correct.map((p, i) => ({ ...p, characterId: correct[(i + 1) % correct.length]!.characterId }));
    g = changeAudioPairs(g, wrong, false, 1003); expect(audioPracticeResult(g, library.clips).scoreUnits).toBe(10000);
    g = changeAudioPairs(g, wrong, true, 1004); expect(audioPracticeResult(g, library.clips).scoreUnits).toBe(0); expect(g.submissions).toBe(2); expect(validateAudioArchive({ schemaVersion: 1, pack: p, game: g }).game).toEqual(g);
    expect(() => changeAudioPairs(g, correct.slice(1), true, 1005)).toThrow(); expect(() => changeAudioPairs(g, [correct[0]!, correct[0]!], false, 1005)).toThrow();
  });
  it("never scores failed clips as wrong and distinguishes an entirely unplayable attempt", () => {
    const p = pack(), { library } = readAudioPack(p); let g = createAudioGame(p, 4, 1000);
    g = reportPracticeAudio(g, g.clipIds[0]!, 1001);
    const remaining = g.clipIds.slice(1).map(id => ({ clipId: id, characterId: library.clips.find(c => c.clipId === id)!.characterId }));
    g = changeAudioPairs(g, remaining, true, 1002); expect(audioPracticeResult(g, library.clips)).toMatchObject({ total: 3, correct: 3, scoreUnits: 10000 });
    for (const id of g.clipIds.slice(1)) g = reportPracticeAudio(g, id, 1003);
    expect(audioPracticeResult(g, library.clips).scoreUnits).toBeNull(); expect(validateAudioArchive({ schemaVersion: 1, pack: p, game: g }).game.failedIds).toHaveLength(4);
  });
  it("floors the total ratio rather than rounding each pair", () => {
    const p = pack(), { library } = readAudioPack(p); let g = createAudioGame(p, 3, 1000);
    const rows = g.clipIds.map(id => ({ clipId: id, characterId: library.clips.find(c => c.clipId === id)!.characterId }));
    g = changeAudioPairs(g, [rows[0]!, { ...rows[1]!, characterId: rows[2]!.characterId }, { ...rows[2]!, characterId: rows[1]!.characterId }], true, 1001);
    expect(audioPracticeResult(g, library.clips).scoreUnits).toBe(3300);
  });
  it("rejects unsafe media, duplicate identities, changed clocks and budgets; discards imported score claims", () => {
    const p = pack(), g = createAudioGame(p, 4, 1000);
    for (const patch of [{ clipIds: [g.clipIds[0], g.clipIds[0]] }, { candidateIds: [...g.candidateIds, "foreign"] }, { failedIds: ["unknown"] }, { plays: { [g.clipIds[0]!]: 3 } }, { plays: { [g.clipIds[0]!]: 1 }, revision: 0 }, { deadlineAt: g.deadlineAt + 1 }, { submitted: [], submittedAt: 1001, submissions: 1, revision: 1, updatedAt: 1001 }]) expect(() => validateAudioArchive({ schemaVersion: 1, pack: p, game: { ...g, ...patch } })).toThrow();
    const restored = validateAudioArchive({ schemaVersion: 1, pack: p, game: { ...g, scoreUnits: 10000, rank: 1, audioPath: "https://evil.invalid/x.webm" } }); expect(restored.game).not.toHaveProperty("scoreUnits"); expect(restored.game).not.toHaveProperty("audioPath");
    for (const path of ["https://evil.invalid/x.webm", "//evil.invalid/x.webm", "/assets/audio/%2e%2e/x.webm", "/assets/audio/../x.webm", "/assets/audio/old.mp3", "/assets/audio/other.opus"]) { const f = facts(); f.clips[0]!.audioPath = path; expect(() => readAudioPack(pack(f))).toThrow(); }
    const duplicate = facts(); duplicate.clips[1]!.characterId = duplicate.clips[0]!.characterId; expect(() => readAudioPack(pack(duplicate))).toThrow();
    expect(() => readAudioPack({ ...p, byteLength: p.byteLength + 1 })).toThrow(); expect(() => readAudioPack({ ...p, sha256: "0".repeat(64) })).toThrow();
    expect(sha256(new Uint8Array([0, 255, 128, 1]))).toBe("6509423fd9da5c225d2f8619ffae394b40f9f7686fee55a38c54b1424ac65f46");
  });
});
