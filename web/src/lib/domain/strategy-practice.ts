import { StrategyNotice } from "@/i18n/strategy";
import { PlayerNotice } from "@/lib/player-notice";
import type { PracticeBase } from "./aptitude-practice";
export const STRATEGY_ACTIONS = ["steady", "sprint", "recover", "follow"] as const;
export type StrategyAction = typeof STRATEGY_ACTIONS[number];
export type StrategyRule = { action: StrategyAction; label: string; positionGain: number; staminaDelta: number; minStamina: number; alternateGain: number; leaderGap: boolean };
export type StrategyTrack = { totalTurns: number; decisionSeconds: number; startPosition: number; startStamina: number; staminaCap: number; followMinGap: number; followMaxGap: number; defaultAction: "steady"; actions: StrategyRule[]; terrainChanges: false; randomEvents: false };
export type StrategyPoint = { position: number; stamina: number };
export type StrategyStep = { action: StrategyAction; at: number; automatic: boolean };
export type StrategyGame = PracticeBase & { modeId: "strategy-race"; targetPosition: number; targetStamina: number; actions: StrategyStep[]; selected: StrategyAction | null };
export function soloStrategyStep(track: StrategyTrack, point: StrategyPoint, action: StrategyAction): StrategyPoint {
  const rule = track.actions.find(r => r.action === action);
  if (!rule) throw new StrategyNotice("choose");
  if (action === "sprint" && point.stamina < rule.minStamina) throw new StrategyNotice("stamina", { minimum: rule.minStamina });
  const gain = action === "follow" ? rule.alternateGain : rule.positionGain;
  const delta = action === "recover" ? Math.max(0, Math.min(track.staminaCap - point.stamina, rule.staminaDelta)) : rule.staminaDelta;
  return { position: point.position + gain, stamina: Math.max(0, Math.min(track.staminaCap, point.stamina + delta)) };
}
export function replayStrategy(track: StrategyTrack, actions: readonly StrategyStep[]) {
  let point = { position: track.startPosition, stamina: track.startStamina };
  return actions.map((step, i) => { const before = point; point = soloStrategyStep(track, point, step.action); return { ...step, turn: i + 1, before, ...point }; });
}
export function strategyPoint(track: StrategyTrack, actions: readonly StrategyStep[]) { return replayStrategy(track, actions).at(-1) ?? { position: track.startPosition, stamina: track.startStamina }; }
export const strategyTurnDeadline = (g: StrategyGame, track: StrategyTrack) => (g.actions.at(-1)?.at ?? g.createdAt) + track.decisionSeconds * 1000;
export const strategyFinished = (g: StrategyGame, track: StrategyTrack) => g.ended || g.actions.length >= track.totalTurns;
export function syncStrategyTimeouts(game: StrategyGame, track: StrategyTrack, now: number): StrategyGame {
  if (strategyFinished(game, track)) return game;
  const actions = [...game.actions]; let deadline = strategyTurnDeadline(game, track);
  while (actions.length < track.totalTurns && now >= deadline) { actions.push({ action: track.defaultAction, at: deadline, automatic: true }); deadline += track.decisionSeconds * 1000; }
  return actions.length === game.actions.length ? game : { ...game, actions, selected: null, updatedAt: now, revision: game.revision + 1 };
}
export function selectStrategy(game: StrategyGame, track: StrategyTrack, action: StrategyAction, now: number): StrategyGame {
  const synced = syncStrategyTimeouts(game, track, now); if (synced !== game) return synced;
  if (strategyFinished(game, track)) throw new StrategyNotice("stopped");
  soloStrategyStep(track, strategyPoint(track, game.actions), action);
  return { ...game, selected: action, updatedAt: now, revision: game.revision + 1 };
}
export function confirmStrategy(game: StrategyGame, track: StrategyTrack, now: number): StrategyGame {
  const synced = syncStrategyTimeouts(game, track, now); if (synced !== game) return synced;
  if (strategyFinished(game, track)) throw new StrategyNotice("stopped");
  if (!game.selected) throw new StrategyNotice("select");
  soloStrategyStep(track, strategyPoint(track, game.actions), game.selected);
  return { ...game, actions: [...game.actions, { action: game.selected, at: now, automatic: false }], selected: null, updatedAt: now, revision: game.revision + 1 };
}
export function strategyReachable(track: StrategyTrack) {
  let states = new Map<string, StrategyPoint & { actions: StrategyAction[] }>([[`${track.startPosition}:${track.startStamina}`, { position: track.startPosition, stamina: track.startStamina, actions: [] }]]);
  for (let turn = 0; turn < track.totalTurns; turn++) {
    const next = new Map<string, StrategyPoint & { actions: StrategyAction[] }>();
    for (const point of states.values()) for (const action of STRATEGY_ACTIONS) {
      try { const result = soloStrategyStep(track, point, action), key = `${result.position}:${result.stamina}`; if (!next.has(key)) next.set(key, { ...result, actions: [...point.actions, action] }); }
      catch (e) { if (!(e instanceof PlayerNotice)) throw e; }
    }
    states = next;
  }
  return states;
}
export function strategyTargets(track: StrategyTrack) {
  const all = [...strategyReachable(track).values()], steady = track.actions.find(a => a.action === "steady")!;
  const targets = all.filter(p => p.position >= track.startPosition + steady.positionGain * track.totalTurns - 4);
  return targets.length ? targets : all;
}
export function strategyResult(game: StrategyGame, track: StrategyTrack) {
  const point = strategyPoint(track, game.actions), complete = game.actions.length === track.totalTurns;
  return { ...point, complete, achieved: complete && point.position === game.targetPosition && point.stamina === game.targetStamina, example: strategyReachable(track).get(`${game.targetPosition}:${game.targetStamina}`)?.actions ?? [] };
}
