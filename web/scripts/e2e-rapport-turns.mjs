/** Real UI writes, five isolated identities, no answer injection. */
import {chromium} from 'playwright';
import {mkdir,writeFile} from 'node:fs/promises';
import {findExecutionLocationWords,readPlayerSurface} from './player-copy-policy.mjs';
const base=process.env.UMA_WEB_BASE;if(!base)throw Error('UMA_WEB_BASE required');
const out=new URL('../../.cache/reports/rapport-turns/',import.meta.url);await mkdir(out,{recursive:true});
const checks=[],errors=[],requests=[],snapshots=[];
const check=(name,ok,detail)=>{checks.push({name,ok:!!ok,detail});console.log(`${ok?'PASS':'FAIL'} ${name}`);if(!ok)throw Error(name)};
const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM});
const contexts=[],pages=[];
for(let i=0;i<5;i++){const c=await browser.newContext({viewport:{width:i?390:1440,height:1000}});contexts.push(c);const p=await c.newPage();p.setDefaultTimeout(12000);p.on('pageerror',e=>errors.push(e.message));p.on('request',r=>{if(r.method()==='POST'&&new URL(r.url()).pathname.endsWith('/submissions'))requests.push({i,path:new URL(r.url()).pathname,body:r.postData(),key:r.headers()['idempotency-key']})});pages.push(p)}
const button=(p,name)=>p.getByRole('button',{name,exact:true});
async function get(i,path){const r=await contexts[i].request.get(base+'/api/v1'+path);if(!r.ok())throw Error(`${path} ${r.status()} ${await r.text()}`);return r.json()}
async function write(p,method,path,action){const wait=p.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1'+path&&r.request().method()===method);await action();const r=await wait,body=await r.json();if(!r.ok())throw Error(`${path} ${r.status()} ${JSON.stringify(body)}`);return body}
async function join(i,rid,code){const p=pages[i];await p.goto(base+'/join?code='+code);await p.getByLabel('显示名',{exact:true}).fill('特别周');await write(p,'POST',`/rooms/${rid}/memberships`,()=>button(p,'加入房间').click());await p.waitForURL(/\/rooms\/rom_/)}
async function answer(i,path){const p=pages[i];const view=await get(i,path),q=view.publicState.questions[view.publicState.currentQuestion-1];if(!q||q.answered)return;await button(p,q.options[0].label).click();await write(p,'POST',path+'/submissions',()=>button(p,q.selfIsChooser?'确认我的偏好':'确认我的预测').click())}
async function waitQuestion(i,path,n){await pages[i].waitForFunction(()=>true);for(let k=0;k<20;k++){const v=await get(i,path);if(v.publicState.currentQuestion===n)return v;await pages[i].waitForTimeout(100)}throw Error('question did not advance')}
async function shot(i,name){await pages[i].screenshot({path:new URL(name,out).pathname,fullPage:true,animations:'disabled'})}
try{
 const hp=pages[0];await hp.goto(base+'/rooms/new?mode=rapport');await hp.getByLabel('你的显示名').fill('特别周');await hp.getByLabel('最多几人参加？').fill('2');
 const made=await write(hp,'POST','/rooms',()=>button(hp,'创建好友房间').click());const rid=made.room.roomId;await hp.getByTestId('room-invite-code').waitFor();await join(1,rid,made.inviteCode);await hp.reload();
 check('普通多人不默认分队',await hp.getByText('需要分队？',{exact:true}).count()===0);
 const opened=await write(hp,'POST',`/rooms/${rid}/rounds`,()=>button(hp,'开始对局').click()),path='/rounds/'+opened.roundId;
 await hp.getByRole('heading',{name:'默契问答',exact:true}).waitFor();await pages[1].getByRole('heading',{name:'默契问答',exact:true}).waitFor();
 let views=await Promise.all([get(0,path),get(1,path)]);snapshots.push(...views);
 check('好友各自独立作答，不共用一份选择',views.every(v=>v.canSubmit)&&views.every(v=>v.publicState.questions.length===1));
 const ids=[];for(let i=0;i<2;i++)ids.push((await get(i,'/rooms/'+rid)).viewer.membershipId);
 check('同名伙伴以六位房内 ID 区分',(await hp.locator('body').innerText()).includes('房内 ID')&&new Set((await get(0,'/rooms/'+rid)).members.map(m=>m.publicCode)).size===2);
 for(const width of [320,390,768,1440]){await hp.setViewportSize({width,height:1000});check(`当前题 ${width}px 无横溢`,await hp.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));check(`当前题 ${width}px 选项至少44px`,await hp.getByRole('button',{name:views[0].publicState.questions[0].options[0].label,exact:true}).evaluate(b=>b.getBoundingClientRect().height>=44));if(width===320||width===1440)await shot(0,'rapport-'+width+'.png')}
 const q=views[0].publicState.questions[0],before=requests.length;
 await button(hp,q.options[0].label).click();check('选中后还需明确确认',requests.length===before);
 let committed=0;await hp.route(base+'/api/v1'+path+'/submissions',async route=>{if(route.request().method()!=='POST')return route.continue();const res=await route.fetch();committed=res.status();await res.body();await route.abort('failed')},{times:1});
 await button(hp,q.selfIsChooser?'确认我的偏好':'确认我的预测').click();await button(hp,'确认刚才的选择').waitFor();
 check('保存后响应丢失，冻结后续选择',committed===201&&await button(hp,q.options[0].label).isDisabled());
 views=await Promise.all([get(0,path),get(1,path)]);check('单人已交不提前揭晓，另一人仍可答',!views[0].canSubmit&&views[1].canSubmit&&!views[1].publicState.questions[0].reveal&&!views[1].privateState.selectedOptionId);
 await answer(1,path);await waitQuestion(1,path,2);await hp.reload();await button(hp,'确认刚才的选择').waitFor();check('换题刷新不自动重发旧选择',requests.length===before+2);
 await button(hp,'确认刚才的选择').click();await button(hp,'确认刚才的选择').waitFor({state:'detached'});
 const mine=requests.filter(r=>r.i===0);check('换题后原确认不回答下一题',JSON.stringify(mine[0])===JSON.stringify(mine[1])&&(await get(0,path)).submissionRevision===1&&(await get(0,path)).publicState.currentQuestion===2);
 views=await Promise.all([get(0,path),get(1,path)]);check('全员确认后统一揭晓并轮换选择者',views.every(v=>!!v.publicState.questions[0].reveal)&&views[0].publicState.questions[0].chooserId!==views[0].publicState.questions[1].chooserId);snapshots.push(...views);
 for(let n=2;n<=views[0].publicState.questionCount;n++){for(let i=0;i<2;i++)await answer(i,path);if(n<views[0].publicState.questionCount)await waitQuestion(0,path,n+1)}
 await hp.getByRole('heading',{name:'本局成绩',exact:true}).waitFor();const end=await get(0,path);snapshots.push(end);check('好友小组完成全局并共享真实100分',end.results.length===2&&end.results.every(s=>s.scoreUnits===10000));
 for(const width of [320,390,768,1440]){await hp.setViewportSize({width,height:1000});check(`成绩 ${width}px 正确切换表格和卡片`,await hp.getByRole('table',{name:'默契问答成绩'}).isVisible()===(width>=768));check(`成绩 ${width}px 无横溢`,await hp.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))}
 await shot(0,'rapport-final-1440.png');
 // Activity host plus four competing identities. All team changes via room UI.
 await hp.goto(base+'/events/new?mode=rapport');await hp.getByLabel('活动名称').fill('学园默契杯');const ev=await write(hp,'POST','/events',()=>button(hp,'创建并进入主持台').click());const eventId=ev.eventId,eventRoom=ev.roomId;await hp.waitForURL(/\/host$/);const info=await get(0,'/rooms/'+eventRoom);
 for(let i=1;i<5;i++)await join(i,eventRoom,info.inviteCode);
 await hp.goto(base+'/rooms/'+eventRoom);await hp.getByText('需要分队？',{exact:true}).click();
 for(const label of ['胡萝卜队','冲刺队']){if(label==='冲刺队')await hp.getByText('新建队伍',{exact:true}).first().click();await hp.getByLabel('队名',{exact:true}).fill(label);await write(hp,'POST',`/rooms/${eventRoom}/teams`,()=>button(hp,'新建队伍').click())}
 for(let i=1;i<5;i++){const s=await get(i,'/rooms/'+eventRoom),label=i<=2?'胡萝卜队':'冲刺队';const item=hp.getByRole('listitem').filter({has:hp.locator('p').filter({hasText:new RegExp('^房内 ID '+s.viewer.publicCode+'$')})}).filter({has:button(hp,`加入「${label}」`)});await write(hp,'PATCH',`/rooms/${eventRoom}/memberships/${s.viewer.membershipId}`,()=>item.getByRole('button',{name:`加入「${label}」`}).click())}
 await hp.goto(base+`/events/${eventId}/host`);await hp.getByLabel('玩法',{exact:true}).selectOption('rapport');await hp.getByLabel('分项名称',{exact:true}).fill('默契决赛');await hp.getByLabel('分项分值上限',{exact:true}).fill('100');await hp.getByLabel('回合数',{exact:true}).fill('1');await write(hp,'POST',`/events/${eventId}/segments`,()=>button(hp,'添加分项').click());
 for(const name of ['开放活动','开始活动'])await write(hp,'POST',`/events/${eventId}/transitions`,()=>button(hp,name).click());
 const row=hp.getByRole('region',{name:'节目单'}).getByRole('listitem').filter({hasText:'默契决赛'}),r=await write(hp,'POST',`/rooms/${eventRoom}/rounds`,()=>button(row,'开局').click()),ep='/rounds/'+r.roundId;
 const controls=hp.getByTestId('event-round-controls');for(const name of ['讲解本局规则','开始作答'])await write(hp,'POST',ep+'/transitions',()=>button(controls,name).click());
 for(let i=1;i<5;i++)await pages[i].getByRole('heading',{name:'默契问答',exact:true}).waitFor();
 let teamViews=await Promise.all([1,2,3,4].map(i=>get(i,ep)));snapshots.push(...teamViews,await get(0,ep));
 check('活动四位选手都能答，主持不能代答',teamViews.every(v=>v.canSubmit)&&!(await get(0,ep)).canSubmit&&teamViews.every(v=>!v.teamControllers?.length));
 await answer(1,ep);await answer(2,ep);check('一队交齐也不提前揭晓',!(await get(1,ep)).publicState.questions[0].reveal&&(await get(3,ep)).publicState.currentQuestion===1);
 for(let i=3;i<5;i++)await answer(i,ep);await waitQuestion(1,ep,2);
 check('不同队有各自选择者且同场换题',(await get(1,ep)).publicState.questions[1].chooserId!==(await get(3,ep)).publicState.questions[1].chooserId);
 for(let n=2;n<=teamViews[0].publicState.questionCount;n++){for(let i=1;i<5;i++)await answer(i,ep);if(n<teamViews[0].publicState.questionCount)await waitQuestion(1,ep,n+1)}
 await pages[1].getByRole('heading',{name:'本局成绩',exact:true}).waitFor();const te=await get(1,ep);snapshots.push(te,await get(0,ep));check('活动结算为两队成绩并进入活动榜单',te.results.length===2&&te.results.every(s=>s.scoreUnits===10000)&&(await get(0,`/events/${eventId}/standings`)).items.length===2);
 check('终局收起等待提示与空倒计时',await pages[1].locator('body').innerText().then(t=>!t.includes('--:--')&&!t.includes('刚才的选择已记录。大家确认后，一起看答案。')));await shot(1,'rapport-team-final-390.png');check('没有执行位置标签',findExecutionLocationWords(await readPlayerSurface(pages[1])).length===0);check('页面无未捕获异常',errors.length===0,errors);
}catch(error){checks.push({name:'execution',ok:false,detail:String(error)});console.error(error);for(let i=0;i<5;i++){await shot(i,`failure-${i}.png`).catch(()=>{});await writeFile(new URL(`failure-${i}.txt`,out),await pages[i].locator('body').innerText().catch(()=>''))}}
finally{await writeFile(new URL('report.json',out),JSON.stringify({checks,errors,limits:['Real Chromium/Go/SQLite. Not a capacity or real-mobile-device test.']},null,2));await writeFile(new URL('snapshots.json',out),JSON.stringify(snapshots,null,2));await browser.close()}
if(checks.some(c=>!c.ok))process.exitCode=1;
