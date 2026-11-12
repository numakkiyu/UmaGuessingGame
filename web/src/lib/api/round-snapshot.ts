// Refresh the authoritative snapshot after accepted writes because submission and control revisions may change
import { roundText } from "@/i18n/round-ui";
import { useCallback, useEffect, useRef, useState } from "react";
import { useApi } from "@/lib/api";
import { ApiError } from "@/lib/api/client";

/** 回合摘要（契约 RoundSummary 的子集，只保留玩法页用得到的） */
export type RoundMeta = {
  roundId: string;
  modeId: string;
  phase: string;
  phaseVersion: number;
  deadlineAt: string | null;
  paused: boolean;
};

export type RoundSnapshotState = {
  /** 原始响应体（已做 {round: ...} 包裹兼容），玩法页自己做投影规范化 */
  raw: unknown;
  meta: RoundMeta | null;
  /** 本人提交修订号：每次提交 +1，下次提交必须用它 */
  submissionRevision: number;
  /**
   * 按提交槽位分开的修订号。
   *
   * 修订号是按「轮次 + 主体 + 槽位」各自累计的，而 submissionRevision 只有一个数字，
   * 对有两个槽位的玩法（创意擂台：作品 + 投票；假档案：假答案 + 投票）只能是其中之一的
   * 值。以前两个页面各自写死「投票用 0」来绕开，等于把服务端的缺陷转嫁给客户端。
   * 缺失的槽位表示「还没在那个槽位提交过」，首次提交用 0。
   */
  submissionRevisionsBySlot: Record<string, number>;
  /** 控制权代次：非控制者提交 0 */
  controlGeneration: number;
  canSubmit: boolean;
  viewerRole: string;
  serverTime: string | null;
  isLoading: boolean;
  isError: boolean;
  error: unknown;
};

const EMPTY: RoundSnapshotState = {
  raw: null,
  meta: null,
  submissionRevision: 0,
  submissionRevisionsBySlot: {},
  controlGeneration: 0,
  canSubmit: true,
  viewerRole: "player",
  serverTime: null,
  isLoading: false,
  isError: false,
  error: null,
};

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}

