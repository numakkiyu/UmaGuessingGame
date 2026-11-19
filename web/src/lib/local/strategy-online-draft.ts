const PREFIX="umaguess:strategy-draft:";
export const strategyActions=["steady","sprint","recover","follow"] as const;
export type StrategyChoice=typeof strategyActions[number];
export function isStrategyChoice(v:unknown):v is StrategyChoice {return typeof v==="string"&&strategyActions.some(a=>a===v);}
export function loadStrategyDraft(scope:string):{action:StrategyChoice|null}|null {
 try{const raw=localStorage.getItem(PREFIX+scope);if(!raw||raw.length>512)return null;const v=JSON.parse(raw);if(!v||!Number.isSafeInteger(v.savedAt)||v.savedAt>Date.now()+60000||v.savedAt<=Date.now()-86400000||(v.action!==null&&!isStrategyChoice(v.action)))return null;return {action:v.action};}catch{return null;}
}
export function saveStrategyDraft(scope:string,action:StrategyChoice|null):boolean {try{localStorage.setItem(PREFIX+scope,JSON.stringify({action,savedAt:Date.now()}));return true;}catch{return false;}}
export function clearStrategyDraft(scope:string){try{localStorage.removeItem(PREFIX+scope);}catch{/* Do not claim persistence. */}}
