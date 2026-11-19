// Revealed ranks retain finishRank and gaps from the source. Draft positions describe the player's proposed ordering only
import type { components } from "./types";
import {validLocalizedNames,type LocalizedNames} from "@/i18n/entities";

type Schemas = components["schemas"];

export type RaceEntry = {
  characterId: string;
  name: string;
  localizedNames?: LocalizedNames;
};

export type RaceSelf = {
  subjectId: string;
  groupingKnown: boolean;
  groups: string[][];
  order: string[];
  submitted: boolean;
  revisions: number;
  correctPairs: number | null;
  totalPairs: number;
};

export type RankedEntry = {
  characterId: string;
  name: string;
  localizedNames?: LocalizedNames;
  /** 真实完赛名次（可能空缺，例如 1,2,3,5,7） */
  finishRank: number;
  tieGroup: string | null;
};

export type RaceAnswer = {
  ranked: RankedEntry[];
  /** 该场赛事实际存在的名次号，用于显示空缺 */
  rankGaps: number[];
  excluded: string[];
};

export type RaceRound = {
  phase: string;
  questionSeconds: number | null;
  cooperative: boolean;
  openBook: boolean;
  raceId: string;
  raceName: string;
  raceDate: string;
  entries: RaceEntry[];
  totalPairs: number;
  clues: string[];
  clueRelations: {firstId:string;secondId:string;kind:"before"|"tied"}[];
  self: RaceSelf | null;
  answer: RaceAnswer | null;
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

function numArray(value: unknown): number[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is number => typeof item === "number" && Number.isFinite(item));
}

export function normalizeRaceRound(view: Schemas["RoundView"] | null | undefined): RaceRound | null {
  if (!view) return null;
  const publicState = asRecord(view.publicState);
  if (!publicState) return null;

  const config = asRecord(publicState["config"]) ?? {};

  const entries: RaceEntry[] = [];
  const rawEntries = publicState["entries"];
  if (Array.isArray(rawEntries)) {
    for (const entry of rawEntries) {
      const record = asRecord(entry);
      if (!record) continue;
      const characterId = str(record["characterId"]);
      if (!characterId) continue;
      entries.push({
        characterId,
        name: (str(record["characterName"]) ?? str(record["name"])) === characterId ? "" : str(record["characterName"]) ?? str(record["name"]) ?? "",
        ...(validLocalizedNames(record["localizedNames"])&&record["localizedNames"]?{localizedNames:record["localizedNames"]}:{}),
      });
    }
  }

  const selfRecord = asRecord(publicState["self"]);
  const self: RaceSelf | null = selfRecord
    ? {
        subjectId: str(selfRecord["subjectId"]) ?? "",
        groupingKnown: Array.isArray(selfRecord["groups"]) && (selfRecord["groups"] as unknown[]).every(g=>Array.isArray(g)&&g.length>0&&g.every(id=>typeof id==="string"&&id)),
        groups: Array.isArray(selfRecord["groups"]) ? (selfRecord["groups"] as unknown[]).filter(Array.isArray).map(g => strArray(g)) : strArray(selfRecord["order"]).map(id => [id]),
        order: strArray(selfRecord["order"]),
        submitted: selfRecord["submitted"] === true,
        revisions: num(selfRecord["revisions"]) ?? 0,
        correctPairs: num(selfRecord["correctPairs"]) ?? (asRecord(publicState["answer"]) ? 0 : null),
        totalPairs: num(selfRecord["totalPairs"]) ?? 0,
      }
    : null;

  /* Recover missing reveal names from the public roster, preferring server-provided names */
  const nameById = new Map<string, string>();
  for (const entry of entries) nameById.set(entry.characterId, entry.name);

  let answer: RaceAnswer | null = null;
  const answerRecord = asRecord(publicState["answer"]);
  if (answerRecord) {
    const ranked: RankedEntry[] = [];
    const rawRanked = answerRecord["ranked"];
    if (Array.isArray(rawRanked)) {
      for (const entry of rawRanked) {
        const record = asRecord(entry);
        if (!record) continue;
        const characterId = str(record["characterId"]);
        const finishRank = num(record["finishRank"]);
        if (!characterId || finishRank === null || !Number.isInteger(finishRank) || finishRank < 1 || finishRank > 1000) continue;
        ranked.push({
          characterId,
          // 服务端 name 优先；缺失时用公开参赛名单里的名字回填，最后才退回 ID
          name: str(record["name"]) && str(record["name"]) !== characterId ? str(record["name"])! : nameById.get(characterId) ?? "",
          ...(validLocalizedNames(record["localizedNames"])&&record["localizedNames"]?{localizedNames:record["localizedNames"]}:{}),
          finishRank,
          tieGroup: str(record["tieGroup"]),
        });
      }
    }
    // 按真实名次排序（服务端可能按其它顺序给出）
    ranked.sort((a, b) => a.finishRank - b.finishRank || a.name.localeCompare(b.name));
    answer = {
      ranked,
      rankGaps: numArray(answerRecord["rankGaps"]).filter(rank=>Number.isInteger(rank)&&rank>0&&rank<=1000),
      excluded: strArray(answerRecord["excluded"]),
    };
  }

  const clues = strArray(publicState["clues"]);
  const clueRelations: RaceRound["clueRelations"] = Array.isArray(publicState["clueRelations"])?publicState["clueRelations"].flatMap(value=>{
    const item=asRecord(value),firstId=str(item?.["firstId"]),secondId=str(item?.["secondId"]),kind=item?.["kind"];
    return firstId&&secondId&&firstId!==secondId&&(kind==="before"||kind==="tied")&&entries.some(e=>e.characterId===firstId)&&entries.some(e=>e.characterId===secondId)?[{firstId,secondId,kind}]:[];
  }):[];
  // Older stored rounds contain only these two exact public sentence forms.
  // Resolve those statements against the already public list, never the answer.
  if (!Array.isArray(publicState["clueRelations"])) {
    for (const clue of clues) {
      const pair: RaceRound["clueRelations"] = [];
      for (const first of entries) for (const second of entries) {
        if(first.characterId===second.characterId)continue;
        if(clue===`${first.name} 领先 ${second.name}`)pair.push({firstId:first.characterId,secondId:second.characterId,kind:"before"});
        if(clue===`${first.name} 与 ${second.name} 并列`)pair.push({firstId:first.characterId,secondId:second.characterId,kind:"tied"});
      }
      if(pair.length===1)clueRelations.push(pair[0]!);
    }
  }

  return {
    phase: typeof publicState["phase"] === "string" ? publicState["phase"] : "",
    questionSeconds: num(config["questionSeconds"]),
    cooperative: config["cooperative"] === true,
    openBook: config["openBook"] === true,
    raceId: str(publicState["raceId"]) ?? "",
    raceName: str(publicState["raceName"]) ?? "",
    raceDate: str(publicState["raceDate"]) ?? "",
    entries,
    totalPairs: num(publicState["totalPairs"]) ?? 0,
    clues,
    clueRelations,
    self,
    answer,
  };
}

