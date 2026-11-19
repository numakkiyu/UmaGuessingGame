import { createHash } from "node:crypto";
import { describe,expect,it } from "vitest";
import { satisfiesWork,sha256FirstWord } from "./matchmaking-hash";

describe("matchmaking work interoperates with SHA-256",()=>{
 it("matches standard digests at padding boundaries and server challenge lengths",()=>{
  for(const text of ["","abc","a".repeat(55),...Array.from({length:100},(_,i)=>`mwp_0123456789abcdef0123456789abcdef:${i*98731}`)]){
   const want=createHash("sha256").update(text).digest().readUInt32BE(0);expect(sha256FirstWord(text)).toBe(want);
  }
 });
 it("finds a proof accepted by standard cryptography with the same encoding",()=>{
  const id="mwp_0123456789abcdef0123456789abcdef";let nonce="";
  for(let n=0;n<1<<20;n++)if(satisfiesWork(id,String(n),12)){nonce=String(n);break;}
  expect(nonce).not.toBe("");expect(createHash("sha256").update(`${id}:${nonce}`).digest().readUInt32BE(0)).toBeLessThan(2**20);
  expect(satisfiesWork(id,nonce,0)).toBe(false);expect(satisfiesWork(id,nonce,21)).toBe(false);
 });
});
