// Submission indicators are mode-specific: an accepted attempt is not always a final answer, and draft uses locked
import type { components } from "./types";

type Schemas = components["schemas"];

/** 一个参赛主体的提交状态 */
export type SubjectProgress = {
  subjectId: string;
  /** 显示名（队伍名或玩家名）；缺失时页面用中性占位 */
  displayName: string;
  /** 是否已提交（draft 用 locked） */
  submitted: boolean;
  known?: boolean;
  /** 尝试次数（部分模式没有，为 null） */
  attempts: number | null;
  solved: boolean;
  failed: boolean;
};

export type SubmissionPhase = "not-started" | "collecting" | "closed";

export type SubmissionTally = {
  /** 已交 */
  submitted: SubjectProgress[];
  /** 未交（可催） */
  outstanding: SubjectProgress[];
  /** 总计 */
  total: number;
  /** 已交占比（0–100，整数） */
  percent: number;
};

/**
 * 从回合投影里读每个主体的提交状态。
 *
 * 字段名按模式不同：draft 用 `locked`，其余用 `submitted`。
 * 两者都没有时（老版本/未知模式）按「未知」处理——返回 submitted=false，
 * 但调用方应结合 `known` 标记避免误判成缺交。
 */
export function readSubjectProgress(
  round: unknown,
  /**
   * subjectId → 显示名 的兜底映射。
   *
   * 正常情况下不需要传：服务端 `RoundView.subjectNames` 已经给出
   * 全部 subject 的显示名（房间层注入，队伍 ID 与成员 ID 都映射到同一个标签）。
   *
   * 这个参数只作为兜底：服务端查名字失败时 subjectNames 为空，
   * 调用方可以传本地映射（例如从房间快照建），否则显示占位。
   */
  fallbackNames: ReadonlyMap<string, string> = new Map(),
): {
  subjects: SubjectProgress[];
  /** 是否成功读到了提交状态字段（false 表示该模式不提供，不能判定缺交） */
  known: boolean;
} {
  const view = asRecord(round);
  if (!view) return { subjects: [], known: false };
  const publicState = asRecord(view["publicState"]) ?? view;
  const rawSubjects = publicState["subjects"];
  if (!Array.isArray(rawSubjects)) return { subjects: [], known: false };

  /*
   * 服务端下发的 subjectNames 是首选来源。
   * 它在 RoundView 顶层（不在 publicState 里），所以从 view 读。
   */
  const serverNames = asRecord(view["subjectNames"]);
  const subjects: SubjectProgress[] = [];
  let known = rawSubjects.length===0;

  for (const entry of rawSubjects) {
    const record = asRecord(entry);
    if (!record) continue;
    const subjectId = typeof record["subjectId"] === "string" ? record["subjectId"] : null;
    if (!subjectId) continue;

    // draft 用 locked；其余用 submitted。两者取「有哪个用哪个」。
    let submitted: boolean;
    if (typeof record["locked"] === "boolean") {
      submitted = record["locked"];
      known = true;
    } else if (typeof record["submitted"] === "boolean") {
      submitted = record["submitted"];
      known = true;
    } else {
      // 该模式不提供提交状态字段：不能判定
      submitted = false;
    }

    // 名字优先级：投影内 displayName → 服务端 subjectNames → 本地兜底映射
    const inRecord = typeof record["displayName"] === "string" ? record["displayName"] : "";
    const fromServer = serverNames && typeof serverNames[subjectId] === "string" ? serverNames[subjectId] : "";
    const fromFallback = fallbackNames.get(subjectId) ?? "";

    subjects.push({
      known: typeof record["locked"] === "boolean" || typeof record["submitted"] === "boolean",
      subjectId,
      displayName: inRecord || fromServer || fromFallback || "",
      submitted,
      attempts: Number.isSafeInteger(record["attempts"]) && Number(record["attempts"])>=0 ? Number(record["attempts"]) : null,
      solved: record["solved"] === true,
      failed: record["failed"] === true,
    });
  }

  return { subjects, known };
}

/** 回合阶段归类：决定「未交」是否可以催 */
export function classifyPhase(phase: string | null | undefined): SubmissionPhase {
  if (!phase) return "not-started";
  if (phase === "briefing") return "not-started";
  if (phase === "collecting") return "collecting";
  if (phase === "prepared") return "not-started";
  return "closed";
}

/**
 * 统计已交 / 未交。
 *
 * 只在 `collecting` 阶段把未交算作「可催」；
 * 其他阶段未交不催（还没开始或已经收卷）。
 */
export function tallySubmissions(
  subjects: readonly SubjectProgress[],
  phase: SubmissionPhase,
): SubmissionTally {
  const submitted = subjects.filter((s) => s.submitted);
  const outstanding = phase === "collecting" ? subjects.filter((s) => !s.submitted && !s.solved && !s.failed && s.known !== false) : [];
  const total = subjects.length;
  return {
    submitted,
    outstanding,
    total,
    percent: total > 0 ? Math.round((submitted.length / total) * 100) : 0,
  };
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}

/** 便捷别名 */
export type RoundView = Schemas["RoundView"];
