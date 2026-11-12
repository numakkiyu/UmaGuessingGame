/** Real UI programme: create, invite, continuous rounds, switch, display and close. */
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import { findExecutionLocationWords, readPlayerSurface } from './player-copy-policy.mjs';
const base=process.env.UMA_WEB_BASE;
if(!base)throw new Error('Set UMA_WEB_BASE to an isolated instance');
const output=new URL('../../.cache/reports/event-rounds/',import.meta.url);
await mkdir(output,{recursive:true});
const checks=[],errors=[],rounds=[];
function check(name,ok,detail){checks.push({name,ok:Boolean(ok),detail});console.log(`${ok?'PASS':'FAIL'} ${name}`);if(!ok)throw new Error(name);}
const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM});
const host=await browser.newContext({viewport:{width:1440,height:1000}}),guest=await browser.newContext({viewport:{width:390,height:844}}),display=await browser.newContext({viewport:{width:1920,height:1080}});
const page=await host.newPage(),player=await guest.newPage(),screen=await display.newPage();
for(const p of [page,player,screen])p.on('pageerror',e=>errors.push(e.message));
const button=(p,name)=>p.getByRole('button',{name,exact:true});
async function get(path){const r=await host.request.get(`${base}/api/v1${path}`);if(!r.ok())throw new Error(`Read ${path}: ${r.status()} ${await r.text()}`);return r.json();}
async function clickWrite(p,name,path,scope=p){const [r]=await Promise.all([p.waitForResponse(r=>r.url().endsWith(`/api/v1${path}`)&&r.request().method()==='POST'),button(scope,name).click()]);const data=await r.json();check(`UI ${name}: accepted`,r.status()===201,{status:r.status(),code:data.code});return data;}
async function shot(p,name){await p.screenshot({path:new URL(`${name}.png`,output).pathname,fullPage:true,animations:'disabled'});}
try{
 await page.goto(base);await page.getByRole('link',{name:'创建活动',exact:true}).click();await page.getByLabel('活动名称',{exact:true}).fill('学园周末娱乐杯');
 const made=page.waitForResponse(r=>r.url().endsWith('/api/v1/events')&&r.request().method()==='POST');await button(page,'创建并进入主持台').click();const event=await(await made).json();const eventId=event.eventId;
 await page.getByRole('heading',{name:'学园周末娱乐杯',exact:true}).waitFor();check('Lobby creates real programme through normal controls',Boolean(eventId));
 const current=await get(`/events/${eventId}`),roomId=current.roomId;const room=await get(`/rooms/${roomId}`);
 check('Organiser hosts without a competing player seat',room.viewer.role==='host',room.viewer);
 const code=await page.getByTestId('event-invite-code').innerText();await player.goto(`${base}/join?code=${code}`);await player.getByLabel('显示名',{exact:true}).fill('特别来宾');await clickWrite(player,'加入房间',`/rooms/${roomId}/memberships`);await player.getByText('本场项目：经典弗一把',{exact:true}).waitFor();
 await page.getByLabel('玩法',{exact:true}).selectOption('sort');await page.getByLabel('分项名称',{exact:true}).fill('档案排序');await page.getByLabel('分项分值上限',{exact:true}).fill('60');await page.getByLabel('回合数',{exact:true}).fill('2');
 const segment=await clickWrite(page,'添加分项',`/events/${eventId}/segments`);await page.getByRole('region',{name:'节目单'}).getByText('档案排序',{exact:true}).waitFor();
 await page.getByLabel('玩法',{exact:true}).selectOption('classic');await page.getByLabel('分项名称',{exact:true}).fill('经典热身');await page.getByLabel('分项分值上限',{exact:true}).fill('40');await page.getByLabel('回合数',{exact:true}).fill('1');
 await clickWrite(page,'添加分项',`/events/${eventId}/segments`);
 await clickWrite(page,'开放活动',`/events/${eventId}/transitions`);await clickWrite(page,'开始活动',`/events/${eventId}/transitions`);
 const programme=page.getByRole('region',{name:'节目单'});
 const segmentRow=programme.getByRole('listitem').filter({hasText:'档案排序'});
 for(let n=1;n<=2;n++){
  const r=await clickWrite(page,n===1?'开局':'继续下一局',`/rooms/${roomId}/rounds`,segmentRow);rounds.push(r.roundId);
  check(`Round ${n} retains configured mode`,r.modeId==='sort');
  const controls=page.getByTestId('event-round-controls');await button(controls,'讲解本局规则').waitFor();await clickWrite(page,'讲解本局规则',`/rounds/${r.roundId}/transitions`,controls);await clickWrite(page,'开始作答',`/rounds/${r.roundId}/transitions`,controls);
  await player.getByRole('heading',{name:/^按.+排序$/}).waitFor();const board=player.getByRole('list',{name:'当前排序，从上到下'});await board.waitFor();
  if(n===1){while(await board.getByRole('button',{name:/与下一名合并为并列/}).count()>0){await board.getByRole('button',{name:/与下一名合并为并列/}).first().click();}}
  const submitted=await clickWrite(player,'最终提交',`/rounds/${r.roundId}/submissions`);check(`Round ${n} confirms actual player's answer`,Boolean(submitted));
  if(n===1){await clickWrite(page,'暂停比赛',`/rounds/${r.roundId}/transitions`,controls);await button(controls,'继续比赛').waitFor();check('Pause remains visible to host',await button(controls,'继续比赛').isVisible());await clickWrite(page,'继续比赛',`/rounds/${r.roundId}/transitions`,controls);await clickWrite(page,'加时 30 秒',`/rounds/${r.roundId}/transitions`,controls);}
  await button(controls,'收卷').click();await clickWrite(page,'确认收卷',`/rounds/${r.roundId}/transitions`,page.getByRole('dialog'));await clickWrite(page,'揭晓答案',`/rounds/${r.roundId}/transitions`,controls);await clickWrite(page,'结算本局',`/rounds/${r.roundId}/transitions`,controls);
  await segmentRow.getByText(`${n} / 2 局完成`,{exact:false}).waitFor();const state=await get(`/events/${eventId}`);const detail=state.segmentDetails.find(s=>s.segmentId===segment.segmentId);
  check(`Round ${n} progression persists`,detail.completedRounds===n&&detail.status===(n===1?'running':'completed'),detail);
  await page.reload();await page.getByRole('heading',{name:'学园周末娱乐杯',exact:true}).waitFor();await segmentRow.getByText(`${n} / 2 局完成`,{exact:false}).waitFor();check(`Round ${n} reload retains programme progress`,true);
 }
 const raw=await Promise.all(rounds.map(id=>get(`/rounds/${id}`)));const roundResults=raw.map(r=>r.results[0]);const expected=Math.floor(roundResults.reduce((v,r)=>v+r.scoreUnits/r.maxUnits,0)*3000+1e-8);const standings=await get(`/events/${eventId}/standings`);
 check('Two real round results aggregate once against 60 point budget',standings.items.length===1&&standings.items[0].scoreUnits===expected,{roundResults,expected,actual:standings.items});
 check('Host does not receive a score',standings.items[0].displayName==='特别来宾');
 await page.getByLabel('分项',{exact:true}).selectOption(segment.segmentId);check('Correction respects selected budget and fractional scores',await page.getByLabel('更正为（分）',{exact:true}).getAttribute('max')==='60'&&await page.getByLabel('更正为（分）',{exact:true}).getAttribute('step')==='0.01');
 const classicRow=programme.getByRole('listitem').filter({hasText:'经典热身'});const classic=await clickWrite(page,'开局',`/rooms/${roomId}/rounds`,classicRow);check('Switch to next saved programme mode',classic.modeId==='classic');
 const controls=page.getByTestId('event-round-controls');await clickWrite(page,'讲解本局规则',`/rounds/${classic.roundId}/transitions`,controls);await clickWrite(page,'开始作答',`/rounds/${classic.roundId}/transitions`,controls);await player.getByRole('combobox').first().waitFor();
 await button(controls,'收卷').click();await clickWrite(page,'确认收卷',`/rounds/${classic.roundId}/transitions`,page.getByRole('dialog'));await clickWrite(page,'揭晓答案',`/rounds/${classic.roundId}/transitions`,controls);await clickWrite(page,'结算本局',`/rounds/${classic.roundId}/transitions`,controls);
 await classicRow.getByText('本项已完成',{exact:true}).waitFor();
 check('Running programme cannot accidentally add items',await button(page,'添加分项').count()===0);
 const grant=await clickWrite(page,'生成大屏配对码',`/events/${eventId}/display-grants`);await screen.goto(`${base}/events/${eventId}/display`);await screen.getByLabel('配对码',{exact:true}).fill(grant.grantCode);await button(screen,'打开大屏').click();await screen.getByText('特别来宾',{exact:true}).first().waitFor();check('Independent display pairs and receives real standings',true);await shot(screen,'display-1920');await screen.reload();await screen.getByText('特别来宾',{exact:true}).first().waitFor();check('Display grant survives refresh',true);
 for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:1000});check(`Host ${width}px no horizontal overflow`,await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));if(width<1024)check(`Host ${width}px current round comes before general actions`,await page.getByTestId('event-round-controls').evaluate(el=>el.getBoundingClientRect().top<document.querySelector('[aria-labelledby=controls-heading]').getBoundingClientRect().top));if(width===320||width===1440)await shot(page,`host-${width}`);}
 for(const p of [page,player,screen])check(`Player surface hides execution location: ${new URL(p.url()).pathname}`,findExecutionLocationWords(await readPlayerSurface(p)).length===0);
 await button(page,'结束活动').click();await clickWrite(page,'结束活动',`/events/${eventId}/transitions`,page.getByRole('dialog'));check('Entire programme completes through UI',(await get(`/events/${eventId}`)).status==='completed');
 check('No uncaught browser errors',errors.length===0,errors);
}catch(error){checks.push({name:'execution',ok:false,detail:String(error)});console.error(error);await shot(page,'failure').catch(()=>{});await writeFile(new URL('failure.txt',output),await page.locator('body').innerText().catch(()=>''));await writeFile(new URL('player-failure.txt',output),await player.locator('body').innerText().catch(()=>''));}
finally{await writeFile(new URL('report.json',output),JSON.stringify({checks,errors,rounds,limits:['All writes through real controls. Read-only API checks provide arithmetic evidence; no claim of load or real hardware verification.']},null,2));await browser.close();}
if(checks.some(c=>!c.ok))process.exitCode=1;
