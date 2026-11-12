/** Online bluff projection. Truth, citation and authors require a real reveal phase. */
import type {components} from "./types";
export type BluffOption={optionId:string;text:string;authorIds:string[];selfAuthored:boolean;voted?:boolean};
export type BluffSelf={subjectId:string|null;decoyCount:number;decoyText:string|null;voted:boolean;voteOptionId:string|null;identifiedTruth:boolean;decoyRemoved:boolean};
export type BluffBreakdown={subjectId:string;truthPoints:number;deceptionPoints:number};
export type BluffRound={prompt:string;promptSource:string|null;truth:string|null;options:BluffOption[];reviewOptions:BluffOption[];optionsGrouped:boolean;decoyCount:number;voteCount:number;scoring:string;subphase:string|null;revealedOptionId:string|null;phase:string|null;self:BluffSelf|null;progress:{subjectId:string;submitted:boolean;voted:boolean}[];voteCounts:Record<string,number>;breakdown:BluffBreakdown[];writeSeconds:number;voteSeconds:number};
function rec(value:unknown):Record<string,unknown>{return value!==null&&typeof value==="object"&&!Array.isArray(value)?value as Record<string,unknown>:{};}
function str(v:unknown):string|null{return typeof v==="string"&&v? v:null;}
function num(v:unknown,fallback=0):number{return typeof v==="number"&&Number.isSafeInteger(v)&&v>=0?v:fallback;}
function list(v:unknown):unknown[]{return Array.isArray(v)?v:[];}
function options(value:unknown,reveal:boolean):BluffOption[]{return list(value).flatMap(raw=>{const r=rec(raw),id=str(r.optionId)??str(r.id);return id?[{optionId:id,text:str(r.text)??"",authorIds:reveal?list(r.authorIds).filter((a):a is string=>typeof a==="string"):[],selfAuthored:r.selfAuthored===true,voted:r.voted===true}]:[];});}
export const OPTIONS_GROUP_THRESHOLD=9;
export function normalizeBluffRound(raw:unknown):BluffRound{
 const view=rec(raw),p=rec(view.publicState??view),meta=rec(view.round),phase=str(meta.phase)??str(p.phase),reveal=phase==="revealed"||phase==="settled",answer=reveal?rec(p.reveal):{},config=rec(p.config),s=rec(p.self);
 const progress=list(p.progress).map(raw=>{const p=rec(raw);return {subjectId:str(p.subjectId)??"",submitted:p.submitted===true,voted:p.voted===true};}).filter(p=>p.subjectId);
 const ballot=options(p.options,reveal),counts:Record<string,number>={};
 if(reveal)for(const [key,value] of Object.entries(rec(answer.voteCounts))){counts[key]=num(value);}
 const self:BluffSelf|null=p.self&&typeof p.self==="object"?{subjectId:str(s.subjectId),decoyCount:num(s.decoyCount),decoyText:str(s.decoyText),voted:s.voted===true,voteOptionId:str(s.voteOptionId),identifiedTruth:s.identifiedTruth===true,decoyRemoved:s.decoyRemoved===true}:null;
 return {prompt:str(p.prompt)??"",promptSource:reveal?str(answer.source)??str(p.promptSource):null,truth:reveal?str(answer.truth):null,options:ballot,reviewOptions:phase!=="cancelled"&&p.subphase==="review"?options(p.reviewOptions,false):[],optionsGrouped:p.optionsGrouped===true||ballot.length>9,decoyCount:typeof p.decoyCount==="number"?num(p.decoyCount):progress.length?progress.filter(p=>p.submitted).length:self?.decoyCount??0,voteCount:typeof p.voteCount==="number"?num(p.voteCount):progress.filter(p=>p.voted).length,scoring:str(p.scoring)??"",subphase:str(p.subphase),revealedOptionId:reveal?str(answer.truthOptionId)??str(p.revealedOptionId):null,phase,self,progress,voteCounts:counts,breakdown:reveal?list(answer.breakdown).flatMap(raw=>{const r=rec(raw),id=str(r.subjectId);return id?[{subjectId:id,truthPoints:num(r.truthPoints),deceptionPoints:num(r.deceptionPoints)}]:[];}):[],writeSeconds:num(config.writeSeconds,60)||60,voteSeconds:num(config.voteSeconds,30)||30};
}
export function isRevealPhase(round:BluffRound):boolean{return round.phase==="revealed"||round.phase==="settled";}
export function groupOptions(options:readonly BluffOption[],perGroup=9):BluffOption[][]{const size=Number.isSafeInteger(perGroup)&&perGroup>0?Math.min(9,perGroup):9;const out:BluffOption[][]=[];for(let i=0;i<options.length;i+=size)out.push(options.slice(i,i+size));return out.length?out:[[]];}
export function bluffText(text:string):string{return text.normalize("NFC").trim();}
export function bluffTextCount(text:string):number{return Array.from(bluffText(text)).length;}
/** Semantic validation keys are translated by the caller. */
export function validateBluffDraft(stage:"decoy"|"vote",value:string,alreadySubmitted:boolean):string|null{if(alreadySubmitted)return null;if(stage==="vote")return value?null:"chooseFirst";const count=bluffTextCount(value);return count<2||count>40?"textLength":null;}
export type BluffState=components["schemas"]["StateBluff"];
