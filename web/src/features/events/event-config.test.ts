import { describe, expect, it } from "vitest";
import { newEventModeConfig } from "./event-config";
import { parseEvent } from "./use-event-data";
import type { Mode } from "@/lib/api/types";
import { formatScoreUnits } from "@/lib/format-score";

describe("event rules and progression",()=>{
  it("shows fractional programme scores without inventing a tie by rounding",()=>{
    expect(formatScoreUnits(3333)).toBe("33.33");
    expect(formatScoreUnits(3334)).toBe("33.34");
    expect(formatScoreUnits(10000)).toBe("100");
  });
  it("creates public classic windows and uses the actual selected mode binding",()=>{
    expect(newEventModeConfig({modeId:"classic"} as Mode)).toMatchObject({variant:"public",winPolicy:"window-attempts",durationSeconds:180});
    expect(newEventModeConfig({modeId:"sort",rulesVersion:"current-rules",informationStyle:"open"} as Mode)).toMatchObject({modeId:"sort",rulesVersion:"current-rules",informationStyle:"open"});
  });
  it("preserves the saved configuration and completed progress across reads",()=>{
    const modeConfig={modeId:"classic",variant:"public",maxWrongGuesses:4,winPolicy:"window-attempts",durationSeconds:240,shadowEnabled:false};
    const {segments}=parseEvent({eventId:"e",roomId:"r",name:"娱乐杯",status:"running",eventVersion:9,segments:["s"],segmentDetails:[{segmentId:"s",modeConfig,roundCount:3,startedRounds:2,completedRounds:1,currentRoundPhase:"collecting",roundId:"second",status:"running"}]});
    expect(segments).toHaveLength(1);
    expect(segments[0]).toMatchObject({modeConfig,roundCount:3,startedRounds:2,completedRounds:1,currentRoundPhase:"collecting",roundId:"second"});
  });
});
