import { describe, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import vectors from "../domain/testdata/classic-comparison.json";
import { sha256 } from "./checksum";
import { compareClassic, guessClassic, classicProgress, type PracticeCharacter, type PracticePack, type ClassicGame } from "@/lib/domain/classic-practice";
import { normalizeRowFromFeedback } from "@/lib/api/classic";
import { readPracticePack, packKey, CLASSIC_RULES } from "./practice-pack";
import { validatePracticeArchive, createClassicGame } from "./practice-store";

const contentJSON=JSON.stringify({characters:[vectors[0]!.answer,{...vectors[0]!.answer,characterId:"other",displayName:"练习乙"}]});
const pack:PracticePack={schemaVersion:1,modeId:"classic",rulesVersion:CLASSIC_RULES,datasetVersion:"test",contentPackVersion:"test",sourceUrls:["https://wiki.biligame.com/umamusume/"],reviewStatus:"source-snapshot",contentJSON,sha256:sha256(contentJSON),byteLength:new TextEncoder().encode(contentJSON).length};
const characters=readPracticePack(pack).characters;
const game:ClassicGame={schemaVersion:1,modeId:"classic",gameId:"local-"+"a".repeat(32),packKey:packKey(pack),rulesVersion:CLASSIC_RULES,answerId:"a",wrongBudget:4,guesses:[],ended:false,createdAt:1000,updatedAt:1000,revision:0};

describe("public pack integrity on secure and LAN contexts",()=>{
  it.each(["","abc","赛马娘 & <练习>","x".repeat(55),"x".repeat(56),"x".repeat(64),"x".repeat(10000)])("matches SHA-256 reference",input=>expect(sha256(input)).toBe(createHash("sha256").update(input).digest("hex")));
  it("rejects corruption, external assets and incompatible rules without deleting the old pack",()=>{
    expect(()=>readPracticePack({...pack,contentJSON:pack.contentJSON+" "})).toThrow();
    expect(()=>readPracticePack({...pack,rulesVersion:"unknown"})).toThrow();
    const bad=JSON.stringify({characters:[{...characters[0],imagePath:"https://evil.invalid/a.webp"},characters[1]]});
    expect(()=>readPracticePack({...pack,contentJSON:bad,sha256:sha256(bad),byteLength:new TextEncoder().encode(bad).length})).toThrow();
    expect(readPracticePack(pack).characters).toHaveLength(2);
  });
  it("keeps optional classified names in saved packs and rejects malformed metadata",()=>{
    const names={translations:["特別週"],japanese:"スペシャルウィーク",english:"Special Week",nicknames:["小特"]};
    const withNames=(searchNames:unknown)=>{
      const contentJSON=JSON.stringify({characters:[{...characters[0],searchNames},characters[1]]});
      return {...pack,contentJSON,sha256:sha256(contentJSON),byteLength:new TextEncoder().encode(contentJSON).length};
    };
    expect(readPracticePack(withNames(names)).characters[0]?.searchNames).toEqual(names);
    for(const value of [null,{english:5},{nicknames:"小特"},{translations:[""]},{secret:"hidden"}])expect(()=>readPracticePack(withNames(value))).toThrow();
    expect(readPracticePack(pack).characters).toHaveLength(2);
  });
});
describe("offline and Go classic comparison parity",()=>{
  it.each(vectors)("$name",test=>{
    expect(compareClassic(test.guess as PracticeCharacter,test.answer as PracticeCharacter,1)).toEqual(normalizeRowFromFeedback(test.expected));
  });
  it("deducts only wrong guesses, refuses duplicates and prevents play after finish",()=>{
    const wrong=guessClassic(game,characters,"other",1001);
    expect(classicProgress(wrong,characters).remainingWrong).toBe(3);
    expect(()=>guessClassic(wrong,characters,"other",1002)).toThrow();
    const correct=guessClassic(wrong,characters,"a",1003);
    expect(classicProgress(correct,characters)).toMatchObject({solved:true,remainingWrong:3,finished:true});
    expect(()=>guessClassic(correct,characters,"other",1004)).toThrow();
  });
  it("does not depend on timestamp alone and preserves the recorded question on resume",()=>{
    let sample=0;
    const random=vi.spyOn(crypto,"getRandomValues").mockImplementation(array=>{
      if(array instanceof Uint32Array)array[0]=sample++;
      else if(array instanceof Uint8Array)array.fill(sample);
      return array;
    });
    const games=Array.from({length:32},()=>createClassicGame(pack,4,1000));
    random.mockRestore();
    expect(new Set(games.map(g=>g.gameId)).size).toBe(32);
    expect(new Set(games.map(g=>g.answerId)).size).toBe(2);
    expect(validatePracticeArchive({schemaVersion:1,game,pack}).game.answerId).toBe(game.answerId);
  });
  it("rejects malformed imported budgets, duplicate actions, unknown references and extra post-win actions",()=>{
    for(const change of [{wrongBudget:100},{guesses:["other","other"]},{answerId:"missing"},{guesses:["a","other"]},{packKey:"elsewhere"}])expect(()=>validatePracticeArchive({schemaVersion:1,game:{...game,...change},pack})).toThrow();
  });
});
