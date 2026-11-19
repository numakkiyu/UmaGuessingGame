import "@/i18n/aptitude-notices";
import i18n from "@/i18n";
import { cardName, validLocalizedNames, type LocalizedNames } from "@/i18n/entities";

import type { components } from "./types";

type Schemas = components["schemas"];

export type DraftAptitudes = {
  surface: Record<string, string>;
  distance: Record<string, string>;
  style: Record<string, string>;
};

export type DraftCard = {
  id: string;
  name: string;
  localizedNames?: LocalizedNames;
  imagePath: string | null;
  price: number;
  aptitudes: DraftAptitudes;
};

export type DraftTask = {
  taskId: string;
  surface: string;
  distance: string;
  style: string;
};

export type DraftTaskScore = {
  taskId: string;
  cardId: string;
  surfacePoints: number;
  distancePoints: number;
  stylePoints: number;
  total: number;
};

export type DraftSelf = {
  subjectId: string;
  /** taskId → characterId */
  assignments: Record<string, string>;
  revisions: number;
  locked: boolean;
  totalPoints: number;
  spent: number;
  remainingBudget: number;
  taskScores: DraftTaskScore[];
};

export type DraftRound = {
  phase: string;
  variant: string;
  budget: number;
  planningSeconds: number | null;
  cards: DraftCard[];
  tasks: DraftTask[];
  self: DraftSelf | null;
  subjects: { subjectId: string; revisions: number; locked: boolean }[];
};

/** 满分：4 任务 × 3 维 × 3 分 = 36 */
export const DRAFT_MAX_POINTS = 36;

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}

