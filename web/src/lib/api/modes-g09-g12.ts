// Preserve absent fields separately from explicit empty values; private clues and unrevealed choices remain scoped to the viewer
import type { components } from "./types";

type Schemas = components["schemas"];

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}
function str(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}
function num(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}
function bool(value: unknown): boolean {
  return value === true;
}
function arr(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

/* ============================================================
 * clue-puzzle（团队情报拼图）
 * ============================================================ */

export type ClueCard = { cardId: string; label: string };
export type ClueSlot = { slotIndex: number; label: string };
export type ClueItem = {
  constraintId: string;
  text: string;
  kind: string | null;
  position?: number;
  /** 线索涉及的卡（服务端给的时候才有） */
  cards: string[];
};

export type CluePuzzleRound = {
  selfSubjectId: string | null;
  answerSeconds: number;
  scores: { subjectId: string; label: string; points: number; rank: number | null }[];
  cards: ClueCard[];
  slots: ClueSlot[];
  /** 自己的私有线索。只在本人界面显示，队员通过交流拼合信息 */
  privateClues: ClueItem[];
  /** 共享区线索（可能为空，`sharedEmptyReason` 说明原因） */
  sharedClues: ClueItem[];
  sharedEmptyReason: string | null;
  constraintCount: number;
  scoring: string;
  /** 当前控制者 */
  controlSubjectId: string | null;
  selfIsController: boolean;
  /** 自己的分配：slotIndex → cardId */
  assignment: Map<number, string>;
  submissions: number;
  phase: string | null;
  /**
   * 正解。只在回合进入终态（或本人是裁判）时由服务端发布，其余时候是 undefined。
   *
   * 服务端一直有这个字段（`answerReveal`），但前端类型里没有、归一化也不读，
   * 于是揭晓后玩家看不到正确答案——「揭晓」这一步在界面上是空的。
   */
  answer?: {
    slots: { slotIndex: number; cardId: string; label: string }[];
    solutionCount: number;
    constraints: ClueItem[];
  };
};

export function normalizeCluePuzzleRound(raw: unknown): CluePuzzleRound {
  const view = asRecord(raw) ?? {};
  const ps = asRecord(view["publicState"]) ?? view;

  const cards: ClueCard[] = arr(ps["cards"])
    .map((entry) => {
      const item = asRecord(entry);
      const cardId = str(item?.["cardId"]) ?? str(item?.["id"]);
      if (!cardId) return null;
      return { cardId, label: str(item?.["label"]) ?? "" };
    })
    .filter((c): c is ClueCard => c !== null);

  const slots: ClueSlot[] = arr(ps["slots"])
    .map((entry) => {
      const item = asRecord(entry);
      const slotIndex = num(item?.["slotIndex"]) ?? num(item?.["index"]);
      if (slotIndex === null) return null;
      return { slotIndex, label: str(item?.["label"]) ?? `第 ${slotIndex} 位` };
    })
    .filter((s): s is ClueSlot => s !== null)
    .sort((a, b) => a.slotIndex - b.slotIndex);

  const readClues = (value: unknown): ClueItem[] =>
    arr(value)
      .map((entry): ClueItem | null => {
        const item = asRecord(entry);
        const constraintId = str(item?.["constraintId"]) ?? str(item?.["clueId"]);
        if (!constraintId) return null;
        return {
          constraintId,
          text: str(item?.["text"]) ?? "",
          kind: str(item?.["kind"]),
          position: num(item?.["position"]) ?? undefined,
          cards: arr(item?.["cards"]).filter((c): c is string => typeof c === "string"),
        };
      })
      .filter((c): c is ClueItem => c !== null);

  const self = asRecord(ps["self"]);
  const assignment = new Map<number, string>();
  const rawAssignment = asRecord(self?.["assignment"]);
  if (rawAssignment) {
    for (const [key, value] of Object.entries(rawAssignment)) {
      const slotIndex = Number(key);
      const cardId = typeof value === "string" ? value : null;
      if (Number.isFinite(slotIndex) && cardId) assignment.set(slotIndex, cardId);
    }
  }

  const sharedRecord = asRecord(ps["shared"]);

  // 正解只在终态/裁判可见。没这个字段时保持 undefined，界面按「未揭晓」渲染。
  const phase = str(asRecord(view["round"])?.["phase"]) ?? str(ps["phase"]);
  const published = phase === "revealed" || phase === "settled";
  const rawAnswer = published ? asRecord(ps["answer"]) : null;
  const answer = rawAnswer
    ? {
        slots: arr(rawAnswer["slots"])
          .map((entry) => {
            const item = asRecord(entry);
            const slotIndex = num(item?.["slotIndex"]);
            const cardId = str(item?.["cardId"]);
            if (slotIndex === null || !cardId) return null;
            return { slotIndex, cardId, label: str(item?.["label"]) ?? "" };
          })
          .filter((x): x is { slotIndex: number; cardId: string; label: string } => x !== null)
          .sort((a, b) => a.slotIndex - b.slotIndex),
        solutionCount: num(rawAnswer["solutionCount"]) ?? 0,
        constraints: readClues(rawAnswer["constraints"]),
      }
    : undefined;

  return {
    selfSubjectId: str(self?.["subjectId"]),
    answerSeconds: num(asRecord(ps["config"])?.["answerSeconds"]) ?? 240,
    cards,
    slots,
    scores: (published ? arr(view["results"]) : []).flatMap(entry => {
      const item = asRecord(entry), subjectId = str(item?.["subjectId"]), units = num(item?.["scoreUnits"]);
      if (!subjectId || units === null || units < 0) return [];
      return [{ subjectId, label: str(asRecord(view["subjectNames"])?.[subjectId]) ?? "", points: units / 100, rank: num(item?.["rank"]) }];
    }).sort((a,b)=>(a.rank??Number.MAX_SAFE_INTEGER)-(b.rank??Number.MAX_SAFE_INTEGER)||a.subjectId.localeCompare(b.subjectId)),
    privateClues: readClues(ps["privateClues"]),
    sharedClues: readClues(Array.isArray(ps["shared"]) ? ps["shared"] : sharedRecord?.["clues"] ?? ps["sharedClues"]),
    sharedEmptyReason: str(ps["sharedEmptyReason"]),
    constraintCount: num(ps["constraintCount"]) ?? 0,
    scoring: str(ps["scoring"]) ?? "按满足的约束数计分",
    controlSubjectId: str(ps["controlSubjectId"]),
    selfIsController: bool(ps["selfIsController"]),
    assignment,
    submissions: num(self?.["submissions"]) ?? 0,
    phase: str(asRecord(view["round"])?.["phase"]) ?? str(ps["phase"]),
    answer,
  };
}

/* ============================================================
 * strategy-race（回合制策略赛道）
 * ============================================================ */

export type RaceActionSpec = {
  action: string;
  label: string;
  positionGain: number;
  staminaDelta: number;
  /** 冲刺的最低体力要求 */
  minStamina: number | null;
  /** 跟跑的领先区间（只有跟跑有） */
  leaderGap: boolean;
  alternateGain: number | null;
};

export type RaceStanding = {
  subjectId: string;
  position: number;
  stamina: number;
};

export type StrategyDecision = { turn:number; subjectId:string; action:string; defaulted:boolean };
export type StrategyTurn = {turn:number;start:RaceStanding[];applied:(RaceStanding & {action:string;defaulted:boolean;leaderGap:boolean;positionGain:number;staminaDelta:number})[]};
export type StrategyRaceRound = {
  totalTurns: number;
  decisionSeconds: number;
  currentTurn: number;
  resolvedTurns: number;
  startStamina: number;
  staminaCap: number;
  followMinGap: number;
  followMaxGap: number;
  /** 无有效行动时的默认动作（开局要告知） */
  defaultAction: string;
  actions: RaceActionSpec[];
  standings: RaceStanding[];
  hasSelf: boolean;
  history: StrategyTurn[];
  final: (RaceStanding & {rank:number;shared:boolean})[];
  self: { subjectId: string | null; position: number; stamina: number; decisions: number; choices:StrategyDecision[] };
  competitive: boolean;
  /** 非官方数据的来源说明（服务端给了就要显示） */
  provenanceNote: string | null;
  phase: string | null;
};

export function normalizeStrategyRaceRound(raw: unknown): StrategyRaceRound {
  const view = asRecord(raw) ?? {};
  const ps = asRecord(view["publicState"]) ?? view;
  const track = asRecord(ps["track"]) ?? {};
  const config = asRecord(ps["config"]) ?? {};

  const actions: RaceActionSpec[] = arr(track["actions"])
    .map((entry) => {
      const item = asRecord(entry);
      const action = str(item?.["action"]);
      if (!action || !["steady","sprint","recover","follow"].includes(action)) return null;
      return {
        action,
        label: str(item?.["label"]) ?? action,
        positionGain: num(item?.["positionGain"]) ?? 0,
        staminaDelta: num(item?.["staminaDelta"]) ?? 0,
        minStamina: num(item?.["minStamina"]),
        leaderGap: bool(item?.["leaderGap"]),
        alternateGain: num(item?.["alternateGain"]),
      };
    })
    .filter((a): a is RaceActionSpec => a !== null);

  const standings: RaceStanding[] = arr(ps["standings"])
    .map((entry) => {
      const item = asRecord(entry);
      const subjectId = str(item?.["subjectId"]);
      if (!subjectId) return null;
      return {
        subjectId,
        position: num(item?.["position"]) ?? 0,
        stamina: num(item?.["stamina"]) ?? 0,
      };
    })
    .filter((s): s is RaceStanding => s !== null);

  const self = asRecord(ps["self"]);
  const provenance = asRecord(ps["provenance"]);
  const points=(value:unknown):RaceStanding[]=>arr(value).flatMap(v=>{const x=asRecord(v);return typeof x?.subjectId==="string"&&typeof x.position==="number"&&Number.isSafeInteger(x.position)&&typeof x.stamina==="number"&&Number.isSafeInteger(x.stamina)?[{subjectId:x.subjectId,position:x.position,stamina:x.stamina}]:[];});
  const choices:StrategyDecision[]=arr(self?.decisions).flatMap(v=>{const x=asRecord(v);return x&&Number.isSafeInteger(x.turn)&&Number(x.turn)>0&&typeof x.subjectId==="string"&&typeof x.action==="string"?[{turn:Number(x.turn),subjectId:x.subjectId,action:x.action,defaulted:x.defaulted===true}]:[];});
  const history:StrategyTurn[]=arr(ps.history).flatMap(v=>{const h=asRecord(v);if(!h||!Number.isSafeInteger(h.turn)||Number(h.turn)<1||Number(h.turn)>Number(ps.resolvedTurns))return [];return [{turn:Number(h.turn),start:points(h.start),applied:arr(h.applied).flatMap(v=>{const x=asRecord(v),p=points([v])[0];return x&&p&&typeof x.action==="string"&&typeof x.positionGain==="number"&&typeof x.staminaDelta==="number"?[{...p,action:x.action,defaulted:x.defaulted===true,leaderGap:x.leaderGap===true,positionGain:x.positionGain,staminaDelta:x.staminaDelta}]:[];})}];});
  const phase=str(asRecord(view["round"])?.["phase"])??str(ps.phase);
  const reveal=phase==="revealed"||phase==="settled";
  const final=reveal?arr(ps.final).flatMap(v=>{const x=asRecord(v),p=points([v])[0];return p&&x&&Number.isSafeInteger(x.rank)?[{...p,rank:Number(x.rank),shared:x.shared===true}]:[];}):[];


  return {
    totalTurns: num(track["totalTurns"]) ?? num(config["totalTurns"]) ?? 8,
    decisionSeconds: num(track["decisionSeconds"]) ?? num(config["decisionSeconds"]) ?? 20,
    currentTurn: num(ps["currentTurn"]) ?? 1,
    resolvedTurns: num(ps["resolvedTurns"]) ?? 0,
    startStamina: num(track["startStamina"]) ?? 6,
    staminaCap: num(track["staminaCap"]) ?? 8,
    followMinGap: num(track["followMinGap"]) ?? 1,
    followMaxGap: num(track["followMaxGap"]) ?? 3,
    defaultAction: str(track["defaultAction"]) ?? "steady",
    actions,
    standings,
    hasSelf: !!str(self?.subjectId),
    history, final,
    self: {
      subjectId: str(self?.["subjectId"]),
      position: num(self?.["position"]) ?? 0,
      stamina: num(self?.["stamina"]) ?? 0,
      decisions: choices.length,
      choices,
    },
    competitive: bool(ps["competitive"]),
    // 游玩页只说「这是设计值」，不把内部文档路径（如 05-…）摆到玩家面前。
    // basis 是给审核与来源页用的溯源信息，留在服务端数据里，不渲染在游玩页。
    provenanceNote:
      provenance && bool(provenance["notOfficialData"])
        ? "本玩法的位置与体力数值是为好玩设计的模拟值，不是官方数据。"
        : null,
    phase,
  };
}

/** 某动作在当前体力下是否可选（冲刺需要体力 ≥ minStamina） */
export function isActionAvailable(spec: RaceActionSpec, stamina: number): boolean {
  if (spec.minStamina === null) return true;
  return stamina >= spec.minStamina;
}

/* ============================================================
 * rapport（默契问答）
 * ============================================================ */

export type RapportOption = { optionId: string; label: string };
export type RapportQuestion = {
  questionId: string; prompt: string; options: RapportOption[];
  chooserId: string | null; selfIsChooser: boolean; answered: boolean;
  selfOptionId: string | null; locked: boolean;
  reveal?: { chosenOptionId: string; chosenLabel: string; correctPredictorIds: string[] };
};
export type RapportRound = {
  questions: RapportQuestion[]; currentQuestion: number; questionCount: number; questionSeconds: number;
  groupId: string | null;
  groups: { groupId: string; chooserId: string; answered: number; members: number; totalSlots: number | null }[];
  scores: { subjectId: string; label: string; points: number; rank: number | null }[];
  totalSlots: number; revealedHits: number | null; revealedSlots: number | null;
  self: { subjectId: string | null; answeredCount: number }; phase: string | null;
};
export function normalizeRapportRound(raw: unknown): RapportRound {
  const view = asRecord(raw) ?? {}, ps = asRecord(view["publicState"]) ?? view;
  const questions: RapportQuestion[] = arr(ps["questions"]).flatMap(entry => {
    const q = asRecord(entry), questionId = str(q?.["questionId"]);
    if (!questionId) return [];
    const rv = bool(q?.["locked"]) ? asRecord(q?.["reveal"]) : null;
    return [{
      questionId, prompt: str(q?.["prompt"]) ?? "",
      options: arr(q?.["options"]).flatMap(entry => {
        const o = asRecord(entry), optionId = str(o?.["optionId"]);
        return optionId ? [{ optionId, label: str(o?.["label"]) ?? "" }] : [];
      }),
      chooserId: str(q?.["chooserId"]), selfIsChooser: bool(q?.["selfIsChooser"]),
      answered: bool(q?.["answered"]), selfOptionId: str(q?.["selfOptionId"]), locked: bool(q?.["locked"]),
      reveal: rv ? { chosenOptionId: str(rv["chosenOptionId"]) ?? "", chosenLabel: str(rv["chosenLabel"]) ?? "", correctPredictorIds: arr(rv["correctPredictorIds"]).filter((id): id is string => typeof id === "string") } : undefined,
    }];
  });
  const phase = str(asRecord(view["round"])?.["phase"]) ?? str(ps["phase"]);
  return {
    questions, questionSeconds: num(asRecord(ps["config"])?.["questionSeconds"]) ?? 20, currentQuestion: num(ps["currentQuestion"]) ?? Math.max(1, questions.findIndex(q => !q.locked) + 1),
    questionCount: num(ps["questionCount"]) ?? questions.length,
    groupId: str(ps["groupId"]),
    groups: arr(ps["groups"]).flatMap(entry => {
      const g = asRecord(entry), groupId = str(g?.["groupId"]), chooserId = str(g?.["chooserId"]);
      return groupId && chooserId ? [{ groupId, chooserId, answered: num(g?.["answered"]) ?? 0, members: num(g?.["members"]) ?? 0, totalSlots: num(g?.["totalSlots"]) }] : [];
    }),
    scores: (phase === "revealed" || phase === "settled" ? arr(view["results"]) : []).flatMap(entry => {
      const score = asRecord(entry), subjectId = str(score?.["subjectId"]), units = num(score?.["scoreUnits"]);
      return subjectId && units !== null && Number.isSafeInteger(units) && units >= 0 && units <= 10000 ? [{ subjectId, label: str(asRecord(view["subjectNames"])?.[subjectId]) ?? "", points: units / 100, rank: num(score?.["rank"]) }] : [];
    }),
    totalSlots: num(ps["totalSlots"]) ?? 0, revealedHits: num(ps["revealedHits"]), revealedSlots: num(ps["revealedSlots"]),
    self: { subjectId: str(asRecord(ps["self"])?.["subjectId"]), answeredCount: questions.filter(q => q.answered).length },
    phase,
  };
}

/* ============================================================
 * creative（创意企划）
 * ============================================================ */

export type CreativePrompt = {
  characterId: string | null;
  characterName: string;
  scene: string;
  constraint: string;
  sceneId: string | null;
  constraintId: string | null;
};

export type CreativeBallotEntry = {
  optionId: string;
  prompt: CreativePrompt | null;
  text: string;
  /** 匿名展示，不显示作者 */
  teamVotes: number | null;
  audienceVotes: number | null;
};

export type CreativeRound = {
 subphase: string | null;
 config:{writeSeconds:number;voteSeconds:number;entertainmentBudget:number;allowAudienceVote:boolean};
 review:CreativeBallotEntry[];
 vote:{eligible:boolean;canVote:boolean;voted:boolean;ownWorkId:string|null;voteWorkId:string|null;voteKind:string|null}|null;
 authors:Record<string,string>;
 progress:{subjectId:string;submitted:boolean;voted:boolean}[];
 scores:{subjectId:string;points:number;rank:number|null}[];
  prompt: CreativePrompt | null;
  maxCodePoints: number;
  /** 服务端下发的文本规则原文（含 NFC 归一化要求） */
  textRules: string;
  scoring: string;
  ballot: CreativeBallotEntry[];
  /** 投票区为空的原因（服务端解释） */
  ballotHiddenReason: string | null;
  self: { subjectId: string | null; submitted: boolean; voted: boolean;workId:string|null;workText:string;hostRejected:boolean };
  phase: string | null;
  /**
   * 投票统计与获奖。只在回合终态时由服务端发布。
   *
   * 服务端 `projection.Result`（`resultView`）一直是有的，但前端类型里没有、
   * 归一化也不读——终态后玩家看不到投票结果与获奖作品。这是 clue-puzzle /
   * pedigree / rapport / sort / co-race-path 之外的第六个同类缺陷。
   */
  result?: {
    works: { workId: string; teamVotes: number; audienceVotes: number; total: number; rank: number; shared: boolean }[];
    teamVotes: number;
    audienceVotes: number;
    validWorks: number;
    award: { workIds: string[]; shared: boolean; budget: number } | null;
 competitiveBonus:{workIds:string[];shared:boolean;budget:number}|null;
  };
};

export function normalizeCreativeRound(raw: unknown): CreativeRound {
  const view = asRecord(raw) ?? {};
  const ps = asRecord(view["publicState"]) ?? view;

  function readPrompt(raw:unknown):CreativePrompt|null {
  const promptRecord = asRecord(raw);
  return promptRecord
    ? {
        characterId: str(promptRecord["characterId"]),
        characterName: str(promptRecord["characterName"]) ?? "",
 sceneId:str(promptRecord["sceneId"]),constraintId:str(promptRecord["constraintId"]),
        scene: str(promptRecord["scene"]) ?? "",
        constraint: str(promptRecord["constraint"]) ?? "",
      }
    : null;
 }
 const prompt=readPrompt(ps["prompt"]);

  /* 投票项以 workId 标识，题面与作品文本保持服务端提供的内容 */
  const readBallot=(raw:unknown):CreativeBallotEntry[]=>arr(raw)
    .map((entry) => {
      const item = asRecord(entry);
      const optionId = str(item?.["workId"]) ?? str(item?.["optionId"]);
      if (!optionId) return null;
      return {
        optionId,
        prompt:readPrompt(item?.["prompt"]),
        text: str(item?.["text"]) ?? "",
        teamVotes: num(item?.["teamVotes"]),
        audienceVotes: num(item?.["audienceVotes"]),
      };
    })
    .filter((b): b is CreativeBallotEntry => b !== null);

  const ballot=readBallot(ps["ballot"]);
  // 投票统计只在终态可见；没这个字段就保持 undefined。
  const phase=str(asRecord(view["round"])?.["phase"])??str(ps["phase"]),reveal=phase==="revealed"||phase==="settled";
  const rawResult = reveal&&ps["subphase"]==="closed"?asRecord(ps["result"]):null;
  const result = rawResult
  ? {
      works: ((Array.isArray(rawResult["works"]) ? rawResult["works"] : []) as unknown[])
        .map((entry) => {
          const item = asRecord(entry);
          const workId = str(item?.["workId"]);
          if (!workId) return null;
          return {
            workId,
            teamVotes: num(item?.["teamVotes"]) ?? 0,
            audienceVotes: num(item?.["audienceVotes"]) ?? 0,
            total: num(item?.["total"]) ?? 0,
            rank: num(item?.["rank"]) ?? 0,
            shared: bool(item?.["shared"]),
          };
        })
        .filter((x): x is { workId: string; teamVotes: number; audienceVotes: number; total: number; rank: number; shared: boolean } => x !== null),
      teamVotes: num(rawResult["teamVotes"]) ?? 0,
      audienceVotes: num(rawResult["audienceVotes"]) ?? 0,
      validWorks: num(rawResult["validWorks"]) ?? 0,
      competitiveBonus: (()=>{const b=asRecord(rawResult["competitiveBonus"]);return b?{workIds:arr(b["workIds"]).filter((id):id is string=>typeof id==="string"),shared:bool(b["shared"]),budget:num(b["budget"])??0}:null;})(),
      award: (() => {
        const a = asRecord(rawResult["award"]);
        if (!a) return null;
        return {
          workIds: (Array.isArray(a["workIds"]) ? a["workIds"] : []).filter(
            (x): x is string => typeof x === "string",
          ),
          shared: bool(a["shared"]),
          budget: num(a["budget"]) ?? 0,
        };
      })(),
    }
  : undefined;
  const self = asRecord(ps["self"]),vote=asRecord(ps["vote"]),config=asRecord(ps["config"]);

  return {
 subphase:str(ps["subphase"]),config:{writeSeconds:num(config?.writeSeconds)??90,voteSeconds:num(config?.voteSeconds)??30,entertainmentBudget:num(config?.entertainmentBudget)??0,allowAudienceVote:config?.allowAudienceVote!==false},review:readBallot(ps["reviewOptions"]),
 vote:vote?{eligible:bool(vote.eligible),canVote:bool(vote.canVote),voted:bool(vote.voted),ownWorkId:str(vote.ownWorkId),voteWorkId:str(vote.voteWorkId),voteKind:str(vote.voteKind)}:null,
 authors:reveal&&ps["subphase"]==="closed"?Object.fromEntries(Object.entries(asRecord(ps["authors"])??{}).filter((entry):entry is [string,string]=>typeof entry[1]==="string")):{},
 progress:arr(ps["progress"]).flatMap(raw=>{const p=asRecord(raw);return p&&str(p.subjectId)?[{subjectId:String(p.subjectId),submitted:bool(p.submitted),voted:bool(p.voted)}]:[];}),
 scores:arr(reveal&&ps["subphase"]==="closed"?view["results"]:[]).flatMap(raw=>{const r=asRecord(raw);return r&&str(r.subjectId)&&Number.isSafeInteger(r.scoreUnits)&&Number(r.scoreUnits)>=0&&Number(r.scoreUnits)<=2000&&Number(r.scoreUnits)%100===0?[{subjectId:String(r.subjectId),points:Number(r.scoreUnits)/100,rank:Number.isSafeInteger(r.rank)&&Number(r.rank)>0?Number(r.rank):null}]:[];}),
    prompt,
    maxCodePoints: num(ps["maxCodePoints"]) ?? 80,
    textRules: str(ps["textRules"]) ?? "",
    scoring: str(ps["scoring"]) ?? "",
    ballot,
    ballotHiddenReason: str(ps["ballotHiddenReason"]),
    self: {
      subjectId: str(self?.["subjectId"]),
      submitted: bool(self?.["submitted"]),
      voted: bool(self?.["voted"]),
      workId:str(self?.workId),workText:str(self?.workText)??"",hostRejected:bool(self?.hostRejected),
    },
    phase: str(asRecord(view["round"])?.["phase"]) ?? str(ps["phase"]),
    result,
  };
}





/**
 * 计数码点（一个汉字/字母/表情都算 1）。
 *
 * 用 `[...text].length` 而不是 `text.length`——后者是 UTF-16 码元，
 * 一个表情会算 2，与 的「一个表情也算 1 个字」不符。
 *
 * NFC 归一化由服务端内部完成（2026-11-09 起），前端不需要也不应该
 * 替玩家改文本。所以这里只计数，不 normalize。
 */
export function countCodePoints(text: string): number {
  return [...text].length;
}

/** 便捷别名 */
export type ClueState = Schemas["StateCluePuzzle"];
export type RaceState = Schemas["StateStrategyRace"];
export type RapportState = Schemas["StateRapport"];
export type CreativeState = Schemas["StateCreative"];
