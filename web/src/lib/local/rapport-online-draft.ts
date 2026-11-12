const PREFIX="umaguess:rapport-draft:";
export function loadRapportDraft(scope:string,options:readonly string[]):{optionId:string|null}|null {
 try {
  const raw=localStorage.getItem(PREFIX+scope);if(!raw||raw.length>1024)return null;
  const v=JSON.parse(raw);
  if(!v||!Number.isSafeInteger(v.savedAt)||v.savedAt>Date.now()+60000||v.savedAt<=Date.now()-86400000||(v.optionId!==null&&(typeof v.optionId!=="string"||!options.includes(v.optionId))))return null;
  return {optionId:v.optionId};
 }catch{return null;}
}
export function saveRapportDraft(scope:string,optionId:string|null):boolean {try{localStorage.setItem(PREFIX+scope,JSON.stringify({optionId,savedAt:Date.now()}));return true;}catch{return false;}}
export function clearRapportDraft(scope:string){try{localStorage.removeItem(PREFIX+scope);}catch{/* Current answer is still available. */}}
