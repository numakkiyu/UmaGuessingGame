/** Six independent identities, actual Go/SQLite. All mutations go through UI. */
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import { findExecutionLocationWords, readPlayerSurface } from './player-copy-policy.mjs';
const base=process.env.UMA_WEB_BASE;if(!base)throw new Error('Set isolated UMA_WEB_BASE');
const out=new URL('../../.cache/reports/clue-teams/',import.meta.url);await mkdir(out,{recursive:true});
const checks=[],errors=[],requests=[];
function check(name,ok,detail){checks.push({name,ok:!!ok,detail});console.log(`${ok?'PASS':'FAIL'} ${name}`);if(!ok)throw new Error(name)}
const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM});
const contexts=[],pages=[];
for(let i=0;i<6;i++){const c=await browser.newContext({viewport:{width:i?390:1440,height:1000}});contexts.push(c);const p=await c.newPage();p.setDefaultTimeout(16000);p.on('pageerror',e=>errors.push(e.message));p.on('request',r=>{if(r.method()==='POST'&&new URL(r.url()).pathname.endsWith('/submissions'))requests.push({path:new URL(r.url()).pathname,body:r.postData(),key:r.headers()['idempotency-key']})});pages.push(p)}
const hp=pages[0],names=['特别周','特别周','小栗帽','玉藻十字','黄金船','东海帝王'];
const button=(p,name)=>p.getByRole('button',{name,exact:true});
async function get(i,path){const r=await contexts[i].request.get(base+'/api/v1'+path);if(!r.ok())throw new Error(`${path}: ${r.status()} ${await r.text()}`);return r.json()}
async function write(p,method,path,action){const wait=p.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1'+path&&r.request().method()===method);await action();const r=await wait;const body=await r.json();if(!r.ok())throw new Error(`${path} ${r.status()} ${JSON.stringify(body)}`);return body}
async function shot(p,name){await p.screenshot({path:new URL(name,out).pathname,fullPage:true,animations:'disabled'})}
async function assignAll(p,cards){for(let i=0;i<cards.length;i++){await p.getByRole('button',{name:/选择位置/}).nth(i).click();await p.getByRole('button',{name:'选择'+cards[i].label,exact:true}).filter({visible:true}).click()}}
try{
 await hp.goto(base+'/rooms/new?mode=clue-puzzle');await hp.getByLabel('你的显示名').fill(names[0]);await hp.getByLabel('最多几人参加？').fill('6');
 const made=await write(hp,'POST','/rooms',()=>button(hp,'创建好友房间').click()),rid=made.room.roomId;
 await hp.getByTestId('room-invite-code').waitFor();
 for(let i=1;i<6;i++){const p=pages[i];await p.goto(base+'/join?code='+made.inviteCode);await p.getByLabel('显示名',{exact:true}).fill(names[i]);await write(p,'POST',`/rooms/${rid}/memberships`,()=>button(p,'加入房间').click());await p.waitForURL(/\/rooms\/rom_/)}
 await hp.reload();await hp.getByText('需要分队？',{exact:true}).click();
 for(const label of ['胡萝卜队','冲刺队']){if(label==='冲刺队')await hp.getByText('新建队伍',{exact:true}).first().click();await hp.getByLabel('队名',{exact:true}).fill(label);await write(hp,'POST',`/rooms/${rid}/teams`,()=>button(hp,'新建队伍').click())}
 const ids=[];
 for(let i=0;i<6;i++){const s=await get(i,'/rooms/'+rid);ids.push(s.viewer.membershipId);const item=hp.getByRole('listitem').filter({has:hp.locator('p').filter({hasText:new RegExp('^房内 ID '+s.viewer.publicCode+'$')})}).filter({has:hp.getByRole('button',{name:`加入「${i<3?'胡萝卜队':'冲刺队'}」`})});await write(hp,'PATCH',`/rooms/${rid}/memberships/${s.viewer.membershipId}`,()=>item.getByRole('button',{name:`加入「${i<3?'胡萝卜队':'冲刺队'}」`}).click())}
 const round=await write(hp,'POST',`/rooms/${rid}/rounds`,()=>button(hp,'开始对局').click()),path='/rounds/'+round.roundId;
 for(const p of pages)await p.getByRole('heading',{name:'团队情报拼图',exact:true}).waitFor();
 check('六位玩家按两队开局，每队三人',(await get(0,'/rooms/'+rid)).members.filter(m=>m.role==='team-captain').length===2);
 const views=await Promise.all(pages.map((_,i)=>get(i,path)));
 check('每人获得私人线索，开局只有两位队长可提交',views.every(v=>v.publicState.privateClues.length>0)&&views.filter(v=>v.canSubmit).length===2);
 for(let team=0;team<2;team++){const all=views.slice(team*3,team*3+3).flatMap(v=>v.publicState.privateClues.map(c=>c.constraintId));check(`第${team+1}队线索按人分开`,new Set(all).size===all.length)}
 check('两队收到相同完整线索集合',JSON.stringify(views.slice(0,3).flatMap(v=>v.publicState.privateClues.map(c=>c.constraintId)).sort())===JSON.stringify(views.slice(3).flatMap(v=>v.publicState.privateClues.map(c=>c.constraintId)).sort()));
 check('线索不展示档案内部编号',views.every(v=>v.publicState.privateClues.every(c=>!c.text.includes('dc-'))));
 check('队员不能编辑，队长能编辑',await pages[1].getByRole('button',{name:/选择位置/}).first().isDisabled()&&await hp.getByRole('button',{name:/选择位置/}).first().isEnabled());
 for(const width of [320,390,768,1440]){await hp.setViewportSize({width,height:1000});check(`${width}px 无横溢`,await hp.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));check(`${width}px 候选表格与卡片正确切换`,await hp.getByRole('region',{name:'候选档案'}).getByRole('table').isVisible()===(width>=768));check(`${width}px 位置触摸区足够`,await hp.getByRole('button',{name:/选择位置/}).evaluateAll(bs=>bs.every(b=>b.getBoundingClientRect().height>=44)));if(width===320||width===1440)await shot(hp,'clue-'+width+'.png')}
 await hp.setViewportSize({width:1440,height:1000});
 const cards=views[0].publicState.cards;
 await assignAll(hp,cards);
 const before=requests.length;let committed;
 await hp.route(base+'/api/v1'+path+'/submissions',async route=>{if(route.request().method()!=='POST')return route.continue();const res=await route.fetch();committed=res.status();await res.body();await route.abort('failed')},{times:1});
 await button(hp,'确认这份分配').click();await button(hp,'确认刚才的分配').waitFor();
 check('分配已保存而响应丢失后禁止继续摆卡',committed===201&&await hp.getByRole('button',{name:/选择位置/}).first().isDisabled());
 const after=await get(0,path),ownTeam=(await get(0,'/rooms/'+rid)).viewer.teamId,controller=after.teamControllers.find(c=>c.teamId===ownTeam);
 check('确认完整分配后代表换人',controller.membershipId!==ids[0]);
 await hp.reload();await button(hp,'确认刚才的分配').waitFor();check('刷新不自动重发',requests.length===before+1);
 await button(hp,'确认刚才的分配').click();await button(hp,'确认刚才的分配').waitFor({state:'detached'});
 check('确认使用相同原请求，不重复分配或交棒',JSON.stringify(requests[before])===JSON.stringify(requests[before+1])&&(await get(0,path)).submissionRevision===1&&(await get(0,path)).teamControllers.find(c=>c.teamId===controller.teamId).membershipId===controller.membershipId);
 const next=ids.indexOf(controller.membershipId);await pages[next].reload();await pages[next].getByRole('button',{name:'确认这份分配',exact:true}).waitFor();
 check('新代表继承本队已确认分配',(await get(next,path)).publicState.self.assignment&&await button(pages[next],'确认这份分配').isEnabled());
 const opponent=await get(3,path);check('其他队没有收到本队分配',!opponent.publicState.teamDraft&&Object.keys(opponent.publicState.self.assignment).length===0&&opponent.submissionRevision===0);
 await assignAll(pages[3],[...cards].reverse());await write(pages[3],'POST',path+'/submissions',()=>button(pages[3],'确认这份分配').click());
 check('另一队可以独立确认且各自记录不同答案',JSON.stringify((await get(3,path)).publicState.self.assignment)!==JSON.stringify((await get(0,path)).publicState.self.assignment)&&(await get(0,path)).submissionRevision===1);
 await shot(pages[next],'clue-next-representative-390.png');
 check('无执行位置标签',findExecutionLocationWords(await readPlayerSurface(hp)).length===0);
 check('页面无未捕获异常',errors.length===0,errors);
}catch(error){console.error(error);checks.push({name:'execution',ok:false,detail:String(error)});await shot(hp,'failure.png').catch(()=>{});await writeFile(new URL('failure.txt',out),await hp.locator('body').innerText().catch(()=>''))}
finally{await writeFile(new URL('report.json',out),JSON.stringify({checks,errors,limits:['Six Chromium identities against isolated Go/SQLite. All mutations through UI. Not a capacity or physical-device test; terminal scoring separately covered by Go/HTTP.']},null,2));await browser.close()}
if(checks.some(c=>!c.ok))process.exitCode=1;
