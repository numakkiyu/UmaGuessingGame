// Face references are opaque; hidden cards must not gain readable names or pair hints through normalization
import type { components } from "./types";
import { settledScore, settledSurrendered } from "./result-score";

type Schemas = components["schemas"];

export type MemoryCardState = "hidden" | "flipped" | "matched";

export type MemoryCard = {
  cardId: string;
  position: number;
  state: MemoryCardState;
  /** 服务端下发的牌面图；hidden 时为 null。前端不得自行拼接 */
  imagePath: string | null;
  /** 揭晓后的可读名（可空，内容包当前是 "???" 占位） */
  label: string | null;
};

export type MemorySelf = {
	/** Confirmed mismatch remains visible briefly to the whole team. */
	revealUntil?: number;
  flipped: string[];
  matched: string[];
  matchedPairs: number;
  wrongAttempts: number;
  attempts: number;
  maxAttempts: number;
  finished: boolean;
  scoreUnits: number;
  surrendered?: boolean;
};

export type MemoryProgress = {
  subjectId: string;
  matchedPairs: number;
  wrongAttempts: number;
  attempts: number;
  finished: boolean;
};

export type MemoryRevealedPair = {
  pairId: string;
  label: string;
  cards: { cardId: string; imagePath: string }[];
};

export type MemoryRound = {
  phase: string;
  viewSeconds: number | null;
  answerSeconds: number | null;
  maxAttempts: number;
  rows: number;
  cols: number;
  cards: MemoryCard[];
  self: MemorySelf | null;
  subjects: MemoryProgress[];
  /** 仅揭晓后存在 */
  revealedPairs: MemoryRevealedPair[];
  /** 本人已完成（配对全中或尝试用尽） */
  finished: boolean;
};

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}

function num(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function str(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}

function strArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string");
}

function normalizeCardState(value: unknown): MemoryCardState {
  return value === "flipped" || value === "matched" ? value : "hidden";
}

function normalizeCard(entry: unknown): MemoryCard | null {
  const record = asRecord(entry);
  if (!record) return null;
  const cardId = str(record["cardId"]);
  if (!cardId) return null;
  const position = num(record["position"]) ?? 0;
  const state = normalizeCardState(record["state"]);
  return {
    cardId,
    position,
    state,
    // 服务端不下发 imagePath 时就是 null（hidden 状态），不是数据缺失
    imagePath: str(record["imagePath"]),
    label: str(record["label"]),
  };
}

export function normalizeMemoryRound(view: Schemas["RoundView"] | null | undefined): MemoryRound | null {
  if (!view) return null;
  const publicState = asRecord(view.publicState);
  if (!publicState) return null;

  const config = asRecord(publicState["config"]) ?? {};
  const cards: MemoryCard[] = [];
  const rawCards = publicState["cards"];
  if (Array.isArray(rawCards)) {
    for (const entry of rawCards) {
      const card = normalizeCard(entry);
      if (card) cards.push(card);
    }
  }
  // 按 position 排序：DOM 顺序必须与棋盘位置一致（不依赖服务端数组顺序）
  cards.sort((a, b) => a.position - b.position);

  const selfRecord = asRecord(publicState["self"]);
  const self: MemorySelf | null = selfRecord
    ? {
		revealUntil: num(selfRecord["revealUntil"]) ?? undefined,
        flipped: strArray(selfRecord["flipped"]),
        matched: strArray(selfRecord["matched"]),
        matchedPairs: num(selfRecord["matchedPairs"]) ?? 0,
        wrongAttempts: num(selfRecord["wrongAttempts"]) ?? 0,
        attempts: num(selfRecord["attempts"]) ?? 0,
        maxAttempts: num(selfRecord["maxAttempts"]) ?? num(config["maxAttempts"]) ?? 20,
        finished: selfRecord["finished"] === true,
        scoreUnits: settledScore(view, selfRecord["subjectId"]) ?? num(selfRecord["scoreUnits"]) ?? 0,
        surrendered: settledSurrendered(view, selfRecord["subjectId"]),
      }
    : null;

  const subjects: MemoryProgress[] = [];
  const rawSubjects = publicState["subjects"];
  if (Array.isArray(rawSubjects)) {
    for (const entry of rawSubjects) {
      const record = asRecord(entry);
      if (!record) continue;
      const subjectId = str(record["subjectId"]);
      if (!subjectId) continue;
      subjects.push({
        subjectId,
        matchedPairs: num(record["matchedPairs"]) ?? 0,
        wrongAttempts: num(record["wrongAttempts"]) ?? 0,
        attempts: num(record["attempts"]) ?? 0,
        finished: record["finished"] === true,
      });
    }
  }

  const revealedPairs: MemoryRevealedPair[] = [];
  const answer = asRecord(publicState["answer"]);
  const rawPairs = answer?.["pairs"];
  if (Array.isArray(rawPairs)) {
    for (const entry of rawPairs) {
      const record = asRecord(entry);
      if (!record) continue;
      const pairCards: { cardId: string; imagePath: string }[] = [];
      const rawPairCards = record["cards"];
      if (Array.isArray(rawPairCards)) {
        for (const cardEntry of rawPairCards) {
          const cardRecord = asRecord(cardEntry);
          if (!cardRecord) continue;
          const cardId = str(cardRecord["cardId"]);
          const imagePath = str(cardRecord["imagePath"]);
          if (cardId && imagePath) pairCards.push({ cardId, imagePath });
        }
      }
      if (pairCards.length === 0) continue;
      revealedPairs.push({
        pairId: str(record["pairId"]) ?? "",
        label: str(record["label"]) ?? "",
        cards: pairCards,
      });
    }
  }

  const finished = self?.finished ?? false;

  return {
    phase: view.round?.phase ?? (typeof publicState["phase"] === "string" ? publicState["phase"] : ""),
    viewSeconds: num(config["viewSeconds"]),
    answerSeconds: num(config["answerSeconds"]),
    maxAttempts: num(config["maxAttempts"]) ?? 20,
    rows: num(publicState["rows"]) ?? 4,
    cols: num(publicState["cols"]) ?? 4,
    cards,
    self,
    subjects,
    revealedPairs,
    finished,
  };
}

/** 观看阶段：所有牌面朝上（服务端把 state 置为 flipped 并下发 imagePath） */
export function isViewingPhase(round: MemoryRound, phase: string | null | undefined): boolean {
  if (phase === "briefing") return true;
  // 回退：服务端在 briefing 把全部牌置为 flipped 且全部有图
  if (round.cards.length === 0) return false;
  return round.cards.every((card) => card.state === "flipped" && card.imagePath !== null) && round.self?.attempts === 0;
}

/** 得分显示：scoreUnits 是 100 units = 1 分 */
export function displayScore(scoreUnits: number): number {
  return Math.round(scoreUnits / 100);
}
