import {readFileSync} from "node:fs";
import {expect,it} from "vitest";
import {LANGUAGES} from ".";
import {rapportOnlineCopy} from "./rapport-online";
import {localizeRapportQuestion,rapportQuestions} from "./rapport-questions";
import {loadRapportDraft,saveRapportDraft} from "@/lib/local/rapport-online-draft";
import {normalizeRapportRound,type RapportQuestion} from "@/lib/api/modes-g09-g12";
function leaves(v:unknown,path=""):Record<string,string>{if(typeof v==="string")return {[path]:v};return Object.assign({},...Object.entries(v as object).map(([k,v])=>leaves(v,`${path}.${k}`)));}
it("三语键和插值一致，原创14题与源文件精确对应且不改选项ID",()=>{
 const source=leaves(rapportOnlineCopy["zh-Hans"]);
 for(const l of LANGUAGES){const target=leaves(rapportOnlineCopy[l]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());for(const[k,v]of Object.entries(source)){const vars=(s:string)=>[...s.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();expect(vars(target[k]!)).toEqual(vars(v));}}
 const bank=JSON.parse(readFileSync("../data/manual/rapport-questions.json","utf8")).questions as {questionId:string;prompt:string;options:string[]}[];
 expect(rapportQuestions).toHaveLength(bank.length);
 for(const q of bank){
  const question:RapportQuestion={questionId:q.questionId,prompt:q.prompt,options:q.options.map((label,i)=>({label,optionId:`${q.questionId}-o${i+1}`})),chooserId:"p",selfIsChooser:false,answered:true,selfOptionId:`${q.questionId}-o2`,locked:true,reveal:{chosenOptionId:`${q.questionId}-o2`,chosenLabel:q.options[1]!,correctPredictorIds:["p"]}};
  for(const l of LANGUAGES){const translated=localizeRapportQuestion(question,l);expect(translated.original).toBe(false);expect(translated.options.map(o=>o.optionId)).toEqual(question.options.map(o=>o.optionId));expect(translated.options).toHaveLength(4);expect(translated.selfOptionId).toBe(question.selfOptionId);expect(translated.reveal?.chosenLabel).toBe(translated.options[1]!.label);expect(translated.reveal?.correctPredictorIds).toEqual(["p"]);expect(localizeRapportQuestion(question,"zh-Hans")).toMatchObject(question);}
 }
});
it("相同编号但原文选项变动时保留原文，不伪造题目或已揭晓标签",()=>{
 const item=rapportQuestions[0]!;
 const q:RapportQuestion={questionId:item.id,prompt:item.source.prompt,options:item.source.options.map((label,i)=>({label,optionId:`${item.id}-o${i+1}`})),chooserId:null,selfIsChooser:false,answered:false,selfOptionId:null,locked:false};
 for(const bad of [{...q,prompt:"新的题面"},{...q,options:[...q.options].reverse()},{...q,options:q.options.map((o,i)=>({...o,label:i===0?"不同含义":o.label}))},{...q,questionId:"unfamiliar"}]){const translated=localizeRapportQuestion(bad,"ja");expect(translated).toMatchObject(bad);expect(translated.original).toBe(true);}
 const revealed={...q,reveal:{chosenOptionId:"unknown",chosenLabel:"原文说明",correctPredictorIds:[]}};expect(localizeRapportQuestion(revealed,"ja").reveal?.chosenLabel).toBe("原文说明");
});
it("草稿只接受当前选项，拒绝畸形过期未来和过大值，空选项也能保存",()=>{
 localStorage.clear();expect(saveRapportDraft("self:round:q","one")).toBe(true);expect(loadRapportDraft("self:round:q",["one"])).toEqual({optionId:"one"});expect(loadRapportDraft("self:round:other",["one"])).toBeNull();expect(loadRapportDraft("self:round:q",["two"])).toBeNull();expect(saveRapportDraft("self:round:q",null)).toBe(true);expect(loadRapportDraft("self:round:q",["one"])).toEqual({optionId:null});
 for(const bad of ["{",JSON.stringify({savedAt:Date.now()-86400001,optionId:"one"}),JSON.stringify({savedAt:Date.now()+120000,optionId:"one"}),JSON.stringify({savedAt:Date.now(),optionId:123}),"x".repeat(1025)]){localStorage.setItem("umaguess:rapport-draft:bad",bad);expect(loadRapportDraft("bad",["one"])).toBeNull();}localStorage.clear();
});
it("成绩只取正式终局results，100单位一分，不信投影final也不在取消造成绩",()=>{
 const raw={round:{phase:"revealed"},publicState:{final:[{scoreUnits:999999}]},results:[{subjectId:"a",scoreUnits:8000,rank:1},{subjectId:"b",scoreUnits:-1},{subjectId:"c",scoreUnits:10001},{subjectId:"d",scoreUnits:0.5}]};
 expect(normalizeRapportRound(raw).scores).toEqual([{subjectId:"a",label:"",points:80,rank:1}]);expect(normalizeRapportRound({...raw,results:[]}).scores).toEqual([]);for(const phase of ["collecting","locked","cancelled"]){expect(normalizeRapportRound({...raw,round:{phase}}).scores).toEqual([]);}
});
