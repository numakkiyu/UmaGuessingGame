// This puzzle describes historical horses, not family relationships between fictional characters. Display the server-provided disclaimer
import type { components } from "./types";

type Schemas = components["schemas"];

/** 一个血统槽位 */
export type PedigreeSlot = {
  slotId: string;
  /** 中文标签：父 / 母 / 父父 / 父母 / 母父 / 母母 */
  label: string;
  /** 槽位含义说明（来自 rules[].means） */
  means: string;
  /** 代际：1 = 父母辈，2 = 祖父母辈 */
  generation: number | null;
};

/** 一匹候选马 */
export type PedigreeCandidate = {
  horseId: string;
  name: string;
  nkbId: string | null;
};

/** 本题的主角（角色与其史实原型） */
export type PedigreeSubject = {
  characterId: string | null;
  characterName: string;
  /** 史实原型名（日文原名） */
  prototypeName: string;
  nkbId: string | null;
};

export type PedigreeRound = {
  progress: {subjectId:string;assignedSlots:number;submissions:number}[];
  subjectId: string;
  hasSelf: boolean;
  slots: PedigreeSlot[];
  candidates: PedigreeCandidate[];
  subject: PedigreeSubject | null;
  /** 服务端给的标准免责声明，含「史实原型血统」措辞 */
  disclaimer: string;
  /** 计分说明（服务端原文） */
  scoring: string;
  /** 已分配槽位：slotId → horseId */
  assignments: Map<string, string>;
  assignedCount: number;
  totalSlots: number;
  submissions: number;
  /** 答题时限（秒），来自 config.answerSeconds */
  answerSeconds: number | null;
  /** 是否双人合作 */
  cooperative: boolean;
  phase: string | null;
  /**
   * 正解。只在回合终态（或本人是裁判）时由服务端发布，其余时候是 undefined。
   *
   * 服务端 `projection.Answer` 一直有这个字段（`answerReveal`，终态时 omitempty
   * 消失即发布），但前端类型里没有、归一化也不读——和 clue-puzzle 同一个缺陷：
   * 揭晓后玩家看不到正确答案。
   */
  answer?: {
    /** 服务端 `revealSlot` 的字段是 slotId / label / horseId / name，不是 characterId。 */
    slots: { slotId: string; label: string; horseId: string; name: string }[];
    solutionCount: number;
  };
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

/**
 * 归一化 pedigree 回合。
 *
 * 同时兼容实测结构与契约字段名——契约补齐前，两边都能读。
 */
export function normalizePedigreeRound(raw: unknown): PedigreeRound {
  const view = asRecord(raw) ?? {};
  const publicState = asRecord(view["publicState"]) ?? view;

  const rules = Array.isArray(publicState["rules"]) ? publicState["rules"] : [];
  const meansBySlot = new Map<string, string>();
  for (const entry of rules) {
    const item = asRecord(entry);
    if (!item) continue;
    const slotId = str(item["slotId"]);
    const means = str(item["means"]);
    if (slotId && means) meansBySlot.set(slotId, means);
  }

  const slots: PedigreeSlot[] = [];
  const rawSlots = Array.isArray(publicState["slots"]) ? publicState["slots"] : [];
  for (const entry of rawSlots) {
    const item = asRecord(entry);
    if (!item) continue;
    const slotId = str(item["slotId"]);
    if (!slotId) continue;
    slots.push({
      slotId,
      label: str(item["label"]) ?? "",
      means: meansBySlot.get(slotId) ?? "",
      generation: num(item["generation"]),
    });
  }

  /*
   * 候选马：实测字段是 `candidates`，契约写的是 `cards`。
   * 两个都读，实测优先。
   */
  const candidates: PedigreeCandidate[] = [];
  const rawCandidates =
    (Array.isArray(publicState["candidates"]) ? publicState["candidates"] : null) ??
    (Array.isArray(publicState["cards"]) ? publicState["cards"] : []);
  for (const entry of rawCandidates) {
    const item = asRecord(entry);
    if (!item) continue;
    const horseId = str(item["horseId"]) ?? str(item["characterId"]) ?? str(item["id"]);
    if (!horseId) continue;
    candidates.push({
      horseId,
      name: str(item["name"]) === horseId ? "" : str(item["name"]) ?? str(item["displayName"]) ?? "",
      nkbId: str(item["nkbId"]),
    });
  }

  // 主角：实测 `subject` 是对象，契约 `prompt` 是字符串
  let subject: PedigreeSubject | null = null;
  const rawSubject = asRecord(publicState["subject"]);
  if (rawSubject) {
    subject = {
      characterId: str(rawSubject["characterId"]),
      characterName: str(rawSubject["characterName"]) ?? "",
      prototypeName: str(rawSubject["prototypeName"]) ?? "",
      nkbId: str(rawSubject["nkbId"]),
    };
  }

  // 自己的已分配槽位
  const assignments = new Map<string, string>();
  const self = asRecord(publicState["self"]);
  const rawAssignments = Array.isArray(self?.["assignments"]) ? self?.["assignments"] : [];
  for (const entry of rawAssignments) {
    const item = asRecord(entry);
    if (!item) continue;
    const slotId = str(item["slotId"]);
    const horseId = str(item["horseId"]);
    if (slotId && horseId) assignments.set(slotId, horseId);
  }

  const config = asRecord(publicState["config"]);

  // 正解只在终态/裁判可见；没这个字段就保持 undefined，界面按「未揭晓」渲染。
  const rawAnswer = asRecord(publicState["answer"]);
  const answer = rawAnswer
    ? {
        slots: (Array.isArray(rawAnswer["slots"]) ? rawAnswer["slots"] : [])
          .map((entry) => {
            const item = asRecord(entry);
            const slotId = str(item?.["slotId"]);
            const horseId = str(item?.["horseId"]);
            if (!slotId || !horseId) return null;
            return {
              slotId,
              label: str(item?.["label"]) ?? "",
              horseId,
              name: str(item?.["name"]) === horseId ? "" : str(item?.["name"]) ?? "",
            };
          })
          .filter((x): x is { slotId: string; label: string; horseId: string; name: string } => x !== null),
        solutionCount: num(rawAnswer["solutionCount"]) ?? 0,
      }
    : undefined;

  return {
    progress: (Array.isArray(publicState["progress"]) ? publicState["progress"] : []).flatMap(raw=>{const p=asRecord(raw),id=str(p?.["subjectId"]);return id?[{subjectId:id,assignedSlots:num(p?.["assignedSlots"])??0,submissions:num(p?.["submissions"])??0}]:[];}),
    subjectId: str(self?.["subjectId"]) ?? "",
    hasSelf: self !== null,
    slots,
    candidates,
    subject,
    disclaimer: str(publicState["disclaimer"]) ?? "",
    scoring: str(publicState["scoring"]) ?? "按正确槽位计分",
    assignments,
    assignedCount: assignments.size,
    totalSlots: slots.length,
    submissions: num(self?.["submissions"]) ?? 0,
    answerSeconds: num(config?.["answerSeconds"]),
    cooperative: config?.["cooperative"] === true,
    phase: str(view["round"] ? asRecord(view["round"])?.["phase"] : publicState["phase"]),
    answer,
  };
}

/**
 * 按代际分组槽位，用于界面分区显示。
 *
 * 第 1 代（父/母）和第 2 代（祖父母辈）分开，因为视觉上它们是不同层级：
 * 玩家先填父母，再填祖辈，分组能减少"填错行"的错误。
 */
export function groupSlotsByGeneration(slots: readonly PedigreeSlot[]): {
  first: PedigreeSlot[];
  second: PedigreeSlot[];
  other: PedigreeSlot[];
} {
  const first: PedigreeSlot[] = [];
  const second: PedigreeSlot[] = [];
  const other: PedigreeSlot[] = [];
  for (const slot of slots) {
    if (slot.generation === 1) first.push(slot);
    else if (slot.generation === 2) second.push(slot);
    else other.push(slot);
  }
  return { first, second, other };
}

/**
 * 校验分配是否可提交（本地预检，不替代服务端）。
 *
 * 规则：至少分配一个槽位。空提交没有意义（服务端也会接受但不得分）。
 * 不检查"是否全部填满"—— 允许截止前修订，部分填写是合法的中间状态。
 */
export function validatePedigreeDraft(assignments: ReadonlyMap<string, string>): string | null {
  if (assignments.size === 0) return "至少先给一个槽位选一匹马。";
  return null;
}

/** 便捷别名 */
export type PedigreeState = Schemas["StatePedigree"];


/** Draft validation never reads private ancestry or grades the choices. */
export type OnlinePedigreeAssignment = {slotId:string;horseId:string};
export function onlinePedigreeDraftValid(value:unknown,round:PedigreeRound):value is OnlinePedigreeAssignment[] {
 if (!Array.isArray(value)||value.length>round.slots.length) return false;
 const slots=new Set<string>(),horses=new Set<string>();
 for (const raw of value) {
  const a=asRecord(raw),slot=a?.["slotId"],horse=a?.["horseId"];
  if(typeof slot!=="string"||typeof horse!=="string"||slots.has(slot)||horses.has(horse)||!round.slots.some(s=>s.slotId===slot)||!round.candidates.some(c=>c.horseId===horse))return false;
  slots.add(slot);horses.add(horse);
 }
 return true;
}
