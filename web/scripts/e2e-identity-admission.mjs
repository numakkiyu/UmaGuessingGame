/** Real Go identity + names; admission visuals use an explicitly synthetic widget.
 * Server Siteverify and signed-grant boundaries are covered in Go tests, not faked
 * by this browser fixture. Never use these routes in application code.
 */
import {chromium} from 'playwright';
import {mkdir,writeFile} from 'node:fs/promises';
import {isDeepStrictEqual} from 'node:util';
const base=process.env.UMA_WEB_BASE;
if(!base)throw new Error('Set isolated UMA_WEB_BASE');
const output=new URL('../../.cache/reports/identity-admission/',import.meta.url);await mkdir(output,{recursive:true});
const checks=[],errors=[];const check=(name,ok,detail)=>{checks.push({name,ok:!!ok,detail});console.log(`${ok?'PASS':'FAIL'} ${name}`);if(!ok)throw new Error(name)};
const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM});
const contexts=[];async function context(options={}){const c=await browser.newContext({viewport:{width:1440,height:1000},...options});contexts.push(c);c.setDefaultTimeout(12000);c.on('page',p=>p.on('pageerror',e=>errors.push(e.message)));return c}
async function form(page,name){await page.goto(base+'/rooms/new?mode=sort');await page.getByLabel('你的显示名').fill(name)}
async function create(page){const response=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1/rooms'&&r.request().method()==='POST'&&r.status()===201);await page.getByRole('button',{name:'创建好友房间',exact:true}).click();const data=await(await response).json();await page.waitForURL(/\/rooms\/rom_/);return data}
try{
 for(const fallback of [false,true]){
  const c=await context();if(fallback)await c.addInitScript(()=>Object.defineProperty(Navigator.prototype,'locks',{get:()=>undefined,configurable:true}));
  const pages=await Promise.all([c.newPage(),c.newPage()]);let created=0;c.on('request',r=>{if(new URL(r.url()).pathname==='/api/v1/sessions'&&r.method()==='POST')created++});
  await Promise.all(pages.map(p=>form(p,fallback?'同名练习乙':'同名练习甲')));
  await Promise.all(pages.map(create));
  const ids=await Promise.all(pages.map(p=>p.evaluate(async()=>{const r=await fetch('/api/v1/sessions/current');return (await r.json()).sessionId})));
  check(`${fallback?'IndexedDB':'Web Locks'} 多页面只创建一个身份`,created===1&&ids[0]===ids[1],{created,ids});
  await pages[0].reload();const restored=await c.request.get(base+'/api/v1/sessions/current');check('刷新保持身份', (await restored.json()).sessionId===ids[0]);
  await c.close();
 }
 const host=await context(),guest=await context({viewport:{width:390,height:844}});const hp=await host.newPage(),gp=await guest.newPage();
 await form(hp,'特别周');const data=await create(hp),rid=data.room.roomId;
 await gp.goto(base+'/join?code='+data.inviteCode);await gp.getByLabel('显示名',{exact:true}).fill('特别周');await gp.getByRole('button',{name:'加入房间',exact:true}).click();await gp.waitForURL(/\/rooms\/rom_/);
 await hp.reload();await hp.getByRole('heading',{name:data.room.name,exact:true}).waitFor();
 const hs=await(await host.request.get(base+'/api/v1/rooms/'+rid)).json(),gs=await(await guest.request.get(base+'/api/v1/rooms/'+rid)).json();
 check('同名保留不同座位并显示固定号码',hs.viewer.membershipId!==gs.viewer.membershipId&&hs.viewer.playerNumber!==gs.viewer.playerNumber&&hs.viewer.displayName!==gs.viewer.displayName,{host:hs.viewer.displayName,guest:gs.viewer.displayName});
 await hp.getByText('需要分队？',{exact:true}).click();
 await hp.getByLabel('队名',{exact:true}).fill('胡萝卜队');await hp.getByRole('button',{name:'新建队伍',exact:true}).click();
 const a=hp.getByRole('listitem',{name:new RegExp(hs.viewer.displayName+'，第')}),b=hp.getByRole('listitem',{name:new RegExp(gs.viewer.displayName+'，第')});
 await a.getByRole('button',{name:'加入「胡萝卜队」'}).click();await a.getByRole('button',{name:'移出队伍'}).waitFor();await b.getByRole('button',{name:'加入「胡萝卜队」'}).click();await b.getByRole('button',{name:'移出队伍'}).waitFor();
 let snap=await(await host.request.get(base+'/api/v1/rooms/'+rid)).json();const members=snap.members.filter(m=>m.teamId);
 check('两名同名入队只有一位队长',members.length===2&&members.filter(m=>m.role==='team-captain').length===1);
 const removed=hp.waitForResponse(r=>r.request().method()==='PATCH'&&r.url().includes('/memberships/'+gs.viewer.membershipId));
 await hp.getByRole('listitem',{name:new RegExp(gs.viewer.displayName+'，第')}).getByRole('button',{name:'移出队伍'}).click();
 check('移出返回已确认响应',(await removed).status()===200);
 snap=await(await host.request.get(base+'/api/v1/rooms/'+rid)).json();check('同名移出准确定位成员',snap.members.find(m=>m.membershipId===hs.viewer.membershipId)?.teamId&&snap.members.find(m=>m.membershipId===gs.viewer.membershipId)?.teamId===null);
 await hp.screenshot({path:new URL('same-name-desktop.png',output).pathname,fullPage:true});await gp.reload();await gp.getByRole('heading',{name:data.room.name,exact:true}).waitFor();await gp.screenshot({path:new URL('same-name-mobile.png',output).pathname,fullPage:true});
 // Synthetic external-service boundary. Domain writes after admission are real.
 const c=await context();const page=await c.newPage();let scriptCalls=0,verifyCalls=0,deny=true;const commands=[];
 await c.route('**/api/v1/config',async route=>{const response=await route.fetch();const body=await response.json();body.admission={enabled:true,siteKey:'ui-fixture',browserSignalsEnabled:true};await route.fulfill({response,json:body})});
 await c.route('https://challenges.cloudflare.com/turnstile/v0/api.js?*',async route=>{scriptCalls++;await route.fulfill({contentType:'application/javascript',body:`window.turnstile={render(el,o){const wrap=document.createElement('div');wrap.style.cssText='background:white;border:1px solid #b6c8d4;border-radius:6px;padding:12px;max-width:100%;font-size:14px';const text=document.createElement('p');text.textContent='验证流程测试示例';wrap.append(text);const button=document.createElement('button');button.textContent='完成验证测试';button.style.cssText='min-height:44px;margin-top:8px';button.onclick=()=>o.callback('test-ui-token');wrap.append(button);el.append(wrap);return 'fixture'},remove(){}};`})});
 await c.route('**/api/v1/sessions/current/admission',async route=>{verifyCalls++;if(verifyCalls===1){await route.fulfill({status:403,contentType:'application/problem+json',json:{title:'Forbidden',code:'admission-invalid'}})}else await route.fulfill({json:{verified:true}})});
 await c.route('**/api/v1/rooms',async route=>{if(route.request().method()!=='POST'){await route.continue();return}commands.push({body:route.request().postDataJSON(),key:route.request().headers()['idempotency-key']});if(deny){deny=false;await route.fulfill({status:403,contentType:'application/problem+json',json:{title:'Forbidden',code:'admission-required'}})}else await route.continue()});
 await form(page,'训练员小周');await page.getByRole('button',{name:'创建好友房间',exact:true}).click();await page.getByRole('dialog').waitFor();await page.getByRole('button',{name:'完成验证测试'}).waitFor();
 for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:900});const layout=await page.evaluate(()=>{const d=document.querySelector('[role=dialog]'),r=d.getBoundingClientRect();return {left:r.left,right:r.right,width:innerWidth,overflow:document.documentElement.scrollWidth>innerWidth,dialogOverflow:d.scrollWidth>d.clientWidth+1}});check(`入场面板 ${width}px 无横溢`,layout.left>=0&&layout.right<=width+1&&!layout.overflow&&!layout.dialogOverflow,layout);await page.screenshot({path:new URL(`admission-${width}.png`,output).pathname,fullPage:true})}
 await page.getByRole('button',{name:'完成验证测试'}).click();await page.getByRole('button',{name:'确认并继续'}).click();await page.getByText('这次确认已失效，请重新完成入场确认').waitFor();check('确定失效不继续创建',commands.length===1&&verifyCalls===1);
 await page.getByRole('button',{name:'重新确认',exact:true}).click();await page.getByRole('button',{name:'完成验证测试'}).last().click();await page.getByRole('button',{name:'确认并继续'}).click();await page.waitForURL(/\/rooms\/rom_/);
 check('重新确认后原操作只补发一次',commands.length===2&&isDeepStrictEqual(commands[0],commands[1])&&verifyCalls===2,{commands:commands.length,verifyCalls});check('外部脚本只加载一次',scriptCalls===1);
 // Cancelling a definite pre-entry refusal must retain the form and never send
 // the creation again or tell the player they lost permission.
 deny=true;commands.length=0;const verifiedBeforeCancel=verifyCalls;
 await form(page,'训练员小周');await page.getByRole('button',{name:'创建好友房间',exact:true}).click();await page.getByRole('dialog').waitFor();await page.getByRole('button',{name:'先返回',exact:true}).click();
 await page.getByRole('alert').filter({hasText:'已返回，尚未开始'}).waitFor();
 check('取消返回保留表单且不创建或校验',commands.length===1&&verifyCalls===verifiedBeforeCancel&&(await page.getByLabel('你的显示名').inputValue())==='训练员小周');
 // Disabled config and independent practice must never pull the external SDK.
 const offline=await context();let unwanted=0;offline.on('request',r=>{if(r.url().includes('challenges.cloudflare.com'))unwanted++});const op=await offline.newPage();await form(op,'安静练习');await create(op);await op.goto(base+'/play/classic');await op.getByRole('heading').first().waitFor();check('关闭验证与独立练习均不加载外部脚本',unwanted===0);
 check('页面无未捕获异常',errors.length===0,errors);
}finally{await writeFile(new URL('report.json',output),JSON.stringify({checks,errors,limits:['Browser admission uses a synthetic external widget and verification route; no production Cloudflare keys or live challenge are claimed.','Go tests separately exercise fixed Siteverify protocol checks and actual signed-grant middleware.']},null,2));await browser.close()}
