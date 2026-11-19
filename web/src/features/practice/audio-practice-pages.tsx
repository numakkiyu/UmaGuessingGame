import { GameStage } from "@/components/uma/game-stage";
import { TrainerAvatar } from "@/components/uma/trainer-avatar";
import { requestConfirmation, useConfirmation } from "@/lib/confirmation";
import { SelectField } from "@/components/ui/select";
import { useTranslation } from "@/i18n";
import { audioCopy, AudioNotice, localizeAudioPresentation } from "@/i18n/audio";
import { usePlayerText } from "@/i18n/player-text";
import { characterName } from "@/i18n/entities";
import { useEffect, useMemo, useRef, useState } from "react";
import { Headphones, Pause, Play, RotateCcw, Volume2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Alert } from "@/components/ui/alert";
import { api } from "@/lib/api";
import { audioPracticeResult, changeAudioPairs, reportPracticeAudio, startAudioPlay, type AudioGame, type PracticeAudioClip } from "@/lib/domain/audio-practice";
import { practiceFinished } from "@/lib/domain/aptitude-practice";
import { audioPlaybackGain } from "@/lib/domain/audio-level";
import { audioStore, createAudioGame, readAudioPack, validateAudioArchive, volatileAudioGames, type AudioArchive, type AudioPack } from "@/lib/local/audio-practice-store";
import { ensurePracticeAudioPresent, loadPracticeAudio, preparePracticeAudio } from "@/lib/local/audio-media";
import { prepareOfflineShell } from "@/lib/local/offline-shell";
import { ActivityPracticeLoader, ActivityPracticeStart, PracticeGameHeading, PracticeInstructions, PracticeNotices, PracticeUtilityActions, PracticeSubmitActions, useActivityPractice, type PracticeAdapter } from "./activity-practice-kit";

const rules = audioCopy["zh-Hans"].rules;
const adapter: PracticeAdapter<AudioPack, AudioGame> = localizeAudioPresentation({
  modeId: "audio", title: "听音辨马", summary: "训练员，光听声音也能认出她吗？戴好耳机，把每段语音和说话的角色配对吧", hint: "音频完整准备好后才开始这局的 90 秒。每段最多听两次，听出是谁就选她吧", prepareLabel: "查看并准备语音", startLabel: "开始听音配对", filePrefix: "umaguess-audio", rules,
  fetch: signal => api.getAudioPracticePack(signal), read: raw => ({ pack: readAudioPack(raw).pack, media: [] }), validate: validateAudioArchive, create: pack => createAudioGame(pack, Math.min(4, readAudioPack(pack).library.clips.length)), store: audioStore, volatile: volatileAudioGames,
  beforeStart: pack => ensurePracticeAudioPresent(readAudioPack(pack).library.clips),
  prepare: async (pack, signal, progress) => {
    const { library } = readAudioPack(pack);
    if (!(await requestConfirmation({ns:"audio",key:"preparationConfirm",values:{count:library.clips.length,size:(library.mediaBytes / 1_000_000).toFixed(2)}},"prepare",signal))) throw new AudioNotice("cancelled");
    const shell = await prepareOfflineShell([], signal, () => undefined);
    const media = await preparePracticeAudio(library.clips, signal, progress); return shell && media;
  },
});
export function AudioPracticeStartPage() {
  const { t } = useTranslation("audio");
  const [count, setCount] = useState(4);
  return <ActivityPracticeStart adapter={adapter} createGame={pack => createAudioGame(pack, count)} options={<div className="space-y-2"><Label htmlFor="audio-count">{t("count")}</Label><SelectField id="audio-count" value={count} onValueChange={value => setCount(Number(value))} options={[...[2, 3, 4, 5, 6].map(n => ({value:n,label:t("countOption", { count: n })}))]} /></div>} />;
}
export function AudioPracticeGamePage() { return <ActivityPracticeLoader adapter={adapter}>{archive => <AudioPracticeGame initial={archive} />}</ActivityPracticeLoader>; }