function num(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

/* 接受 RoundView 或 round 包装；版本字段从顶层读取，只向页面输出玩法语义字段 */
export function parseRoundSnapshot(body: unknown): {
  raw: unknown;
  meta: RoundMeta | null;
  submissionRevision: number;
  submissionRevisionsBySlot: Record<string, number>;
  controlGeneration: number;
  canSubmit: boolean;
  viewerRole: string;
  serverTime: string | null;
} {
  const outer = asRecord(body);
  if (!outer) {
    return { raw: null, meta: null, submissionRevision: 0, submissionRevisionsBySlot: {}, controlGeneration: 0, canSubmit: true, viewerRole: "player", serverTime: null };
  }
  // {round: {...}} 包裹兼容
  const view = asRecord(outer["round"]) && !("publicState" in outer) ? (outer["round"] as Record<string, unknown>) : outer;

  const roundRecord = asRecord(view["round"]) ?? {};
  const roundId = typeof roundRecord["roundId"] === "string" ? roundRecord["roundId"] : null;
  const meta: RoundMeta | null = roundId
    ? {
        roundId,
        modeId: typeof roundRecord["modeId"] === "string" ? roundRecord["modeId"] : "",
        phase: typeof roundRecord["phase"] === "string" ? roundRecord["phase"] : "",
        phaseVersion: num(roundRecord["phaseVersion"]) ?? 1,
        deadlineAt: typeof roundRecord["deadlineAt"] === "string" ? roundRecord["deadlineAt"] : null,
        paused: roundRecord["paused"] === true,
      }
    : null;

  return {
    raw: view,
    meta,
    submissionRevision: num(view["submissionRevision"]) ?? 0,
    submissionRevisionsBySlot: parseSlotRevisions(view["submissionRevisionsBySlot"]),
    controlGeneration: num(view["controlGeneration"]) ?? 0,
    canSubmit: view["canSubmit"] !== false,
    viewerRole: typeof view["viewerRole"] === "string" ? view["viewerRole"] : "player",
    serverTime: typeof view["serverTime"] === "string" ? view["serverTime"] : null,
  };
}

/**
 * 解析按槽位的修订号映射。
 *
 * 只保留非负整数；无法识别的值一律丢弃，让调用方退回「首次提交用 0」，
 * 而不是把一个坏值当修订号发出去。
 */
function parseSlotRevisions(value: unknown): Record<string, number> {
  const record = asRecord(value);
  if (!record) return {};
  const out: Record<string, number> = {};
  for (const [slot, raw] of Object.entries(record)) {
    const n = num(raw);
    if (typeof n === "number" && n >= 0) out[slot] = n;
  }
  return out;
}

/**
 * 取某个槽位的修订号；该槽位从未提交过时返回 0。
 *
 * 页面应优先用它而不是 submissionRevision，除非该玩法只有一个槽位。
 */
export function revisionForSlot(state: RoundSnapshotState, slot: string): number {
  return state.submissionRevisionsBySlot[slot] ?? 0;
}

/** 从快照里取 publicState（各玩法的公开投影） */
export function publicStateOf(raw: unknown): Record<string, unknown> | null {
  const view = asRecord(raw);
  if (!view) return null;
  return asRecord(view["publicState"]);
}

/** 从快照里取 results / evidence（揭晓后才出现） */
export function resultsOf(raw: unknown): unknown[] {
  const view = asRecord(raw);
  const results = view?.["results"];
  return Array.isArray(results) ? results : [];
}

export type RoundSnapshot = RoundSnapshotState & {
  refetch: () => Promise<boolean>;
  /** 提交成功后的标准收尾：重读快照（含新的 revision） */
  refreshAfterSubmit: () => Promise<boolean>;
};

/**
 * 读取并跟踪一个回合快照。
 *
 * @param roundId 回合 ID；null 表示尚未开局
 */
export function useRoundSnapshot(roundId: string | null, displayOnly = false): RoundSnapshot {
  const { api } = useApi();
  const [state, setState] = useState<RoundSnapshotState>(() => ({
    ...EMPTY,
    isLoading: Boolean(roundId),
  }));

  // StrictMode 双挂载下，旧请求回来不能覆盖新状态
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // 同一时刻只保留最后一次请求的结果，避免乱序覆盖
  const requestSeq = useRef(0);

  const refetch = useCallback(async () => {
    if (!roundId) {
      setState({ ...EMPTY, isLoading: false });
      return false;
    }
    const seq = ++requestSeq.current;
    setState((prev) => ({ ...prev, isLoading: prev.raw === null, isError: false, error: null }));
    try {
      const response = await api.getRound(roundId, undefined, displayOnly);
      if (!mounted.current || seq !== requestSeq.current) return false;
      const parsed = parseRoundSnapshot(response.data);
      if (displayOnly && (parsed.viewerRole !== "display" || parsed.meta?.roundId !== roundId)) throw new Error("Invalid display projection");
      setState({
        raw: parsed.raw,
        meta: parsed.meta,
        submissionRevision: parsed.submissionRevision,
        submissionRevisionsBySlot: parsed.submissionRevisionsBySlot,
        controlGeneration: parsed.controlGeneration,
        canSubmit: parsed.canSubmit,
        viewerRole: parsed.viewerRole,
        serverTime: parsed.serverTime,
        isLoading: false,
        isError: false,
        error: null,
      });
      return true;
    } catch (error) {
      if (!mounted.current || seq !== requestSeq.current) return false;
      const denied = error instanceof ApiError && (error.isUnauthenticated || error.isForbidden || error.isNotFound);
      setState((prev) => ({ ...(denied ? { ...EMPTY, canSubmit: false } : prev), isLoading: false, isError: true, error }));
    }
    return false;
  }, [api, roundId, displayOnly]);

  useEffect(() => {
    void refetch();
  }, [refetch]);

  return { ...state, refetch, refreshAfterSubmit: refetch };
}

/**
 * 提交动作的公共状态机。
 *
 * 三种状态对应 ：
 * - idle：可再次提交
 * - submitting：等待回执，禁用重复点击（但不全屏阻塞其他区域）
 * - confirming：超时，结果未知；必须用同一个幂等键重查，不能生成新键
 */
export type SubmitPhase =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "confirming"; idempotencyKey: string };

/** 玩法阶段的中文标签（只用于展示，不参与判定） */
export const PHASE_LABELS: Record<string, string> = Object.fromEntries(
  ["prepared", "briefing", "collecting", "locked", "revealed", "settled", "cancelled"].map(phase => [phase, ""]),
);
for (const phase of Object.keys(PHASE_LABELS)) Object.defineProperty(PHASE_LABELS, phase, { enumerable: true, get: () => roundText(`phases.${phase}`) });

export function phaseLabel(phase: string | null | undefined): string {
  if (!phase) return roundText("notStarted");
  return Object.hasOwn(PHASE_LABELS, phase) ? PHASE_LABELS[phase]! : roundText("unknown");
}

/** 终态：不再接受提交（页面必须切到 ended 状态，禁止用旧状态继续提交） */
export function isTerminalPhase(phase: string | null | undefined): boolean {
  return phase === "revealed" || phase === "settled" || phase === "cancelled";
}

/** 可提交窗口 */
export function isSubmittablePhase(phase: string | null | undefined): boolean {
  return phase === "briefing" || phase === "collecting";
}
