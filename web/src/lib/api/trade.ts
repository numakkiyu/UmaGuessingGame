// Hands are public while assigned objectives are private. Proposals and responses use the dedicated trade endpoints
import type { components } from "./types";

type Schemas = components["schemas"];

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}
function str(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}
function num(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}
function arr(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

/** 一个卡实例（具体到某张牌） */
export type TradeInstance = {
  instanceId: string;
  /** 卡种 id（抽象，不绑定角色） */
  cardId: string;
  label: string;
};

/** 一个卡种 */
export type TradeCardKind = {
  cardId: string;
  label: string;
};

/** 一个目标（计分条件） */
export type TradeObjective = {
  objectiveId: string;
  text: string;
  kinds: string[];
  points: number;
  /** 只在 selfObjectives 里有 */
  satisfied: boolean | null;
};

/** 一条提案 */
export type TradeRecord = {
  tradeId: string;
  /** 这条提案属于第几轮 */
  round: number | null;
  senderSubjectId: string;
  recipientSubjectId: string;
  offeredInstanceId: string;
  requestedInstanceId: string;
  /** pending / accepted / declined / expired / cancelled */
  status: string;
  /** 是否是发给我的 */
  isIncoming: boolean;
  /** 是否是我发的 */
  isOutgoing: boolean;
};

export type TradeRound = {
  rounds: number;
  roundSeconds: number;
  competitive: boolean;
  instances: TradeInstance[];
  cardKinds: TradeCardKind[];
  objectives: TradeObjective[];
  /** 自己的两个目标（含是否满足） */
  selfObjectives: TradeObjective[];
  /** 自己的手牌（实例 id） */
  selfHand: string[];
  /** 全部人的手牌：subjectId → 实例 id 列表（公开） */
  hands: Map<string, string[]>;
  trades: TradeRecord[];
  tradesEmptyReason: string | null;
  currentRound: number;
  selfSubjectId: string | null;
  phase: string | null;
  result: { subjectId: string; completed: number; total: number; points: number; satisfiedIds: string[]; unsatisfiedIds: string[] }[];
  scores: { subjectId: string; points: number; rank: number | null }[];
};

export function normalizeTradeRound(raw: unknown): TradeRound {
  const view = asRecord(raw) ?? {};
  const ps = asRecord(view["publicState"]) ?? view;
  const phase = str(asRecord(view["round"])?.["phase"]) ?? str(ps["phase"]);
  const config = asRecord(ps["config"]);
  const rounds = num(config?.["rounds"]) ?? 3;
  const published = (phase === "revealed" || phase === "settled");
  const result = published ? asRecord(ps["result"]) : null;
  const int = (value: unknown, max: number) => typeof value === "number" && Number.isSafeInteger(value) && value >= 0 && value <= max;
  const strings = (value: unknown) => arr(value).filter((v): v is string => typeof v === "string");

  const instances: TradeInstance[] = arr(ps["instances"])
    .map((entry) => {
      const item = asRecord(entry);
      const instanceId = str(item?.["instanceId"]);
      if (!instanceId) return null;
      return {
        instanceId,
        cardId: str(item?.["cardId"]) ?? "",
        label: str(item?.["label"]) ?? "",
      };
    })
    .filter((i): i is TradeInstance => i !== null);

  const cardKinds: TradeCardKind[] = arr(ps["cardKinds"])
    .map((entry) => {
      const item = asRecord(entry);
      const cardId = str(item?.["cardId"]) ?? str(item?.["kind"]);
      if (!cardId) return null;
      return { cardId, label: str(item?.["label"]) ?? "" };
    })
    .filter((k): k is TradeCardKind => k !== null);

  const readObjectives = (value: unknown, withSatisfied: boolean): TradeObjective[] =>
    arr(value)
      .map((entry) => {
        const item = asRecord(entry);
        const objectiveId = str(item?.["objectiveId"]);
        if (!objectiveId) return null;
        return {
          objectiveId,
          text: str(item?.["text"]) ?? "",
          kinds: arr(item?.["kinds"]).filter((k): k is string => typeof k === "string"),
          points: num(item?.["points"]) ?? 0,
          satisfied: withSatisfied ? item?.["satisfied"] === true : null,
        };
      })
      .filter((o): o is TradeObjective => o !== null);

  const hands = new Map<string, string[]>();
  const rawHands = asRecord(ps["hands"]);
  if (rawHands) {
    for (const [subjectId, list] of Object.entries(rawHands)) {
      hands.set(
        subjectId,
        arr(list).filter((id): id is string => typeof id === "string"),
      );
    }
  }

  const self = asRecord(ps["self"]);
  const selfSubjectId = str(self?.["subjectId"]);

  const trades: TradeRecord[] = arr(ps["trades"])
    .map((entry) => {
      const item = asRecord(entry);
      const tradeId = str(item?.["tradeId"]);
      if (!tradeId) return null;
      /*
       * 实测字段是 senderId / recipientId（值是 subject id），
       * 不是我最初猜的 senderSubjectId / senderMembershipId。
       * 保留旧的兼容读取，但实测名优先。
       */
      const senderSubjectId =
        str(item?.["senderId"]) ?? str(item?.["senderSubjectId"]) ?? str(item?.["senderMembershipId"]) ?? "";
      const recipientSubjectId =
        str(item?.["recipientId"]) ??
        str(item?.["recipientSubjectId"]) ??
        str(item?.["recipientMembershipId"]) ??
        "";
      return {
        tradeId,
        round: num(item?.["round"]),
        senderSubjectId,
        recipientSubjectId,
        offeredInstanceId: str(item?.["offeredInstanceId"]) ?? "",
        requestedInstanceId: str(item?.["requestedInstanceId"]) ?? "",
        status: str(item?.["status"]) ?? "pending",
        isIncoming: selfSubjectId !== null && recipientSubjectId === selfSubjectId,
        isOutgoing: selfSubjectId !== null && senderSubjectId === selfSubjectId,
      };
    })
    .filter((t): t is TradeRecord => t !== null);

  return {
    rounds,
    roundSeconds: num(config?.["roundSeconds"]) ?? 60,
    competitive: config?.["competitive"] === true,
    instances,
    cardKinds,
    objectives: readObjectives(ps["objectives"], false),
    selfObjectives: readObjectives(ps["selfObjectives"], true),
    selfHand: arr(self?.["hand"]).filter((id): id is string => typeof id === "string"),
    hands,
    trades,
    tradesEmptyReason: str(ps["tradesEmptyReason"]),
    currentRound: num(ps["currentRound"]) ?? 1,
    selfSubjectId,
    phase,
    result: arr(result?.["players"]).flatMap(entry => {
      const p = asRecord(entry), id = str(p?.subjectId);
      return id && int(p?.completed, 2) && int(p?.total, 2) && Number(p?.completed) <= Number(p?.total) && int(p?.points, 100)
        ? [{subjectId:id, completed:Number(p?.completed), total:Number(p?.total), points:Number(p?.points), satisfiedIds:strings(p?.satisfiedIds), unsatisfiedIds:strings(p?.unsatisfiedIds)}] : [];
    }),
    scores: published && result ? arr(view.results).flatMap(entry => {
      const p = asRecord(entry), id = str(p?.subjectId);
      return id && int(p?.scoreUnits, 10000) ? [{subjectId:id, points:Number(p?.scoreUnits)/100, rank:int(p?.rank, 8) && Number(p?.rank)>0 ? Number(p?.rank) : null}] : [];
    }) : [],
  };
}

/** 实例 id → 展示标签 */
export function instanceLabelMap(round: TradeRound): Map<string, string> {
  const map = new Map<string, string>();
  for (const instance of round.instances) map.set(instance.instanceId, instance.label);
  return map;
}

/** 待我处理的提案（发给我的、还在 pending 的） */
export function incomingPending(round: TradeRound): TradeRecord[] {
  return round.trades.filter((t) => t.isIncoming && t.status === "pending");
}

/** 我发出、还在等对方回应的提案 */
export function outgoingPending(round: TradeRound): TradeRecord[] {
  return round.trades.filter((t) => t.isOutgoing && t.status === "pending");
}

/** 已结束的提案（历史记录） */
export function resolvedTrades(round: TradeRound): TradeRecord[] {
  return round.trades.filter((t) => t.status !== "pending");
}

/** 便捷别名 */
export type TradeState = Schemas["StateTrade"];
