import type { LocalizedNames } from "@/i18n/entities";

import type { components } from "./types";

type Schemas = components["schemas"];

export type CellStatus = "correct" | "near" | "wrong" | "unknown";

/** 九个字段的稳定顺序（与 的字段顺序一致） */
export const FIELD_ORDER = [
  "star",
  "surface",
  "distance",
  "style",
  "sex",
  "g1",
  "g23",
  "grade",
  "dormitory",
] as const;

export type ClassicField = (typeof FIELD_ORDER)[number];

/** 字段中文名：契约只有机器名，显示名由前端提供 */
export const FIELD_LABELS: Record<ClassicField, string> = {
  star: "星级",
  surface: "场地",
  distance: "距离",
  style: "跑法",
  sex: "性别",
  g1: "G1 适性",
  g23: "G2/G3 适性",
  grade: "学段",
  dormitory: "宿舍",
};

/** 手机默认展示的核心四项 */
export const CORE_FIELDS: readonly ClassicField[] = ["star", "surface", "distance", "style"];
/** 其余五项（展开后完整可达， 要求展开后完整） */
export const SECONDARY_FIELDS: readonly ClassicField[] = ["sex", "g1", "g23", "grade", "dormitory"];

export type NormalizedCell = {
  field: ClassicField;
  value: string;
  status: CellStatus;
};

export type NormalizedGuessRow = {
  characterId: string;
  displayName: string;
  localizedNames?: LocalizedNames;
  avatarPath: string | null;
  /** 外链头像（BiliWiki），无则 null；组件会回退到 avatarPath */
  avatarRemoteUrl: string | null;
  attempt: number;
  correct: boolean;
  cells: NormalizedCell[];
};

export type NormalizedRound = {
  roundId: string;
  modeId: string;
  phase: string;
  phaseVersion: number;
  deadlineAt: string | null;
  paused: boolean;
  /** 剩余错误额度；未知时为 null，界面显示「—」而不是 0 */
  remainingWrong: number | null;
  /** 已提交次数 */
  attempts: number;
  solved: boolean;
  failed: boolean;
  surrendered: boolean;
  /** 按提交先后升序 */
  rows: NormalizedGuessRow[];
  /** 本局错误额度上限 */
  wrongBudget: number | null;
  /**
   * 对手影子：只给「第几次 + 每格状态」，不给角色身份与字段值。
   *
   * 服务端 `projection.Shadows`（`shadowRow`）一直是有的，但前端类型里没有、
   * 归一化也不读——对手的影子进度在界面上完全看不到。这是「服务端发、前端不读」
   * 的第 7 个实例（前六个：clue-puzzle / pedigree / rapport / sort /
   * co-race-path / creative）。
   *
   * 明确要求「对手影子以独立区呈现，明确『对手第几次』，不能混入自己的历史」。
   */
  shadows: { attempt: number; statuses: { field: ClassicField; status: CellStatus }[] }[];
  submissionRevision: number;
  controlGeneration: number;
  canSubmit: boolean;
  viewerRole: string;
  /** 揭晓后的答案（仅 revealed/settled 且服务端下发时存在） */
  revealedAnswerCharacterId: string | null;
  /** 揭晓答案的显示名（服务端 publicState.answer.name）；取不到时为 null */
  revealedAnswerName: string | null;
  /** 归一化过程中发现的契约差异，供诊断（不展示给普通用户） */
  shapeWarnings: string[];
};

const FIELD_SET: ReadonlySet<string> = new Set(FIELD_ORDER);

function isClassicField(value: string): value is ClassicField {
  return FIELD_SET.has(value);
}

function toStatus(value: unknown): CellStatus {
  return value === "correct" || value === "near" || value === "wrong" || value === "unknown"
    ? value
    : "unknown";
}

/**
 * 归一化 cells：同时接受契约的「对象」形态与实测的「数组」形态。
 * 未知字段被丢弃并记录，避免把服务端新增字段误当已知字段渲染。
 */
