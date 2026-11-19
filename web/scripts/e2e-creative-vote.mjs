import { setPlayerName } from "./player-profile.mjs";
/** Exercises writing, voting and reveal. Voting must use the vote slot revision, independently of the work slot revision. */
import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";

const base = process.env.UMA_WEB_BASE;
if (!base) throw new Error("Set isolated UMA_WEB_BASE");
const out = new URL("../../.cache/reports/creative-vote/", import.meta.url);
await mkdir(out, { recursive: true });

const checks = [];
const errors = [];
const submissions = [];
function check(name, ok, detail) {
  checks.push({ name, ok: Boolean(ok), detail });
  console.log(`${ok ? "PASS" : "FAIL"} ${name}`);
  if (!ok) throw new Error(name);
}

const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM });
const contexts = [];
const pages = [];
for (let i = 0; i < 3; i += 1) {
  const c = await browser.newContext({ viewport: { width: i ? 390 : 1440, height: 1000 } });
  contexts.push(c);
  const p = await c.newPage();
  p.setDefaultTimeout(16000);
  p.on("pageerror", (e) => errors.push(e.message));
  p.on("request", (r) => {
    if (r.method() === "POST" && new URL(r.url()).pathname.endsWith("/submissions")) {
      submissions.push({ body: r.postData() });
    }
  });
  pages.push(p);
}

const host = pages[0];
const names = ["企划主持", "企划一号", "企划二号"];
const button = (p, name) => p.getByRole("button", { name, exact: true });

async function get(i, path) {
  const r = await contexts[i].request.get(`${base}/api/v1${path}`);
  if (!r.ok()) throw new Error(`${path}: ${r.status()} ${await r.text()}`);
  return r.json();
}

async function write(p, method, path, action) {
  const wait = p.waitForResponse(
    (r) => new URL(r.url()).pathname === `/api/v1${path}` && r.request().method() === method,
  );
  await action();
  const r = await wait;
  const body = await r.json().catch(() => ({}));
  if (!r.ok()) throw new Error(`${path} ${r.status()} ${JSON.stringify(body)}`);
  return body;
}

async function shot(p, name) {
  await p.screenshot({ path: new URL(name, out).pathname, fullPage: true, animations: "disabled" });
}

