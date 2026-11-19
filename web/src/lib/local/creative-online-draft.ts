const PREFIX="umaguess:creative-draft:";
export type CreativeDraft={text:string;selected:string|null;excluded:string[]};
export function loadCreativeDraft(scope:string):CreativeDraft|null {
 try{
  const raw=localStorage.getItem(PREFIX+scope);if(!raw||raw.length>16000)return null;
  const v=JSON.parse(raw);if(!v||!Number.isSafeInteger(v.savedAt)||v.savedAt>Date.now()+60000||v.savedAt<=Date.now()-86400000||typeof v.text!=="string"||v.text.length>4096||(v.selected!==null&&(typeof v.selected!=="string"||v.selected.length>128))||!Array.isArray(v.excluded)||v.excluded.length>8||v.excluded.some((id:unknown)=>typeof id!=="string"||id.length>128)||new Set(v.excluded).size!==v.excluded.length)return null;
  return {text:v.text,selected:v.selected,excluded:v.excluded};
 }catch{return null;}
}
export function saveCreativeDraft(scope:string,draft:CreativeDraft):boolean {try{localStorage.setItem(PREFIX+scope,JSON.stringify({...draft,savedAt:Date.now()}));return true;}catch{return false;}}
