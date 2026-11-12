import { describe, expect, it } from "vitest";
import { validEventHostCommand, wholeNumber, scoreUnits } from "./event-host-command";
import { manualRoundLock, validRoundControl } from "./round-control-command";
import { eventHostCopy } from "@/i18n/event-host";
import { eventControlsCopy } from "@/i18n/event-controls";
import { eventResultsCopy } from "@/i18n/event-results";
import { LANGUAGES } from "@/i18n";
const config={modeId:"rapport",rulesVersion:"v2",contentPackId:"immutable-pack",informationStyle:"open",config:{questionSeconds:17,questions:[{label:"自定义规则"}]}};
describe("event commands preserve saved rules and validate boundaries",()=>{
 it("replays full custom mode configuration and segment binding, rejecting unknown command fields",()=>{
  const cmd={kind:"round",roomId:"room",body:{segmentId:"saved-segment",modeConfig:config}};
  expect(validEventHostCommand(cmd)).toBe(true);
  expect(validEventHostCommand({...cmd,csrfToken:"private"})).toBe(false);
  expect(validEventHostCommand({...cmd,body:{...cmd.body,secret:"hidden"}})).toBe(false);
  expect(cmd.body.modeConfig).toEqual(config);
  expect(validEventHostCommand({kind:"round",roomId:"r",body:{segmentId:"s",modeConfig:{modeId:"classic",maxWrongGuesses:8,winPolicy:"solo",durationSeconds:0,shadowEnabled:false}}})).toBe(true);
  expect(validEventHostCommand({kind:"grant",body:{}})).toBe(false);
 });
 it("validates whole scores and Unicode labels without silently rounding, clamping or truncating",()=>{
  for(const value of["","1.5","101","-1","1e2"])expect(wholeNumber(value,0,100)).toBeNull();
  expect(wholeNumber("0",0,100)).toBe(0);expect(wholeNumber("100",0,100)).toBe(100);
  for(const value of["","33.334","100.01","-0.01","1e1"])expect(scoreUnits(value,100)).toBeNull();
  expect(scoreUnits("33.33",100)).toBe(3333);expect(scoreUnits("0.29",100)).toBe(29);
  const body={modeConfig:config,label:"🏇".repeat(48),scoreBudget:100,roundCount:20,competitive:true};
  expect(validEventHostCommand({kind:"segment",body})).toBe(true);
  expect(validEventHostCommand({kind:"segment",body:{...body,label:"🏇".repeat(49)}})).toBe(false);
  expect(validEventHostCommand({kind:"segment",body:{...body,roundCount:1.5}})).toBe(false);
 });
 it("validates correction reasons and phase versions and blocks window skipping",()=>{
  const body={segmentId:"s",subjectId:"p",reason:"🏇".repeat(5),newScoreUnits:3333,expectedEventVersion:7};
  expect(validEventHostCommand({kind:"correction",body})).toBe(true);
  expect(validEventHostCommand({kind:"correction",body:{...body,reason:"🏇".repeat(241)}})).toBe(false);
  expect(validRoundControl({target:"cancel",expectedPhaseVersion:1})).toBe(false);
  expect(validRoundControl({target:"cancel",expectedPhaseVersion:1,reason:"🏇".repeat(240)})).toBe(true);
  expect(validRoundControl({target:"collecting",expectedPhaseVersion:1,extensionSeconds:31})).toBe(false);
  for(const id of["trade","rapport","strategy-race","creative","bluff"])expect(manualRoundLock(id)).toBe(false);
  expect(manualRoundLock("sort")).toBe(true);
 });
 it("all three locale catalogs preserve semantic keys and interpolation variables",()=>{
  for(const catalog of[eventHostCopy,eventResultsCopy,eventControlsCopy]){
   const source=catalog["zh-Hans"];
   for(const lang of LANGUAGES){
    const translated=catalog[lang] as Record<string,string>;
    expect(Object.keys(translated).sort()).toEqual(Object.keys(source).sort());
    for(const [key,value] of Object.entries(source)){
     const variables=(v:string)=>[...v.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();
     expect(translated[key]?.trim()).toBeTruthy();expect(variables(translated[key]!)).toEqual(variables(value));
    }
   }
  }
 });
});
