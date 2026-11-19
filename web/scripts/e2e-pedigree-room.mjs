#!/usr/bin/env node
import { setPlayerName } from "./player-profile.mjs";
import { chromium } from "playwright";
import { randomBytes } from "node:crypto";
import { writeFile } from "node:fs/promises";
import { resolve } from "node:path";
const base=process.env.UMA_WEB_BASE??"http://127.0.0.1:8081",out=resolve(new URL("../../.cache/reports/",import.meta.url).pathname);
const browser=await chromium.launch({headless:true,...(process.env.PLAYWRIGHT_CHROMIUM?{executablePath:process.env.PLAYWRIGHT_CHROMIUM}:{})}),page=await browser.newPage({viewport:{width:1440,height:1000}}),checks=[],errors=[];
const check=(name,ok,detail)=>{checks.push({name,ok,detail});if(!ok)throw Error(name);};
page.on("pageerror",e=>errors.push(e.message));
try{
 await page.goto(base+"/play/pedigree/online");await setPlayerName(page, "拼图验证");await page.getByRole("button",{name:"开始对局",exact:true}).click();await page.getByRole("heading",{name:"血统槽位",exact:true}).waitFor({timeout:30000});
 check("room keeps historical disclaimer and natural scoring instructions",await page.locator("summary").filter({hasText:"史实原型血统"}).isVisible()&&await page.getByText("按答对的位置占比换算为 100 分，最后取整数；未填写的按未答处理。",{exact:true}).isVisible()&&!/floor\(/.test(await page.locator("body").innerText()));
 await page.getByRole("button",{name:"父：还没选",exact:true}).click();await page.getByRole("region",{name:"候选原型马",exact:true}).getByRole("button").first().click();
 const pending=page.waitForResponse(r=>/\/rounds\/[^/]+\/submissions$/.test(r.url())&&r.request().method()==="POST");await page.getByRole("button",{name:"提交血统",exact:true}).click();const response=await pending,receipt=await response.json();check("room accepts a real partial answer",response.ok()&&receipt.outcome==="accepted",{status:response.status()});await page.getByText("已提交。截止前可以继续修改，以最后一次提交为准。",{exact:true}).waitFor();
 const roundId=new URL(response.url()).pathname.split("/")[4],snapshot=await(await page.request.get(base+"/api/v1/rounds/"+roundId)).json(),state=snapshot.publicState??snapshot.round?.publicState;
 check("room hides answer and correctness before reveal",state.self.assignments.length===1&&state.self.submissions===1&&!state.answer&&!/(?:correctSlots|scoreUnits)/.test(JSON.stringify(receipt.feedback??{})));
 const body=response.request().postDataJSON(),headers=await response.request().allHeaders();body.submissionRevision+=1;body.modeAction.assignments=[{slotId:state.slots[0].slotId,horseId:state.candidates[0].horseId},{slotId:state.slots[0].slotId,horseId:state.candidates[1].horseId}];
 const malformed=await page.request.post(response.url(),{headers:{"Origin":base,"X-CSRF-Token":headers["x-csrf-token"],"Idempotency-Key":randomBytes(24).toString("hex")},data:body}),problem=await malformed.json();check("real API rejects multiple candidates for one slot",malformed.status()===422&&problem.code==="duplicate_slot",{status:malformed.status(),code:problem.code});
 const after=await(await page.request.get(base+"/api/v1/rounds/"+roundId)).json(),afterState=after.publicState??after.round?.publicState;check("rejected malformed answer leaves saved answer and revision intact",JSON.stringify(afterState.self)===JSON.stringify(state.self));
 await page.reload();await page.getByRole("heading",{name:"血统槽位",exact:true}).waitFor();check("room refresh preserves previous submission and allows revisions",await page.getByRole("button",{name:"再次提交",exact:true}).isEnabled());await page.setViewportSize({width:320,height:740});check("room controls fit narrow screen",await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));check("room hides execution location and layout-specific directions",!/(本机|本地|服务器|云端|服务端|客户端|右边的|左边的)/.test(await page.locator("body").innerText()));await page.screenshot({path:resolve(out,"pedigree-room-320.png"),fullPage:true,animations:"disabled"});check("room has no uncaught exceptions",errors.length===0,errors);
}catch(error){checks.push({name:"execution",ok:false,detail:String(error)});await page.screenshot({path:resolve(out,"pedigree-room-failure.png"),fullPage:true}).catch(()=>{});await writeFile(resolve(out,"pedigree-room-failure.txt"),await page.locator("body").innerText());}
finally{await writeFile(resolve(out,"pedigree-room-e2e.json"),JSON.stringify({checks,errors},null,2));await browser.close();}
console.log(JSON.stringify(checks,null,2));if(checks.some(c=>!c.ok))process.exitCode=1;
