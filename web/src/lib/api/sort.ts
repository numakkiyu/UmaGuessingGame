// Sorting uses ascending order; revealed values and feedback are read from the server projection
import type { components } from "./types";
import { validLocalizedNames, type LocalizedNames } from "@/i18n/entities";

type Schemas = components["schemas"];

/** 排序维度 → 中文标题。config.key 的取值，未知值安全回退。 */
export const CRITERION_LABELS: Record<string, string> = {
  height: "身高",
  birthday: "生日",
  "event-date": "赛事日期",
  eventDate: "赛事日期",
};

export function criterionLabel(key: string | null | undefined): string {
  if (!key) return "排序";
  return CRITERION_LABELS[key] ?? "排序";
}

export type SortCard = {
  localizedNames?: LocalizedNames;
  characterId: string;
  name: string;
  /** 本地 WebP 回退路径（始终存在） */
  imagePath: string | null;
  /** 外链优先地址（可能缺失） */
  remoteImageUrl: string | null;
  /**
   * 展示用文本（如 "165cm"）。
   * 闭卷模式下服务端不下发，此处为 null；界面必须能不带它工作。
   */
  display: string | null;
  /**
   * 排序用的真实值。
   * 闭卷模式下服务端不下发，此处为 null。
   * 任何排序逻辑都不得依赖它——那是答案（G06 要求闭卷）。
   */
  raw: number | null;
};

export type SortRound = {
  roundId: string;
  phase: string;
  phaseVersion: number;
  deadlineAt: string | null;
  paused: boolean;
  /** 排序维度键 */
  criterionKey: string | null;
  questionSeconds: number | null;
  cards: SortCard[];
  /** 总关系对数 = n*(n-1)/2，用于解释分数 */
  totalPairs: number;
  /** 本人已提交的排序（null 表示还没提交） */
  myOrder: string[][] | null;
  /** Older flat records cannot establish the original tie choices. */
  myOrderGroupingKnown?: boolean;
  /**
   * 揭晓：真实顺序与每张卡的真实值。只在回合终态（或本人是裁判）时由服务端发布。
   *
   * 服务端 `projection.Answer`（`answerReveal`）一直是有的，但前端类型里没有、
   * 归一化也不读——揭晓后玩家看不到正确排序。这是 clue-puzzle / pedigree /
   * rapport 之外的第四个同类缺陷。
   */
  answer?: {
    order: string[];
    values: { characterId: string; value: number }[];
  };
  submitted: boolean;
  correctPairs: number;
  scoreUnits: number;
  submissionRevision: number;
  controlGeneration: number;
  canSubmit: boolean;
  viewerRole: string;
  shapeWarnings: string[];
};

