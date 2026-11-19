import type {BluffRound} from "@/lib/api/bluff";
export type BluffDraft={text:string;vote:string|null;excluded:string[]};
const PREFIX="umaguess:bluff-draft:";
export function loadBluffDraft(scope:string,round:BluffRound):BluffDraft|null{
 try{const raw=localStorage.getItem(PREFIX+scope);if(!raw||raw.length>4096)return null;const v=JSON.parse(raw);
 if(!v||!Number.isSafeInteger(v.savedAt)||v.savedAt>Date.now()+60000||v.savedAt<=Date.now()-86400000||typeof v.text!=="string"||v.text.length>160||!Array.isArray(v.excluded)||v.excluded.length>32||new Set(v.excluded).size!==v.excluded.length)return null;
 const choices=new Set(round.options.filter(o=>!o.selfAuthored).map(o=>o.optionId)),review=new Set(round.reviewOptions.map(o=>o.optionId));
 return {text:v.text,vote:typeof v.vote==="string"&&choices.has(v.vote)?v.vote:null,excluded:v.excluded.filter((id:unknown):id is string=>typeof id==="string"&&review.has(id))};
 }catch{return null;}
}
export function saveBluffDraft(scope:string,draft:BluffDraft):boolean{try{localStorage.setItem(PREFIX+scope,JSON.stringify({...draft,savedAt:Date.now()}));return true;}catch{return false;}}
export function clearBluffDraft(scope:string){try{localStorage.removeItem(PREFIX+scope);}catch{/* Do not claim saved state. */}}