/**
 * 初始草稿：必须打乱。
 *
 * 服务端 entries 已按 characterId 排序（源码注释明确「数组位置不含结果暗示」），
 * 但直接沿用会退化成「按 ID 升序」这种隐含规律，等于给玩家一个假的起点。
 * 这里用 characterId 的稳定散列打乱：
 * - 同一回合多次渲染顺序一致（不会每次重排，用户不会看花眼）；
 * - 不同回合顺序不同。
 */
export function initialRaceDraft(entries: readonly RaceEntry[], roundId = ""): string[] {
  return [...entries]
    .sort((a, b) => {
      const ha = stableHash(`${roundId}:${a.characterId}`);
      const hb = stableHash(`${roundId}:${b.characterId}`);
      if (ha !== hb) return ha - hb;
      return a.characterId.localeCompare(b.characterId);
    })
    .map((entry) => entry.characterId);
}

/** FNV-1a 32 位散列：稳定、无依赖、同一输入永远同值 */
function stableHash(input: string): number {
  let hash = 0x811c9dc5;
  for (let index = 0; index < input.length; index += 1) {
    hash ^= input.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash;
}

/** 上移一位 */
export function moveUp(order: readonly string[], index: number): string[] {
  if (index <= 0 || index >= order.length) return [...order];
  const next = [...order];
  const prev = next[index - 1]!;
  next[index - 1] = next[index]!;
  next[index] = prev;
  return next;
}

/** 下移一位 */
export function moveDown(order: readonly string[], index: number): string[] {
  if (index < 0 || index >= order.length - 1) return [...order];
  const next = [...order];
  const following = next[index + 1]!;
  next[index + 1] = next[index]!;
  next[index] = following;
  return next;
}

/**
 * 提交草稿 → 契约的 groups[][]。
 *
 * RaceOrder.groups 是二维数组，同组即并列（契约 RaceOrder.groups: string[][]）。
 * 本地草稿用「每个名次一个组」的扁平顺序，提交时把并列组合并成同组。
 * 目前 UI 只提供顺序调整（不含并列操作），所以每组一张；
 * 并列由服务端按真实名次判定，前端不预判。
 */
export function toGroups(order: readonly string[]): string[][] {
  return order.map((id) => [id]);
}

/** 校验：必须覆盖全部参赛者、无重复 */
export function validateRaceDraft(order: readonly string[], entries: readonly RaceEntry[]): string[] {
  const problems: string[] = [];
  const expected = new Set(entries.map((entry) => entry.characterId));
  const seen = new Set<string>();
  for (const id of order) {
    if (seen.has(id)) problems.push(`「${id}」重复出现`);
    seen.add(id);
    if (!expected.has(id)) problems.push(`「${id}」不在本场参赛名单里`);
  }
  for (const id of expected) {
    if (!seen.has(id)) problems.push(`缺少参赛者「${id}」`);
  }
  return problems;
}

/**
 * 名次空缺提示。
 *
 * 真实名次可能空缺（例如 1,2,3,5,7，第 4、6 名被取消资格）。
 * 这里算出「实际存在的名次」与「1..最大名次之间缺哪些」，供揭晓表格显示。
 */
export function rankGapsOf(answer: RaceAnswer): { present: number[]; missing: number[] } {
  const present = answer.rankGaps.length > 0 ? [...answer.rankGaps].sort((a, b) => a - b) : [];
  if (present.length === 0) {
    // 服务端没给 rankGaps 时，从 ranked 推导
    const derived = Array.from(new Set(answer.ranked.map((entry) => entry.finishRank))).sort((a, b) => a - b);
    present.push(...derived);
  }
  if (present.length === 0) return { present: [], missing: [] };
  const max = present[present.length - 1]!;
  const set = new Set(present);
  const missing: number[] = [];
  for (let rank = 1; rank <= max; rank += 1) {
    if (!set.has(rank)) missing.push(rank);
  }
  return { present, missing };
}