function AudioPracticeGame({ initial }: { initial: AudioArchive }) {
  const { t } = useTranslation("audio");
  const [mediaMessage, setMediaMessage, setMediaError] = usePlayerText("audio");
  const p = useActivityPractice(adapter, initial, pack => createAudioGame(pack, initial.game.clipIds.length)), { game, now, act, busy, stale } = p;
  const { library } = useMemo(() => readAudioPack(initial.pack), [initial.pack]);
  const clips = useMemo(() => initial.game.clipIds.map(id => library.clips.find(c => c.clipId === id)!), [initial.game.clipIds, library.clips]);
  const [loadedSession, setSession] = useState<{ clips: typeof clips; reload: number; value: Awaited<ReturnType<typeof loadPracticeAudio>> } | null>(null), [playingId, setPlaying] = useState<string | null>(null), [preparing, setPreparing] = useState(false), [reload, setReload] = useState(0);
  const source = useRef<AudioBufferSourceNode | null>(null), abort = useRef<AbortController | null>(null);
  const [troubleshooting, setTroubleshooting] = useState(false);
  const finished = practiceFinished(game, now), locked = finished || busy || stale, result = finished ? audioPracticeResult(game, library.clips) : null;
  const confirmAction = useConfirmation(`${game.gameId}:${game.revision}:${locked}`);
  const session = loadedSession?.clips === clips && loadedSession.reload === reload ? loadedSession.value : null;
  const playing = finished || stale ? null : playingId;
  useEffect(() => {
    const controller = new AbortController(); let active = true, loaded: Awaited<ReturnType<typeof loadPracticeAudio>> | null = null;
    void loadPracticeAudio(clips, controller.signal).then(value => { if (!active) { void value.context.close(); return; } loaded = value; setSession({ clips, reload, value }); }).catch(error => { if (active) setMediaError(error, "mediaLoadFailed"); });
    return () => { active = false; controller.abort(); source.current?.stop(); source.current = null; if (loaded) void loaded.context.close(); };
  }, [clips, reload, setMediaError]);
  useEffect(() => { if (finished || stale) { source.current?.stop(); source.current = null; } }, [finished, stale]);
  useEffect(() => () => abort.current?.abort(), []);
  function stop() { const current = source.current; source.current = null; current?.stop(); setPlaying(null); }
  async function play(clip: PracticeAudioClip) {
    if (playing === clip.clipId) { stop(); return; }
    if (locked || !session || !session.buffers.has(clip.clipId)) return;
    try {
      await session.context.resume();
      if (!await act((g, at) => startAudioPlay(g, clip.clipId, at))) return;
      stop();
      // This runs only from the play button, after async audio preparation, never during render.
      // eslint-disable-next-line react-hooks/purity
      if (Date.now() >= game.deadlineAt) return;
      const node = session.context.createBufferSource(); node.buffer = session.buffers.get(clip.clipId)!;
      const gain=session.context.createGain(); gain.gain.value=audioPlaybackGain(Array.from({length:node.buffer.numberOfChannels},(_,i)=>node.buffer!.getChannelData(i)));
      node.connect(gain); gain.connect(session.context.destination);
      node.onended = () => { if (source.current === node) { source.current = null; setPlaying(null); } node.disconnect(); gain.disconnect(); };
      source.current = node; node.start(); setPlaying(clip.clipId); setMediaMessage("");
    } catch { setMediaMessage("playbackFailed"); }
  }
  async function prepareAgain() {
    if (preparing) return; stop(); setPreparing(true); const controller = new AbortController(); abort.current = controller;
    try { await adapter.prepare!(initial.pack, controller.signal, (done, total) => setMediaMessage("preparing", { done, total })); setMediaMessage("prepared"); setReload(n => n + 1); }
    catch (error) { if (!controller.signal.aborted) setMediaError(error, "preparationFailed"); }
    finally { setPreparing(false); }
  }
  const name = (id: string | null) => { const clip = library.clips.find(c => c.characterId === id); return clip ? characterName({ displayName: clip.characterName, localizedNames: clip.localizedNames }) : t("unanswered"); };
  function controls(clip: PracticeAudioClip, index: number) {
    const failed = game.failedIds.includes(clip.clipId), unavailable = session?.failed.has(clip.clipId), used = game.plays[clip.clipId] ?? 0;
    return <div className="space-y-2"><div className="flex flex-wrap items-center gap-2"><Button variant="secondary" aria-label={t(playing === clip.clipId ? "stopClip" : "playClip", { clip: index + 1 })} disabled={locked || !session || unavailable || failed || used >= 2 && playing !== clip.clipId} onClick={() => void play(clip)}>{playing === clip.clipId ? <Pause className="size-4" aria-hidden="true" /> : <Play className="size-4" aria-hidden="true" />}{t(playing === clip.clipId ? "stop" : "play")}</Button><span className="text-xs tabular-nums text-[var(--color-muted)]">{failed ? t("excluded") : t("playsRemaining", { count: Math.max(0, 2 - used) })}</span>{playing === clip.clipId ? <Volume2 className="size-4 text-[var(--color-blue-deep)] motion-safe:animate-pulse" aria-label={t("playing")} /> : null}</div>{!failed && !finished && (troubleshooting || unavailable) ? <Button variant="ghost" size="sm" disabled={busy || stale} onClick={() => void report(clip.clipId)}>{t("report")}</Button> : null}{unavailable && !failed ? <p className="text-xs leading-5 text-[var(--color-muted)]">{t("needsMedia")}</p> : null}</div>;
  }
  async function report(clipId: string) {
    stop();
    if (!locked && await confirmAction({ns:"audio",key:"failureConfirm"},"report")) await act((g, at) => reportPracticeAudio(g, clipId, at));
  }
  function renderPairSelect(clip: PracticeAudioClip, index: number) {
    const value = game.draft.find(pair => pair.clipId === clip.clipId)?.characterId ?? "";
    return <SelectField aria-label={t("selectClip", { clip: index + 1 })} value={value} disabled={locked || game.failedIds.includes(clip.clipId)} onValueChange={value => void act((g, at) => changeAudioPairs(g, [...g.draft.filter(pair => pair.clipId !== clip.clipId), ...(value ? [{ clipId: clip.clipId, characterId: value }] : [])], false, at))} options={[({value:"",label:t("choose")}),...game.candidateIds.map(id => ({value:id,disabled:game.draft.some(pair => pair.characterId === id && pair.clipId !== clip.clipId),label:<span className="uma-audio-character"><TrainerAvatar id={id} size={32} /><span>{name(id)}</span></span>}))]} />;
  }
  return <GameStage mode="audio" className="uma-stack mx-auto max-w-5xl">
    <PracticeGameHeading title={adapter.title} modeId="audio" deadlineAt={game.deadlineAt} now={now} finished={finished} />
    <PracticeNotices saved={p.saved} stale={stale} message={p.message} />
    {!session && !finished ? <p role="status" className="text-sm">{t("restoring")}</p> : null}
    {mediaMessage ? <p role="status" className="text-sm leading-6">{mediaMessage}</p> : null}
    {session?.failed.size && !finished ? <Alert fault tone="warning" title={t("someFailed")}><p>{t("someFailedHint")}</p><Button variant="secondary" disabled={preparing} onClick={() => void prepareAgain()}>{t("prepareAgain")}</Button></Alert> : null}
    {result ? <section className="uma-panel uma-reveal-row space-y-3 p-4 sm:p-6" aria-labelledby="audio-result"><h2 id="audio-result" className="text-xl font-bold">{t(result.scoreUnits === null ? "noMediaResult" : result.correct === result.total ? "complete" : "result")}</h2><p className="text-xl font-bold text-[var(--color-action-green)]">{result.scoreUnits === null ? t("unscored") : t("score", { score: result.scoreUnits / 100, correct: result.correct, total: result.total })}</p><p className="text-sm leading-6 text-[var(--color-muted)]">{t(game.submitted ? "savedResult" : "unsavedResult")}</p><Button variant="primary" disabled={busy} onClick={() => void p.again()}><RotateCcw className="size-4" aria-hidden="true" />{t("again")}</Button></section> : null}
    {result ? <ol className="space-y-3">{result.rows.map((row, i) => <li key={row.clip.clipId} className="uma-panel uma-reveal-row space-y-2 p-4"><h3 className="font-bold">{t("clipResult", { clip: i + 1, name: name(row.clip.characterId) })}</h3><p className="text-sm">{row.failed ? t("failedResult") : row.correct ? t("correct") : t("selected", { name: name(row.selected) })}</p><p className="text-sm leading-6 text-[var(--color-muted)]">{t("transcript", { text: row.clip.transcript })}</p></li>)}</ol> : <section className="uma-panel min-w-0 p-3 sm:p-5" aria-labelledby="audio-pairs"><h2 id="audio-pairs" className="flex items-center gap-2 text-lg font-bold"><Headphones className="size-5 text-[var(--color-blue-deep)]" aria-hidden="true" />{t("pairTitle")}</h2><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("pairHint")}</p><Button variant="ghost" size="sm" onClick={()=>setTroubleshooting(value=>!value)}>{t(troubleshooting ? "closeHelp" : "help")}</Button>{troubleshooting?<p className="text-sm leading-6 text-[var(--color-muted)]">{t("helpHint")}</p>:null}<div className="mt-4 hidden md:block"><table className="w-full table-fixed text-left text-sm"><caption className="sr-only">{t("caption")}</caption><thead><tr className="border-b border-[var(--color-line)]"><th scope="col" className="w-24 py-3">{t("clipHeading")}</th><th scope="col" className="py-3">{t("playHeading")}</th><th scope="col" className="w-[42%] py-3">{t("chooseHeading")}</th></tr></thead><tbody>{clips.map((clip, i) => <tr key={clip.clipId} data-playing={playing === clip.clipId} className="border-b border-[var(--color-line)] last:border-0"><th scope="row" className="py-4 align-top">{t("clip", { clip: i + 1 })}</th><td className="py-4 pr-3 align-top">{controls(clip, i)}</td><td className="py-4 align-top">{renderPairSelect(clip, i)}</td></tr>)}</tbody></table></div><ol className="mt-4 space-y-3 md:hidden">{clips.map((clip, i) => <li key={clip.clipId} data-playing={playing === clip.clipId} className="uma-audio-clip space-y-3 rounded-xl border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3"><h3 className="font-bold">{t("clip", { clip: i + 1 })}</h3>{controls(clip, i)}{renderPairSelect(clip, i)}</li>)}</ol><div className="uma-audio-submit sticky z-20 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--color-line)] bg-[var(--color-panel)] p-3 shadow-sm"><p role="status" className="text-sm leading-6">{t(game.submitted ? "savedStatus" : "unsavedStatus", { paired: game.draft.length, total: game.clipIds.length - game.failedIds.length })}</p><PracticeSubmitActions saveLabel={t("save")} saveDisabled={locked || game.draft.length + game.failedIds.length !== game.clipIds.length} submitDisabled={locked || game.draft.length + game.failedIds.length !== game.clipIds.length} save={() => p.act((g, at) => changeAudioPairs(g, g.draft, true, at))} submit={() => p.submitAndFinish((g, at) => changeAudioPairs(g, g.draft, true, at))} /></div></section>}
    <PracticeInstructions rules={adapter.rules} /><PracticeUtilityActions finished={finished} busy={busy} stale={stale} exportRecord={p.exportRecord} finish={p.finish} />
  </GameStage>;
}
