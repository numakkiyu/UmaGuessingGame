#!/usr/bin/env node
import { chromium } from "playwright";
import { mkdir,writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const base=process.env.UMA_WEB_BASE??"http://127.0.0.1:8081",output=resolve(process.env.UMA_EVIDENCE_DIR??"../.cache/reports");await mkdir(output,{recursive:true});
const browser=await chromium.launch({headless:true,...(process.env.PLAYWRIGHT_CHROMIUM?{executablePath:process.env.PLAYWRIGHT_CHROMIUM}:{})});
const context=await browser.newContext({viewport:{width:1440,height:1000},acceptDownloads:true}),page=await context.newPage();const checks=[],requests=[],errors=[];
page.on("request",r=>{const path=new URL(r.url()).pathname;if(path.startsWith("/api/"))requests.push({method:r.method(),path});});page.on("pageerror",e=>errors.push(e.message));
const check=(name,ok,detail)=>{checks.push({name,ok,detail:structuredClone(detail)});if(!ok)throw new Error(name+": "+JSON.stringify(detail));};
async function state(){return page.evaluate(async()=>{
 const db=await new Promise((resolve,reject)=>{const r=indexedDB.open("umaguess-activity-practice",1);r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
 const get=(store,key)=>new Promise((resolve,reject)=>{const r=db.transaction(store).objectStore(store).get(key);r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
 const game=await get("games",location.pathname.split("/").at(-1)),p=await get("packs",game.packKey);db.close();return {game,profiles:JSON.parse(p.pack.contentJSON).cards};
});}
try{
 await page.clock.install();
 await page.goto(base+"/play/sort");await page.getByRole("button",{name:"准备排序资料",exact:true}).waitFor();check("entry has zero business requests",requests.length===0,requests);
 const preparing=page.waitForResponse(r=>new URL(r.url()).pathname==="/api/v1/practice-packs/sort");
 await page.getByRole("button",{name:"准备排序资料",exact:true}).click();
 const prepared=await preparing;check("public preparation succeeds without identity",prepared.ok(),{status:prepared.status()});
 await page.getByRole("button",{name:"开始排序",exact:true}).waitFor({timeout:30000});
 check("prepares only public sorting pack",requests.length===1&&requests[0].path==="/api/v1/practice-packs/sort",requests);
 await page.getByRole("button",{name:"开始排序",exact:true}).click();await page.waitForURL(/\/practice\/sort\/local-/);await page.getByRole("button",{name:"保存作答",exact:true}).waitFor();
 const initial=await state(),url=page.url(),before=requests.length;
 check("six unique cards and closed book values hidden",initial.game.cardIds.length===6&&new Set(initial.game.cardIds).size===6&&!await page.getByText(/排序值/).count());
 await context.setOffline(true);
 await page.getByRole("button",{name:"把「第 2 名」上移",exact:true}).click();
 await page.waitForFunction(async()=>{const db=await new Promise(r=>{const q=indexedDB.open("umaguess-activity-practice",1);q.onsuccess=()=>r(q.result);});const g=await new Promise(r=>{const q=db.transaction("games").objectStore("games").get(location.pathname.split("/").at(-1));q.onsuccess=()=>r(q.result);});db.close();return g.revision===1;});
 await page.getByRole("button",{name:"保存作答",exact:true}).click();await page.getByText("当前排序已保存，时间到后揭晓。",{exact:true}).waitFor();
 const saved=await state();
 check("offline reorder and explicit submit persist",saved.game.revision===2&&saved.game.submitted[0][0]===initial.game.cardIds[1]);
 await page.screenshot({path:resolve(output,"sort-practice-desktop.png"),fullPage:true});
 await page.reload();await page.getByRole("button",{name:"保存作答",exact:true}).waitFor();const restored=await state();
 check("offline refresh preserves exact question timer and submitted groups",restored.game.deadlineAt===initial.game.deadlineAt&&JSON.stringify(restored.game.submitted)===JSON.stringify(saved.game.submitted));
 await page.setViewportSize({width:320,height:740});
 check("narrow phone reflows controls without overflow",await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await page.screenshot({path:resolve(output,"sort-practice-narrow.png"),fullPage:true});
 await page.setViewportSize({width:390,height:844});check("mobile has no horizontal overflow",await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.screenshot({path:resolve(output,"sort-practice-mobile.png"),fullPage:true});
 // A stale page must not overwrite a newer revision.
 const other=await context.newPage();await other.goto(url);await other.getByRole("button",{name:"保存作答",exact:true}).waitFor();
 await page.getByRole("button",{name:"把「第 1 名」与下一名合并为并列",exact:true}).click();await page.getByText("排序已改动，记得再保存一次。",{exact:true}).waitFor();
 await other.getByText("另一页已经更新了这局排序",{exact:false}).waitFor();check("stale tab cannot overwrite",await other.getByRole("button",{name:"保存作答",exact:true}).isDisabled());await other.close();
 // Finish grades the confirmed answer, never the unsubmitted draft.
 page.once("dialog",d=>d.accept());await page.getByRole("button",{name:"结束并揭晓",exact:true}).click();await page.getByRole("heading",{name:"正确顺序",exact:true}).waitFor();
 const final=await state();
 const values=Object.fromEntries(final.game.cardIds.map(id=>[id,final.profiles.find(c=>c.characterId===id).values[final.game.key]]));
 const positions=Object.fromEntries(saved.game.submitted.flatMap((g,i)=>g.map(id=>[id,i])));let correct=0,total=0;
 for(let i=0;i<final.game.cardIds.length;i++)for(let j=i+1;j<final.game.cardIds.length;j++){const a=final.game.cardIds[i],b=final.game.cardIds[j];total++;if(Math.sign(values[a]-values[b])===Math.sign(positions[a]-positions[b]))correct++;}
 check("result matches confirmed pair relations",await page.getByText(`本局得分 ${Math.floor(100*correct/total)} 分 · ${correct} / ${total} 对关系正确`,{exact:true}).isVisible());
 check("early finish retains last submitted answer",final.game.ended&&JSON.stringify(final.game.submitted)===JSON.stringify(saved.game.submitted));
 await page.getByRole("heading",{name:/排序结果揭晓|漂亮，顺序全对！/}).evaluate(el=>Promise.all(el.closest("section").getAnimations({subtree:true}).map(a=>a.finished)));
 await page.evaluate(()=>window.scrollTo(0,0));await page.screenshot({path:resolve(output,"sort-practice-result.png"),fullPage:true});
 const downloading=page.waitForEvent("download");await page.getByRole("button",{name:"导出排序记录",exact:true}).click();check("export works offline",(await downloading).suggestedFilename().startsWith("umaguess-sort-"));
 await page.getByRole("button",{name:"再排一局",exact:true}).click();await page.waitForFunction(old=>location.href!==old,url);await page.getByRole("button",{name:"保存作答",exact:true}).waitFor();
 await page.clock.fastForward(91000);await page.getByRole("heading",{name:"正确顺序",exact:true}).waitFor();
 check("deadline reveals without an automatic answer",await page.getByText("本局得分 0 分 · 0 / 15 对关系正确",{exact:true}).isVisible());
 await page.reload();await page.getByRole("heading",{name:"正确顺序",exact:true}).waitFor();
 check("expired question does not restart on refresh",await page.getByText("本局结束",{exact:true}).isVisible());
 check("entire play and refresh use zero business requests",requests.length===before,requests.slice(before));
 check("player copy hides execution location",!/(本机|服务器|云端|本地|服务端|客户端)/.test(await page.locator("body").innerText()));check("no page errors",errors.length===0,errors);
}catch(error){checks.push({name:"execution",ok:false,detail:String(error)});await page.screenshot({path:resolve(output,"sort-practice-failure.png"),fullPage:true}).catch(()=>{});await writeFile(resolve(output,"sort-practice-failure.txt"),await page.locator("body").innerText());}
finally{await writeFile(resolve(output,"sort-practice-e2e.json"),JSON.stringify({base,checks,requests,errors},null,2));await browser.close();}
console.log(JSON.stringify(checks,null,2));if(checks.some(c=>!c.ok))process.exitCode=1;
