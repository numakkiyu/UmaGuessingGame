const PREFIX="umaguess:clue-draft:";
export type ClueDraft={assignment:Map<number,string>;activeSlot:number|null};
export function loadClueDraft(scope:string,slots:number[],cards:string[]):ClueDraft|null {
 try{const raw=localStorage.getItem(PREFIX+scope);if(!raw||raw.length>4096)return null;const v=JSON.parse(raw);if(!v||!Number.isSafeInteger(v.savedAt)||v.savedAt>Date.now()+60000||v.savedAt<=Date.now()-86400000||!Array.isArray(v.assignment)||v.assignment.length>6||v.activeSlot!==null&&!slots.includes(v.activeSlot))return null;const assignment=new Map<number,string>(),used=new Set<string>();for(const row of v.assignment){if(!Array.isArray(row)||row.length!==2||!slots.includes(row[0])||!cards.includes(row[1])||assignment.has(row[0])||used.has(row[1]))return null;assignment.set(row[0],row[1]);used.add(row[1]);}return {assignment,activeSlot:v.activeSlot};}catch{return null;}
}
export function saveClueDraft(scope:string,draft:ClueDraft):boolean {try{localStorage.setItem(PREFIX+scope,JSON.stringify({assignment:[...draft.assignment],activeSlot:draft.activeSlot,savedAt:Date.now()}));return true;}catch{return false;}}