function toNumberOrNull(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function normalizeCard(raw: unknown): SortCard | null {
  if (typeof raw !== "object" || raw === null) return null;
  const record = raw as Record<string, unknown>;
  const characterId = typeof record["characterId"] === "string" ? record["characterId"] : null;
  if (!characterId) return null;

  /*
   * display / raw 在闭卷模式下不出现，这是正常状态而不是数据缺失：
   * 要求真值只在揭晓后下发。因此这里不产生警告，只如实记为 null。
   */
  const display = typeof record["display"] === "string" && record["display"] !== "" ? record["display"] : null;
  const rawValue = toNumberOrNull(record["raw"]);

  return {
    characterId,
    name: typeof record["name"] === "string" && record["name"] !== characterId ? record["name"] : "",
    ...(validLocalizedNames(record["localizedNames"]) && record["localizedNames"] ? { localizedNames: record["localizedNames"] } : {}),
    imagePath: typeof record["imagePath"] === "string" ? record["imagePath"] : null,
    remoteImageUrl: typeof record["remoteImageUrl"] === "string" ? record["remoteImageUrl"] : null,
    display,
    raw: rawValue,
  };
}

/** Keep the recorded groups. Legacy flat orders have no recoverable tie data;
 * never infer a participant's choice from the answer values. */
function normalizeSubmittedOrder(value: unknown): string[][] | null {
  if (!Array.isArray(value) || value.length === 0) return null;
  if (Array.isArray(value[0])) return normalizeOrder(value);
  const ids = value.filter((id): id is string => typeof id === "string" && id.length > 0);
  return ids.length ? ids.map(id => [id]) : null;
}

/** 把 groups 归一化为 string[][]，丢弃非法项 */
function normalizeOrder(value: unknown): string[][] | null {
  if (!Array.isArray(value)) return null;
  const groups: string[][] = [];
  for (const group of value) {
    if (!Array.isArray(group)) continue;
    const ids = group.filter((id): id is string => typeof id === "string" && id.length > 0);
    if (ids.length > 0) groups.push(ids);
  }
  return groups.length > 0 ? groups : null;
}

export function normalizeSortRound(view: Schemas["RoundView"] | null | undefined): SortRound | null {
  if (!view) return null;
  const warnings: string[] = [];

  const publicState = view.publicState as Record<string, unknown> | undefined;
  const config = (publicState?.["config"] ?? {}) as Record<string, unknown>;
  const self = (publicState?.["self"] ?? {}) as Record<string, unknown>;

  const rawCards = publicState?.["cards"];
  const cards: SortCard[] = [];
  if (Array.isArray(rawCards)) {
    for (const entry of rawCards) {
      const card = normalizeCard(entry);
      if (card) cards.push(card);
    }
  }

  // 契约字段缺失时记录，便于诊断（不阻塞渲染）
  if (publicState && !("criterion" in publicState)) {
    warnings.push("契约的 criterion/direction 未出现在响应中，已改用 config.key");
  }

  const totalPairsRaw = publicState?.["totalPairs"];
  const totalPairs =
    toNumberOrNull(totalPairsRaw) ?? (cards.length > 1 ? (cards.length * (cards.length - 1)) / 2 : 0);

  // 正解只在终态/裁判可见；没这个字段就保持 undefined，界面按「未揭晓」渲染。
  const answerRaw = publicState?.["answer"];
  const rawAnswer =
    typeof answerRaw === "object" && answerRaw !== null
      ? (answerRaw as Record<string, unknown>)
      : null;
  const answer = rawAnswer
    ? {
        order: (Array.isArray(rawAnswer["order"]) ? rawAnswer["order"] : []).filter(
          (x): x is string => typeof x === "string",
        ),
        values: ((Array.isArray(rawAnswer["values"]) ? rawAnswer["values"] : []) as unknown[])
          .map((entry) => {
            const item =
              typeof entry === "object" && entry !== null
                ? (entry as Record<string, unknown>)
                : null;
            const characterId = typeof item?.["characterId"] === "string" ? item["characterId"] : null;
            const value = toNumberOrNull(item?.["value"]);
            if (!characterId || value === null) return null;
            return { characterId, value };
          })
          .filter((x): x is { characterId: string; value: number } => x !== null),
      }
    : undefined;

  return {
    roundId: view.round.roundId,
    phase: view.round.phase,
    phaseVersion: view.round.phaseVersion,
    deadlineAt: view.round.deadlineAt,
    paused: view.round.paused,
    criterionKey: typeof config["key"] === "string" ? config["key"] : null,
    questionSeconds: toNumberOrNull(config["questionSeconds"]),
    cards,
    totalPairs,
    myOrder: normalizeOrder(self["groups"]) ?? normalizeSubmittedOrder(self["order"]),
    myOrderGroupingKnown: normalizeOrder(self["groups"]) !== null || (Array.isArray(self["order"]) && Array.isArray(self["order"][0])),
    answer,
    submitted: self["submitted"] === true,
    correctPairs: toNumberOrNull(self["correctPairs"]) ?? 0,
    /*
     * 实测：提交后 publicState.self 不含 scoreUnits，只有 correctPairs/totalPairs
     * （提交回执 feedback 里有 scoreUnits，但快照没有）。
     * 因此这里按契约公式自行换算，
     * 而不是显示 0——那会让满分玩家看到「得分 0」。
     * 一旦后端在快照里补上 scoreUnits，这里会优先用它的值。
     */
    scoreUnits: (() => {
      const fromSelf = toNumberOrNull(self["scoreUnits"]);
      if (fromSelf !== null) return fromSelf;
      const correct = toNumberOrNull(self["correctPairs"]) ?? 0;
      const total = toNumberOrNull(self["totalPairs"]) ?? 0;
      if (total <= 0) return 0;
      return Math.floor((100 * correct) / total) * 100;
    })(),
    submissionRevision:
      (view as { submissionRevision?: number }).submissionRevision ?? toNumberOrNull(self["revisions"]) ?? 0,
    controlGeneration: (view as { controlGeneration?: number }).controlGeneration ?? 0,
    viewerRole: view.viewerRole,
    canSubmit: (view as {canSubmit?: boolean}).canSubmit !== false,
    shapeWarnings: warnings,
  };
}

/**
 * 本地草稿排序。
 *
 * 用二维数组表达并列：同组内即并列，组间有序。
 */
export type SortDraft = string[][];

/** Stable singleton groups keep rerenders consistent without using answer values or merging ties. */
export function initialDraft(cards: readonly SortCard[]): SortDraft {
  const shuffled = [...cards].sort((a, b) => {
    const ha = stableHash(a.characterId);
    const hb = stableHash(b.characterId);
    if (ha !== hb) return ha - hb;
    return a.characterId.localeCompare(b.characterId);
  });
  return shuffled.map((card) => [card.characterId]);
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

/**
 * 把某组与相邻组合并（表达「这两组同值，应并列」）。
 * 拖拽必须有键盘替代；并列也必须能用按钮完成，不能只能拖。
 */
/** 上移一组（与上一组交换位置）。用按钮而非拖拽，键盘天然可达。 */
export function moveUp(draft: SortDraft, index: number): SortDraft {
  if (index <= 0 || index >= draft.length) return draft;
  const next = draft.map((group) => [...group]);
  const prev = next[index - 1]!;
  next[index - 1] = next[index]!;
  next[index] = prev;
  return next;
}

/** 下移一组 */
export function moveDown(draft: SortDraft, index: number): SortDraft {
  if (index < 0 || index >= draft.length - 1) return draft;
  const next = draft.map((group) => [...group]);
  const following = next[index + 1]!;
  next[index + 1] = next[index]!;
  next[index] = following;
  return next;
}

/**
 * 把某组与相邻组合并（表达「这两组同值，应并列」）。
 * 合并由玩家操作，不由前端按真值自动完成。
 */
export function mergeWithNext(draft: SortDraft, index: number): SortDraft {
  if (index < 0 || index >= draft.length - 1) return draft;
  const next = draft.map((group) => [...group]);
  const merged = [...next[index]!, ...next[index + 1]!];
  next.splice(index, 2, merged);
  return next;
}

/** 拆开一个并列组为单张（撤销错误的并列） */
export function splitGroup(draft: SortDraft, index: number): SortDraft {
  if (index < 0 || index >= draft.length) return draft;
  const group = draft[index]!;
  if (group.length <= 1) return draft;
  const next = draft.map((g) => [...g]);
  next.splice(index, 1, ...group.map((id) => [id]));
  return next;
}

/** Stable issues for player copy; no internal card identifiers leave validation. */
export type SortDraftIssue = "empty" | "duplicate" | "unknown" | "missing";
export function sortDraftIssues(draft: SortDraft, cards: readonly Pick<SortCard,"characterId">[]): SortDraftIssue[] {
  const problems: SortDraftIssue[] = [];
  const expected = new Set(cards.map(card => card.characterId));
  const seen = new Set<string>();
  for (const group of draft) {
    if (!group.length) problems.push("empty");
    for (const id of group) {
      if (seen.has(id)) problems.push("duplicate");
      seen.add(id);
      if (!expected.has(id)) problems.push("unknown");
    }
  }
  for (const id of expected) if (!seen.has(id)) problems.push("missing");
  return problems;
}
/** Compatibility for existing non-UI consumers. */
export function validateDraft(draft: SortDraft, cards: readonly SortCard[]): string[] {
  const labels = { empty: "存在空分组", duplicate: "有卡片重复出现", unknown: "有卡片不在本题中", missing: "缺少卡片" };
  return sortDraftIssues(draft, cards).map(issue => labels[issue]);
}
