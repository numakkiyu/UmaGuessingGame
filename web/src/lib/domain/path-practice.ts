import { PathNotice } from "@/i18n/path";
import type { LocalizedNames } from "@/i18n/entities";
import { practiceFinished, type PracticeBase } from "./aptitude-practice";
export const PATH_DURATION = 180000, PATH_MAX_EDGES = 6;
export type PathNode = { characterId: string; name: string; imagePath: string; localizedNames?: LocalizedNames };
export type PathEdge = { from: string; to: string; raceId: string; raceName: string; date: string };
export type PracticeGraph = { nodes: PathNode[]; edges: PathEdge[] };
export type PathGame = PracticeBase & { modeId: "co-race-path"; style: "open" | "closed"; start: string; goal: string; draft: string[]; submitted: string[] | null; submittedAt: number | null; submissions: number };
export const pathPair = (a: string, b: string) => a < b ? `${a}|${b}` : `${b}|${a}`;
export function pathIndex(graph: PracticeGraph) {
  const adjacency = new Map(graph.nodes.map(n => [n.characterId, new Set<string>()])), evidence = new Map<string, PathEdge[]>();
  for (const e of graph.edges) { adjacency.get(e.from)?.add(e.to); adjacency.get(e.to)?.add(e.from); const key = pathPair(e.from, e.to); evidence.set(key, [...(evidence.get(key) ?? []), e]); }
  return { adjacency, evidence };
}
export function routesFrom(graph: PracticeGraph, start: string): Map<string, string[]> {
  const { adjacency } = pathIndex(graph); if (!adjacency.has(start)) return new Map();
  const queue = [start], routes = new Map<string, string[]>([[start, [start]]]);
  for (let i = 0; i < queue.length; i++) {
    const id = queue[i]!;
    for (const next of adjacency.get(id) ?? []) if (!routes.has(next)) { routes.set(next, [...routes.get(id)!, next]); queue.push(next); }
  }
  return routes;
}
export function shortestRoute(graph: PracticeGraph, start: string, goal: string): string[] | null { return routesFrom(graph, start).get(goal) ?? null; }
export function pathShape(path: unknown, graph: PracticeGraph, start: string): path is string[] {
  return Array.isArray(path) && path.length >= 1 && path.length <= PATH_MAX_EDGES + 1 && path[0] === start && new Set(path).size === path.length && path.every(id => typeof id === "string" && graph.nodes.some(n => n.characterId === id));
}
export function checkRoute(graph: PracticeGraph, path: readonly string[], start: string) {
  if (!pathShape(path, graph, start)) { const notice = new PathNotice("shape"); return { message: notice.message, step: null, notice }; }
  const { evidence } = pathIndex(graph);
  for (let i = 0; i + 1 < path.length; i++) if (!evidence.has(pathPair(path[i]!, path[i + 1]!))) { const notice = new PathNotice("missing", { step: i + 1 }); return { message: notice.message, step: i + 1, notice }; }
  return null;
}
export function pathScore(actualEdges: number, shortestEdges: number) { return Math.max(20, 100 - 10 * (actualEdges - shortestEdges)) * 100; }
export function changePath(game: PathGame, graph: PracticeGraph, path: string[], submit: boolean, now: number): PathGame {
  if (practiceFinished(game, now)) throw new PathNotice("stopped");
  if (!pathShape(path, graph, game.start)) throw new PathNotice("shape");
  if (submit) { const issue = checkRoute(graph, path, game.start); if (issue) throw issue.notice; }
  return { ...game, draft: [...path], submitted: submit ? [...path] : game.submitted, submittedAt: submit ? now : game.submittedAt, submissions: game.submissions + (submit ? 1 : 0), revision: game.revision + 1, updatedAt: now };
}
export function pathResult(game: PathGame, graph: PracticeGraph) {
  const shortest = shortestRoute(graph, game.start, game.goal)!, route = game.submitted ?? [], arrived = route.at(-1) === game.goal && !checkRoute(graph, route, game.start);
  const { evidence } = pathIndex(graph);
  const steps = (path: string[]) => path.slice(0, -1).map((from, i) => ({ from, to: path[i + 1]!, evidence: evidence.get(pathPair(from, path[i + 1]!)) ?? [] }));
  return { arrived: Boolean(arrived), shortest, scoreUnits: arrived ? pathScore(route.length - 1, shortest.length - 1) : 0, submittedSteps: steps(route), referenceSteps: steps(shortest) };
}