function normalizeCells(raw: unknown, warnings: string[]): NormalizedCell[] {
  const byField = new Map<ClassicField, NormalizedCell>();

  if (Array.isArray(raw)) {
    for (const entry of raw) {
      if (typeof entry !== "object" || entry === null) continue;
      const record = entry as Record<string, unknown>;
      const field = typeof record["field"] === "string" ? record["field"] : null;
      if (!field) continue;
      if (!isClassicField(field)) {
        warnings.push(`未知字段被忽略：${field}`);
        continue;
      }
      byField.set(field, {
        field,
        value: typeof record["value"] === "string" ? record["value"] : "",
        status: toStatus(record["status"]),
      });
    }
  } else if (typeof raw === "object" && raw !== null) {
    // 契约形态：{ star: {value, status}, ... }
    for (const [field, cellRaw] of Object.entries(raw as Record<string, unknown>)) {
      if (!isClassicField(field)) {
        warnings.push(`未知字段被忽略：${field}`);
        continue;
      }
      const cell = (typeof cellRaw === "object" && cellRaw !== null ? cellRaw : {}) as Record<string, unknown>;
      byField.set(field, {
        field,
        value: typeof cell["value"] === "string" ? cell["value"] : "",
        status: toStatus(cell["status"]),
      });
    }
  }

  // 按稳定顺序输出；缺失字段补 unknown，保证九格永远完整可读
  const cells: NormalizedCell[] = [];
  for (const field of FIELD_ORDER) {
    const cell = byField.get(field);
    if (cell) cells.push(cell);
    else {
      cells.push({ field, value: "—", status: "unknown" });
      warnings.push(`缺少字段：${field}`);
    }
  }
  return cells;
}

/**
 * 从单条提交回执的 feedback 构造一行记录。
 * 与快照路径共用同一套归一化（normalizeRow），避免两条路径的渲染结果不一致。
 */
export function normalizeRowFromFeedback(feedback: unknown, index = 0): NormalizedGuessRow | null {
  const warnings: string[] = [];
  const record = feedback && typeof feedback === "object" ? feedback as Record<string, unknown> : null;
  return normalizeRow(record?.row ?? feedback, index, warnings);
}

function normalizeRow(raw: unknown, index: number, warnings: string[]): NormalizedGuessRow | null {
  if (typeof raw !== "object" || raw === null) return null;
  const record = raw as Record<string, unknown>;
  const characterId = typeof record["characterId"] === "string" ? record["characterId"] : null;
  if (!characterId) return null;

  const attemptRaw = record["attempt"];
  const attempt = typeof attemptRaw === "number" && Number.isFinite(attemptRaw) ? attemptRaw : index + 1;

  return {
    characterId,
    displayName: typeof record["displayName"] === "string" ? record["displayName"] : characterId,
    avatarPath: typeof record["avatarPath"] === "string" ? record["avatarPath"] : null,
    avatarRemoteUrl: typeof record["remoteImageUrl"] === "string" ? record["remoteImageUrl"] : null,
    attempt,
    correct: record["correct"] === true,
    cells: normalizeCells(record["cells"], warnings),
  };
}

/** 从 avatarPath 提取角色 ID，用于头像组件（受控路径校验在组件内做） */
export function characterIdFromAvatarPath(path: string | null): string | null {
  if (!path) return null;
  const match = /\/assets\/avatars\/([A-Za-z0-9_-]+)\.png$/.exec(path);
  return match?.[1] ?? null;
}

type SelfState = {
  remainingWrong?: unknown;
  attempts?: unknown;
  solved?: unknown;
  failed?: unknown;
  rows?: unknown;
};

