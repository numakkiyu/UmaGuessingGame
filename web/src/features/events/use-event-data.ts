// Host, display and results use the same normalized event and standings data
import { useCallback, useEffect, useRef, useState } from "react";
import { eventText } from "@/i18n/event-results";
import { useApi } from "@/lib/api";
import { entertainmentOnlyMode, type EventModeConfig } from "./event-config";

export type EventMeta = {
  eventId: string;
  roomId: string;
  name: string;
  status: string;
  eventVersion: number;
  tiePolicy?: "shared-rank" | "announced-tiebreak" | null;
};

/** Event details supplement segment IDs; missing labels are derived from ordinal and mode. */
export type EventItemResult = {publicCode?:string;subjectId:string;displayName:string;scoreUnits:number;maxUnits:number};

export type SegmentRef = {
  results: EventItemResult[];
  competitive: boolean;
  segmentId: string;
  /** 序号（来自 segmentDetails.ordinal）；无详情时为 null */
  ordinal: number | null;
  /** 玩法标识（来自 segmentDetails.modeConfig.modeId）；无详情时为 null */
  modeId: string | null;
  modeConfig: EventModeConfig | null;
  startedRounds: number;
  completedRounds: number;
  currentRoundPhase: string | null;
  /** 分项状态：planned / running / completed / void */
  status: string | null;
  scoreBudget: number | null;
  roundCount: number | null;
  /**
   * 绑定的回合 ID；空字符串表示这个分项还没有回合。
   *
   * 为什么重要：`POST /rooms/{id}/rounds` 只有在显式传 segmentId 时
   * 才会执行 `UPDATE event_segments SET round_id = ?`（rounds.go:182）。
   * 不传不会报错，只是静默不绑定——分项永远停在 planned、总榜永远为空。
   * 所以主持台必须能看到「这个分项有没有回合」。
   */
  roundId: string | null;
  /** 创建分项时填的名称（后端已补 segmentDTO.name） */
  name: string | null;
  /** 是否拿到了完整详情（false 表示只有 ID） */
  hasDetail: boolean;
};

export type SegmentScore = {
  segmentId: string;
  scoreUnits: number;
  maxUnits: number;
};

export type StandingItem = {
  publicCode?: string;
  subjectId: string;
  displayName: string;
  rank: number;
  scoreUnits: number;
  segments: SegmentScore[];
};

export type Standings = {
  eventId: string;
  eventVersion: number;
  items: StandingItem[];
  correctionIds: string[];
};

