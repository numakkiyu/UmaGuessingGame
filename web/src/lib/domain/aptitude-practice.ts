import type { DraftAptitudes, DraftTask } from "@/lib/api/draft";
import { taskPointsFor } from "@/lib/api/draft";
import { AptitudeNotice } from "@/i18n/aptitude-notices";
import type { LocalizedNames } from "@/i18n/entities";

export type AptitudeCard = { id: string; name: string; localizedNames?: LocalizedNames; imagePath: string; aptitudes: DraftAptitudes };
export type PricedCard = AptitudeCard & { price: number };
export type PracticeBase = { schemaVersion: 1; modeId: string; gameId: string; packKey: string; rulesVersion: string; createdAt: number; updatedAt: number; deadlineAt: number; ended: boolean; revision: number };
export type GridClaim = { cellIndex: number; characterId: string; at: number };
export type GridGame = PracticeBase & { modeId: "aptitude-grid"; rowAxis: "distance" | "style"; cardIds: string[]; claims: GridClaim[] };
export type DraftGame = PracticeBase & { modeId: "draft"; marketIds: string[]; taskIds: string[]; draft: Record<string, string>; submitted: Record<string, string> | null; submittedAt: number | null };
export const GRID_DURATION = 180000, DRAFT_DURATION = 240000, DRAFT_BUDGET = 24;
export const DISTANCES = ["短", "英", "中", "长"], STYLES = ["逃", "先", "差", "追"], SURFACES = ["草地", "泥地"];
export function gridConditions(rowAxis: GridGame["rowAxis"], index: number) {
  const row = Math.floor(index / 3), col = index % 3;
  return { distance: DISTANCES[rowAxis === "distance" ? row : col]!, style: STYLES[rowAxis === "distance" ? col : row]! };
}
export function satisfiesGrid(card: AptitudeCard, distance: string, style: string): boolean { return card.aptitudes.distance[distance] === "A" && card.aptitudes.style[style] === "A"; }
export function gridSolution(cards: readonly AptitudeCard[], rowAxis: GridGame["rowAxis"]): string[] | null {
  const owners = new Map<string, number>(), result = Array<string>(9);
  function assign(index: number, seen: Set<string>): boolean {
    const { distance, style } = gridConditions(rowAxis, index);
    for (const card of cards) {
      if (seen.has(card.id) || !satisfiesGrid(card, distance, style)) continue;
      seen.add(card.id); const previous = owners.get(card.id);
      if (previous === undefined || assign(previous, seen)) { owners.set(card.id, index); result[index] = card.id; return true; }
    }
    return false;
  }
  for (let i = 0; i < 9; i++) if (!assign(i, new Set())) return null;
  return result;
}
export const practiceFinished = (g: PracticeBase, now: number) => g.ended || now >= g.deadlineAt;
export const gridFinished = (g: GridGame, now: number) => practiceFinished(g, now) || g.claims.length === 9;
export function claimGrid(g: GridGame, cards: readonly AptitudeCard[], index: number, id: string, now: number): GridGame {
  if (gridFinished(g, now)) throw new AptitudeNotice("stopped");
  if (!Number.isInteger(index) || index < 0 || index > 8) throw new AptitudeNotice("chooseCell");
  if (g.claims.some(c => c.cellIndex === index)) throw new AptitudeNotice("occupied");
  if (g.claims.some(c => c.characterId === id)) throw new AptitudeNotice("used");
  const card = cards.find(c => c.id === id); if (!card || !g.cardIds.includes(id)) throw new AptitudeNotice("candidate");
  const { distance, style } = gridConditions(g.rowAxis, index);
  if (!satisfiesGrid(card, distance, style)) throw new AptitudeNotice("mismatch");
  return { ...g, claims: [...g.claims, { cellIndex: index, characterId: id, at: now }], updatedAt: now, revision: g.revision + 1 };
}
export function publishedPracticePrice(a: DraftAptitudes): number {
  return 3 + Math.ceil(DISTANCES.filter(v => a.distance[v] === "A").length / 2) + Math.ceil(STYLES.filter(v => a.style[v] === "A").length / 2) + (a.surface["泥地"] === "A" ? 1 : 0);
}
export function draftFeasibleSets(cards: readonly PricedCard[], stopAt = Infinity): number {
  let count = 0;
  for (let a = 0; a < cards.length; a++) for (let b = a + 1; b < cards.length; b++) for (let c = b + 1; c < cards.length; c++) for (let d = c + 1; d < cards.length; d++) {
    if (cards[a]!.price + cards[b]!.price + cards[c]!.price + cards[d]!.price <= DRAFT_BUDGET && ++count >= stopAt) return count;
  }
  return count;
}
export function lineupScore(assignments: Readonly<Record<string, string>>, cards: readonly PricedCard[], tasks: readonly DraftTask[]) {
  if (tasks.length !== 4 || Object.keys(assignments).length !== 4 || Object.keys(assignments).some(id => !tasks.some(t => t.taskId === id))) throw new AptitudeNotice("fourTasks");
  const used = new Set<string>(); let spent = 0, totalPoints = 0;
  const scores = tasks.map(task => {
    const id = Object.hasOwn(assignments, task.taskId) ? assignments[task.taskId] : undefined;
    const card = cards.find(c => c.id === id); if (!card) throw new AptitudeNotice("incomplete");
    if (used.has(card.id)) throw new AptitudeNotice("duplicate");
    used.add(card.id); spent += card.price; const points = taskPointsFor(card, task); totalPoints += points.total;
    return { taskId: task.taskId, cardId: card.id, ...points };
  });
  if (spent > DRAFT_BUDGET) throw new AptitudeNotice("budget");
  return { spent, totalPoints, scores, scoreUnits: Math.floor(totalPoints * 100 / 36) * 100 };
}
export function changeLineup(g: DraftGame, tasks: readonly DraftTask[], taskId: string, cardId: string, now: number): DraftGame {
  if (practiceFinished(g, now) || g.submitted) throw new AptitudeNotice("ended");
  if (!tasks.some(t => t.taskId === taskId) || cardId && !g.marketIds.includes(cardId)) throw new AptitudeNotice("market");
  const draft = { ...g.draft }; if (cardId) draft[taskId] = cardId; else delete draft[taskId];
  return { ...g, draft, updatedAt: now, revision: g.revision + 1 };
}
export function submitLineup(g: DraftGame, cards: readonly PricedCard[], tasks: readonly DraftTask[], now: number): DraftGame {
  if (practiceFinished(g, now) || g.submitted) throw new AptitudeNotice("ended");
  lineupScore(g.draft, cards, tasks);
  return { ...g, submitted: { ...g.draft }, submittedAt: now, ended: true, updatedAt: now, revision: g.revision + 1 };
}
// At reveal only: enumerate the small 12-card market, without assuming a greedy
// per-task best choice is globally optimal under uniqueness and budget limits.
export type BestLineup = { assignments: Record<string, string>; totalPoints: number; spent: number; scoreUnits: number };
export function bestLineup(cards: readonly PricedCard[], tasks: readonly DraftTask[]): BestLineup | null {
  let best: BestLineup | null = null;
  const used = new Set<string>(), assignments: Record<string, string> = {};
  function visit(index: number, spent: number, points: number) {
    if (index === tasks.length) { if (!best || points > best.totalPoints || points === best.totalPoints && spent < best.spent) best = { assignments: { ...assignments }, totalPoints: points, spent, scoreUnits: Math.floor(points * 100 / 36) * 100 }; return; }
    const task = tasks[index]!;
    for (const card of cards) {
      if (used.has(card.id) || spent + card.price > DRAFT_BUDGET) continue;
      used.add(card.id); assignments[task.taskId] = card.id; visit(index + 1, spent + card.price, points + taskPointsFor(card, task).total); used.delete(card.id); delete assignments[task.taskId];
    }
  }
  visit(0, 0, 0); return best;
}
