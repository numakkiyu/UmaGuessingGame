/** Real UI invitation and saved-configuration start, on an isolated instance. */
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
import { findExecutionLocationWords, readPlayerSurface } from './player-copy-policy.mjs';
const base=process.env.UMA_WEB_BASE;
if(!base)throw new Error('Set UMA_WEB_BASE to isolated instance');
const output=new URL('../../.cache/reports/friend-entry/',import.meta.url);await mkdir(output,{recursive:true});
const checks=[],errors=[];
function check(name,ok,detail){checks.push({name,ok:Boolean(ok),detail});console.log(`${ok?'PASS':'FAIL'} ${name}`);if(!ok)throw new Error(name);}
const browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM});
const host=await browser.newContext({viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write']});
const guest=await browser.newContext({viewport:{width:390,height:844}});
const page=await host.newPage(),other=await guest.newPage();
for(const p of [page,other])p.on('pageerror',e=>errors.push(e.message));
const button=(p,name)=>p.getByRole('button',{name,exact:true});
try{
 await page.goto(base);await page.getByRole('link',{name:'创建好友房间',exact:true}).click();await page.getByRole('heading',{name:'和朋友约一局',exact:true}).waitFor();
 check('Lobby offers real friend creation',new URL(page.url()).pathname==='/rooms/new');
 for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:1000});check(`Creation ${width}px no overflow`,await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));}
 await page.getByLabel('这局玩什么？',{exact:true}).selectOption('sort');await page.getByLabel('你的显示名',{exact:true}).fill('约局房主');await page.getByLabel('房间名称（可不填）',{exact:true}).fill('周末档案排序');
 await page.waitForFunction(()=>[...document.querySelectorAll('button')].some(b=>b.textContent.includes('创建好友房间')&&!b.disabled));
 await page.screenshot({path:new URL('creation-1440.png',output).pathname,fullPage:true,animations:'disabled'});
 const made=page.waitForResponse(r=>r.url().endsWith('/api/v1/rooms')&&r.request().method()==='POST');
 await button(page,'创建好友房间').click();const response=await made;check('Friend room created using selected mode',response.status()===201&&response.request().postDataJSON().modeConfig.modeId==='sort');
 const created=await response.json(),roomId=created.room.roomId,code=created.inviteCode;
 await page.getByText('本场项目：排序',{exact:true}).waitFor();await page.getByTestId('room-invite-code').waitFor();
 check('Invitation survives reload',await (async()=>{await page.reload();await page.getByTestId('room-invite-code').waitFor();return(await page.getByTestId('room-invite-code').innerText())===code;})());
 await button(page,'复制邀请链接').click();const invitation=await page.evaluate(()=>navigator.clipboard.readText());check('Copy creates usable code link',new URL(invitation).searchParams.get('code')===code);
 await other.goto(invitation);await other.getByRole('heading',{name:'加入房间',exact:true}).waitFor();check('Share link fills invitation code',await other.getByLabel('邀请码',{exact:true}).inputValue()===code);
 await other.getByLabel('显示名',{exact:true}).fill('约局来宾');const joined=other.waitForResponse(r=>r.url().includes('/memberships')&&r.request().method()==='POST');await button(other,'加入房间').click();check('Joining submits membership before room navigation',(await joined).status()===201);
 await other.getByText('本场项目：排序',{exact:true}).waitFor();check('Guest cannot see manager invitation or start controls',await other.getByTestId('room-invite-code').count()===0&&await button(other,'开始对局').count()===0);
 await page.getByText('约局来宾',{exact:true}).first().waitFor();check('Host observes guest without reload',await page.getByText('约局来宾',{exact:true}).first().isVisible());
 await button(other,'准备好了').click();await button(other,'取消准备').waitFor();check('Member can confirm ready',await button(other,'取消准备').isVisible());
 for(const width of [320,390,768,1440]){await page.setViewportSize({width,height:1000});check(`Lobby ${width}px no overflow`,await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));if(width===320||width===1440)await page.screenshot({path:new URL(`lobby-${width}.png`,output).pathname,fullPage:true});}
 const start=page.waitForResponse(r=>r.url().endsWith(`/rooms/${roomId}/rounds`)&&r.request().method()==='POST');await button(page,'开始对局').click();const begun=await start;const round=await begun.json();check('Default start uses saved sort, never hardcoded classic',begun.status()===201&&round.modeId==='sort'&&Object.keys(begun.request().postDataJSON()).length===0,round.modeId);
 await page.getByRole('heading',{name:/^按.+排序$/}).waitFor();await other.getByRole('heading',{name:/^按.+排序$/}).waitFor();check('Both real identities enter selected game',await page.getByRole('heading',{name:/^按.+排序$/}).isVisible()&&await other.getByRole('heading',{name:/^按.+排序$/}).isVisible());
 check('System player copy hides execution location',findExecutionLocationWords(await readPlayerSurface(page)).length===0&&findExecutionLocationWords(await readPlayerSurface(other)).length===0);
 await page.goto(`${base}/rooms/new?mode=classic`);await page.getByLabel('你的显示名',{exact:true}).waitFor();await page.getByLabel('你的显示名',{exact:true}).fill('约局房主');check('Classic duel fixes two participant seats',await page.getByLabel('最多几人参加？',{exact:true}).getAttribute('max')==='2');
 const classicResponse=page.waitForResponse(r=>r.url().endsWith('/api/v1/rooms')&&r.request().method()==='POST');await button(page,'创建好友房间').click();const classic=await(await classicResponse).json();await page.getByTestId('room-invite-code').waitFor();
 await other.goto(`${base}/join?code=${classic.inviteCode}`);await other.getByLabel('显示名',{exact:true}).fill('约局来宾');await button(other,'加入房间').click();await other.getByText('本场项目：经典弗一把',{exact:true}).waitFor();await page.getByText('约局来宾',{exact:true}).first().waitFor();
 const classicStart=page.waitForResponse(r=>r.url().endsWith(`/rooms/${classic.room.roomId}/rounds`)&&r.request().method()==='POST');await button(page,'开始对局').click();check('Two-person timed classic configuration prepares successfully',(await classicStart).status()===201);
 await page.getByRole('combobox').first().waitFor();
 check('No uncaught browser errors',errors.length===0,errors);
}catch(e){checks.push({name:'execution',ok:false,detail:String(e)});await page.screenshot({path:new URL('failure.png',output).pathname,fullPage:true}).catch(()=>{});await writeFile(new URL('failure.txt',output),await page.locator('body').innerText().catch(()=>''));await writeFile(new URL('guest-failure.txt',output),await other.locator('body').innerText().catch(()=>''));}
finally{await writeFile(new URL('report.json',output),JSON.stringify({checks,errors,limits:['Real two-browser entry and startup; no claim of all modes settlement or large-event load.']},null,2));await browser.close();}
if(checks.some(c=>!c.ok))process.exitCode=1;
