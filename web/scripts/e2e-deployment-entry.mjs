import {createMatchingTicket} from "./matchmaking-support.mjs";
/** Real configured processes: entry routing and API policy must agree. */
import { chromium } from 'playwright';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { createServer } from 'node:net';
import { mkdir, mkdtemp, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { findExecutionLocationWords, readPlayerSurface } from './player-copy-policy.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url));
const evidence=resolve(root,'.cache/reports/deployment-entry');
await mkdir(evidence,{recursive:true});
await mkdir(resolve(root,'.cache/runtime'),{recursive:true});
const checks=[],errors=[];
const check=(name,ok,detail)=>{checks.push({name,ok:Boolean(ok),detail});console.log(`${ok?'PASS':'FAIL'} ${name}`);if(!ok)throw new Error(name);};
const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM});
const classic={modeId:'classic',maxWrongGuesses:4,winPolicy:'first-correct',durationSeconds:0,shadowEnabled:false};
const forms={local:false,verified:false,solo:false,friend:false,matchmaking:false,event:false};
const modePolicy=Object.fromEntries(['classic','aptitude-grid','draft','race-rebuild','co-race-path','sort','memory','bluff','clue-puzzle','strategy-race','rapport','creative','trade','pedigree','audio'].map(mode=>[mode,{verified:false,friend:false}]));
modePolicy.classic.verified=true;modePolicy.sort.friend=true;

async function instance(name,policy,run){
 const data=await mkdtemp(resolve(root,`.cache/runtime/deploy-${name}-`));
 const config=resolve(data,'config.json');await writeFile(config,JSON.stringify(policy,null,2));
 const probe=createServer();probe.listen(0,'127.0.0.1');await once(probe,'listening');const port=probe.address().port;await new Promise(r=>probe.close(r));
 const base=`http://127.0.0.1:${port}`;
 const child=spawn(resolve(process.env.UMA_SERVER_BIN ?? resolve(root,'bin/uma-server')),['-addr',`127.0.0.1:${port}`,'-data',data,'-content',resolve(process.env.UMA_CONTENT_DIR ?? resolve(root,'content')),'-web',resolve(root,'web/dist'),'-config',config,'-allowed-origins',base,'-log-level','warn'],{cwd:root,stdio:['ignore','pipe','pipe']});
 let log='';child.stdout.on('data',d=>{log+=d});child.stderr.on('data',d=>{log+=d});
 const exited=once(child,'exit');
 const contexts=[];
 const context=async()=>{const c=await browser.newContext({viewport:{width:390,height:844}});contexts.push(c);return c;};
 try{
  let ready=false;
  for(let attempt=0;attempt<100;attempt++){
   if(child.exitCode!==null)throw new Error(`Configured process stopped: ${log}`);
   ready=await fetch(`${base}/health/ready`).then(r=>r.ok).catch(()=>false);
   if(ready)break;
   await new Promise(r=>setTimeout(r,100));
  }
  check(`${name}: configured process ready`,ready);
  await run({base,context});
 }finally{
  for(const c of contexts)await c.close();
  if(child.exitCode===null){child.kill('SIGTERM');let timer;try{await Promise.race([exited,new Promise((_,reject)=>{timer=setTimeout(()=>{child.kill('SIGKILL');reject(new Error('Process did not stop'));},5000)})]);}finally{clearTimeout(timer);}}
  await writeFile(resolve(evidence,`${name}-process.log`),log);
 }
}

async function identity(context,base,name){
 const session=await context.request.post(`${base}/api/v1/sessions`,{data:{displayName:name},headers:{Origin:base}});
 if(session.status()!==201)throw new Error(`Session creation ${session.status()}`);
 const {csrfToken}=await session.json();
 return async(path,data)=>{
  const response=await context.request.post(`${base}/api/v1${path}`,{data,headers:{Origin:base,'X-CSRF-Token':csrfToken,'Idempotency-Key':crypto.randomUUID()}});
  return {status:response.status(),body:await response.json()};
 };
}

