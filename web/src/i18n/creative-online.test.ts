import {readFileSync} from "node:fs";
import {expect,it} from "vitest";
import {LANGUAGES} from ".";
import {creativeOnlineCopy} from "./creative-online";
import {creativePrompts,localizeCreativePrompt} from "./creative-prompts";
import {normalizeCreativeRound} from "@/lib/api/modes-g09-g12";
import {loadCreativeDraft,saveCreativeDraft} from "@/lib/local/creative-online-draft";
import {validCreativeCommand,validCreativeReview} from "@/lib/api/game-commands";
function leaves(v:unknown,path=""):Record<string,string>{if(typeof v==="string")return {[path]:v};return Object.assign({},...Object.entries(v as object).map(([k,v])=>leaves(v,`${path}.${k}`)));}
it("三语键与变量完整对应，24场景12要求与真实源文精确一致",()=>{
 const source=leaves(creativeOnlineCopy["zh-Hans"]);
 for(const l of LANGUAGES){const target=leaves(creativeOnlineCopy[l]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[k,v]of Object.entries(source)){const vars=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(vars(target[k]!)).toEqual(vars(v));}}
 const bank=JSON.parse(readFileSync("../data/manual/creative-prompts.json","utf8"));
 expect(creativePrompts).toHaveLength(bank.scenes.length+bank.constraints.length);
 for(const item of [...bank.scenes,...bank.constraints]){const known=creativePrompts.find(p=>p.id===(item.sceneId??item.constraintId));expect(known?.source).toBe(item.text);expect(known?.ja).toBeTruthy();expect(known?.["zh-Hant"]).toBeTruthy();}
 const prompt={characterId:"1001",characterName:"特别周",sceneId:"sc-01",scene:bank.scenes[0].text,constraintId:"ct-11",constraint:bank.constraints[10].text};
 for(const l of LANGUAGES){const p=localizeCreativePrompt(prompt,l);expect(p.original).toBe(false);expect(p.characterId).toBe(prompt.characterId);expect(p.constraint).toContain("后来");}
 expect(localizeCreativePrompt({...prompt,scene:"同号新题"},"ja")).toMatchObject({scene:"同号新题",original:true});
});
it("草稿隔离、空稿、畸形和过期保护；确认只收规范化作品和个人选票",()=>{
 localStorage.clear();const draft={text:"",selected:null,excluded:[]};expect(saveCreativeDraft("person:round:work:team:2",draft)).toBe(true);expect(loadCreativeDraft("person:round:work:team:2")).toEqual(draft);expect(loadCreativeDraft("other:round:work:team:2")).toBeNull();
 for(const bad of ["{",JSON.stringify({...draft,savedAt:Date.now()-86400001}),JSON.stringify({...draft,savedAt:Date.now()+120000}),JSON.stringify({...draft,savedAt:Date.now(),excluded:["one","one"]}),"x".repeat(16001)]){localStorage.setItem("umaguess:creative-draft:bad",bad);expect(loadCreativeDraft("bad")).toBeNull();}
 const body={phaseVersion:3,submissionRevision:0,controlGeneration:0,modeAction:{kind:"creative.work",text:"é".repeat(80)}};expect(validCreativeCommand(body)).toBe(true);for(const text of ["e\u0301"," ","a".repeat(81)])expect(validCreativeCommand({...body,modeAction:{kind:"creative.work",text}})).toBe(false);
 expect(validCreativeCommand({...body,modeAction:{kind:"creative.vote",optionId:"w2"}})).toBe(true);expect(validCreativeCommand({...body,controlGeneration:1,modeAction:{kind:"creative.vote",optionId:"w2"}})).toBe(false);
 expect(validCreativeReview({target:"creative-voting",expectedPhaseVersion:4,excludedOptionIds:[]})).toBe(true);expect(validCreativeReview({target:"creative-voting",expectedPhaseVersion:4,excludedOptionIds:["w1","w1"]})).toBe(false);
});
it("正式分只读已公布关闭后的results，100单位一分，取消不造成绩",()=>{
 const raw={round:{phase:"settled"},publicState:{subphase:"closed",authors:{w1:"one"},result:{works:[],teamVotes:0,audienceVotes:0,validWorks:0},final:[{scoreUnits:999999}]},results:[{subjectId:"one",scoreUnits:2000},{subjectId:"bad",scoreUnits:999999},{subjectId:"negative",scoreUnits:-1}]};
 expect(normalizeCreativeRound(raw).scores).toEqual([{subjectId:"one",points:20,rank:null}]);for(const phase of ["collecting","locked","cancelled"]){const r=normalizeCreativeRound({...raw,round:{phase}});expect(r.scores).toEqual([]);expect(r.authors).toEqual({});expect(r.result).toBeUndefined();}
});
