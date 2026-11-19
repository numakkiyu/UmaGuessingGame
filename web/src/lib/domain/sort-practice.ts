import { SortNotice, sortDisplay } from "@/i18n/sort";
import { type LocalizedNames } from "@/i18n/entities";
import type { SortCard, SortDraft } from "@/lib/api/sort";

export type SortKey = "height" | "birthday";
export type SortProfile = {characterId:string;displayName:string;localizedNames?:LocalizedNames;imagePath:string;values:Partial<Record<SortKey,number>>};
export type SortGame = {
 schemaVersion:1;modeId:"sort";gameId:string;packKey:string;rulesVersion:string;
 key:SortKey;openBook:boolean;cardIds:string[];draft:SortDraft;submitted:SortDraft|null;
 createdAt:number;updatedAt:number;deadlineAt:number;ended:boolean;revision:number;
};
export function validSortGroups(groups:unknown,ids:readonly string[]):groups is SortDraft {
 if(!Array.isArray(groups)||groups.length<1||groups.length>6||groups.some(g=>!Array.isArray(g)||g.length<1||g.length>6||g.some(id=>typeof id!=="string")))return false;
 const flat=(groups as SortDraft).flat();return flat.length===ids.length&&new Set(flat).size===ids.length&&flat.every(id=>ids.includes(id));
}
// Exact relation scoring shared with Go through the tracked golden fixture.
export function scoreSort(groups:SortDraft,values:Readonly<Record<string,number>>):{correct:number;total:number;scoreUnits:number} {
 const ids=Object.keys(values);
 if(!validSortGroups(groups,ids))throw new SortNotice("groups");
 const positions=new Map(groups.flatMap((group,index)=>group.map(id=>[id,index] as const)));
 let correct=0,total=0;
 for(let i=0;i<ids.length;i++)for(let j=i+1;j<ids.length;j++){
  const a=ids[i]!,b=ids[j]!;total++;
  if(Math.sign(positions.get(a)!-positions.get(b)!)===Math.sign(values[a]!-values[b]!))correct++;
 }
 return {correct,total,scoreUnits:total?Math.floor(100*correct/total)*100:0};
}
export function sortCards(game:SortGame,profiles:readonly SortProfile[],reveal=false,language?:string):SortCard[] {
 return game.cardIds.map(id=>{
  const p=profiles.find(c=>c.characterId===id)!;
  const value=p.values[game.key]!;
  const visible=game.openBook||reveal;
  return {characterId:id,name:p.displayName,localizedNames:p.localizedNames,imagePath:p.imagePath,remoteImageUrl:null,raw:visible?value:null,display:visible?sortDisplay(game.key,value,language):null};
 });
}
export function sortFinished(game:SortGame,now:number):boolean{return game.ended||now>=game.deadlineAt;}
export function sortResult(game:SortGame,profiles:readonly SortProfile[]) {
 const values=Object.fromEntries(game.cardIds.map(id=>[id,profiles.find(c=>c.characterId===id)!.values[game.key]!]));
 const score=game.submitted?scoreSort(game.submitted,values):{correct:0,total:game.cardIds.length*(game.cardIds.length-1)/2,scoreUnits:0};
 const answer=game.cardIds.slice().sort((a,b)=>values[a]!-values[b]!||a.localeCompare(b));
 const groups:SortDraft=[];
 for(const id of answer){const last=groups.at(-1);if(last&&values[last[0]!]===values[id])last.push(id);else groups.push([id]);}
 return {...score,groups};
}
export function changeSort(game:SortGame,groups:SortDraft,action:"draft"|"submit",now:number):SortGame {
 if(sortFinished(game,now))throw new SortNotice("stopped");
 if(!validSortGroups(groups,game.cardIds))throw new SortNotice("groups");
 const copied=groups.map(g=>[...g]);
 return {...game,draft:copied,submitted:action==="submit"?copied:game.submitted,revision:game.revision+1,updatedAt:Math.max(now,game.updatedAt)};
}
