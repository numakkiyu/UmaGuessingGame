/* 归一化音频候选与片段，兼容 clips[] 和内容包字段。播放次数是休闲规则，不能阻止录音或外部播放 */
import type { components } from "./types";

type Schemas = components["schemas"];

/** 一段可播放的音频 */
export type AudioClip = {
  clipId: string;
  /** 本地路由（公开的，不是安全措施） */
  audioPath: string;
  /** 时长（毫秒）；0 表示未知 */
  durationMillis: number;
  /** 每段可播放次数（客户端休闲规则） */
  maxPlays: number;
  /** 这条是否被标记为「听不了」（本地状态，不来自服务端） */
  reported?: boolean;
};

/** 一个候选角色 */
export type AudioCandidate = {
  candidateId: string;
  name: string;
  /** 头像路径（可能为空） */
  imagePath: string | null;
};

export type AudioRound = {
  subjectId:string|null; hasSelf:boolean; ready:boolean; answerSeconds:number;
  progress:{subjectId:string;ready:boolean;pairedClips:number;submissions:number;playbackFailures:number}[];
  clips: AudioClip[];
  candidates: AudioCandidate[];
  /** 是否要求全部预载成功才能开始 */
  preloadRequired: boolean;
  /** 计分说明（服务端原文） */
  scoring: string;
  /** 已配对的 clipId → candidateId */
  matches: Map<string, string>;
  matchedCount: number;
  totalClips: number;
  submissions: number;
  phase: string | null;
  playbackFailures: string[];
  reportedFailureCount: number;
  answer: { clipId: string; characterId: string; transcript: string }[] | null;
};

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}

function str(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}

function num(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

/** 默认每段播放次数 */
export const DEFAULT_MAX_PLAYS = 2;

export function normalizeAudioRound(raw: unknown): AudioRound {
  const view = asRecord(raw) ?? {};
  const publicState = asRecord(view["publicState"]) ?? view;

  const clips: AudioClip[] = [];
  const rawClips = Array.isArray(publicState["clips"]) ? publicState["clips"] : [];
  for (const entry of rawClips) {
    const item = asRecord(entry);
    if (!item) continue;
    // 缺少 clipId 时使用音频文件名；兼容 audioPath 与 assetUrl。
    const audioPath = str(item["audioPath"]) ?? str(item["assetUrl"]) ?? "";
    const clipId =
      str(item["clipId"]) ??
      (audioPath ? audioPath.slice(audioPath.lastIndexOf("/") + 1) : null);
    if (!clipId || !audioPath) continue;
    clips.push({
      clipId,
      audioPath,
      // 实测 durationMillis；契约写的是 durationMs
      durationMillis: num(item["durationMillis"]) ?? num(item["durationMs"]) ?? 0,
      maxPlays: Math.min(2,Math.max(1,Math.floor(num(item["maxPlays"]) ?? DEFAULT_MAX_PLAYS))),
    });
  }

  const candidates: AudioCandidate[] = [];
  const rawCandidates = Array.isArray(publicState["candidates"]) ? publicState["candidates"] : [];
  for (const entry of rawCandidates) {
    const item = asRecord(entry);
    if (!item) continue;
    const candidateId =
      str(item["candidateId"]) ?? str(item["characterId"]) ?? str(item["id"]);
    if (!candidateId) continue;
    candidates.push({
      candidateId,
      name: [str(item["name"]),str(item["displayName"])].find(v=>v&&v!==candidateId) ?? "",
      imagePath: str(item["imagePath"]),
    });
  }

  const matches = new Map<string, string>();
  const self = asRecord(publicState["self"]);
  const reveal = asRecord(publicState["answer"]);
  const rawMatches = Array.isArray(self?.["matches"]) ? self?.["matches"] : [];
  for (const entry of rawMatches) {
    const item = asRecord(entry);
    if (!item) continue;
    const clipId = str(item["clipId"]);
    const candidateId = str(item["candidateId"]) ?? str(item["characterId"]);
    if (clipId && candidateId) matches.set(clipId, candidateId);
  }

  return {
    subjectId:str(self?.["subjectId"]),hasSelf:!!self,ready:self?.["ready"]===true,answerSeconds:num(asRecord(publicState["config"])?.["answerSeconds"])??90,
    progress:(Array.isArray(publicState["progress"])?publicState["progress"]:[]).flatMap((v:unknown)=>{const r=asRecord(v);return r&&str(r.subjectId)?[{subjectId:String(r.subjectId),ready:r.ready===true,pairedClips:num(r.pairedClips)??0,submissions:num(r.submissions)??0,playbackFailures:num(r.playbackFailures)??0}]:[];}),
    clips,
    candidates,
    preloadRequired: publicState["preloadRequired"] !== false,
    scoring: str(publicState["scoring"]) ?? "每对正确得 25 分（4 段基线）",
    matches,
    matchedCount: matches.size,
    totalClips: clips.length,
    submissions: num(self?.["submissions"]) ?? 0,
    phase: str(view["round"] ? asRecord(view["round"])?.["phase"] : publicState["phase"]),
    playbackFailures: Array.isArray(self?.["playbackFailures"]) ? self["playbackFailures"].filter((v): v is string=>typeof v==="string") : [],
    reportedFailureCount: (Array.isArray(publicState["progress"])?publicState["progress"]:[]).reduce((n:number,p:unknown)=>n+(num(asRecord(p)?.["playbackFailures"])??0),0),
    answer: reveal && Array.isArray(reveal["clips"]) ? reveal["clips"].flatMap((r:unknown)=>{const c=asRecord(r);return c&&str(c["clipId"])&&str(c["characterId"])?[{clipId:String(c["clipId"]),characterId:String(c["characterId"]),transcript:str(c["transcript"])??""}]:[];}) : null,
  };
}

/**
 * 预载一段音频。
 *
 * 返回 true 表示可以播放（解码成功），false 表示这段听不了。
 *
 * 播放失败不等于答错：调用方拿到 false 时应显示
 * 「这段听不了」入口，而不是把玩家标记为答错。
 */
export async function preloadClip(audioPath: string, signal?: AbortSignal): Promise<boolean> {
  if (!audioPath || signal?.aborted) return false;
  return new Promise((resolve) => {
    const audio = new Audio();
    let settled = false;
    const done = (ok: boolean) => {
      if (settled) return; settled = true;
      clearTimeout(timer); signal?.removeEventListener("abort",abort);
      audio.oncanplaythrough = null;
      audio.onerror = null;
      audio.pause(); audio.removeAttribute("src"); audio.load();
      resolve(ok);
    };
    const abort = () => done(false);
    audio.oncanplaythrough = () => done(true);
    audio.onerror = () => done(false);
    // 超时也视为不可用（网络慢不该让整页卡住）
    const timer = window.setTimeout(() => done(false), 15000);
    signal?.addEventListener("abort",abort,{once:true});
    audio.preload = "auto";
    audio.src = audioPath;
  });
}

/**
 * 本地预检。
 *
 * 必须配对所有能听的段——服务端对不完整提交返回 422 `incomplete_matches`。
 * 听不了的段走 `playbackFailures` 报告，主持作废重开；不能移除分母提高成绩。
 */
export function validateAudioDraft(
  matches: ReadonlyMap<string, string>,
  playableClipIds: readonly string[],
): string | null {
  if (matches.size === 0) return "至少先给一段音频选一个角色。";
  const missing = playableClipIds.filter((clipId) => !matches.has(clipId));
  if (missing.length > 0) return `还有 ${missing.length} 段没配角色。`;
  return null;
}

/** 便捷别名 */
export type AudioState = Schemas["StateAudio"];
