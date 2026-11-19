import { publishPracticeChange } from "./practice-channel";
import { LocalizedNotice } from "@/lib/player-notice";
import { SortNotice } from "@/i18n/sort";
import { validLocalizedNames } from "@/i18n/entities";
import type { components } from "@/lib/api/schema";
import type { SortGame, SortKey, SortProfile } from "@/lib/domain/sort-practice";
import { validSortGroups } from "@/lib/domain/sort-practice";
import { newPracticeId, PracticeConflict, practiceWriterId } from "./practice-store";
import { sha256 } from "./checksum";

export type SortPack=components["schemas"]["SortPracticePack"];
export type SortArchive={schemaVersion:1;game:SortGame;pack:SortPack};
const invalid=()=>new SortNotice("incomplete");
const object=(v:unknown):v is Record<string,unknown>=>typeof v==="object"&&v!==null&&!Array.isArray(v);
export const sortPackKey=(p:SortPack)=>`sort:${p.contentPackVersion}:${p.sha256}`;
export function readSortPack(raw:unknown):{pack:SortPack;profiles:SortProfile[]} {
 if(!object(raw)||raw.schemaVersion!==1||raw.modeId!=="sort"||raw.rulesVersion!=="2.0.0-draft.1"||raw.reviewStatus!=="source-snapshot")throw invalid();
 for(const key of ["datasetVersion","contentPackVersion"])if(typeof raw[key]!=="string"||!raw[key]||raw[key].length>160)throw invalid();
 if(!Array.isArray(raw.sourceUrls)||raw.sourceUrls.length>10||raw.sourceUrls.some(s=>typeof s!=="string"||s.length>500||!s.startsWith("https://")))throw invalid();
 if(typeof raw.contentJSON!=="string"||raw.contentJSON.length>1_000_000||!(/^[a-f0-9]{64}$/).test(String(raw.sha256))||new TextEncoder().encode(raw.contentJSON).length!==raw.byteLength||sha256(raw.contentJSON)!==raw.sha256)throw invalid();
 let content:unknown;try{content=JSON.parse(raw.contentJSON);}catch{throw invalid();}
 if(!object(content)||!Array.isArray(content.cards)||content.cards.length<6||content.cards.length>1000)throw invalid();
 const ids=new Set<string>(),profiles:SortProfile[]=[];
 for(const c of content.cards){
  if(!object(c)||typeof c.characterId!=="string"||!(/^[A-Za-z0-9_-]{1,80}$/).test(c.characterId)||ids.has(c.characterId)||typeof c.displayName!=="string"||!c.displayName||c.displayName.length>100||typeof c.imagePath!=="string"||!(/^$|^\/assets\/avatars\/[A-Za-z0-9_-]+\.(?:png|webp)$/).test(c.imagePath)||!object(c.values))throw invalid();
  if(!validLocalizedNames(c.localizedNames))throw invalid();
  ids.add(c.characterId);
  const values:SortProfile["values"]={};
  for(const [key,value] of Object.entries(c.values)){
   if(key!=="height"&&key!=="birthday"||!Number.isInteger(value))throw invalid();
   const n=value as number;
   if(key==="height"&&(n<50||n>300))throw invalid();
   if(key==="birthday"){
    const m=Math.floor(n/100),d=n%100,days=[31,29,31,30,31,30,31,31,30,31,30,31];
    if(m<1||m>12||d<1||d>days[m-1]!)throw invalid();
   }
   values[key]=n;
  }
  if(!Object.keys(values).length)throw invalid();
  profiles.push({characterId:c.characterId,displayName:c.displayName,...(c.localizedNames?{localizedNames:c.localizedNames}:{}),imagePath:c.imagePath,values});
 }
 const pack:SortPack={schemaVersion:1,modeId:"sort",rulesVersion:raw.rulesVersion,datasetVersion:raw.datasetVersion as string,contentPackVersion:raw.contentPackVersion as string,sourceUrls:raw.sourceUrls as string[],reviewStatus:"source-snapshot",contentJSON:raw.contentJSON,sha256:raw.sha256 as string,byteLength:raw.byteLength as number};
 return {pack,profiles};
}
/** Offer only keys that can produce a complete, distinguishable six-card question. */
export function availableSortKeys(profiles: readonly SortProfile[]): SortKey[] {
 return (["height", "birthday"] as const).filter(key => {
  const values=profiles.flatMap(profile=>profile.values[key]===undefined?[]:[profile.values[key]]);
  return values.length>=6&&new Set(values).size>=2;
 });
}
function randomBelow(limit:number,now:number):number {
 const max=Math.floor(0x100000000/limit)*limit;let n:number;
 do{n=(crypto.getRandomValues(new Uint32Array(1))[0]!^(now>>>0))>>>0;}while(n>=max);return n%limit;
}
export function createSortGame(pack:SortPack,key:SortKey,openBook:boolean,now=Date.now()):SortGame {
 const pool=readSortPack(pack).profiles.filter(c=>c.values[key]!==undefined);
 if(pool.length<6||new Set(pool.map(c=>c.values[key])).size<2)throw new SortNotice("notEnough");
 for(let i=pool.length-1;i>0;i--){const j=randomBelow(i+1,now);[pool[i],pool[j]]=[pool[j]!,pool[i]!];}
 const selected=pool.slice(0,6);
 if(new Set(selected.map(c=>c.values[key])).size===1)selected[5]=pool.slice(6).find(c=>c.values[key]!==selected[0]!.values[key])!;
 const cardIds=selected.map(c=>c.characterId);
 return {schemaVersion:1,modeId:"sort",gameId:newPracticeId(),packKey:sortPackKey(pack),rulesVersion:pack.rulesVersion,key,openBook,cardIds,draft:cardIds.map(id=>[id]),submitted:null,createdAt:now,updatedAt:now,deadlineAt:now+90000,ended:false,revision:0};
}
export function validateSortArchive(raw:unknown):SortArchive {
 if(!object(raw)||raw.schemaVersion!==1||!object(raw.game))throw invalid();
 const {pack,profiles}=readSortPack(raw.pack),g=raw.game;
 if(g.schemaVersion!==1||g.modeId!=="sort"||typeof g.gameId!=="string"||!(/^local-[a-f0-9]{32}$/).test(g.gameId)||g.packKey!==sortPackKey(pack)||g.rulesVersion!==pack.rulesVersion||(g.key!=="height"&&g.key!=="birthday")||typeof g.openBook!=="boolean"||typeof g.ended!=="boolean")throw invalid();
 if(!Array.isArray(g.cardIds)||g.cardIds.length!==6||new Set(g.cardIds).size!==6||g.cardIds.some(id=>typeof id!=="string"||!profiles.some(p=>p.characterId===id&&p.values[g.key as SortKey]!==undefined)))throw invalid();
 if(!validSortGroups(g.draft,g.cardIds)||g.submitted!==null&&!validSortGroups(g.submitted,g.cardIds))throw invalid();
 for(const key of ["createdAt","updatedAt","deadlineAt","revision"])if(!Number.isSafeInteger(g[key])||Number(g[key])<0||Number(g[key])>8_640_000_000_000_000)throw invalid();
 if(Number(g.createdAt)<=0||Number(g.updatedAt)<Number(g.createdAt)||g.deadlineAt!==Number(g.createdAt)+90000||Number(g.revision)>100000)throw invalid();
 const game:SortGame={schemaVersion:1,modeId:"sort",gameId:g.gameId,packKey:g.packKey as string,rulesVersion:g.rulesVersion as string,key:g.key,openBook:g.openBook,cardIds:[...g.cardIds] as string[],draft:g.draft.map(a=>[...a]),submitted:g.submitted===null?null:g.submitted.map(a=>[...a]),createdAt:g.createdAt as number,updatedAt:g.updatedAt as number,deadlineAt:g.deadlineAt as number,ended:g.ended,revision:g.revision as number};
 return {schemaVersion:1,game,pack};
}
let dbPromise:Promise<IDBDatabase>|undefined;
function openDB(){
 return dbPromise??=(new Promise<IDBDatabase>((resolve,reject)=>{
  if(!globalThis.indexedDB){reject(new LocalizedNotice("storage"));return;}
  const r=indexedDB.open("umaguess-activity-practice",1);
  r.onupgradeneeded=()=>{r.result.createObjectStore("packs",{keyPath:"key"});r.result.createObjectStore("games",{keyPath:"gameId"});};
  r.onerror=()=>reject(new LocalizedNotice("storage"));r.onblocked=()=>reject(new LocalizedNotice("blocked"));
  r.onsuccess=()=>{const db=r.result;db.onversionchange=()=>{db.close();dbPromise=undefined;};resolve(db);};
 }).catch((e:unknown)=>{dbPromise=undefined;throw e;}));
}
async function read<T>(store:string,key?:string):Promise<T>{
 const db=await openDB();return new Promise((resolve,reject)=>{
  const r=key===undefined?db.transaction(store).objectStore(store).getAll():db.transaction(store).objectStore(store).get(key);
  r.onsuccess=()=>resolve(r.result as T);r.onerror=()=>reject(new LocalizedNotice("storage"));
 });
}
export async function latestSortPack():Promise<SortPack|null>{
 const entries=await read<{pack:SortPack;installedAt:number}[]>("packs");entries.sort((a,b)=>b.installedAt-a.installedAt);
 for(const e of entries)try{return readSortPack(e.pack).pack;}catch{/* Preserve older packs and games. */}return null;
}
export async function installSortPack(pack:SortPack){
 const checked=readSortPack(pack).pack,db=await openDB();await new Promise<void>((resolve,reject)=>{
  const tx=db.transaction("packs","readwrite");tx.objectStore("packs").put({key:sortPackKey(checked),pack:checked,installedAt:Date.now()});tx.oncomplete=()=>resolve();tx.onabort=()=>reject(new LocalizedNotice("storage"));tx.onerror=()=>reject(new LocalizedNotice("storage"));
 });
}
export async function listSortGames(){return (await read<SortGame[]>("games")).filter(g=>g.modeId==="sort").sort((a,b)=>b.updatedAt-a.updatedAt);}
export async function loadSortGame(id:string):Promise<SortArchive|null>{
 const game=await read<SortGame|undefined>("games",id);if(!game)return null;
 const record=await read<{pack:SortPack}|undefined>("packs",game.packKey);
 if(!record)throw new SortNotice("missingPack");
 return validateSortArchive({schemaVersion:1,game,pack:record.pack});
}
export async function saveSortGame(game:SortGame,expectedRevision:number|null){
 const db=await openDB();await new Promise<void>((resolve,reject)=>{
  let conflict=false;const tx=db.transaction("games","readwrite"),store=tx.objectStore("games"),request=store.get(game.gameId);
  request.onsuccess=()=>{const previous=request.result as SortGame|undefined;if(expectedRevision===null?!!previous:previous?.revision!==expectedRevision){conflict=true;tx.abort();return;}store.put(game);};
  tx.oncomplete=()=>resolve();tx.onabort=()=>reject(conflict?new PracticeConflict():new LocalizedNotice("storage"));tx.onerror=()=>reject(new LocalizedNotice("storage"));
 });notify(game.gameId);
}
export async function deleteSortGame(id:string){const db=await openDB();await new Promise<void>((resolve,reject)=>{const tx=db.transaction("games","readwrite");tx.objectStore("games").delete(id);tx.oncomplete=()=>resolve();tx.onabort=()=>reject(new LocalizedNotice("storage"));tx.onerror=()=>reject(new LocalizedNotice("storage"));});notify(id);}
function notify(gameId:string){publishPracticeChange("umaguess-activity-changes",{gameId,writerId:practiceWriterId});}
export const volatileSortGames=new Map<string,SortArchive & {savedRevision?:number|null}>();
