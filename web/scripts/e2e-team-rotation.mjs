import { setPlayerName } from "./player-profile.mjs";
/** Four real browser identities, real Go/SQLite. Every mutation uses UI controls. */
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import { findExecutionLocationWords, readPlayerSurface } from './player-copy-policy.mjs';
const base=process.env.UMA_WEB_BASE;if(!base)throw new Error('Set an isolated UMA_WEB_BASE');
const output=new URL('../../.cache/reports/team-rotation/',import.meta.url);await mkdir(output,{recursive:true});
const checks=[],errors=[];function check(name,ok,detail){checks.push({name,ok:!!ok,detail});console.log(`${ok?'PASS':'FAIL'} ${name}`);if(!ok)throw new Error(name)}
const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM});
const contexts=[],pages=[];
for(let i=0;i<4;i++){const c=await browser.newContext({viewport:i===0?{width:1440,height:1000}:{width:390,height:844}});contexts.push(c);const p=await c.newPage();p.setDefaultTimeout(12000);p.on('pageerror',e=>errors.push(e.message));pages.push(p)}
const hp=pages[0],names=['特别周','特别周','小栗帽','玉藻十字'];
const button=(p,name)=>p.getByRole('button',{name,exact:true});
async function get(i,path){const r=await contexts[i].request.get(base+'/api/v1'+path);if(!r.ok())throw new Error(`${path} ${r.status()} ${await r.text()}`);return r.json()}
async function write(p,method,path,action){const wait=p.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1'+path&&r.request().method()===method);await action();const r=await wait,body=await r.json();if(!r.ok())throw new Error(`${method} ${path} ${r.status()} ${JSON.stringify(body)}`);return body}
async function waitControl(i,mine){await pages[i].getByRole('status').filter({hasText:mine?'轮到你确认全队作答':/轮到.+确认作答/}).waitFor()}
async function create(mode){
 await hp.goto(base+'/rooms/new?mode='+mode);await setPlayerName(hp, names[0]);await hp.getByLabel('最多几人参加？').fill('4');
 const data=await write(hp,'POST','/rooms',()=>button(hp,'创建好友房间').click());const rid=data.room.roomId;
 await hp.getByTestId('room-invite-code').waitFor();
 for(let i=1;i<4;i++){const p=pages[i];await p.goto(base+'/join?code='+data.inviteCode);await setPlayerName(p, names[i]);await write(p,'POST',`/rooms/${rid}/memberships`,()=>button(p,'加入房间').click());await p.waitForURL(/\/rooms\/rom_/)}
 await hp.reload();await hp.getByText('需要分队？',{exact:true}).click();
 for(const label of ['胡萝卜队','冲刺队']){if(label==='冲刺队')await hp.getByText('新建队伍',{exact:true}).first().click();await hp.getByLabel('队名',{exact:true}).fill(label);await write(hp,'POST',`/rooms/${rid}/teams`,()=>button(hp,'新建队伍').click())}
 const ids=[];
 for(let i=0;i<4;i++){const s=await get(i,'/rooms/'+rid);ids.push(s.viewer.membershipId);const entry=hp.getByRole('listitem',{name:new RegExp(s.viewer.displayName+'，第')});const team=i<2?'胡萝卜队':'冲刺队';await write(hp,'PATCH',`/rooms/${rid}/memberships/${s.viewer.membershipId}`,()=>entry.getByRole('button',{name:`加入「${team}」`}).click());await hp.getByRole('region',{name:'队伍：'+team}).getByRole('listitem',{name:new RegExp(s.viewer.displayName+'，第')}).waitFor()}
 const ready=await get(0,'/rooms/'+rid);check(mode+'：四人按两队参赛，每队一个队长',ready.members.length===4&&ready.members.filter(m=>m.role==='team-captain').length===2&&new Set(ready.members.map(m=>m.teamId)).size===2);
 const round=await write(hp,'POST',`/rooms/${rid}/rounds`,()=>button(hp,'开始对局').click());
 for(const p of pages)await p.getByRole('region',{name:'队伍作答安排'}).waitFor();
 check(mode+'：初始代表为两位队长',(await get(0,'/rounds/'+round.roundId)).teamControllers.every(c=>[ids[0],ids[2]].includes(c.membershipId)));
 return {rid,roundId:round.roundId,ids};
}
async function flip(i,rid){
 const p=pages[i],board=p.getByRole('group',{name:'4 行 4 列记忆牌面'}),before=await get(i,'/rounds/'+rid);
 const refreshed=p.waitForResponse(async r=>new URL(r.url()).pathname==='/api/v1/rounds/'+rid&&r.request().method()==='GET'&&r.ok()&&(await r.json()).submissionRevision>before.submissionRevision);
 const result=await write(p,'POST',`/rounds/${rid}/submissions`,()=>board.getByRole('button',{name:/未翻开/}).first().click());await refreshed;
 await p.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));return result;
}
async function act(i,roundId,turn){
 const p=pages[i];await p.getByText(`第 ${turn} / 8 回合`,{exact:true}).waitFor();await waitControl(i,true);
 const section=p.getByRole('region',{name:'选一个行动'});await section.getByRole('button',{name:/稳进/}).click();
 const result=await write(p,'POST',`/rounds/${roundId}/submissions`,()=>button(p,'提交这个回合的行动').click());
 await p.getByRole('status').filter({hasText:'轮到'}).waitFor();
 await p.waitForFunction(turn=>document.body.innerText.includes('行动已确认，等待本回合揭晓')||document.body.innerText.includes(`第 ${turn+1} / 8 回合`)||document.body.innerText.includes('比赛已结束。'),turn);
 return result;
}
try{
 const memory=await create('memory');const first=await get(0,'/rounds/'+memory.roundId);
 check('好友翻牌保留共同观看窗口',first.round.phase==='briefing'&&Date.parse(first.round.deadlineAt)>Date.now());
 const before=first.round.deadlineAt;await pages[1].reload();await pages[1].getByRole('group',{name:'4 行 4 列记忆牌面'}).waitFor();
 check('观看中刷新不会多给时间',(await get(1,'/rounds/'+memory.roundId)).round.deadlineAt===before);
 check('观看中所有翻牌按钮禁用',await pages[1].getByRole('group',{name:'4 行 4 列记忆牌面'}).getByRole('button').evaluateAll(bs=>bs.every(b=>b.disabled)));
 await hp.getByRole('group',{name:'4 行 4 列记忆牌面'}).getByRole('button',{name:/未翻开/}).first().waitFor();
 await flip(0,memory.roundId);check('第一张由原代表继续',(await get(0,'/rounds/'+memory.roundId)).teamControllers.find(c=>c.membershipId===memory.ids[0])?.generation===0);
 await flip(0,memory.roundId);await waitControl(1,true);await waitControl(0,false);
 let v=await get(1,'/rounds/'+memory.roundId);check('第二张完成后自动接棒并保留队伍修订',v.canSubmit&&v.controlGeneration===1&&v.submissionRevision===2&&v.publicState.self.attempts===1);
 check('旧代表翻牌按钮禁用',await hp.getByRole('group',{name:'4 行 4 列记忆牌面'}).getByRole('button').evaluateAll(bs=>bs.every(b=>b.disabled)));
 await pages[1].reload();await waitControl(1,true);check('代表刷新仍有同队进度',(await get(1,'/rounds/'+memory.roundId)).submissionRevision===2);
 await flip(1,memory.roundId);await flip(1,memory.roundId);await waitControl(0,true);
 check('下一次完整尝试轮回第一位代表',(await get(0,'/rounds/'+memory.roundId)).controlGeneration===2);
 await hp.getByText('安排队伍代表',{exact:true}).click();await hp.getByLabel('本次代表（胡萝卜队）').selectOption(memory.ids[1]);await write(hp,'POST',`/rooms/${memory.rid}/control-leases`,()=>button(hp,'交给胡萝卜队选定的队员').click());await waitControl(1,true);
 check('主持可按真实队伍手动交棒',(await get(1,'/rounds/'+memory.roundId)).controlGeneration===3);
 for(const width of [320,390,768,1440]){await hp.setViewportSize({width,height:1000});const layout=await hp.evaluate(()=>({overflow:document.documentElement.scrollWidth>innerWidth,touch:[...document.querySelectorAll('[aria-label="4 行 4 列记忆牌面"] button')].every(b=>b.getBoundingClientRect().width>=44&&b.getBoundingClientRect().height>=44)}));check(`翻牌 ${width}px 排版与触摸区`,!layout.overflow&&layout.touch,layout);if(width===320||width===1440)await hp.screenshot({path:new URL(`memory-${width}.png`,output).pathname,fullPage:true,animations:'disabled'})}
 const race=await create('strategy-race');await waitControl(0,true);await waitControl(2,true);
 await act(0,race.roundId,1);check('策略赛单队提交不提前换人',(await get(0,'/rounds/'+race.roundId)).controlGeneration===0);
 await act(2,race.roundId,1);await waitControl(1,true);await waitControl(3,true);
 v=await get(1,'/rounds/'+race.roundId);check('两队揭晓后同时轮换驾驶员',v.teamControllers.every(c=>[race.ids[1],race.ids[3]].includes(c.membershipId)&&c.generation===1)&&v.publicState.currentTurn===2);
 await pages[1].reload();await waitControl(1,true);check('策略赛刷新保留共同作答修订',(await get(1,'/rounds/'+race.roundId)).submissionRevision===1);
 await act(1,race.roundId,2);await act(3,race.roundId,2);await waitControl(0,true);await waitControl(2,true);
 check('策略赛第三回合轮回原代表',(await get(0,'/rounds/'+race.roundId)).controlGeneration===2);
 for(const width of [320,390,768,1440]){await hp.setViewportSize({width,height:1000});check(`策略赛 ${width}px 无横溢`,await hp.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));if(width===320||width===1440)await hp.screenshot({path:new URL(`strategy-${width}.png`,output).pathname,fullPage:true,animations:'disabled'})}
 for(let turn=3;turn<=8;turn++){const a=turn%2?0:1,b=turn%2?2:3;await act(a,race.roundId,turn);await act(b,race.roundId,turn);if(turn<8){await waitControl(a===0?1:0,true);await waitControl(b===2?3:2,true)}}
 await hp.getByText('比赛已结束。',{exact:true}).waitFor();v=await get(0,'/rounds/'+race.roundId);check('八回合结束保留最终代表',v.teamControllers.every(c=>c.generation===7)&&['locked','revealed','settled'].includes(v.round.phase));
 for(let attempt=0;attempt<16&&v.round.phase!=='settled';attempt++){await new Promise(resolve=>setTimeout(resolve,500));v=await get(0,'/rounds/'+race.roundId)}
 check('自动结算只有两支队伍成绩',v.round.phase==='settled'&&v.results?.length===2&&v.results.every(r=>r.subjectType==='team'));
 for(const p of pages)check('玩家文字不展示执行位置',findExecutionLocationWords(await readPlayerSurface(p)).length===0);
 check('页面无未捕获异常',errors.length===0,errors);
}catch(e){checks.push({name:'execution',ok:false,detail:String(e)});console.error(e);await hp.screenshot({path:new URL('failure.png',output).pathname,fullPage:true}).catch(()=>{});for(let i=0;i<4;i++)await writeFile(new URL(`failure-${i}.txt`,output),await pages[i].locator('body').innerText().catch(()=>''))}
finally{await writeFile(new URL('report.json',output),JSON.stringify({checks,errors,limits:['Four independent real identities; UI writes and read-only evidence. Not a capacity test or real mobile hardware test.']},null,2));await browser.close()}
if(checks.some(c=>!c.ok))process.exitCode=1;