export type EventDataState = {
  event: EventMeta | null;
  standings: Standings | null;
  segments: SegmentRef[];
  isLoading: boolean;
  isError: boolean;
  error: unknown;
  /** 最近一次成功读取的时间（用于大屏显示「数据新鲜度」） */
  loadedAt: number | null;
  refetch: () => Promise<boolean>;
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

export function parseEvent(body: unknown): { event: EventMeta | null; segments: SegmentRef[] } {
  const record = asRecord(body);
  if (!record) return { event: null, segments: [] };
  const eventId = str(record["eventId"]);
  if (!eventId) return { event: null, segments: [] };

  /*
   * 先读 segmentDetails（有 ordinal/玩法/状态），再补 segments 里没被覆盖的 ID。
   * 两个字段都读，这样即使后端只给其一也能工作。
   */
  const byId = new Map<string, SegmentRef>();

  const rawDetails = record["segmentDetails"];
  if (Array.isArray(rawDetails)) {
    for (const entry of rawDetails) {
      const segRecord = asRecord(entry);
      if (!segRecord) continue;
      const segmentId = str(segRecord["segmentId"]);
      if (!segmentId) continue;
      const modeConfig = asRecord(segRecord["modeConfig"]);
      byId.set(segmentId, {
        segmentId,
        results: Array.isArray(segRecord["results"]) ? segRecord["results"].flatMap((entry:unknown)=>{
          const row=asRecord(entry);if(!row)return [];
          const subjectId=str(row["subjectId"]),scoreUnits=num(row["scoreUnits"]),maxUnits=num(row["maxUnits"]);
          return subjectId && scoreUnits!==null && maxUnits!==null ? [{publicCode: publicCodeOf(row["publicCode"]),subjectId,displayName:str(row["displayName"]) ?? "",scoreUnits,maxUnits}] : [];
        }) : [],
        competitive: !entertainmentOnlyMode(modeConfig ? str(modeConfig["modeId"]) ?? undefined : undefined) && segRecord["competitive"] !== false,
        ordinal: num(segRecord["ordinal"]),
        modeId: modeConfig ? str(modeConfig["modeId"]) : null,
        modeConfig: modeConfig as EventModeConfig | null,
        startedRounds: num(segRecord["startedRounds"]) ?? (str(segRecord["roundId"]) ? 1 : 0),
        completedRounds: num(segRecord["completedRounds"]) ?? (segRecord["status"] === "completed" ? 1 : 0),
        currentRoundPhase: str(segRecord["currentRoundPhase"]),
        status: str(segRecord["status"]),
        scoreBudget: num(segRecord["scoreBudget"]),
        roundCount: num(segRecord["roundCount"]),
        // 空字符串按「未绑定」处理（后端用 "" 表示没有回合）
        roundId: str(segRecord["roundId"]),
        name: str(segRecord["name"]),
        hasDetail: true,
      });
    }
  }

  const rawSegments = record["segments"];
  if (Array.isArray(rawSegments)) {
    for (const entry of rawSegments) {
      // 契约是字符串 ID 数组；也容忍对象形态
      if (typeof entry === "string" && entry !== "") {
        if (!byId.has(entry)) {
          byId.set(entry, {
            segmentId: entry,
            results: [],
            competitive: true,
            ordinal: null,
            modeId: null,
            modeConfig: null, startedRounds: 0, completedRounds: 0, currentRoundPhase: null,
            status: null,
            scoreBudget: null,
            roundCount: null,
            roundId: null,
            name: null,
            hasDetail: false,
          });
        }
        continue;
      }
      const segRecord = asRecord(entry);
      const segmentId = segRecord ? str(segRecord["segmentId"]) : null;
      if (segmentId && !byId.has(segmentId)) {
        byId.set(segmentId, {
          segmentId,
          results: [],
          competitive: true,
          ordinal: null,
          modeId: null,
          modeConfig: null, startedRounds: 0, completedRounds: 0, currentRoundPhase: null,
          status: null,
          scoreBudget: null,
          roundCount: null,
          roundId: null,
          name: null,
          hasDetail: false,
        });
      }
    }
  }

  const segments = Array.from(byId.values()).sort((a, b) => {
    // 有 ordinal 的按序号排，没有的排后面（保持稳定）
    if (a.ordinal !== null && b.ordinal !== null) return a.ordinal - b.ordinal;
    if (a.ordinal !== null) return -1;
    if (b.ordinal !== null) return 1;
    return a.segmentId.localeCompare(b.segmentId);
  });

  return {
    event: {
      eventId,
      roomId: str(record["roomId"]) ?? "",
      name: str(record["name"]) ?? "",
      status: str(record["status"]) ?? "draft",
      eventVersion: num(record["eventVersion"]) ?? 0,
      tiePolicy: ["shared-rank","announced-tiebreak"].includes(String(record["tiePolicy"])) ? record["tiePolicy"] as "shared-rank" | "announced-tiebreak" : null,
    },
    segments,
  };
}

function parseStandings(body: unknown): Standings | null {
  const record = asRecord(body);
  if (!record) return null;
  const eventId = str(record["eventId"]);
  if (!eventId) return null;

  const items: StandingItem[] = [];
  const rawItems = record["items"];
  if (Array.isArray(rawItems)) {
    for (const entry of rawItems) {
      const item = asRecord(entry);
      if (!item) continue;
      const subjectId = str(item["subjectId"]);
      if (!subjectId) continue;
      const segments: SegmentScore[] = [];
      const rawSegments = item["segments"];
      if (Array.isArray(rawSegments)) {
        for (const segEntry of rawSegments) {
          const seg = asRecord(segEntry);
          if (!seg) continue;
          const segmentId = str(seg["segmentId"]);
          if (!segmentId) continue;
          segments.push({
            segmentId,
            scoreUnits: num(seg["scoreUnits"]) ?? 0,
            maxUnits: num(seg["maxUnits"]) ?? 0,
          });
        }
      }
      items.push({
        publicCode: publicCodeOf(item["publicCode"]),
        subjectId,
        // Leave missing names empty so the UI uses a neutral label instead of an internal ID
        displayName: str(item["displayName"]) ?? "",
        rank: num(item["rank"]) ?? 0,
        scoreUnits: num(item["scoreUnits"]) ?? 0,
        segments,
      });
    }
  }
  // 服务端应已排序，这里再按分数稳定排一次（同分保持原相对顺序，不自行判并列）
  items.sort((a, b) => a.rank - b.rank || b.scoreUnits - a.scoreUnits);

  const correctionIds: string[] = [];
  const rawCorrections = record["correctionIds"];
  if (Array.isArray(rawCorrections)) {
    for (const entry of rawCorrections) {
      if (typeof entry === "string" && entry !== "") correctionIds.push(entry);
    }
  }

  return {
    eventId,
    eventVersion: num(record["eventVersion"]) ?? 0,
    items,
    correctionIds,
  };
}

export type UseEventDataOptions = {
  displayOnly?: boolean;
  /** 轮询间隔（毫秒）。0 表示不轮询（结果页手动刷新）。 */
  pollIntervalMs?: number;
};

/**
 * 读取活动数据。
 *
 * 两个请求并行发出：活动元信息（名称/状态）与总榜（成绩）。
 * 任一失败都算整体失败——因为页面无法在缺一半数据时给出正确画面。
 */
export function useEventData(eventId: string | null, options: UseEventDataOptions = {}): EventDataState {
  const { api } = useApi();
  const { pollIntervalMs = 0, displayOnly = false } = options;

  const [state, setState] = useState<{
    event: EventMeta | null;
    standings: Standings | null;
    segments: SegmentRef[];
    isLoading: boolean;
    isError: boolean;
    error: unknown;
    loadedAt: number | null;
  }>({
    event: null,
    standings: null,
    segments: [],
    isLoading: Boolean(eventId),
    isError: false,
    error: null,
    loadedAt: null,
  });

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const requestSeq = useRef(0);

  const refetch = useCallback(async () => {
    if (!eventId) {
      setState((prev) => ({ ...prev, isLoading: false }));
      return false;
    }
    const seq = ++requestSeq.current;
    setState((prev) => ({ ...prev, isLoading: prev.event === null, isError: false, error: null }));
    try {
      const [eventResponse, standingsResponse] = await Promise.all([
        api.getEvent(eventId, undefined, displayOnly),
        api.getStandings(eventId, undefined, displayOnly),
      ]);
      if (!mounted.current || seq !== requestSeq.current) return false;
      const parsedEvent = parseEvent(eventResponse.data);
      const standings = parseStandings(standingsResponse.data);
      if (!parsedEvent.event || parsedEvent.event.eventId !== eventId || !standings || standings.eventId !== eventId) throw new Error("Invalid event response");
      setState({
        event: parsedEvent.event,
        standings,
        segments: parsedEvent.segments,
        isLoading: false,
        isError: false,
        error: null,
        loadedAt: Date.now(),
      });
      return true;
    } catch (error) {
      if (!mounted.current || seq !== requestSeq.current) return false;
      setState((prev) => ({ ...prev, isLoading: false, isError: true, error }));
      return false;
    }
  }, [api, eventId, displayOnly]);

  useEffect(() => {
    void refetch();
  }, [refetch]);

  // 可选轮询：主持台与大屏需要，结果页不需要
  useEffect(() => {
    if (!eventId || pollIntervalMs <= 0) return;
    const timer = window.setInterval(() => {
      // 后台标签页不轮询
      if (document.visibilityState === "visible") void refetch();
    }, pollIntervalMs);
    const onVisible = () => {
      if (document.visibilityState === "visible") void refetch();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [eventId, pollIntervalMs, refetch]);

  return { ...state, refetch };
}

/* 缺失显示名时使用中性占位，不展示内部 subjectId */
export function displayNameOf(item: { displayName: string; subjectId: string }): string {
  const name = item.displayName.trim();
  if (name) return name;
  return eventText("unnamed");
}

export function publicCodeOf(value: unknown): string | undefined {return typeof value === "string" && /^[0-9]{6}$/.test(value) ? value : undefined;}