/** 把 GET /rounds/{id} 的响应归一化成页面模型 */
export function normalizeClassicRound(view: Schemas["RoundView"] | null | undefined): NormalizedRound | null {
  if (!view) return null;
  const warnings: string[] = [];

  const round = view.round;
  const publicState = view.publicState as Record<string, unknown> | undefined;
  const self = (publicState?.["self"] ?? null) as SelfState | null;

  // 优先读取 publicState.self.rows，兼容 privateState.guesses 中的历史记录
  let rawRows: unknown = self?.rows ?? null;
  if (rawRows === null || rawRows === undefined) {
    const privateState = (view as { privateState?: unknown }).privateState;
    if (privateState && typeof privateState === "object") {
      const guesses = (privateState as Record<string, unknown>)["guesses"];
      if (Array.isArray(guesses)) rawRows = guesses;
    }
  }

  const rows: NormalizedGuessRow[] = [];
  if (Array.isArray(rawRows)) {
    rawRows.forEach((entry, index) => {
      const row = normalizeRow(entry, index, warnings);
      if (row) rows.push(row);
    });
  }

  // 按 attempt 升序，保证「历史记录」顺序稳定（不在 render 中改服务器数组）
  rows.sort((a, b) => a.attempt - b.attempt);

  const attemptsRaw = self?.attempts;
  const attempts =
    typeof attemptsRaw === "number" && Number.isFinite(attemptsRaw) ? attemptsRaw : rows.length;

  const config = publicState?.["config"] as Record<string, unknown> | undefined;
  const budgetRaw = config?.["wrongBudget"];
  const wrongBudget = typeof budgetRaw === "number" && Number.isFinite(budgetRaw) ? budgetRaw : null;

  /*
   * 额度归一化。
   *
   * 实测：玩家首次提交前，服务端返回 remainingWrong = 0，但 config.wrongBudget = 8。
   * 0 是「尚未初始化」的占位值，不是「已经用完」——mode 在第一次 Apply 时才写入真实额度
   * （classic/mode.go：if st.Attempts == 0 && st.RemainingWrong == 0 { st.RemainingWrong = pub.Config.WrongBudget }）。
   *
   * 直接显示 0 会让用户在还没猜之前就以为额度耗尽。因此：
   * 还没提交过（attempts === 0）且服务端报 0 时，用 wrongBudget 作为显示值。
   * 一旦有提交记录，就以服务端值为准，不做任何推断。
   */
  const remainingRaw = self?.remainingWrong;
  const serverRemaining =
    typeof remainingRaw === "number" && Number.isFinite(remainingRaw) ? remainingRaw : null;

  let remainingWrong: number | null = serverRemaining;
  if (attempts === 0 && serverRemaining === 0 && wrongBudget !== null && wrongBudget > 0) {
    // 常规兼容，不记入 shapeWarnings（那是给用户看的契约差异提示，不该被这条噪音占满）
    remainingWrong = wrongBudget;
  }

  if (view.viewerRole === "player" && Object.keys((view as { privateState?: object }).privateState ?? {}).length === 0) {
    warnings.push("privateState 为空，已从 publicState.self 读取历史与额度");
  }

  // 对手影子：服务端只给「第几次 + 每格状态」，没有角色身份与字段值。
  // 读不到就空数组——界面按「暂无影子」渲染，不能因为缺数据而整块消失。
  const rawShadows = publicState?.["shadows"];
  const shadows = (Array.isArray(rawShadows) ? rawShadows : [])
    .map((entry) => {
      if (typeof entry !== "object" || entry === null) return null;
      const rec = entry as Record<string, unknown>;
      const attempt = typeof rec["attempt"] === "number" ? rec["attempt"] : null;
      if (attempt === null) return null;
      const rawStatuses = Array.isArray(rec["statuses"]) ? rec["statuses"] : [];
      const statuses = rawStatuses
        .map((c) => {
          if (typeof c !== "object" || c === null) return null;
          const cr = c as Record<string, unknown>;
          const field = typeof cr["field"] === "string" ? (cr["field"] as ClassicField) : null;
          if (!field) return null;
          return { field, status: toStatus(cr["status"]) };
        })
        .filter((c): c is { field: ClassicField; status: CellStatus } => c !== null);
      return { attempt, statuses };
    })
    .filter((x): x is { attempt: number; statuses: { field: ClassicField; status: CellStatus }[] } => x !== null);

  return {
    roundId: round.roundId,
    modeId: round.modeId,
    phase: round.phase,
    phaseVersion: round.phaseVersion,
    deadlineAt: round.deadlineAt,
    paused: round.paused,
    remainingWrong,
    attempts,
    solved: self?.solved === true,
    failed: self?.failed === true,
    surrendered: (view as { privateState?: { surrendered?: unknown } }).privateState?.surrendered === true,
    rows,
    wrongBudget,
    shadows,
    submissionRevision: (view as { submissionRevision?: number }).submissionRevision ?? 0,
    controlGeneration: (view as { controlGeneration?: number }).controlGeneration ?? 0,
    viewerRole: view.viewerRole,
    canSubmit: (view as {canSubmit?: boolean}).canSubmit !== false,
    revealedAnswerCharacterId:
      (view as { revealedAnswerCharacterId?: string }).revealedAnswerCharacterId ?? null,
    revealedAnswerName: answerNameFromPublicState(view.publicState),
    shapeWarnings: warnings,
  };
}

/** 已猜过的角色 ID 集合，用于搜索排除 */
export function guessedCharacterIds(round: NormalizedRound | null): ReadonlySet<string> {
  const set = new Set<string>();
  if (!round) return set;
  for (const row of round.rows) set.add(row.characterId);
  return set;
}

// answerNameFromPublicState 从公开投影里取揭晓答案的名字。
//
// 名字只在揭晓后由 modes.Project 放进 publicState.answer；未揭晓时拿不到，
// 返回 null 而不是空串，调用方据此区分「没有名字」与「名字是空」。
function answerNameFromPublicState(publicState: unknown): string | null {
  if (!publicState || typeof publicState !== "object") return null;
  const answer = (publicState as { answer?: { name?: unknown } }).answer;
  const name = answer?.name;
  return typeof name === "string" && name.length > 0 ? name : null;
}
