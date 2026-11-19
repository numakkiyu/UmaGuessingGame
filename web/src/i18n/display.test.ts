import { expect,it } from "vitest";
import { displayCopy } from "./display";
it("display locales share semantic keys and interpolation variables",()=>{
 const base=displayCopy["zh-Hans"];
 for(const messages of Object.values(displayCopy)){
 expect(Object.keys(messages).sort()).toEqual(Object.keys(base).sort());
 for(const key of Object.keys(base) as (keyof typeof base)[])expect([...messages[key].matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort()).toEqual([...base[key].matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort());
 }
});
