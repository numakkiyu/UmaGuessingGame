import type { components } from "@/lib/api/schema";
import { PATH_DURATION, checkRoute, pathShape, routesFrom, shortestRoute, type PathEdge, type PathGame, type PathNode, type PracticeGraph } from "@/lib/domain/path-practice";
import { PathNotice } from "@/i18n/path";
import { validLocalizedNames } from "@/i18n/entities";
import { activityStore } from "./activity-store";
import { incompletePractice as invalid, practiceId, practiceObject as object, readPracticeBase, readPracticeEnvelope } from "./practice-envelope";
import { newPracticeId } from "./practice-store";
import { shufflePractice } from "./random-draw";
export type PathPack = components["schemas"]["PathPracticePack"];
export type PathArchive = { schemaVersion: 1; pack: PathPack; game: PathGame };
export const pathPackKey = (p: PathPack) => `${p.modeId}:${p.contentPackVersion}:${p.sha256}`;
const text = (v: unknown): v is string => typeof v === "string" && v.length > 0 && v.length <= 160;
const date = (v: unknown): v is string => typeof v === "string" && /^\d{4}-\d{2}-\d{2}$/.test(v) && Number.isFinite(Date.parse(v)) && new Date(v).toISOString().slice(0, 10) === v;
export function readPathPack(raw: unknown): { pack: PathPack; graph: PracticeGraph } {
  const { pack, content } = readPracticeEnvelope(raw, "co-race-path");
  if (!Array.isArray(content.nodes) || content.nodes.length < 2 || content.nodes.length > 300 || !Array.isArray(content.edges) || !content.edges.length || content.edges.length > 5000) throw invalid();
  const nodes: PathNode[] = content.nodes.map(n => { if (!object(n) || !practiceId(n.characterId) || !text(n.name) || !validLocalizedNames(n.localizedNames)) throw invalid(); return { characterId: n.characterId, name: n.name, imagePath: `/assets/avatars/${n.characterId}.webp`, ...(n.localizedNames ? { localizedNames: n.localizedNames } : {}) }; });
  const ids = new Set(nodes.map(n => n.characterId)); if (ids.size !== nodes.length) throw invalid();
  const seen = new Set<string>();
  const edges: PathEdge[] = content.edges.map(e => {
    if (!object(e) || !practiceId(e.from) || !practiceId(e.to) || !ids.has(e.from) || !ids.has(e.to) || e.from === e.to || !practiceId(e.raceId) || !text(e.raceName) || !date(e.date)) throw invalid();
    const [from, to] = [e.from, e.to].sort(), key = `${from}|${to}|${e.raceId}`;
    if (seen.has(key)) throw invalid(); seen.add(key);
    return { from: from!, to: to!, raceId: e.raceId, raceName: e.raceName, date: e.date };
  });
  return { pack, graph: { nodes, edges } };
}
export function createPathGame(pack: PathPack, style: PathGame["style"] = "open", now = Date.now()): PathGame {
  const { graph } = readPathPack(pack), nodes = shufflePractice(graph.nodes, now);
  // Prefer a multi-step puzzle. Bounded traversal; disconnected nodes are never
  // paired and a small graph with only adjacent pairs can still offer a round.
  let chosen: [string, string] | null = null, fallback: [string, string] | null = null;
  outer: for (const start of nodes) {
    const routes = routesFrom(graph, start.characterId);
    for (const goal of nodes) {
      if (start.characterId === goal.characterId) continue;
      const route = routes.get(goal.characterId); if (!route || route.length > 7) continue;
      fallback ??= [start.characterId, goal.characterId];
      if (route.length >= 3) { chosen = [start.characterId, goal.characterId]; break outer; }
    }
  }
  chosen ??= fallback; if (!chosen) throw new PathNotice("unavailable");
  const [start, goal] = chosen;
  return { schemaVersion: 1, modeId: "co-race-path", gameId: newPracticeId(), packKey: pathPackKey(pack), rulesVersion: pack.rulesVersion, style, start, goal, draft: [start], submitted: null, submittedAt: null, submissions: 0, createdAt: now, updatedAt: now, deadlineAt: now + PATH_DURATION, ended: false, revision: 0 };
}
export function validatePathArchive(raw: unknown): PathArchive {
  if (!object(raw) || raw.schemaVersion !== 1 || !object(raw.game)) throw invalid();
  const { pack, graph } = readPathPack(raw.pack), g = raw.game, base = readPracticeBase(g, pack, PATH_DURATION);
  if (!practiceId(g.start) || !practiceId(g.goal) || g.start === g.goal || g.style !== "open" && g.style !== "closed") throw invalid();
  const shortest = shortestRoute(graph, g.start, g.goal);
  if (!shortest || shortest.length > 7 || !pathShape(g.draft, graph, g.start) || g.submitted !== null && (!pathShape(g.submitted, graph, g.start) || checkRoute(graph, g.submitted, g.start))) throw invalid();
  if (!Number.isSafeInteger(g.submissions) || Number(g.submissions) < 0 || Number(g.submissions) > base.revision) throw invalid();
  if (g.submitted) { if (!Number.isSafeInteger(g.submittedAt) || Number(g.submittedAt) < base.createdAt || Number(g.submittedAt) > base.updatedAt || Number(g.submittedAt) >= base.deadlineAt || Number(g.submissions) < 1) throw invalid(); }
  else if (g.submittedAt !== null || g.submissions !== 0) throw invalid();
  return { schemaVersion: 1, pack, game: { ...base, modeId: "co-race-path", style: g.style, start: g.start, goal: g.goal, draft: [...g.draft], submitted: g.submitted ? [...g.submitted] : null, submittedAt: g.submittedAt as number | null, submissions: Number(g.submissions) } };
}
export const pathStore = activityStore<PathPack, PathGame>("umaguess-path-practice", validatePathArchive, pathPackKey);
export const volatilePathGames = new Map<string, PathArchive & { savedRevision?: number | null }>();