try{
 await instance('selected',{deploymentMode:'lan',forms:{...forms,verified:true,friend:true},modes:modePolicy,limits:{playersPerRoom:4}},async({base,context})=>{
  const ctx=await context(),page=await ctx.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base);await page.getByRole('heading',{name:'训练员，来弗一把！',exact:true}).waitFor();
  check('selected: home never falls through to remote solo',await page.locator('a[href="/play/classic/verified"]').count()===0);
  await page.goto(`${base}/friends`);await page.locator('a[href="/rooms/new?mode=sort"]').waitFor();check('selected: friend hub uses permitted friend flow',await page.locator('a[href="/rooms/new?mode=sort"]').count()>0);
  check('selected: disabled match entry absent',await page.locator('a[href="/matchmaking"]').count()===0);
  await page.locator('a[href="/rooms/new?mode=sort"]').click();await page.getByLabel('这局玩什么？',{exact:true}).waitFor();
  const options=await (async()=>{await page.locator('#friend-mode').click();const values=await page.getByRole('option').evaluateAll(els=>els.map(e=>e.dataset.value));await page.keyboard.press('Escape');return values;})();
  check('selected: friend form offers only permitted mode',options.length===1&&options[0]==='sort',options);
  check('selected: configured participant cap reaches form',await page.getByLabel('最多几人参加？',{exact:true}).getAttribute('max')==='4');
  const call=await identity(ctx,base,'配置测试员');
  const denied=await call('/rooms',{name:'不可创建',kind:'friend',modeConfig:classic,maxPlayers:2,maxSpectators:0,visibility:'invite',allowSpectators:false});
  check('selected: direct disabled friend request refused',denied.status===403&&denied.body.code==='play-disabled');
  const matching=await call('/matchmaking-tickets',{modeConfig:classic});
  check('selected: direct disabled queue request refused',matching.status===403&&matching.body.code==='play-disabled');
  const allowed=await call('/rooms',{name:'排序约局',kind:'friend',modeConfig:{modeId:'sort',informationStyle:'closed'},maxPlayers:4,maxSpectators:0,visibility:'invite',allowSpectators:false});
  check('selected: allowed friend request succeeds',allowed.status===201,allowed.status);
  // Practice routes also recover already prepared records. A configuration
  // switch refuses new pack distribution; it cannot revoke downloaded records.
  await page.goto(`${base}/play/classic`);await page.getByRole('button',{name:'准备挑战资料',exact:true}).waitFor();
  const refused=page.waitForResponse(r=>r.url().includes('/practice-packs/')&&r.request().method()==='GET');
  await page.getByRole('button',{name:'准备挑战资料',exact:true}).click();
  check('selected: disabled practice cannot obtain fresh pack',(await refused).status()===403);
  await page.getByRole('status').filter({hasText:'可以返回大厅'}).waitFor();
  check('selected: preparation refusal gives player next step',await page.getByRole('button',{name:'开始挑战',exact:true}).count()===0);
  check('selected: player copy hides configuration location',findExecutionLocationWords(await readPlayerSurface(page)).length===0);
  await page.screenshot({path:resolve(evidence,'disabled-entry-390.png'),fullPage:true});
 });
 await instance('matching',{deploymentMode:'lan',forms:{...forms,matchmaking:true}},async({base,context})=>{
  const a=await context(),b=await context(),page=await a.newPage();page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base);await page.locator('a[href="/matchmaking"]').first().waitFor();
  check('matching: matching-only entry is reachable',await page.locator('a[href="/matchmaking"]').count()>0&&await page.getByRole('link',{name:'创建好友房间',exact:true}).count()===0);
  const one=await identity(a,base,'先排队'),two=await identity(b,base,'后排队');
  const first=await createMatchingTicket({call:(_method,path,body)=>one(path,body)}),second=await createMatchingTicket({call:(_method,path,body)=>two(path,body)});
  check('matching: matching-only policy permits both tickets',first.status===201&&second.status===201);
  await one(`/matchmaking-tickets/${first.body.ticketId}/responses`,{accept:true});
  const paired=await two(`/matchmaking-tickets/${second.body.ticketId}/responses`,{accept:true});
  check('matching: matching-only policy creates shared room',paired.status===201&&Boolean(paired.body.roomId));
  const start=await one(`/rooms/${paired.body.roomId}/rounds`,{});
  check('matching: confirmed room started automatically; participant cannot create another round',start.status===403,start.status);
  const room=await a.request.get(`${base}/api/v1/rooms/${paired.body.roomId}`);const snapshot=await room.json();
  check('matching: public snapshot preserves purpose and mode',room.status()===200&&snapshot.kind==='matchmaking'&&snapshot.modeId==='classic'&&snapshot.currentRound?.phase==='collecting');
 });
 check('No uncaught browser errors',errors.length===0,errors);
}catch(error){checks.push({name:'execution',ok:false,detail:String(error)});console.error(error);}
finally{await browser.close();await writeFile(resolve(evidence,'report.json'),JSON.stringify({checks,errors,limits:['Isolated single-process LAN policy integration; does not claim internet TLS or event-load validation.']},null,2));}
if(checks.some(c=>!c.ok))process.exitCode=1;
