import { describe, expect, it } from "vitest";
import fixtures from "@/lib/domain/testdata/sort-relations.json";
import { changeSort, scoreSort, sortCards, sortResult, type SortProfile } from "@/lib/domain/sort-practice";
import { availableSortKeys, createSortGame, readSortPack, validateSortArchive, type SortPack } from "./sort-practice-store";
import { sha256 } from "./checksum";
function pack():SortPack {
 const profiles:SortProfile[]=Array.from({length:8},(_,i)=>({characterId:`c${i}`,displayName:`角色${i}`,imagePath:`/assets/avatars/c${i}.webp`,values:{height:150+Math.floor(i/2),birthday:101+i}}));
 const contentJSON=JSON.stringify({cards:profiles});
 return {schemaVersion:1,modeId:"sort",rulesVersion:"2.0.0-draft.1",datasetVersion:"ds",contentPackVersion:"pack",sourceUrls:["https://wiki.biligame.com/umamusume/"],reviewStatus:"source-snapshot",contentJSON,sha256:sha256(contentJSON),byteLength:new TextEncoder().encode(contentJSON).length};
}
describe("shared sorting relations",()=>{for(const f of fixtures)it(f.name,()=>expect(scoreSort(f.groups,f.values)).toEqual({correct:f.correct,total:f.total,scoreUnits:f.scoreUnits}));});
describe("sorting practice",()=>{
 it("hides closed-book values and scores only the last saved answer",()=>{
  const p=pack(),profiles=readSortPack(p).profiles,game=createSortGame(p,"height",false,1000);
  expect(sortCards(game,profiles).every(c=>c.display===null&&c.raw===null)).toBe(true);
  expect(sortCards({...game,openBook:true},profiles).every(c=>c.display&&c.raw!==null)).toBe(true);
  const answer=sortResult(game,profiles).groups;
  const saved=changeSort(game,answer,"submit",2000);
  const draft=changeSort(saved,[...answer].reverse(),"draft",3000);
  expect(sortResult(draft,profiles).scoreUnits).toBe(10000);
  expect(sortResult(game,profiles).scoreUnits).toBe(0);
  expect(()=>changeSort(draft,answer,"submit",game.deadlineAt)).toThrow();
 });
 it("rejects repeated cards, bad dates, tampering and changed timer on import",()=>{
  const p=pack(),g=createSortGame(p,"birthday",false,1000);
  expect(()=>scoreSort([["a","a"]],{a:1,b:2})).toThrow();
  expect(()=>readSortPack({...p,contentJSON:p.contentJSON+" "})).toThrow();
  const bad=JSON.stringify({cards:readSortPack(p).profiles.map(c=>({...c,values:{birthday:230}}))});
  expect(()=>readSortPack({...p,contentJSON:bad,sha256:sha256(bad),byteLength:new TextEncoder().encode(bad).length})).toThrow();
  expect(()=>validateSortArchive({schemaVersion:1,pack:p,game:{...g,deadlineAt:g.deadlineAt+1}})).toThrow();
  expect(validateSortArchive({schemaVersion:1,pack:p,game:{...g,extraSecret:"discard"}}).game).not.toHaveProperty("extraSecret");
 });
 it("draws six unique profiles and rejects a non-distinguishable pool",()=>{
  const p=pack(),profiles=readSortPack(p).profiles;
  for(let i=0;i<20;i++){
   const game=createSortGame(p,"height",false,1000+i);
   expect(new Set(game.cardIds).size).toBe(6);
   expect(new Set(game.cardIds.map(id=>profiles.find(c=>c.characterId===id)!.values.height)).size).toBeGreaterThan(1);
  }
  const contentJSON=JSON.stringify({cards:profiles.map(c=>({...c,values:{height:150}}))});
  expect(()=>createSortGame({...p,contentJSON,sha256:sha256(contentJSON),byteLength:new TextEncoder().encode(contentJSON).length},"height",false)).toThrow();
 });
});


it("offers only keys with six known values and a meaningful comparison",()=>{
 const profiles=readSortPack(pack()).profiles;
 expect(availableSortKeys(profiles)).toEqual(["height","birthday"]);
 const heightOnly=profiles.map((p,i)=>({...p,values:i<2?p.values:{height:p.values.height}}));
 expect(availableSortKeys(heightOnly)).toEqual(["height"]);
 expect(availableSortKeys(profiles.map(p=>({...p,values:{height:160}})))).toEqual([]);
 expect(availableSortKeys(profiles.slice(0,5))).toEqual([]);
});
