<script setup lang="ts">
import { computed } from "vue";
import { useData } from "vitepress";

const { lang } = useData();
const labels = {
  "zh-Hans": {
    title: "对局与数据流",
    browser: "浏览器 · React / TypeScript",
    ui: "页面、三语文案与交互",
    solo: "单人挑战：出题、作答与存档",
    online: "联机：提交动作，读取获准状态",
    server: "Go · 应用服务",
    rules: "会话与权限、房间与规则",
    transport: "REST 动作、SSE / WebSocket 通知",
    storage: "SQLite · 持久状态",
    records: "成员、作答、成绩与命令回执",
    persistence: "单进程持有、WAL、一致性备份",
    content: "版本化内容包",
    build: "JSON 资料与来源 → 内容编译 → public / private",
    scope: "单人挑战使用公开资料，联机答案与未揭晓状态由服务端保存",
  },
  "zh-Hant": {
    title: "對局與資料流程",
    browser: "瀏覽器 · React / TypeScript",
    ui: "頁面、三語文案與互動",
    solo: "單人挑戰：出題、作答與存檔",
    online: "連線對局：送出動作，讀取獲准狀態",
    server: "Go · 應用服務",
    rules: "會話與權限、房間與規則",
    transport: "REST 動作、SSE / WebSocket 通知",
    storage: "SQLite · 持久狀態",
    records: "成員、作答、成績與命令回執",
    persistence: "單一程序持有、WAL、一致性備份",
    content: "版本化內容包",
    build: "JSON 資料與來源 → 內容編譯 → public / private",
    scope: "單人挑戰使用公開資料，連線答案與未揭曉狀態由伺服器保存",
  },
  ja: {
    title: "対局とデータの流れ",
    browser: "ブラウザー · React / TypeScript",
    ui: "画面・三言語・操作",
    solo: "一人用：出題・回答・保存",
    online: "対戦：操作を送信、許可された状態を取得",
    server: "Go · アプリケーション",
    rules: "セッション・権限・ルーム・ルール",
    transport: "REST 操作・SSE / WebSocket 通知",
    storage: "SQLite · 永続データ",
    records: "参加者・回答・成績・受領記録",
    persistence: "単一プロセス・WAL・整合性のあるバックアップ",
    content: "バージョン付き問題パック",
    build: "JSON の資料と出典 → ビルド → public / private",
    scope: "一人用は公開データを使用し、対戦の正解や未公開状態はサーバーで管理",
  },
};
const t = computed(() => labels[lang.value === "ja" ? "ja" : lang.value === "zh-Hant" ? "zh-Hant" : "zh-Hans"]);
</script>

<template>
  <figure class="architecture-diagram" aria-labelledby="architecture-caption">
    <figcaption id="architecture-caption">{{ t.title }}</figcaption>
    <div class="architecture-node architecture-browser">
      <strong>{{ t.browser }}</strong>
      <span>{{ t.ui }}</span>
      <span>{{ t.solo }}</span>
      <span>{{ t.online }}</span>
    </div>
    <div class="architecture-services">
      <div class="architecture-node architecture-server">
        <strong>{{ t.server }}</strong>
        <span>{{ t.rules }}</span>
        <span>{{ t.transport }}</span>
      </div>
      <div class="architecture-node architecture-storage">
        <strong>{{ t.storage }}</strong>
        <span>{{ t.records }}</span>
        <span>{{ t.persistence }}</span>
      </div>
    </div>
    <div class="architecture-node architecture-content">
      <strong>{{ t.content }}</strong>
      <span>{{ t.build }}</span>
    </div>
    <p>{{ t.scope }}</p>
  </figure>
</template>

<style scoped>
.architecture-diagram { margin:24px 0; padding:24px; border:1px solid var(--vp-c-divider); border-radius:14px; background:var(--vp-c-bg-alt); font-size:14px; line-height:1.7; }
figcaption { margin-bottom:18px; font-size:18px; font-weight:600; }
.architecture-node { display:grid; gap:4px; padding:16px 18px; border:1px solid var(--vp-c-divider); border-radius:10px; background:var(--vp-c-bg); }
.architecture-node strong { color:var(--vp-c-text-1); font-size:15px; }
.architecture-node span { color:var(--vp-c-text-2); }
.architecture-browser { grid-template-columns:minmax(0,1fr) minmax(0,1fr); gap:4px 20px; }
.architecture-browser strong { grid-column:1/-1; }
.architecture-browser span:first-of-type { grid-row:span 2; }
.architecture-services { position:relative; display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1fr); gap:24px; margin-block:28px; }
.architecture-services::before, .architecture-content::before { content:"↑"; position:absolute; left:25%; top:-28px; display:grid; place-items:center; width:20px; height:28px; color:var(--vp-c-brand-1); }
.architecture-services::before { content:"↓"; }
.architecture-server { position:relative; background:color-mix(in srgb,var(--vp-c-brand-soft) 50%,var(--vp-c-bg)); }
.architecture-server::after { content:"→"; position:absolute; right:-25px; top:calc(50% - 12px); width:24px; text-align:center; color:var(--vp-c-brand-1); }
.architecture-content { position:relative; }
.architecture-diagram p { margin:16px 0 0; font-size:12px; line-height:1.8; color:var(--vp-c-text-2); }
@container document (max-width:540px) {
  .architecture-diagram { padding:18px; }
  .architecture-services { grid-template-columns:1fr; gap:20px; }
  .architecture-browser { grid-template-columns:1fr; }
  .architecture-browser span:first-of-type { grid-row:auto; }
  .architecture-node { padding:14px; }
  .architecture-server::after { content:"↓"; right:calc(50% - 12px); top:auto; bottom:-22px; height:20px; }
  .architecture-services::before { left:calc(50% - 10px); }
  .architecture-content::before { display:none; }
}
</style>
