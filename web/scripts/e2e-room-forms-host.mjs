/** Independent browser identities, real Go/SQLite, all mutations via visible UI. */
import {chromium} from 'playwright';
import {mkdir,writeFile} from 'node:fs/promises';
const base=process.env.UMA_WEB_BASE;if(!base)throw new Error('Set isolated UMA_WEB_BASE');
const out=new URL('../../.cache/reports/room-forms-host/',import.meta.url);await mkdir(out,{recursive:true});
const checks=[],errors=[],streamResponses=[];
const check=(name,ok,detail)=>{checks.push({name,ok:!!ok,detail});console.log(`${ok?'PASS':'FAIL'} ${name}`);if(!ok)throw new Error(name)};
const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM});
const contexts=[],pages=[];
for(let i=0;i<3;i++){const c=await browser.newContext({viewport:{width:i?390:1440,height:1000}});contexts.push(c);const p=await c.newPage();p.setDefaultTimeout(12000);p.on('pageerror',e=>errors.push(e.message));p.on('response',r=>{if(new URL(r.url()).pathname.endsWith('/stream'))streamResponses.push({identity:i,status:r.status()})});pages.push(p)}
const button=(p,name)=>p.getByRole('button',{name,exact:true});
async function write(p,method,path,action){const wait=p.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1'+path&&r.request().method()===method);await action();const r=await wait;const body=r.status()===204?null:await r.json();if(!r.ok())throw new Error(`${method} ${path} ${r.status()} ${JSON.stringify(body)}`);return body}
async function room(i,id){const r=await contexts[i].request.get(base+'/api/v1/rooms/'+id);if(!r.ok())throw new Error(`${r.status()} ${await r.text()}`);return r.json()}
async function leave(i,id){const p=pages[i];await p.goto(base+'/rooms/'+id);await p.getByText('房间管理与退出',{exact:true}).click();await button(p,'退出房间').click();const before=await room(i,id);await write(p,'DELETE',`/rooms/${id}/memberships/${before.viewer.membershipId}`,()=>button(p,'确认退出').click());await p.waitForURL(base+'/')}
try{
 const p=pages[0];await p.goto(base+'/');await p.getByRole('button',{name:/单人游玩/}).waitFor();
 for(const [form,pattern] of [['单人游玩',/^\/play\//],['好友多人',/^\/(rooms\/new|matchmaking)/],['活动团队',/^\/events\/new/]]){
  await p.getByRole('button',{name:new RegExp(form)}).click();const links=await p.locator('a.group').evaluateAll(as=>as.map(a=>new URL(a.href).pathname));check(form+'仅显示对应形式的玩法入口',links.length>0&&links.every(v=>pattern.test(v)),links.length);
 }
 for(const width of [320,390,768,1440]){await p.setViewportSize({width,height:1000});check(`大厅 ${width}px 无横溢`,await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));if(width===320||width===1440)await p.screenshot({path:new URL(`lobby-${width}.png`,out).pathname,fullPage:true,animations:'disabled'})}
 await p.goto(base+'/rooms/new?mode=classic');await p.getByLabel('你的显示名').fill('特别周');const friend=await write(p,'POST','/rooms',()=>button(p,'创建好友房间').click());await p.waitForURL(/\/rooms\/rom_/);await p.getByTestId('room-invite-code').waitFor();
 check('普通多人没有默认分队入口',await p.getByText('需要分队？',{exact:true}).count()===0);check('普通多人显示房间匹配码',(await p.locator('body').innerText()).includes('房间匹配码'));
 await leave(0,friend.room.roomId);
 await p.goto(base+'/events/new');await p.getByLabel('活动名称').fill('特雷森周末游玩会');const event=await write(p,'POST','/events',()=>button(p,'创建并进入主持台').click());await p.waitForURL(/\/events\/evs_.+\/host/);
 const rid=event.roomId,initial=await room(0,rid),code=initial.inviteCode;
 check('活动创建者自动成为主持人',initial.viewer.capabilities.includes('host'));
 for(let i=1;i<3;i++){await pages[i].goto(base+'/join?code='+code);await pages[i].getByLabel('显示名',{exact:true}).fill(i===1?'特别周':'小栗帽');await write(pages[i],'POST',`/rooms/${rid}/memberships`,()=>button(pages[i],'加入房间').click());await pages[i].waitForURL(/\/rooms\/rom_/)}
 await p.goto(base+'/rooms/'+rid);await p.getByRole('heading',{name:'成员',exact:true}).waitFor();await p.getByText('已连接',{exact:true}).waitFor();check('连续切换房间后同步可恢复',true);const full=await room(0,rid);const codes=full.members.map(m=>m.publicCode);check('全员房内 ID 六位且不重复',codes.every(v=>/^\d{6}$/.test(v))&&new Set(codes).size===3,codes);
 const next=await room(1,rid),nextId=next.viewer.membershipId;
 check('团队房保留分队入口',await p.getByText('需要分队？',{exact:true}).count()===1);
 for(const width of [320,390,768,1440]){await p.setViewportSize({width,height:1000});check(`成员名单 ${width}px 无横溢`,await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));const table=await p.getByRole('table').isVisible();check(`成员名单 ${width}px 采用正确布局`,width>=768?table:!table);if(width===320||width===1440)await p.screenshot({path:new URL(`roster-${width}.png`,out).pathname,fullPage:true,animations:'disabled'})}
 await p.getByText('房间管理与退出',{exact:true}).click();await p.getByLabel('转交主持人').selectOption(nextId);check('同名转交对象包含房内 ID',await p.getByRole('option',{name:new RegExp(next.viewer.publicCode)}).count()===1);
 await button(p,'转交主持人').click();await p.getByRole('dialog').waitFor();check('转交确认标明对象',await p.getByRole('dialog').innerText().then(text=>text.includes(next.viewer.publicCode)));
 await write(p,'POST',`/rooms/${rid}/host-transfers`,()=>button(p,'确认转交').click());
 const afterOld=await room(0,rid),afterNew=await room(1,rid);check('交接后旧主持失去权限，新主持可管理',!afterOld.viewer.capabilities.includes('host')&&!afterOld.viewer.capabilities.includes('manage-room')&&afterNew.viewer.capabilities.includes('host'));
 check('新主持仍保留自己的参赛身份',afterNew.viewer.role==='player'&&afterNew.viewer.capabilities.includes('play'));
 await pages[1].reload();check('刷新保留房内 ID',(await room(1,rid)).viewer.publicCode===next.viewer.publicCode);
 await contexts[1].setOffline(true);await contexts[1].setOffline(false);check('短暂断网不交接主持',(await room(1,rid)).viewer.capabilities.includes('host'));
 await leave(1,rid);const remaining=await room(0,rid);const hosts=remaining.members.filter(m=>m.capabilities.includes('host'));check('主持退出后只产生一位合格接任者',hosts.length===1&&hosts[0].membershipId!==nextId);
 await pages[1].goto(base+'/join?code='+code);await pages[1].getByLabel('显示名',{exact:true}).fill('特别周');await write(pages[1],'POST',`/rooms/${rid}/memberships`,()=>button(pages[1],'加入房间').click());check('离开后重入保持房内 ID',(await room(1,rid)).viewer.publicCode===next.viewer.publicCode);
 await leave(1,rid);await leave(2,rid);await leave(0,rid);check('最后一人可退出而不遗留控制页面',new URL(p.url()).pathname==='/');
 check('页面无未捕获异常',errors.length===0,errors);
}catch(error){checks.push({name:'execution',ok:false,detail:String(error)});console.error(error);for(let i=0;i<3;i++){await pages[i].screenshot({path:new URL(`failure-${i}.png`,out).pathname,fullPage:true}).catch(()=>{});await writeFile(new URL(`failure-${i}.txt`,out),await pages[i].locator('body').innerText().catch(()=>''))}}
finally{await writeFile(new URL('report.json',out),JSON.stringify({checks,errors,streamResponses,limits:['Three independent Chromium identities and real HTTP/SQLite. Not a load test or real mobile-device test.']},null,2));await browser.close()}
if(checks.some(v=>!v.ok))process.exitCode=1;
