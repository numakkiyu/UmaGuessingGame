#!/usr/bin/env node
import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { resolve } from "node:path";
const exec=promisify(execFile),base=process.env.UMA_WEB_BASE??"http://127.0.0.1:8081";
if(!process.env.UMA_TEST_DB)throw new Error("Set UMA_TEST_DB to an isolated test database, never a live deployment");
const db=resolve(process.env.UMA_TEST_DB),output=resolve(process.env.UMA_EVIDENCE_DIR??"../.cache/reports");await mkdir(output,{recursive:true});
const browser=await chromium.launch({headless:true,...(process.env.PLAYWRIGHT_CHROMIUM?{executablePath:process.env.PLAYWRIGHT_CHROMIUM}:{})});
const context=await browser.newContext({viewport:{width:1440,height:1000}}),page=await context.newPage();
const checks=[],requests=[],errors=[];let sockets=0;
page.on("request",r=>{const path=new URL(r.url()).pathname;if(path.startsWith("/api/"))requests.push({method:r.method(),path});});page.on("websocket",()=>sockets++);page.on("pageerror",e=>errors.push(e.message));
const check=(name,ok,detail)=>{checks.push({name,ok,detail:structuredClone(detail)});if(!ok)throw new Error(name+": "+JSON.stringify(detail));};
async function sql(query){return JSON.parse((await exec(process.env.SQLITE3??"/usr/bin/sqlite3",["-readonly","-json",db,query])).stdout);}
async function guess(name){await page.getByRole("combobox").fill(name);await page.getByRole("option").filter({hasText:name}).first().click();await page.getByRole("button",{name:"猜一次",exact:true}).click();}
try{
 await page.goto(base+"/play/classic/verified");await page.getByRole("button",{name:"开始挑战",exact:true}).click();await page.waitForURL(/\/challenges\/chl_/);await page.getByRole("combobox").waitFor();
 const id=page.url().split("/").at(-1);if(!/^chl_[a-f0-9]{32}$/.test(id))throw new Error("unexpected test challenge id");
 const records=await sql(`SELECT c.answer_id,p.characters_json FROM verified_challenges c JOIN verified_content p ON p.id=c.content_id WHERE c.id='${id}'`);
 const pool=JSON.parse(records[0].characters_json),answer=pool.find(c=>c.ID===records[0].answer_id),wrong=pool.find(c=>c.ID!==answer.ID);
 check("uses challenge endpoint and no rooms",requests.some(r=>r.method==="POST"&&r.path==="/api/v1/challenges")&&!requests.some(r=>/rooms|streams|events/.test(r.path)),requests);
 const before=requests.length;
 // Lose the first acknowledgement after the real server commits it.
 let discarded=false;
 await page.route(`**/api/v1/challenges/${id}/guesses`,async route=>{
  if(!discarded){discarded=true;await route.fetch();await route.abort("failed");}else await route.continue();
 });
 await guess(wrong.Name);await page.getByRole("button",{name:"确认刚才的结果",exact:true}).waitFor();
 const afterLoss=(await sql(`SELECT revision FROM verified_challenges WHERE id='${id}'`))[0].revision;
 check("lost acknowledgement leaves one persisted guess",afterLoss===1,afterLoss);
 await page.getByRole("button",{name:"确认刚才的结果",exact:true}).click();await page.getByText("还可猜错 7 次",{exact:true}).waitFor();
 check("retry does not duplicate action",(await sql(`SELECT revision FROM verified_challenges WHERE id='${id}'`))[0].revision===1);
 check("one request per attempt plus one explicit retry",requests.slice(before).filter(r=>r.path.endsWith("/guesses")).length===2,requests.slice(before));
 await page.screenshot({path:resolve(output,"verified-challenge-desktop.png"),fullPage:true});
 const url=page.url();await page.reload();await page.getByText("还可猜错 7 次",{exact:true}).waitFor();
 // A second page restores the same identity and rotates its CSRF secret.
 const sibling=await context.newPage();await sibling.goto(url);await sibling.getByRole("combobox").waitFor();await sibling.close();
 const beforeRecovery=requests.length;
 await guess(answer.Name);await page.getByRole("heading",{name:"漂亮，找到她了！"}).waitFor();await page.getByText(/本局得分 92 分/).waitFor();
 const recovered=requests.slice(beforeRecovery);
 check("second page does not break the original action",recovered.filter(r=>r.path.endsWith("/guesses")).length===2&&recovered.filter(r=>r.path.endsWith("/csrf")).length===1&&!recovered.some(r=>r.method==="POST"&&r.path==="/api/v1/sessions"),recovered);
 check("server-calculated solve survives refresh",(await sql(`SELECT status,revision FROM verified_challenges WHERE id='${id}'`))[0].status==="solved");
 await page.setViewportSize({width:390,height:844});await page.getByRole("heading",{name:"漂亮，找到她了！"}).evaluate(el=>Promise.all(el.closest("section").getAnimations({subtree:true}).map(animation=>animation.finished)));await page.screenshot({path:resolve(output,"verified-challenge-mobile.png"),fullPage:true});
 check("mobile result fits viewport",await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 const idleBefore=requests.length;await page.waitForTimeout(2200);
 check("no idle polling or realtime connection",requests.length===idleBefore&&sockets===0&&!requests.some(r=>/streams|rooms|events/.test(r.path)),requests.slice(idleBefore));
 check("player copy hides execution location",!/(本机|服务器|云端|本地运行|本地判题)/.test(await page.locator("body").innerText()));
 check("no page errors",errors.length===0,errors);
 check("deep link remains same challenge",page.url()===url);
}catch(error){checks.push({name:"execution",ok:false,detail:String(error)});await page.screenshot({path:resolve(output,"verified-challenge-failure.png"),fullPage:true}).catch(()=>{});await writeFile(resolve(output,"verified-challenge-failure.txt"),await page.locator("body").innerText());}
finally{await writeFile(resolve(output,"verified-challenge-e2e.json"),JSON.stringify({base,checks,requests,errors},null,2));await browser.close();}
console.log(JSON.stringify(checks,null,2));if(checks.some(c=>!c.ok))process.exitCode=1;
