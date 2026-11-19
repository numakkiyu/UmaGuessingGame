#!/usr/bin/env node
import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const base=process.env.UMA_WEB_BASE??"http://127.0.0.1:8081";
const output=resolve(process.env.UMA_EVIDENCE_DIR??"../.cache/reports");
await mkdir(output,{recursive:true});
const browser=await chromium.launch({headless:true,...(process.env.PLAYWRIGHT_CHROMIUM?{executablePath:process.env.PLAYWRIGHT_CHROMIUM}:{})});
const context=await browser.newContext({viewport:{width:1440,height:1000},acceptDownloads:true});
const page=await context.newPage();const checks=[],requests=[],errors=[];
page.on("request",r=>{if(new URL(r.url()).pathname.startsWith("/api/")&&new URL(r.url()).pathname!=="/api/v1/config")requests.push({method:r.method(),path:new URL(r.url()).pathname});});
page.on("pageerror",e=>errors.push(e.message));
const check=(name,ok,detail)=>{checks.push({name,ok,detail:structuredClone(detail)});if(!ok)throw new Error(name+": "+JSON.stringify(detail));};
async function state() {return page.evaluate(async()=>{
 const db=await new Promise((resolve,reject)=>{const r=indexedDB.open("umaguess-local-practice",1);r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
 const game=await new Promise((resolve,reject)=>{const r=db.transaction("games").objectStore("games").get(location.pathname.split("/").at(-1));r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
 const pack=await new Promise((resolve,reject)=>{const r=db.transaction("packs").objectStore("packs").get(game.packKey);r.onsuccess=()=>resolve(r.result.pack);r.onerror=()=>reject(r.error);});
 db.close();return {game,characters:JSON.parse(pack.contentJSON).characters};
});}
async function guess(character) {
 await page.locator("input[role=combobox]").fill(character.displayName);
 await page.getByRole("option").filter({hasText:character.displayName}).first().click();
 await page.getByRole("button",{name:"猜一次",exact:true}).click();
 await page.waitForFunction(name=>document.querySelectorAll('[data-guess-row="true"],table tbody tr').length>0&&document.body.innerText.includes(name),character.displayName);
}
try {
 await page.goto(base+"/play/classic");await page.getByRole("button",{name:"准备挑战资料",exact:true}).waitFor();
 check("entry does not restore cloud identity",requests.length===0,requests);
 await page.getByRole("button",{name:"准备挑战资料",exact:true}).click();
 await page.getByRole("button",{name:"开始挑战",exact:true}).waitFor({timeout:60000});
 check("preparation makes one public pack request only",requests.length===1&&requests[0].path==="/api/v1/practice-packs/classic",requests);
 check("offline shell installed",await page.evaluate(()=>navigator.serviceWorker.controller!==null));
 await page.getByRole("button",{name:"开始挑战",exact:true}).click();await page.waitForURL(/\/practice\/local-/);
 const initial=await state(),url=page.url(),before=requests.length;
 const wrong=initial.characters.find(c=>c.characterId!==initial.game.answerId),answer=initial.characters.find(c=>c.characterId===initial.game.answerId);
 await context.setOffline(true);await guess(wrong);await page.getByText("还可猜错 7 次",{exact:true}).waitFor();
 await page.screenshot({path:resolve(output,"local-practice-desktop.png"),fullPage:true});
 check("desktop defaults to real table",await page.getByRole("table").isVisible());
 const mobile=await context.newPage();await mobile.setViewportSize({width:390,height:844});await mobile.goto(url);await mobile.getByText("还可猜错 7 次",{exact:true}).waitFor();
 check("mobile uses cards",await mobile.locator('[data-guess-row="true"]').count()===1&&!await mobile.getByRole("table").count());
 check("mobile does not overflow",await mobile.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await mobile.screenshot({path:resolve(output,"local-practice-mobile.png"),fullPage:true});await mobile.close();
 await page.reload();await page.getByText("还可猜错 7 次",{exact:true}).waitFor();const restored=await state();
 check("offline refresh preserves question and one wrong attempt",restored.game.answerId===initial.game.answerId&&restored.game.guesses.length===1);
 await guess(answer);await page.getByRole("heading",{name:"漂亮，找到她了！"}).waitFor();
 check("correct answer does not deduct error allowance",(await state()).game.guesses.length===2);
 const downloading=page.waitForEvent("download");await page.getByRole("button",{name:"导出挑战记录",exact:true}).click();check("record export works offline",(await downloading).suggestedFilename().endsWith(".json"));
 await page.getByRole("button",{name:"再来一局",exact:true}).click();await page.waitForFunction(old=>location.href!==old,url);await page.getByText("还可猜错 8 次",{exact:true}).waitFor();
 check("all play and offline refresh make zero business requests",requests.length===before,requests.slice(before));
 check("no misleading reconnection",!await page.getByText(/重连|正在连接服务器/).count());check("no page errors",errors.length===0,errors);
 const other=await context.newPage();await other.goto(page.url());await other.getByText("还可猜错 8 次",{exact:true}).waitFor();
 await other.evaluate(()=>{window.__changes=[];window.__changeChannel=new BroadcastChannel("umaguess-practice-changes");window.__changeChannel.onmessage=e=>window.__changes.push(e.data);});
 const next=await state();await guess(next.characters.find(c=>c.characterId!==next.game.answerId));await page.getByText("还可猜错 7 次",{exact:true}).waitFor();
 await writeFile(resolve(output,"local-practice-multitab-diagnostic.json"),JSON.stringify({before:next.game,after:(await state()).game,otherBody:await other.locator("body").innerText(),broadcasts:await other.evaluate(()=>window.__changes)},null,2));
 await other.getByText("另一页已经更新了这局挑战",{exact:false}).waitFor();
 check("stale tab refuses overwriting progress",await other.locator("input[role=combobox]").isDisabled());await other.close();
} catch(error) {await page.screenshot({path:resolve(output,"local-practice-failure.png"),fullPage:true}).catch(()=>{});await writeFile(resolve(output,"local-practice-failure.txt"),await page.locator("body").innerText());checks.push({name:"execution",ok:false,detail:String(error)});}
finally {await writeFile(resolve(output,"local-practice-e2e.json"),JSON.stringify({base,checks,requests,errors},null,2));await browser.close();}
console.log(JSON.stringify(checks,null,2));if(checks.some(c=>!c.ok))process.exitCode=1;
