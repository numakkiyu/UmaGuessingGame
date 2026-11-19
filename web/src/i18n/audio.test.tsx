import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { changeLanguage, LANGUAGES } from ".";
import { audioCopy, AudioNotice } from "./audio";
import { errorPlayerText, resolvePlayerText } from "./player-text";
import { characterName } from "./entities";
import { AudioPracticeStartPage, AudioPracticeGamePage } from "@/features/practice/audio-practice-pages";
import { api } from "@/lib/api";
import { audioPracticeResult, changeAudioPairs, reportPracticeAudio, startAudioPlay } from "@/lib/domain/audio-practice";
import { createAudioGame, audioStore, readAudioPack, validateAudioArchive } from "@/lib/local/audio-practice-store";
import { loadPracticeAudio } from "@/lib/local/audio-media";
import { sha256 } from "@/lib/local/checksum";
vi.mock("@/lib/local/audio-media", () => ({ loadPracticeAudio: vi.fn(), ensurePracticeAudioPresent: vi.fn(), preparePracticeAudio: vi.fn() }));
const language = (lang: typeof LANGUAGES[number]) => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); vi.restoreAllMocks(); vi.mocked(loadPracticeAudio).mockReset(); await changeLanguage("zh-Hans"); });
function content() { return { clips: Array.from({ length: 6 }, (_, i) => ({ clipId: `clip${i}`, characterId: `char${i}`, characterName: `合成角色${i}`, localizedNames: { ja: `テスト${i}`, "zh-Hant": `測試${i}` }, transcript: "合成テストの原文です", audioPath: `/assets/audio/tone-${i}.webm`, durationMillis: 1000, byteLength: 99, sha256: "a".repeat(64) })), mediaBytes: 594 }; }
function pack(data: unknown = content()) { const contentJSON = JSON.stringify(data); return { schemaVersion: 1 as const, modeId: "audio" as const, rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", reviewStatus: "source-snapshot" as const, sourceUrls: ["https://example.invalid/"], contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length }; }
function leaves(value: unknown, prefix = ""): Record<string, string> { if (typeof value === "string") return { [prefix]: value }; return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${prefix}.${key}`))); }
describe("audio practice localization with synthetic metadata only", () => {
  it("keeps rule copy keys and placeholders aligned", () => {
    const source = leaves(audioCopy["zh-Hans"]);
    for (const lang of LANGUAGES) { const target = leaves(audioCopy[lang]); expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort()); for (const [key, text] of Object.entries(source)) { expect(target[key]?.trim()).toBeTruthy(); const params = (s: string) => [...s.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort(); expect(params(target[key]!)).toEqual(params(text)); } }
  });
  it("keeps clip count and one pending fetch while rephrasing preparation errors", async () => {
    vi.spyOn(audioStore, "latestPack").mockResolvedValue(null); vi.spyOn(audioStore, "list").mockResolvedValue([]);
    let reject!: (error: unknown) => void, signal!: AbortSignal;
    const fetch = vi.spyOn(api, "getAudioPracticePack").mockImplementation(s => { signal = s!; return new Promise((_resolve, fail) => { reject = fail; }); });
    render(<MemoryRouter><AudioPracticeStartPage /></MemoryRouter>); fireEvent.change(screen.getByLabelText("这局听几段？"), { target: { value: "6" } }); fireEvent.click(await screen.findByRole("button", { name: "查看并准备语音" })); await language("ja");
    expect((screen.getByLabelText("音声は何本にする？") as HTMLSelectElement).value).toBe("6"); expect(screen.getByText(audioCopy.ja.rules[0])).toBeTruthy(); expect(signal.aborted).toBe(false); expect(fetch).toHaveBeenCalledTimes(1);
    await act(async () => { reject(new AudioNotice("cancelled")); }); await screen.findByText(audioCopy.ja.notices.cancelled); await language("zh-Hant"); expect(screen.getByText(audioCopy["zh-Hant"].notices.cancelled)).toBeTruthy();
  });
  it("keeps original media pairing ids play budget excluded clips and saved response through locale changes", async () => {
    const p = pack(), { library } = readAudioPack(p); let g = createAudioGame(p, 4, 1000); g = startAudioPlay(g, g.clipIds[0]!, 1001); g = startAudioPlay(g, g.clipIds[0]!, 1002); g = reportPracticeAudio(g, g.clipIds[1]!, 1003);
    const answer = g.clipIds.filter(id => !g.failedIds.includes(id)).map(id => ({ clipId: id, characterId: library.clips.find(c => c.clipId === id)!.characterId }));
    g = changeAudioPairs(g, answer, true, 1004); g = changeAudioPairs(g, answer.slice(1), false, 1005); const before = JSON.stringify(g);
    for (const lang of LANGUAGES) {
      await language(lang); const clip = library.clips[0]!; expect(characterName({ displayName: clip.characterName, localizedNames: clip.localizedNames })).toBe(lang === "ja" ? "テスト0" : lang === "zh-Hant" ? "測試0" : "合成角色0"); expect(audioPracticeResult(g, library.clips)).toMatchObject({ scoreUnits: 10000, total: 3, correct: 3 }); expect(() => startAudioPlay(g, g.clipIds[0]!, 1006)).toThrow(); expect(validateAudioArchive({ schemaVersion: 1, pack: p, game: g }).game).toEqual(g); expect(JSON.stringify(g)).toBe(before); expect(readAudioPack(p).library.clips[0]?.transcript).toBe("合成テストの原文です");
    }
    const c = content(), { localizedNames: _names, ...old } = c.clips[0]!; expect(readAudioPack(pack({ ...c, clips: [old, ...c.clips.slice(1)] })).library.clips[0]?.localizedNames).toBeUndefined(); expect(() => readAudioPack(pack({ ...c, clips: [{ ...old, localizedNames: { ja: "" } }, ...c.clips.slice(1)] }))).toThrow();
    const error = errorPlayerText(new AudioNotice("exhausted"), "audio", "mediaLoadFailed"); await language("ja"); expect(resolvePlayerText(error)).toBe(audioCopy.ja.notices.exhausted);
  });
  it("rephrases a media loader error without restarting decoding or the saved game", async () => {
    const p = pack(), game = createAudioGame(p, 2), before = JSON.stringify(game); vi.spyOn(audioStore, "load").mockResolvedValue({ schemaVersion: 1, game, pack: p }); vi.mocked(loadPracticeAudio).mockRejectedValue(new AudioNotice("decode"));
    render(<MemoryRouter initialEntries={[`/practice/audio/${game.gameId}`]}><Routes><Route path="/practice/audio/:gameId" element={<AudioPracticeGamePage />} /></Routes></MemoryRouter>);
    await screen.findByText(audioCopy["zh-Hans"].notices.decode); await language("ja"); expect(screen.getByText(audioCopy.ja.notices.decode)).toBeTruthy(); expect(loadPracticeAudio).toHaveBeenCalledTimes(1); expect(audioStore.load).toHaveBeenCalledTimes(1); expect(JSON.stringify(game)).toBe(before);
  });
});
