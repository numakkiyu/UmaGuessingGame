/** Real UI writes against isolated Go/SQLite; drop only responses after commit. */
import { chromium } from 'playwright';
import { mkdir, writeFile } from 'node:fs/promises';
const base = process.env.UMA_WEB_BASE;
if (!base) throw new Error('Set isolated UMA_WEB_BASE');
const out = new URL('../../.cache/reports/gameplay-recovery/', import.meta.url);
await mkdir(out, { recursive: true });
const checks = [], errors = [], requests = [];
function check(name, ok, detail) { checks.push({ name, ok: !!ok, detail }); console.log(`${ok ? 'PASS' : 'FAIL'} ${name}`); if (!ok) throw new Error(name); }
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM });
const contexts = [], pages = [];
for (let i = 0; i < 3; i++) {
  const context = await browser.newContext({ viewport: { width: i ? 390 : 1440, height: 1000 } });
  contexts.push(context); const page = await context.newPage(); page.setDefaultTimeout(14000);
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => { if (request.method() === 'POST' && /\/(submissions|trades|responses)$/.test(new URL(request.url()).pathname)) requests.push({ url: new URL(request.url()).pathname, body: request.postData(), key: request.headers()['idempotency-key'] }); });
  pages.push(page);
}
const button = (page, name) => page.getByRole('button', { name, exact: true });
async function get(i, path) { const response = await contexts[i].request.get(base + '/api/v1' + path); if (!response.ok()) throw new Error(`${path}: ${response.status()}`); return response.json(); }
async function write(page, path, action) { const wait = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1' + path && r.request().method() === 'POST'); await action(); const response = await wait; if (!response.ok()) throw new Error(`${path} ${response.status()} ${await response.text()}`); return response.json(); }
async function create(mode, count) {
  const p = pages[0]; await p.goto(base + '/rooms/new?mode=' + mode);
  await p.getByLabel('你的显示名').fill('特别周'); await p.getByLabel('最多几人参加？').fill(String(count));
  const room = await write(p, '/rooms', () => button(p, '创建好友房间').click());
  await p.getByTestId('room-invite-code').waitFor(); const rid = room.room.roomId;
  for (let i = 1; i < count; i++) { const page = pages[i]; await page.goto(base + '/join?code=' + room.inviteCode); await page.getByLabel('显示名', { exact: true }).fill(i === 1 ? '特别周' : '小栗帽'); await write(page, `/rooms/${rid}/memberships`, () => button(page, '加入房间').click()); await page.waitForURL(/\/rooms\/rom_/); }
  await p.reload(); const round = await write(p, `/rooms/${rid}/rounds`, () => button(p, '开始对局').click());
  return { roomId: rid, roundId: round.roundId };
}
async function dropCommittedResponse(page, path, action) {
  const url = base + '/api/v1' + path;
  let status = null;
  await page.route(url, async route => { if (route.request().method() !== 'POST') return route.continue(); const response = await route.fetch(); status = response.status(); await response.body(); await route.abort('failed'); }, { times: 1 });
  await action();
  return () => status;
}
async function confirmSame(page, label, index) {
  await button(page, label).waitFor(); await page.reload(); await button(page, label).waitFor();
  check('刷新不自动重发待确认操作', requests.length === index + 1);
  const original = requests[index];
  const response = page.waitForResponse(r => new URL(r.url()).pathname === original.url && r.request().method() === 'POST' && r.ok());
  await button(page, label).click(); await response;
  await button(page, label).waitFor({ state: 'detached' });
  check('确认复用原路径、选项及操作', JSON.stringify(requests[index + 1]) === JSON.stringify(original));
}
try {
  const memory = await create('memory', 2), p = pages[0];
  const board = p.getByRole('group', { name: '4 行 4 列记忆牌面' });
  await board.getByRole('button', { name: /未翻开/ }).first().waitFor();
  const before = await get(0, '/rounds/' + memory.roundId), start = requests.length;
  const committed = await dropCommittedResponse(p, `/rounds/${memory.roundId}/submissions`, () => board.getByRole('button', { name: /未翻开/ }).first().click());
  await button(p, '查看提交结果').click({ trial: true });
  check('翻牌已保存但响应丢失时保持待确认', committed() === 201 && (await get(0, '/rounds/' + memory.roundId)).submissionRevision === before.submissionRevision + 1);
  check('待确认期间不能再翻别的牌', await board.getByRole('button').evaluateAll(buttons => buttons.every(b => b.disabled)));
  for (const width of [320, 1440]) { await p.setViewportSize({ width, height: 1000 }); check(`翻牌确认 ${width}px 无横溢`, await p.evaluate(() => document.documentElement.scrollWidth <= innerWidth)); await p.screenshot({ path: new URL('memory-confirm-' + width + '.png', out).pathname, fullPage: true, animations: 'disabled' }); }
  await confirmSame(p, '查看提交结果', start);
  check('确认没有多翻一次', (await get(0, '/rounds/' + memory.roundId)).submissionRevision === before.submissionRevision + 1);
  check('确认后能继续翻牌', await board.getByRole('button', { name: /未翻开/ }).first().isEnabled());

  const trade = await create('trade', 3), path = '/rounds/' + trade.roundId;
  await p.getByRole('heading', { name: '我的手牌', exact: true }).waitFor();
  const first = await get(0, path), second = await get(1, path), member = (await get(1, '/rooms/' + trade.roomId)).viewer;
  check('三位玩家各有四张牌与两条秘密任务', first.publicState.self.hand.length === 4 && second.publicState.selfObjectives.length === 2 && Object.keys(first.publicState.hands).length === 3);
  const mine = first.publicState.self.hand[0], theirs = second.publicState.self.hand[0];
  // Card buttons follow the recipient selector; use their actual catalogue label
  // and the last matching button so duplicate card kinds remain selectable.
  async function selectOffer() {
    await p.getByRole('region', { name: '我的手牌', exact: true }).getByRole('button').first().click();
    await p.getByRole('button', { name: new RegExp('房内 ID ' + member.publicCode) }).click();
    const section = p.getByRole('region', { name: '发起提案', exact: true });
    await section.locator('h3').filter({ hasText: '想要他的' }).waitFor();
    await section.locator('h3').filter({ hasText: '想要他的' }).locator('xpath=following-sibling::ul').getByRole('button').first().click();
  }
  await selectOffer(); const proposalStart = requests.length;
  const proposal = await dropCommittedResponse(p, path + '/trades', () => button(p, '发出提案').click());
  await button(p, '确认刚才的结果').click({ trial: true }); check('提案保存后丢响应仍可确认', proposal() === 201);
  await confirmSame(p, '确认刚才的结果', proposalStart);
  const proposed = await get(0, path); check('确认没有重复发提案', proposed.publicState.trades.length === 1);
  const id = proposed.publicState.trades[0].tradeId;
  const receiver = pages[1]; await receiver.reload(); await button(receiver, '接受').waitFor();
  const responseStart = requests.length;
  const accepted = await dropCommittedResponse(receiver, path + '/trades/' + id + '/responses', () => button(receiver, '接受').click());
  await button(receiver, '确认刚才的结果').click({ trial: true }); check('接受保存后丢响应仍可确认', accepted() === 201);
  await confirmSame(receiver, '确认刚才的结果', responseStart);
  const afterA = await get(0, path), afterB = await get(1, path);
  check('双方手牌只交换一次，个人任务保留', afterA.publicState.self.hand.includes(theirs) && afterB.publicState.self.hand.includes(mine) && !afterA.publicState.self.hand.includes(mine) && afterA.publicState.selfObjectives.length === 2 && afterB.publicState.selfObjectives.length === 2);
  check('同名交易对象用昵称与六位码辨识', (await receiver.locator('body').innerText()).includes('房内 ID ' + member.publicCode));
  await receiver.getByText('已连接', { exact: true }).waitFor();
  for (const width of [320, 390, 768, 1440]) { await receiver.setViewportSize({ width, height: 1000 }); check(`交易页 ${width}px 无横溢`, await receiver.evaluate(() => document.documentElement.scrollWidth <= innerWidth)); if (width === 320 || width === 1440) await receiver.screenshot({ path: new URL('trade-' + width + '.png', out).pathname, fullPage: true, animations: 'disabled' }); }
  // A normal second proposal and rejection prove the recovered swap did not
  // strand either participant in a stale hand or a permanent busy state.
  await p.reload(); await selectOffer(); await write(p, path + '/trades', () => button(p, '发出提案').click());
  await receiver.reload(); await write(receiver, path + '/trades/' + (await get(0, path)).publicState.trades.find(t => t.status === 'pending').tradeId + '/responses', () => button(receiver, '拒绝').click());
  check('恢复后能继续交易并拒绝', (await get(0, path)).publicState.trades.some(t => t.status === 'rejected'));
  check('无未捕获页面异常', errors.length === 0, errors);
} catch (error) { checks.push({ name: 'execution', ok: false, detail: String(error) }); console.error(error); for (let i = 0; i < 3; i++) { await pages[i].screenshot({ path: new URL('failure-' + i + '.png', out).pathname, fullPage: true }).catch(() => {}); await writeFile(new URL('failure-' + i + '.txt', out), await pages[i].locator('body').innerText().catch(() => '')); } }
finally { await writeFile(new URL('report.json', out), JSON.stringify({ checks, errors, requests, limits: ['Three independent Chromium contexts; real UI writes; response loss after backend commit. Not a capacity or real mobile-device test.'] }, null, 2)); await browser.close(); }
if (checks.some(check => !check.ok)) process.exitCode = 1;
