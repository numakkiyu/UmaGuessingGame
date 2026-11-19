/** Accept any valid path; reject cycles and repeated nodes. */
import type { components } from "./types";
import {validLocalizedNames,type LocalizedNames} from "@/i18n/entities";

type Schemas = components["schemas"];

export type PathNode = {
  characterId: string;
  name: string;
  imagePath: string | null;
  localizedNames?: LocalizedNames;
};

export type PathEvidence = {
  evidenceId: string;
  sourceUrl: string;
  label: string;
  raceId: string;
};

export type PathNeighbor = {
  characterId: string;
  name: string;
  evidence: PathEvidence[];
};

/** 一条有赛事证据的同场边（无向）。仅在开卷局由服务端下发。 */
export type PathEdge = {
  from: string;
  to: string;
  raceId: string;
  raceName: string;
  date: string;
};

export type PathSelf = {
  subjectId: string;
  path: string[];
  submitted: boolean;
  revisions: number;
  edges: number;
  arrived: boolean;
  scoreUnits: number;
  evidence: PathEdge[];
};

export type PathRound = {
  phase: string;
  maxEdges: number;
  questionSeconds: number | null;
  openBook: boolean;
  start: PathNode | null;
  goal: PathNode | null;
  shortestEdges: number | null;
  nodes: PathNode[];
  /** 开卷局的完整边表；闭卷局为空数组（服务端不下发）。 */
  edges: PathEdge[];
  self: PathSelf | null;
  answer: { shortestPath: string[]; evidence: PathEdge[] } | null;
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

function normalizeNode(value: unknown): PathNode | null {
  const record = asRecord(value);
  if (!record) return null;
  const characterId = str(record["characterId"]);
  if (!characterId) return null;
  return {
    characterId,
    name: str(record["name"]) === characterId ? "" : str(record["name"]) ?? "",
    imagePath: resolveNodeImage(str(record["imagePath"]), characterId),
    ...(validLocalizedNames(record["localizedNames"]) && record["localizedNames"] ? {localizedNames:record["localizedNames"]} : {}),
  };
}

/**
 * 节点头像地址。
 *
 * 实测（2026-11-08）：本模式的 publicState 里 start/goal/nodes 的 `imagePath`
 * 全部是空字符串，因为内容包的 `co-race-path_graph.json` 节点只发布
 * characterId + name，没有图片字段（后端 rec.Str("imagePath") 取到空）。
 * 于是起点/终点卡会渲染成空白圆圈 —— 玩家看不到是谁。
 *
 * 处理：服务端给了路径就用它；为空时按 characterId 回退到受控头像目录
 * `/assets/avatars/<id>.webp`（其他模式用的就是这条路径，实测 200）。
 * 只接受受控字符集，避免把任意字符串拼进 URL。
 *
 * 这是展示兜底，不改变任何规则或数据语义。后端/内容包补上 imagePath 后
 * 这个回退自然不再触发。
 */
function resolveNodeImage(raw: string | null, characterId: string): string | null {
  if (raw) return raw;
  if (!/^[A-Za-z0-9_-]+$/.test(characterId)) return null;
  return `/assets/avatars/${characterId}.webp`;
}

export function normalizePathRound(view: Schemas["RoundView"] | null | undefined): PathRound | null {
  if (!view) return null;
  const publicState = asRecord(view.publicState);
  if (!publicState) return null;

  const config = asRecord(publicState["config"]) ?? {};

  const nodes: PathNode[] = [];
  const rawNodes = publicState["nodes"];
  if (Array.isArray(rawNodes)) {
    for (const entry of rawNodes) {
      const node = normalizeNode(entry);
      if (node) nodes.push(node);
    }
  }

  const selfRecord = asRecord(publicState["self"]);
  const self: PathSelf | null = selfRecord
    ? {
        subjectId: str(selfRecord["subjectId"]) ?? "",
        path: strArray(selfRecord["path"]),
        submitted: selfRecord["submitted"] === true,
        revisions: num(selfRecord["revisions"]) ?? 0,
        edges: num(selfRecord["edges"]) ?? 0,
        arrived: selfRecord["arrived"] === true,
        scoreUnits: num(selfRecord["scoreUnits"]) ?? 0,
        evidence: normalizePathEdges(selfRecord["evidence"]),
      }
    : null;

  // 服务端 `answerReveal` 的字段是 `path`（[]string），不是 `shortestPath`。
  // 读错名字会让 path 恒为空数组，揭晓区的「最少边数路线」整段不渲染——
  // 而所有其他检查都是绿的（类型对了、归一化也「跑」了，只是永远拿到空）。
  // 开卷局的完整边表。闭卷局服务端不下发，这里得到空数组——
  // 「没有边」和「不开放边」在界面上要区分，所以 openBook 单独读。
  const edges = config["openBook"] === true ? normalizePathEdges(publicState["edges"]) : [];

  const answerRecord = asRecord(publicState["answer"]);
  const answer = answerRecord
    ? { shortestPath: strArray(answerRecord["path"]), evidence: normalizePathEdges(answerRecord["evidence"]) }
    : null;

  return {
    phase: typeof publicState["phase"] === "string" ? publicState["phase"] : "",
    maxEdges: num(config["maxEdges"]) ?? 6,
    questionSeconds: num(config["questionSeconds"]),
    openBook: config["openBook"] === true,
    start: normalizeNode(publicState["start"]),
    goal: normalizeNode(publicState["goal"]),
    shortestEdges: num(publicState["shortestEdges"]),
    nodes,
    edges,
    self,
    answer,
  };
}

function normalizePathEdges(value:unknown): PathEdge[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap(entry=>{
    const r=asRecord(entry),from=str(r?.["from"]),to=str(r?.["to"]);
    return from && to && from!==to ? [{from,to,raceId:str(r?.["raceId"])??"",raceName:str(r?.["raceName"])??"",date:str(r?.["date"])??""}] : [];
  });
}

/** Shape validation deliberately does not consult hidden edges or shortest paths. */
export function pathDraftValid(path:unknown,round:PathRound):path is string[] {
  return Array.isArray(path) && path.length>=1 && path.length<=round.maxEdges+1 && path[0]===round.start?.characterId && new Set(path).size===path.length && path.every(id=>typeof id==="string" && round.nodes.some(node=>node.characterId===id));
}

/** 邻居接口响应规范化 */
export function normalizeNeighbors(body: unknown): PathNeighbor[] {
  const record = asRecord(body);
  if (!record) return [];
  const raw = record["neighbors"];
  if (!Array.isArray(raw)) return [];

  const out: PathNeighbor[] = [];
  for (const entry of raw) {
    const item = asRecord(entry);
    if (!item) continue;
    const character = asRecord(item["character"]);
    if (!character) continue;
    // 实测 character 里是 characterId + displayName（不是 name）
    const characterId = str(character["characterId"]) ?? str(character["cardId"]);
    if (!characterId) continue;

    const evidence: PathEvidence[] = [];
    const rawEvidence = item["evidence"];
    if (Array.isArray(rawEvidence)) {
      for (const ev of rawEvidence) {
        const evRecord = asRecord(ev);
        if (!evRecord) continue;
        evidence.push({
          evidenceId: str(evRecord["evidenceId"]) ?? "",
          sourceUrl: str(evRecord["sourceUrl"]) ?? "",
          label: str(evRecord["label"]) ?? "",
          raceId: str(evRecord["raceId"]) ?? "",
        });
      }
    }

    out.push({
      characterId,
      name: str(character["displayName"]) ?? str(character["name"]) ?? characterId,
      evidence,
    });
  }
  // 按名字排序，避免依赖服务端返回顺序
  out.sort((a, b) => a.name.localeCompare(b.name, "zh-Hans-CN"));
  return out;
}

/**
 * 尝试把某节点接到路径末尾。
 *
 * 环/重复拒绝。返回 null 表示不允许（已走过），
 * 调用方据此给出明确提示，而不是静默忽略。
 */
export function tryAppend(path: readonly string[], characterId: string): string[] | null {
  if (path.includes(characterId)) return null;
  return [...path, characterId];
}

/** 撤回最后一步（只在还没提交时可用） */
export function popLast(path: readonly string[]): string[] {
  return path.length === 0 ? [] : path.slice(0, -1);
}

/** 边数 = 节点数 - 1（起终点都在路径里） */
export function edgeCount(path: readonly string[]): number {
  return Math.max(0, path.length - 1);
}

/** Accept any valid path; reject cycles and repeated nodes. */
export function validatePath(path: readonly string[], round: PathRound): string[] {
  const problems: string[] = [];
  if (!round.start || !round.goal) return ["本局缺少起点或终点"];

  if (path.length === 0) {
    problems.push("还没有选择路线");
    return problems;
  }
  if (path[0] !== round.start.characterId) {
    problems.push("路线必须从起点开始");
  }
  if (path[path.length - 1] !== round.goal.characterId) {
    problems.push("路线还没到达终点");
  }
  const seen = new Set<string>();
  for (const id of path) {
    if (seen.has(id)) problems.push(`路线里重复经过了「${id}」`);
    seen.add(id);
  }
  if (edgeCount(path) > round.maxEdges) {
    problems.push(`路线用了 ${edgeCount(path)} 条边，超过上限 ${round.maxEdges}`);
  }
  return problems;
}

/**
 * 评分公式：max(20, 100 - 10 × (实际边数 - 最短边数))。
 * 仅用于结果页解释，最终分只来自服务端。
 */
export function explainScore(actualEdges: number, shortestEdges: number | null): number {
  if (shortestEdges === null) return 0;
  return Math.max(20, 100 - 10 * (actualEdges - shortestEdges));
}

/** 显示用分数：scoreUnits 是 100 units = 1 分 */
export function displayScore(scoreUnits: number): number {
  return Math.round(scoreUnits / 100);
}
