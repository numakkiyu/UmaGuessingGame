import { test } from "node:test";
import assert from "node:assert/strict";
import { findViolations } from "../check-player-copy.mjs";
import { findExecutionLocationWords } from "../player-copy-policy.mjs";

const words = source => findViolations(source).violations.map(v => v.word);

test("compact styled JSX cannot bypass player copy checks", () => {
  assert.deepEqual(words('<div className="x"><span>服务器模式</span><Button>本机挑战</Button></div>'), ["服务器", "本机"]);
});
test("multiline text, expression and template fragments are checked", () => {
  const source = '<p>\n本地挑战\n</p>; <p>{"云端挑战"}</p>; const msg = `正在向服务器发送 ${count} 次作答`;';
  assert.deepEqual(words(source), ["本地", "云端", "服务器"]);
});
test("accessible labels and placeholders are player facing", () => {
  assert.deepEqual(words('<input className="x" aria-label="本机记录" placeholder="服务器地址" title="云端成绩" />'), ["本机", "服务器", "云端"]);
});
test("comments and actual internal attributes do not become player copy", () => {
  assert.deepEqual(words('// 本机、服务器、幂等键\n/* 云端 */\nconst x = <p data-help="服务器" className="本地">开始挑战</p>;'), []);
});
test("one internal attribute does not exempt adjacent visible strings", () => {
  assert.deepEqual(words('<Button key="local" data-info="本机" onClick={() => setMessage("服务器暂时不可用")}>开始</Button>'), ["服务器"]);
});
test("escaped prose is checked with source locations", () => {
  const result = findViolations('const x = 1;\nconst msg = "\\u672c\\u673a挑战";');
  assert.equal(result.violations[0].word, "本机");
  assert.equal(result.violations[0].line, 2);
});
test("code identifiers and paths do not trigger implementation-word matches", () => {
  assert.deepEqual(words('type Kind = "session"; const key = "session"; const path = "/服务器/session"; const msg = "进度已保存";'), []);
});
test("namespaced internal keys are excluded but rendered technical labels remain checked", () => {
  assert.deepEqual(words('const lock = "uma:anonymous-session"; new BroadcastChannel("uma:session-updates"); indexedDB.open("uma:session-coordination", 1);'), []);
  assert.deepEqual(words('<p>{"uma:anonymous-session"}</p><input aria-label="uma:session-updates" /><p>session: restore failed</p>'), ["session", "session", "session"]);
  assert.deepEqual(words('const message = "session: restore failed";'), ["session"]);
});
test("necessary safe issue number is allowed; technical label is rejected", () => {
  assert.deepEqual(words('<p className="x">问题编号：{id}</p>'), []);
  assert.deepEqual(words('<p className="x">诊断编号：{id}</p>'), ["诊断编号"]);
});

test("English execution labels cannot reintroduce location in visible copy", () => {
  assert.deepEqual(words('<span>Local</span><button title="SERVER mode">开始</button><p>Runs on-device</p>'), ["local", "server", "on-device"]);
  assert.deepEqual(findExecutionLocationWords("Clover and Silver, your score is saved."), []);
});

test("regional copy cannot restore execution-location labels", () => {
  assert.deepEqual(words('<p>本機挑戰</p><input aria-label="伺服器連線" /><p>ローカル挑戰</p>'), ["本機", "伺服器", "ローカル"]);
});

test("player-facing recovery actions remain understandable and location free", () => {
  assert.deepEqual(findExecutionLocationWords("单人挑战 · 计分挑战 · 好友对战 · 参加活动。连接中断，请检查网络后重试。进度暂时无法保存，离开前请导出记录。"), []);
});

test("catalogs reject reviewed developer phrases without banning internal diagnostics", () => {
  const source = 'export const copy = { detail: "场地变化首版关闭，保证基线可解释", data: "该字段数据未知" };';
  assert.deepEqual(findViolations(source, "src/i18n/rules.ts").violations.map(v => v.word), ["基线", "首版关闭", "字段"]);
  assert.deepEqual(findViolations('const diagnostic = "缺少字段";', "src/lib/parser.ts").violations, []);
  assert.deepEqual(findViolations('const copy = { detail: "这项资料暂缺，请选择其他角色" };', "src/i18n/classic.ts").violations, []);
});

test("regional catalogs reject migration notes, code formulas and assistant narration", () => {
  for (const text of ["这题使用旧的游玩流程", "開始空投票階段", "あなたの側の回答", "得点は max(20, value)", "作為 AI", "AIとしてご案内します"]) {
    assert.ok(findViolations(`const copy = ${JSON.stringify(text)};`, "src/i18n/rules.ts").violations.length > 0);
  }
  assert.deepEqual(findViolations('const recovery = "这份记录缺少并列信息，请保留原文件";', "src/i18n/rules.ts").violations, []);
});

test("catalogs reject implementation narration while keeping real player limits and language notices", () => {
  for (const text of ["下载速度没有额外加分", "下載速度沒有額外加分", "ダウンロードの速さで得点は変わりません", "无法防止另外录音", "候選馬池", "提交修订", "切换语言不会改变线索含义"]) {
    assert.ok(findViolations(`const copy = ${JSON.stringify(text)};`, "src/i18n/rules.ts").violations.length > 0);
  }
  for (const text of ["最多同时保留 3 局进行中的挑战", "题面部分内容保留原文，请按指定用词写作", "同一张牌不能同时用于两个目标", "請查看最後儲存的答案", "チームメイトと神経衰弱で遊ぼう"]) {
    assert.deepEqual(findViolations(`const copy = ${JSON.stringify(text)};`, "src/i18n/rules.ts").violations, []);
  }
});

test("reviewed explanatory detours are caught while meaningful rule distinctions remain allowed", () => {
  for (const text of [
    "主持会作废重开，不把故障当作答错",
    "題目問的是玩家偏好，不代表官方角色性格或人氣排名",
    "公式のキャラクター設定や人気順位を表すものではありません",
  ]) {
    assert.ok(findViolations(`const copy = ${JSON.stringify(text)};`, "src/i18n/rules.ts").violations.length > 0);
  }
  for (const text of [
    "每人一票，不能投自己或本队的作品",
    "未保存的修改不会自动交卷",
    "这里展示原型马的血统，不是角色之间的家庭关系",
    "MIT 许可不包含角色图片与语音的使用权",
    "このゲームは Cygames の公式ゲームではありません",
  ]) {
    assert.deepEqual(findViolations(`const copy = ${JSON.stringify(text)};`, "src/i18n/rules.ts").violations, []);
  }
});

test("functional interfaces reject sample account and result narration", () => {
  for (const text of ["保留品牌预设", "品牌預設", "示例成绩", "範例榜單", "已退出预览账户", "仅供预览", "プレビュー用のスコア", "サンプルスコア"]) {
    assert.ok(findViolations(`const copy = ${JSON.stringify(text)};`, "src/i18n/account.ts").violations.length > 0);
  }
  for (const text of ["预设", "Google 登录", "此玩法需要登录", "本场成绩", "暂无上榜成绩", "实验性功能", "進度已儲存", "ログインが必要です"]) {
    assert.deepEqual(findViolations(`const copy = ${JSON.stringify(text)};`, "src/i18n/account.ts").violations, []);
  }
});
