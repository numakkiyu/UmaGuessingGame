import type {AudioRound} from "@/lib/api/audio";
export type AudioDraft={matches:[string,string][];reported:string[]};
const PREFIX="umaguess:audio-draft:",PLAYS="umaguess:audio-plays:";
export function validAudioDraft(value:unknown,round:AudioRound):value is AudioDraft{
 if(!value||typeof value!=="object")return false;const d=value as AudioDraft;
 if(!Array.isArray(d.matches)||!Array.isArray(d.reported)||d.matches.length>6||d.reported.length>6)return false;
 const clips=new Set(round.clips.map(c=>c.clipId)),candidates=new Set(round.candidates.map(c=>c.candidateId)),seen=new Set(),used=new Set();
 for(const id of d.reported){if(typeof id!=="string"||!clips.has(id)||seen.has(id))return false;seen.add(id);}
 for(const row of d.matches){if(!Array.isArray(row)||row.length!==2||!clips.has(row[0])||!candidates.has(row[1])||seen.has(row[0])||used.has(row[1]))return false;seen.add(row[0]);used.add(row[1]);}return true;
}
export function loadAudioDraft(scope:string,round:AudioRound):AudioDraft|null{
 try{const raw=localStorage.getItem(PREFIX+scope);if(!raw||raw.length>8192)return null;const v=JSON.parse(raw);if(!v||!Number.isSafeInteger(v.savedAt)||v.savedAt>Date.now()+60000||v.savedAt<Date.now()-86400000||!validAudioDraft(v,round))return null;return {matches:v.matches,reported:v.reported};}catch{return null;}
}
export function saveAudioDraft(scope:string,draft:AudioDraft):boolean{try{localStorage.setItem(PREFIX+scope,JSON.stringify({...draft,savedAt:Date.now()}));return true;}catch{return false;}}
export function removeAudioDraft(scope:string){try{localStorage.removeItem(PREFIX+scope);}catch{/* Keep memory state. */}}
export function loadAudioPlays(scope:string,round:AudioRound):Map<string,number>{
 try{const raw=sessionStorage.getItem(PLAYS+scope);if(!raw||raw.length>4096)return new Map();const values:unknown=JSON.parse(raw);if(!Array.isArray(values)||values.length>6)return new Map();const result=new Map<string,number>();for(const row of values){if(!Array.isArray(row)||row.length!==2)return new Map();const clip=round.clips.find(c=>c.clipId===row[0]);if(!clip||result.has(row[0])||!Number.isSafeInteger(row[1])||row[1]<1||row[1]>clip.maxPlays)return new Map();result.set(row[0],row[1]);}return result;}catch{return new Map();}
}
export function saveAudioPlays(scope:string,values:Map<string,number>):boolean{try{sessionStorage.setItem(PLAYS+scope,JSON.stringify([...values]));return true;}catch{return false;}}