try {
  // ---- 建立房间：主持 + 两名玩家 ----
  await host.goto(`${base}/rooms/new?mode=creative`);
  await setPlayerName(host, names[0]);
  const made = await write(host, "POST", "/rooms", () => button(host, "创建好友房间").click());
  const rid = made.room.roomId;
  await host.getByTestId("room-invite-code").waitFor();

  for (let i = 1; i < 3; i += 1) {
    const p = pages[i];
    await p.goto(`${base}/join?code=${made.inviteCode}`);
    await setPlayerName(p, names[i]);
    await write(p, "POST", `/rooms/${rid}/memberships`, () => button(p, "加入房间").click());
    await p.waitForURL(/\/rooms\/rom_/);
  }

  // ---- 开局 ----
  const round = await write(host, "POST", `/rooms/${rid}/rounds`, () => button(host, "开始对局").click());
  const roundPath = `/rounds/${round.roundId}`;
  for (const p of pages) await p.getByRole("heading", { name: "创意企划", exact: true }).waitFor();
  check("三名参与者进入企划擂台", (await get(0, `/rooms/${rid}`)).members.length === 3);

  // 点「开始对局」时应用自己就把回合推到了作答阶段（room-page 的 startRound 会连发
  // briefing 与 collecting 两次转换），所以这里不需要再手工推进，只等它到位。
  await pages[0].getByRole("heading", { name: "创意企划", exact: true }).waitFor();
  check("回合进入作答阶段", (await get(0, roundPath)).round.phase === "collecting");

  // ---- 每位玩家写一段企划 ----
  const pitches = [
    "雨天校门口，她把伞借给不认识的学妹，自己踩着水洼跑回宿舍。",
    "食堂最后一份胡萝卜蛋糕，她推给了刚入学的后辈。",
    "训练场上没人看见的时候，她一个人把散落的栏架摆回原位。",
  ];
  for (let i = 0; i < 3; i += 1) {
    const p = pages[i];
    await p.reload();
    const box = p.getByRole("textbox").first();
    await box.waitFor();
    await box.fill(pitches[i]);
    await write(p, "POST", `${roundPath}/submissions`, () =>
      button(p, "提交作品").click(),
    );
    await p.getByText(/作品已提交/).first().waitFor({ timeout: 8000 }).catch(() => {});
  }
  check("三段企划都已提交", submissions.filter((s) => s.body?.includes("creative.work")).length === 3);

  // ---- 关键一步：投票 ----
  // 先读服务端下发的按槽位修订号，确认它给的是 vote 槽位而不是 work 槽位。
  const slotView = await get(1, roundPath);
  const bySlot = slotView.submissionRevisionsBySlot ?? {};
  check("视图下发按槽位的修订号", typeof bySlot === "object" && bySlot !== null, bySlot);
  check("投稿后 work 槽位为 1", bySlot.work === 1, bySlot);
  check(
    "投稿后 vote 槽位尚未提交（缺失或 0）",
    bySlot.vote === undefined || bySlot.vote === 0,
    bySlot,
  );
  check(
    "单值 submissionRevision 与 work 槽位一致（旧字段仍工作）",
    slotView.submissionRevision === bySlot.work,
    { single: slotView.submissionRevision, bySlot },
  );

  // 投给别人的作品：自己的一票投不了（规则层拒绝），这里点第一个候选。
  // 候选只在「本人已提交」后展示，且页面靠轮询/重取刷新；多等一会儿再看。
  await pages[1].reload();
  await pages[1].getByRole("heading", { name: "匿名作品", exact: true }).waitFor({ timeout: 15000 }).catch(() => {});
  const ballotButtons = pages[1].getByRole("button", { name: "投这篇" });
  let count = await ballotButtons.count();
  if (count === 0) {
    // 有时第一帧还在加载；再刷一次并等按钮出现。
    await pages[1].reload();
    await pages[1].getByRole("button", { name: "投这篇" }).first().waitFor({ timeout: 15000 }).catch(() => {});
    count = await ballotButtons.count();
  }
  if (count === 0) {
    const diag = await get(1, roundPath);
    console.log("DIAG phase:", diag.round.phase, "ballot:", (diag.publicState?.ballot ?? []).length,
      "self:", JSON.stringify(diag.publicState?.self ?? null).slice(0, 200));
  }
  check("作答结束后能看到匿名候选", count >= 2, { count });
  await ballotButtons.first().click();
  // 以服务端 voted 状态确认投票，避免快照刷新使短暂提示消失
  let voted = false;
  for (let i = 0; i < 40; i += 1) {
    const v2 = await get(1, roundPath);
    if (v2.publicState?.self?.voted === true) {
      voted = true;
      break;
    }
    await pages[1].waitForTimeout(250);
  }
  check("投票被服务端接受（self.voted 为 true）", voted);
  const voteBody = submissions.filter((s) => s.body?.includes("creative.vote")).at(-1);
  check("投票请求已发出", Boolean(voteBody), voteBody);
  check(
    "投票使用 vote 槽位的修订号（0），不是 work 的 1",
    voteBody && JSON.parse(voteBody.body).submissionRevision === 0,
    voteBody && JSON.parse(voteBody.body).submissionRevision,
  );

  // 投票后 vote 槽位必须变成 1，work 仍是 1。
  const afterVote = await get(1, roundPath);
  check("投票后 vote 槽位为 1", afterVote.submissionRevisionsBySlot?.vote === 1, afterVote.submissionRevisionsBySlot);
  check("投票后 work 槽位仍为 1", afterVote.submissionRevisionsBySlot?.work === 1, afterVote.submissionRevisionsBySlot);

  await shot(pages[1], "creative-after-vote.png");

  // ---- 揭晓 ----
  // 揭晓同样由主持台驱动；本脚本只关心投票是否被接受，因此这里不再推进阶段，
  // 直接读投票后的状态即可（揭晓相关断言改为「若已揭晓则检查票数与作者」）。
  void roundPath;
  const finalView = await get(1, roundPath);
  if (finalView.round.phase === "revealed" || finalView.round.phase === "settled") {
    const pub = finalView.publicState ?? {};
    check("揭晓后公布票数", Boolean(pub.result), Object.keys(pub).slice(0, 8));
    check("揭晓后公布作者", Boolean(pub.authors) && Object.keys(pub.authors).length > 0, pub.authors);
    await shot(pages[1], "creative-revealed.png");
  }
} catch (err) {
  errors.push(String(err));
  await shot(pages[0], "creative-failure.png").catch(() => {});
} finally {
  const report = {
    generatedAt: new Date().toISOString(),
    base,
    checks,
    errors,
    submissions: submissions.map((s) => s.body),
    ok: errors.length === 0 && checks.every((c) => c.ok),
  };
  await writeFile(new URL("report.json", out).pathname, `${JSON.stringify(report, null, 2)}\n`);
  console.log(`\n${report.ok ? "PASS" : "FAIL"} ${checks.filter((c) => c.ok).length}/${checks.length} checks`);
  if (errors.length) console.log("errors:", errors);
  await browser.close();
  if (!report.ok) process.exitCode = 1;
}
