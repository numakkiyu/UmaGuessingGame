import { describe, expect, it } from "vitest";
import baseline from "../../../../data/manual/rules-track.json";
import fixtures from "@/lib/domain/testdata/strategy-actions.json";
import { confirmStrategy, replayStrategy, selectStrategy, soloStrategyStep, strategyFinished, strategyPoint, strategyReachable, strategyResult, strategyTargets, strategyTurnDeadline, syncStrategyTimeouts, type StrategyAction } from "@/lib/domain/strategy-practice";
import { createStrategyGame, readStrategyPack, validateStrategyArchive, type StrategyPack } from "./strategy-practice-store";
import { sha256 } from "./checksum";
function pack(content: unknown = { track: baseline.track, simulation: true, notOfficialData: true }): StrategyPack { const contentJSON = JSON.stringify(content); return { schemaVersion: 1, modeId: "strategy-race", rulesVersion: "2.0.0-draft.1", datasetVersion: "ds", contentPackVersion: "pack", sourceUrls: ["https://wiki.biligame.com/umamusume/"], reviewStatus: "source-snapshot", contentJSON, sha256: sha256(contentJSON), byteLength: new TextEncoder().encode(contentJSON).length }; }
const p = pack(), track = readStrategyPack(p).track;
describe("shared Go/browser single-team strategy actions", () => {
  for (const f of fixtures) it(f.name, () => {
    const apply = () => soloStrategyStep(track, { position: f.position, stamina: f.stamina }, f.action as StrategyAction);
    if (f.rejected) expect(apply).toThrow(); else expect(apply()).toEqual({ position: f.wantPosition, stamina: f.wantStamina });
  });
});
describe("strategy target progression and recovery", () => {
  it("selecting does not move, confirming commits once and starts the next turn clock", () => {
    let g = createStrategyGame(p, 1000); g = selectStrategy(g, track, "sprint", 2000);
    expect(strategyPoint(track, g.actions)).toEqual({ position: 0, stamina: 6 }); expect(g.actions).toHaveLength(0);
    g = confirmStrategy(g, track, 3000); expect(g.actions).toEqual([{ action: "sprint", at: 3000, automatic: false }]); expect(g.selected).toBeNull(); expect(strategyTurnDeadline(g, track)).toBe(23000);
    expect(() => confirmStrategy(g, track, 3001)).toThrow(); expect(validateStrategyArchive({ schemaVersion: 1, pack: p, game: g }).game).toEqual(g);
  });
  it("cutoff uses steady even for a selected sprint and catches up old absolute deadlines once", () => {
    let g = selectStrategy(createStrategyGame(p, 1000), track, "sprint", 2000);
    g = syncStrategyTimeouts(g, track, 61000); expect(g.actions.map(a => a.action)).toEqual(["steady", "steady", "steady"]); expect(g.actions.map(a => a.at)).toEqual([21000,41000,61000]); expect(g.selected).toBeNull(); expect(strategyPoint(track,g.actions).position).toBe(9); expect(strategyTurnDeadline(g,track)).toBe(81000);
    expect(syncStrategyTimeouts(g, track, 61000)).toBe(g); expect(validateStrategyArchive({ schemaVersion: 1, pack: p, game: g }).game).toEqual(g);
    const ended = syncStrategyTimeouts(g, track, 200000); expect(ended.actions).toHaveLength(8); expect(strategyFinished(ended,track)).toBe(true); expect(syncStrategyTimeouts(ended,track,300000)).toBe(ended);
  });
  it("late confirmation cannot submit the previous choice into a new turn", () => {
    const g = selectStrategy(createStrategyGame(p,1000),track,"sprint",2000), late = confirmStrategy(g,track,21000);
    expect(late.actions[0]).toEqual({ action:"steady",at:21000,automatic:true }); expect(late.selected).toBeNull(); expect(late.actions).toHaveLength(1);
  });
  it("every selectable target has an eight-turn witness, many target states vary the task", () => {
    const targets = strategyTargets(track); expect(targets.length).toBeGreaterThan(10); expect(new Set(targets.map(t=>t.stamina)).size).toBeGreaterThan(2);
    for (const target of targets) { const steps = target.actions.map((action,i)=>({action,at:1000+i,automatic:false})); const result = strategyPoint(track,steps); expect(result).toMatchObject({position:target.position,stamina:target.stamina}); expect(steps).toHaveLength(8); }
    const g = createStrategyGame(p,1000); expect(strategyReachable(track).has(`${g.targetPosition}:${g.targetStamina}`)).toBe(true);
  });
  it("only complete exact position AND stamina reaches a target and no competitive score exists", () => {
    const target = strategyTargets(track)[0]!, g = { ...createStrategyGame(p,1000),targetPosition:target.position,targetStamina:target.stamina,actions:target.actions.map((action,i)=>({action,at:1001+i,automatic:false})) };
    const result = strategyResult(g,track); expect(result.achieved).toBe(true); expect(result).not.toHaveProperty("scoreUnits"); expect(result).not.toHaveProperty("rank");
    expect(strategyResult({...g,actions:g.actions.slice(0,7),ended:true},track).achieved).toBe(false); expect(strategyResult({...g,targetStamina:target.stamina===0?1:0},track).achieved).toBe(false);
  });
  it("insufficient stamina rejects without a new action and recovery caps at published ceiling", () => {
    let g = createStrategyGame(p,1000);
    for(let i=0;i<3;i++)g=confirmStrategy(selectStrategy(g,track,"sprint",1001+i),track,1001+i);
    expect(strategyPoint(track,g.actions).stamina).toBe(0); expect(()=>selectStrategy(g,track,"sprint",1010)).toThrow(); expect(g.actions).toHaveLength(3);
    const steps = Array.from({length:8},(_,i)=>({action:"recover" as const,at:1001+i,automatic:false})); expect(replayStrategy(track,steps).every(s=>s.stamina<=8)).toBe(true);
  });
  it("rejects fabricated timestamps, defaults, unavailable actions, targets and log shape", () => {
    const selected = selectStrategy(createStrategyGame(p,1000),track,"steady",1001), g = confirmStrategy(selected,track,1002);
    for(const patch of [{targetPosition:999},{targetStamina:99},{deadlineAt:g.deadlineAt+1000},{actions:[{action:"steady",at:21000,automatic:false}]},{actions:[{action:"sprint",at:1002,automatic:true}]},{actions:[{action:"steady",at:1001,automatic:true}]},{actions:[{action:"steady",at:999,automatic:false}]},{actions:Array(9).fill(g.actions[0])},{selected:"cheat"},{revision:0}]) expect(()=>validateStrategyArchive({schemaVersion:1,pack:p,game:{...g,...patch}})).toThrow();
    const imported=validateStrategyArchive({schemaVersion:1,pack:p,game:{...g,position:999,stamina:999,scoreUnits:10000,rank:1}}).game; expect(imported).not.toHaveProperty("position"); expect(imported).not.toHaveProperty("rank");
    expect(()=>readStrategyPack(pack({track:baseline.track,simulation:false,notOfficialData:true}))).toThrow(); expect(()=>readStrategyPack(pack({track:{...baseline.track,actions:[baseline.track.actions[0],baseline.track.actions[0],...baseline.track.actions.slice(2)]},simulation:true,notOfficialData:true}))).toThrow();
  });
});