function num(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function str(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}

function gradeTable(value: unknown): Record<string, string> {
  const record = asRecord(value);
  if (!record) return {};
  const out: Record<string, string> = {};
  for (const [key, grade] of Object.entries(record)) {
    if (typeof grade === "string" && grade !== "") out[key] = grade;
  }
  return out;
}

function normalizeCard(entry: unknown): DraftCard | null {
  const record = asRecord(entry);
  if (!record) return null;
  const id = str(record["id"]);
  if (!id) return null;
  const apt = asRecord(record["aptitudes"]) ?? {};
  return {
    id,
    name: str(record["name"]) === id ? "" : str(record["name"]) ?? "",
    ...(validLocalizedNames(record["localizedNames"]) && record["localizedNames"] ? { localizedNames: record["localizedNames"] } : {}),
    imagePath: str(record["imagePath"]),
    price: num(record["price"]) ?? 0,
    aptitudes: {
      surface: gradeTable(apt["surface"]),
      distance: gradeTable(apt["distance"]),
      style: gradeTable(apt["style"]),
    },
  };
}

function normalizeTask(entry: unknown): DraftTask | null {
  const record = asRecord(entry);
  if (!record) return null;
  const taskId = str(record["taskId"]);
  if (!taskId) return null;
  return {
    taskId,
    surface: str(record["surface"]) ?? "",
    distance: str(record["distance"]) ?? "",
    style: str(record["style"]) ?? "",
  };
}

function normalizeTaskScore(entry: unknown): DraftTaskScore | null {
  const record = asRecord(entry);
  if (!record) return null;
  const taskId = str(record["taskId"]);
  const cardId = str(record["cardId"]);
  if (!taskId || !cardId) return null;
  return {
    taskId,
    cardId,
    surfacePoints: num(record["surfacePoints"]) ?? 0,
    distancePoints: num(record["distancePoints"]) ?? 0,
    stylePoints: num(record["stylePoints"]) ?? 0,
    total: num(record["total"]) ?? 0,
  };
}

export function normalizeDraftRound(view: Schemas["RoundView"] | null | undefined): DraftRound | null {
  if (!view) return null;
  const publicState = asRecord(view.publicState);
  if (!publicState) return null;

  const config = asRecord(publicState["config"]) ?? {};

  const cards: DraftCard[] = [];
  const rawCards = publicState["cards"];
  if (Array.isArray(rawCards)) {
    for (const entry of rawCards) {
      const card = normalizeCard(entry);
      if (card) cards.push(card);
    }
  }

  const tasks: DraftTask[] = [];
  const rawTasks = publicState["tasks"];
  if (Array.isArray(rawTasks)) {
    for (const entry of rawTasks) {
      const task = normalizeTask(entry);
      if (task) tasks.push(task);
    }
  }

  const selfRecord = asRecord(publicState["self"]);
  let self: DraftSelf | null = null;
  if (selfRecord) {
    const assignmentsRecord = asRecord(selfRecord["assignments"]) ?? {};
    const assignments: Record<string, string> = {};
    for (const [taskId, characterId] of Object.entries(assignmentsRecord)) {
      if (typeof characterId === "string" && characterId !== "") assignments[taskId] = characterId;
    }
    const taskScores: DraftTaskScore[] = [];
    const rawTaskScores = selfRecord["taskScores"];
    if (Array.isArray(rawTaskScores)) {
      for (const entry of rawTaskScores) {
        const ts = normalizeTaskScore(entry);
        if (ts) taskScores.push(ts);
      }
    }
    self = {
      subjectId: str(selfRecord["subjectId"]) ?? "",
      assignments,
      revisions: num(selfRecord["revisions"]) ?? 0,
      locked: selfRecord["locked"] === true,
      totalPoints: num(selfRecord["totalPoints"]) ?? 0,
      spent: num(selfRecord["spent"]) ?? 0,
      remainingBudget: num(selfRecord["remainingBudget"]) ?? (num(config["budget"]) ?? 0) - (num(selfRecord["spent"]) ?? 0),
      taskScores,
    };
  }

  const subjects: { subjectId: string; revisions: number; locked: boolean }[] = [];
  const rawSubjects = publicState["subjects"];
  if (Array.isArray(rawSubjects)) {
    for (const entry of rawSubjects) {
      const record = asRecord(entry);
      if (!record) continue;
      const subjectId = str(record["subjectId"]);
      if (!subjectId) continue;
      subjects.push({
        subjectId,
        revisions: num(record["revisions"]) ?? 0,
        locked: record["locked"] === true,
      });
    }
  }

  return {
    phase: typeof publicState["phase"] === "string" ? publicState["phase"] : "",
    variant: str(config["variant"]) ?? "solo",
    budget: num(config["budget"]) ?? 24,
    planningSeconds: num(config["planningSeconds"]),
    cards,
    tasks,
    self,
    subjects,
  };
}

/** 等级 → 分数（A=3 / B=2 / C=1 / 其他 0） */
export function gradePoints(grade: string | undefined): number {
  if (grade === "A") return 3;
  if (grade === "B") return 2;
  if (grade === "C") return 1;
  return 0;
}

/** 单个任务在某张卡上的三维得分（纯本地展示，不是成绩预测） */
export function taskPointsFor(card: DraftCard, task: DraftTask): {
  surface: number;
  distance: number;
  style: number;
  total: number;
} {
  const surface = gradePoints(card.aptitudes.surface[task.surface]);
  const distance = gradePoints(card.aptitudes.distance[task.distance]);
  const style = gradePoints(card.aptitudes.style[task.style]);
  return { surface, distance, style, total: surface + distance + style };
}

export type LineupDraft = {
  /** taskId → characterId（本地草稿，未提交） */
  assignments: Record<string, string>;
};

export type LineupProblem = {
  /** 定位到具体槽位，不只 toast */
  taskId: string | null;
  message: string;
  /** Empty slots are pending; invalid selections are errors. */
  severity: "pending" | "error";
};

/**
 * 本地校验：重复角色、超预算、空槽位。
 *
 * 提交前一屏展示尚未满足条件、角色重复、预算超支；
 * 错误定位到槽位。
 */
export function validateLineup(
  draft: LineupDraft,
  tasks: readonly DraftTask[],
  cards: readonly DraftCard[],
  budget: number,
): LineupProblem[] {
  const problems: LineupProblem[] = [];
  const byId = new Map(cards.map((card) => [card.id, card]));
  const usedBy = new Map<string, string[]>();
  let spent = 0;

  for (const task of tasks) {
    const cardId = draft.assignments[task.taskId];
    if (!cardId) {
      problems.push({ taskId: task.taskId, message: i18n.t("pending",{ns:"aptitudeNotices"}), severity: "pending" });
      continue;
    }
    const card = byId.get(cardId);
    if (!card) {
      problems.push({ taskId: task.taskId, message: i18n.t("notInPool",{ns:"aptitudeNotices"}), severity: "error" });
      continue;
    }
    spent += card.price;
    const list = usedBy.get(cardId) ?? [];
    list.push(task.taskId);
    usedBy.set(cardId, list);
  }

  for (const [cardId, taskIds] of usedBy) {
    if (taskIds.length > 1) {
      const card = byId.get(cardId);
      problems.push({
        taskId: taskIds[0] ?? null,
        message: i18n.t("reused",{ns:"aptitudeNotices",name:card ? cardName(card) : cardId,count:taskIds.length}),
        severity: "error",
      });
    }
  }

  if (spent > budget) {
    problems.push({
      taskId: null,
      message: i18n.t("overspent",{ns:"aptitudeNotices",spent,budget}),
      severity: "error",
    });
  }

  return problems;
}

/** 本地已花预算（用于 sticky 预算条） */
export function spentOf(draft: LineupDraft, cards: readonly DraftCard[]): number {
  const byId = new Map(cards.map((card) => [card.id, card]));
  let spent = 0;
  for (const cardId of Object.values(draft.assignments)) {
    spent += byId.get(cardId)?.price ?? 0;
  }
  return spent;
}

/**
 * 草稿持久化。
 * 只存 taskId→characterId，不存任何答案或他人数据。
 */
const DRAFT_PREFIX = "umaguess:draft-lineup:";
const DRAFT_TTL_MS = 24 * 60 * 60 * 1000;

type StoredDraft = { assignments: Record<string, string>; savedAt: number };

export function loadLineupDraft(roundId: string): LineupDraft | null {
  try {
    const raw = localStorage.getItem(`${DRAFT_PREFIX}${roundId}`);
    if (!raw || raw.length > 16384) return null;
    const parsed: unknown = JSON.parse(raw);
    const record = asRecord(parsed);
    if (!record) return null;
    const savedAt = num(record["savedAt"]);
    if (savedAt === null || savedAt > Date.now() + 60000 || Date.now() - savedAt > DRAFT_TTL_MS) {
      localStorage.removeItem(`${DRAFT_PREFIX}${roundId}`);
      return null;
    }
    const assignmentsRecord = asRecord(record["assignments"]);
    if (!assignmentsRecord || Array.isArray(record["assignments"]) || Object.keys(assignmentsRecord).length > 4) return null;
    const assignments: Record<string, string> = {};
    for (const [taskId, characterId] of Object.entries(assignmentsRecord)) {
      if (!taskId || taskId.length > 160 || typeof characterId !== "string" || !characterId || characterId.length > 160) return null;
      assignments[taskId] = characterId;
    }
    return { assignments };
  } catch {
    return null;
  }
}

export function saveLineupDraft(roundId: string, draft: LineupDraft): boolean {
  try {
    const payload: StoredDraft = { assignments: draft.assignments, savedAt: Date.now() };
    localStorage.setItem(`${DRAFT_PREFIX}${roundId}`, JSON.stringify(payload));
    return true;
  } catch {
    return false;
  }
}

export function clearLineupDraft(roundId: string): void {
  try {
    localStorage.removeItem(`${DRAFT_PREFIX}${roundId}`);
  } catch {
    /* ignore */
  }
}
