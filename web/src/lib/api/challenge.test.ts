import { describe, expect, it } from "vitest";
import { challengeGuessChoices } from "./challenge";
import type { Character, components } from "./types";

const identity = {characterId:"old",displayName:"旧题角色",avatarPath:"/assets/avatars/old.webp"};
const view = {characterIds:["old"],characters:[identity]} as components["schemas"]["ChallengeView"];
const current:Character = {characterId:"new",displayName:"新角色",aliases:[],entityKind:"uma",datasetVersion:"updated"};

describe("frozen challenge candidates",()=>{
 it("keeps old choices after a directory update or failure",()=>{
  for(const directory of [[],[current]]){
   const choices=challengeGuessChoices(view,directory);
   expect(choices.map(c=>c.characterId)).toEqual(["old"]);
   expect(choices[0]?.displayName).toBe("旧题角色");
  }
 });
 it("enriches permitted identities without admitting current-only entries",()=>{
  const known={...current,characterId:"old",displayName:"地区译名"};
  expect(challengeGuessChoices(view,[current,known])).toEqual([known]);
 });
 it("filters repeated or conflicting identities and supports older receipts",()=>{
  expect(challengeGuessChoices({...view,characters:[identity,identity,{...identity,characterId:"new"}]},[current])).toHaveLength(1);
  expect(challengeGuessChoices({...view,characters:undefined},[current])).toEqual([]);
  expect(challengeGuessChoices(undefined,[current])).toEqual([]);
 });
});
