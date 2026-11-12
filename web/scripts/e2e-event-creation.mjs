/** Real response-loss recovery; all writes go through the ordinary creation form. */
import {chromium} from 'playwright';
import {mkdir,writeFile} from 'node:fs/promises';
import {spawnSync} from 'node:child_process';
import {isDeepStrictEqual} from 'node:util';
import {findExecutionLocationWords,readPlayerSurface} from './player-copy-policy.mjs';
const base=process.env.UMA_WEB_BASE,db=process.env.UMA_TEST_DB;
if(!base||!db)throw new Error('Set isolated UMA_WEB_BASE and UMA_TEST_DB');
const output=new URL('../../.cache/reports/event-creation/',import.meta.url);await mkdir(output,{recursive:true});
const checks=[],errors=[],commands={rooms:[],events:[]};
const check=(name,ok,detail)=>{checks.push({name,ok:Boolean(ok),detail});console.log(`${ok?'PASS':'FAIL'} ${name}`);if(!ok)throw new Error(name);};
const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM});
const context=await browser.newContext({viewport:{width:390,height:844}}),page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
let dropRoom=true,dropEvent=true;
await context.route('**/api/v1/{rooms,events}',async route=>{
 const req=route.request();if(req.method()!=='POST')return route.continue();
 const kind=new URL(req.url()).pathname.endsWith('/rooms')?'rooms':'events';
 const response=await route.fetch(),data=await response.json();
 commands[kind].push({body:req.postDataJSON(),key:req.headers()['idempotency-key'],status:response.status(),id:kind==='rooms'?data.room?.roomId:data.eventId});
 if((kind==='rooms'&&dropRoom)||(kind==='events'&&dropEvent)){if(kind==='rooms')dropRoom=false;else dropEvent=false;await route.abort('failed');}else await route.fulfill({response});
});
const button=(name)=>page.getByRole('button',{name,exact:true});
const failed=()=>page.getByRole('alert').filter({hasText:'这次安排还没有确认完成'}).waitFor();
async function counts(sessionId){const r=spawnSync('python3',['-c',`import sqlite3,json,sys
c=sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True)
print(json.dumps({"rooms":c.execute("SELECT COUNT(*) FROM rooms WHERE owner_session_id=?",(sys.argv[2],)).fetchone()[0],"events":c.execute("SELECT COUNT(*) FROM event_sessions WHERE host_session_id=?",(sys.argv[2],)).fetchone()[0]}))`,db,sessionId],{encoding:'utf8'});if(r.status!==0)throw new Error(r.stderr);return JSON.parse(r.stdout);}
try{
 await page.goto(`${base}/events/new`);await page.getByLabel('活动名称',{exact:true}).waitFor();
 const config=await(await context.request.get(`${base}/api/v1/config`)).json();
 check('Capacity inputs reflect actual published limits',await page.getByLabel('参赛名额',{exact:true}).getAttribute('max')===String(config.maxRoomMembers)&&await page.getByLabel('观众名额',{exact:true}).getAttribute('max')===String(config.maxRoomSpectators));
 const seats=Math.min(20,config.maxRoomMembers),spectators=Math.min(7,config.maxRoomSpectators);
 const name='训练员'.repeat(16);await page.getByLabel('活动名称',{exact:true}).fill(name);await page.getByLabel('参赛名额',{exact:true}).fill(String(seats));await page.getByLabel('观众名额',{exact:true}).fill(String(spectators));
 for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:1000});check(`Creation ${width}px no overflow`,await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));if(width===320||width===1440)await page.screenshot({path:new URL(`form-${width}.png`,output).pathname,fullPage:true});}
 await page.getByLabel('参赛名额',{exact:true}).fill(String(config.maxRoomMembers+1));check('Capacity above configured limit cannot submit',await button('创建并进入主持台').isDisabled());await page.getByLabel('参赛名额',{exact:true}).fill(String(seats));
 await button('创建并进入主持台').click();await failed();check('Lost room response stops before programme write',commands.rooms.length===1&&commands.events.length===0);
 check('Unknown result freezes the original arrangement',await page.getByLabel('活动名称',{exact:true}).isDisabled()&&await page.getByLabel('参赛名额',{exact:true}).isDisabled());
 await page.reload();await button('确认创建结果').waitFor();check('Reload restores the original name and capacities',await page.getByLabel('活动名称',{exact:true}).inputValue()===name&&await page.getByLabel('参赛名额',{exact:true}).inputValue()===String(seats));
 check('Reload never automatically repeats writes',commands.rooms.length===1&&commands.events.length===0);
 await button('确认创建结果').click();await failed();check('Room confirmation reuses exact original command',commands.rooms.length===2&&commands.rooms[0].id===commands.rooms[1].id&&commands.rooms[0].key===commands.rooms[1].key&&isDeepStrictEqual(commands.rooms[0].body,commands.rooms[1].body),commands.rooms);
 check('Long activity name keeps room name within 48 characters',[...commands.rooms[0].body.name].length<=48&&commands.rooms[0].body.maxPlayers===seats&&commands.rooms[0].body.maxSpectators===spectators);
 check('Lost event response leaves room recovery checkpoint',await page.evaluate(()=>JSON.parse(sessionStorage.getItem('umaguess:pending-event')).roomId)!==null);
 await page.reload();await button('确认创建结果').waitFor();await button('确认创建结果').click();await page.getByRole('heading',{name,exact:true}).waitFor();
 check('Event confirmation neither creates another room nor programme',commands.rooms.length===2&&commands.events.length===2&&commands.events[0].id===commands.events[1].id&&commands.events[0].key===commands.events[1].key);
 const session=await(await context.request.get(`${base}/api/v1/sessions/current`)).json();check('Database has exactly one resource of each kind for organiser',JSON.stringify(await counts(session.sessionId))===JSON.stringify({rooms:1,events:1}));
 check('Success clears the unfinished arrangement',await page.evaluate(()=>sessionStorage.getItem('umaguess:pending-event')===null));
 const room=await(await context.request.get(`${base}/api/v1/rooms/${commands.rooms[0].id}`)).json();check('Organiser is host and does not occupy a player seat',room.viewer.role==='host');
 await page.getByLabel('玩法',{exact:true}).selectOption('creative');check('Creative item clearly uses entertainment rules',await page.getByText('这个项目作娱乐展示，单独保留成绩，不计入正式总榜。',{exact:true}).isVisible()&&await page.getByLabel('计入正式总榜',{exact:true}).count()===0);
 await page.getByLabel('玩法',{exact:true}).selectOption('trade');check('Trading item cannot opt into formal standings',await page.getByLabel('计入正式总榜',{exact:true}).count()===0);
 check('All player copy hides execution location',findExecutionLocationWords(await readPlayerSurface(page)).length===0);
 await page.goto(`${base}/events/new`);await page.getByLabel('活动名称',{exact:true}).waitFor();check('A completed attempt does not freeze future new activity forms',await page.getByLabel('活动名称',{exact:true}).isEnabled());
 // An expired saved command must be a deliberate new arrangement, never a replay.
 await page.evaluate(()=>sessionStorage.setItem('umaguess:pending-event',JSON.stringify({createdAt:Date.now()-24*60*60*1000,sessionId:'old',room:{name:'旧活动',kind:'event',modeConfig:{modeId:'classic',variant:'public',winPolicy:'window-attempts',maxWrongGuesses:8,durationSeconds:180,shadowEnabled:false},maxPlayers:1,maxSpectators:0,allowSpectators:false,visibility:'invite'},event:{name:'旧活动',tiePolicy:'shared-rank'},roomKey:'a'.repeat(48),eventKey:'b'.repeat(48),roomId:null})));
 await page.reload();await page.getByRole('alert').filter({hasText:'先核对之前的安排'}).waitFor();check('Expired operation is never automatically replayed',await button('确认创建结果').isDisabled()&&commands.rooms.length===2&&commands.events.length===2);
 check('No uncaught browser errors',errors.length===0,errors);
}catch(error){checks.push({name:'execution',ok:false,detail:String(error)});console.error(error);await page.screenshot({path:new URL('failure.png',output).pathname,fullPage:true}).catch(()=>{});await writeFile(new URL('failure.txt',output),await page.locator('body').innerText().catch(()=>''));}
finally{await writeFile(new URL('report.json',output),JSON.stringify({checks,errors,commandCounts:{rooms:commands.rooms.length,events:commands.events.length},limits:['Responses intentionally dropped after actual processing. No writes bypass the UI. Capacity inputs are verified, not a load claim.']},null,2));await browser.close();}
if(checks.some(c=>!c.ok))process.exitCode=1;
