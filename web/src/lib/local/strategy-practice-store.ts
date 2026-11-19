import type { components } from "@/lib/api/schema";
import { STRATEGY_ACTIONS, replayStrategy, strategyTargets, type StrategyAction, type StrategyGame, type StrategyRule, type StrategyStep, type StrategyTrack } from "@/lib/domain/strategy-practice";
import { activityStore } from "./activity-store";
import { incompletePractice as invalid, practiceObject as object, readPracticeBase, readPracticeEnvelope } from "./practice-envelope";
import { newPracticeId } from "./practice-store";
import { shufflePractice } from "./random-draw";
export type StrategyPack = components["schemas"]["StrategyPracticePack"];
export type StrategyArchive = { schemaVersion: 1; pack: StrategyPack; game: StrategyGame };
export const strategyPackKey = (p: StrategyPack) => `${p.modeId}:${p.contentPackVersion}:${p.sha256}`;
const integer = (v: unknown, min: number, max: number): v is number => Number.isSafeInteger(v) && Number(v) >= min && Number(v) <= max;
const action = (v: unknown): v is StrategyAction => STRATEGY_ACTIONS.includes(v as StrategyAction);
export function readStrategyPack(raw: unknown) {
  const { pack, content } = readPracticeEnvelope(raw, "strategy-race"), t = content.track;
  if (content.simulation !== true || content.notOfficialData !== true || !object(t) || t.totalTurns !== 8 || t.decisionSeconds !== 20 || !integer(t.startPosition, 0, 100) || !integer(t.staminaCap, 1, 20) || !integer(t.startStamina, 0, t.staminaCap) || !integer(t.followMinGap, 1, 20) || !integer(t.followMaxGap, t.followMinGap, 20) || t.defaultAction !== "steady" || t.terrainChanges !== false || t.randomEvents !== false || !Array.isArray(t.actions) || t.actions.length !== 4) throw invalid();
  const actions: StrategyRule[] = t.actions.map(a => {
    if (!object(a) || !action(a.action) || typeof a.label !== "string" || !a.label || a.label.length > 20 || !integer(a.positionGain, 0, 20) || !integer(a.staminaDelta, -20, 20) || !integer(a.minStamina ?? 0, 0, t.staminaCap as number) || !integer(a.alternateGain ?? 0, 0, 20)) throw invalid();
    if (a.action === "sprint" && Number(a.minStamina ?? 0) <= 0 || a.action === "follow" && (Number(a.alternateGain ?? 0) <= 0 || a.leaderGap !== true) || a.action !== "follow" && a.leaderGap === true) throw invalid();
    return { action: a.action, label: a.label, positionGain: a.positionGain, staminaDelta: a.staminaDelta, minStamina: Number(a.minStamina ?? 0), alternateGain: Number(a.alternateGain ?? 0), leaderGap: a.leaderGap === true };
  });
  if (new Set(actions.map(a => a.action)).size !== 4) throw invalid();
  const track: StrategyTrack = { totalTurns: 8, decisionSeconds: 20, startPosition: t.startPosition, startStamina: t.startStamina, staminaCap: t.staminaCap, followMinGap: t.followMinGap, followMaxGap: t.followMaxGap, defaultAction: "steady", actions, terrainChanges: false, randomEvents: false };
  return { pack, track };
}
export function createStrategyGame(pack: StrategyPack, now = Date.now()): StrategyGame {
  const { track } = readStrategyPack(pack), target = shufflePractice(strategyTargets(track), now)[0]!;
  return { schemaVersion: 1, modeId: "strategy-race", gameId: newPracticeId(), packKey: strategyPackKey(pack), rulesVersion: pack.rulesVersion, targetPosition: target.position, targetStamina: target.stamina, actions: [], selected: null, createdAt: now, updatedAt: now, deadlineAt: now + track.totalTurns * track.decisionSeconds * 1000, ended: false, revision: 0 };
}
export function validateStrategyArchive(raw: unknown): StrategyArchive {
  if (!object(raw) || raw.schemaVersion !== 1 || !object(raw.game)) throw invalid();
  const { pack, track } = readStrategyPack(raw.pack), g = raw.game, base = readPracticeBase(g, pack, track.totalTurns * track.decisionSeconds * 1000);
  if (!integer(g.targetPosition, 0, 1000) || !integer(g.targetStamina, 0, track.staminaCap) || !strategyTargets(track).some(t => t.position === g.targetPosition && t.stamina === g.targetStamina) || !Array.isArray(g.actions) || g.actions.length > track.totalTurns || g.selected !== null && !action(g.selected)) throw invalid();
  let start = base.createdAt;
  const actions: StrategyStep[] = g.actions.map(a => {
    if (!object(a) || !action(a.action) || typeof a.automatic !== "boolean" || !integer(a.at, start, base.updatedAt)) throw invalid();
    const deadline = start + track.decisionSeconds * 1000;
    if (a.automatic ? a.at !== deadline || a.action !== track.defaultAction : a.at >= deadline) throw invalid();
    start = a.at;
    return { action: a.action, automatic: a.automatic, at: a.at };
  });
  try { replayStrategy(track, actions); } catch { throw invalid(); }
  if (actions.length && base.revision < 1 || g.selected !== null && (base.revision < 1 || actions.length === track.totalTurns)) throw invalid();
  if (g.selected !== null) { try { replayStrategy(track, [...actions, { action: g.selected as StrategyAction, automatic: false, at: base.updatedAt }]); } catch { throw invalid(); } }
  return { schemaVersion: 1, pack, game: { ...base, modeId: "strategy-race", targetPosition: g.targetPosition, targetStamina: g.targetStamina, actions, selected: g.selected as StrategyAction | null } };
}
export const strategyStore = activityStore<StrategyPack, StrategyGame>("umaguess-strategy-practice", validateStrategyArchive, strategyPackKey);
export const volatileStrategyGames = new Map<string, StrategyArchive & { savedRevision?: number | null }>();
