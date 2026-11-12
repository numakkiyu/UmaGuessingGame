import { test } from "node:test";
import assert from "node:assert/strict";
import { findViolations } from "../check-player-copy.mjs";
import { findExecutionLocationWords } from "../player-copy-policy.mjs";

const words = source => findViolations(source).violations.map(v => v.word);

test("compact styled JSX cannot bypass player copy checks", () => {
  assert.deepEqual(words('<div className="x"><span>服务器模式</span><Button>本机练习</Button></div>'), ["服务器", "本机"]);
});
test("multiline text, expression and template fragments are checked", () => {
  const source = '<p>\n本地练习\n</p>; <p>{"云端挑战"}</p>; const msg = `正在向服务器发送 ${count} 次作答`;';
  assert.deepEqual(words(source), ["本地", "云端", "服务器"]);
});
test("accessible labels and placeholders are player facing", () => {
  assert.deepEqual(words('<input className="x" aria-label="本机记录" placeholder="服务器地址" title="云端成绩" />'), ["本机", "服务器", "云端"]);
});
test("comments and actual internal attributes do not become player copy", () => {
  assert.deepEqual(words('// 本机、服务器、幂等键\n/* 云端 */\nconst x = <p data-help="服务器" className="本地">开始练习</p>;'), []);
});
test("one internal attribute does not exempt adjacent visible strings", () => {
  assert.deepEqual(words('<Button key="local" data-info="本机" onClick={() => setMessage("服务器暂时不可用")}>开始</Button>'), ["服务器"]);
});
test("escaped prose is checked with source locations", () => {
  const result = findViolations('const x = 1;\nconst msg = "\\u672c\\u673a练习";');
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
  assert.deepEqual(words('<p>本機練習</p><input aria-label="伺服器連線" /><p>ローカル練習</p>'), ["本機", "伺服器", "ローカル"]);
});

test("player-facing recovery actions remain understandable and location free", () => {
  assert.deepEqual(findExecutionLocationWords("自由练习 · 计分挑战 · 好友对战 · 参加活动。连接中断，请检查网络后重试。进度暂时无法保存，离开前请导出记录。"), []);
});
