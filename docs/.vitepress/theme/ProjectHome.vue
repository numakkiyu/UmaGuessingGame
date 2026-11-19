<script setup lang="ts">
import ModeGallery from "./ModeGallery.vue";
import { computed } from "vue";
import { useData, withBase } from "vitepress";
const { lang } = useData();
const language = computed(() =>
  lang.value === "ja" ? "ja" : lang.value === "zh-Hant" ? "zh-Hant" : "zh-Hans",
);
const prefix = computed(() =>
  language.value === "zh-Hans" ? "/" : `/${language.value}/`,
);
const link = (name: string) => withBase(prefix.value + name);
const words = {
  "zh-Hans": {
    title: "你有多了解\n《赛马娘》？",
    lead: "从九项线索猜出角色，也可以拼记忆、连赛场，和朋友办一场课间同乐赛",
    play: "了解玩法",
    start: "运行游戏",
    screenshot: "游戏大厅，桌面界面",
    mobile: "随机匹配，手机界面",
    artwork: "待兼唐怀瑟、特别周、东海帝王和黄金船的卡牌派对",
    title2: "按自己的节奏，或和大家一起",
    modes: [
      [
        "单人挑战",
        "独自推理，随时继续",
        "不必注册，准备好题目后即可游玩。支持的单人玩法可保存进度，按自己的节奏完成",
      ],
      [
        "好友游玩",
        "分享房间码，约好就开局",
        "沿用全局昵称与头像，创建房间后邀请朋友，同一局中查看彼此的作答进度",
      ],
      [
        "随机匹配",
        "找位训练员切磋",
        "选择开放的双人或四人赛制，找到玩家并确认后进入对局",
      ],
      [
        "派对模式",
        "把同乐赛交给主持人",
        "安排队伍、玩法与回合，用展示大屏分享赛况。主持权限可以转交",
      ],
    ],
    title3: "玩法图鉴",
    games: [
      ["角色知识", "经典弗一把、适性交叉占格、档案排序与血统"],
      ["观察与记忆", "记忆翻牌、听音辨马"],
      ["历史与关联", "历史赛况复原、同场之路"],
      ["策略与同乐", "公开编队、回合策略、团队拼图、默契问答等"],
    ],
    availability: "具体开放的玩法由站点设置与题库决定",
    title4: "从这里继续",
    routes: [
      ["快速开始", "把游戏运行起来"],
      ["配置参考", "选择玩法和联机方式"],
      ["技术架构说明", "了解模块与数据流"],
      ["第三方依赖与资料来源", "查看出典和许可"],
    ],
    language: "简体中文、繁體中文、日本語",
    responsive: "桌面、平板与手机",
    note: "非官方同人开源项目，由 BHCN STUDIO 维护",
  },
  "zh-Hant": {
    title: "你有多了解\n《賽馬娘》？",
    lead: "從九項提示猜出角色，也能考驗記憶、串連賽場，和朋友辦一場課間同樂賽",
    play: "了解玩法",
    start: "執行遊戲",
    screenshot: "遊戲大廳，桌面介面",
    mobile: "隨機配對，手機介面",
    artwork: "待兼詩歌劇、特別週、東海帝皇與黃金船的卡牌派對",
    title2: "照自己的步調，也能和大家一起玩",
    modes: [
      [
        "單人挑戰",
        "自己推理，隨時接著玩",
        "不必註冊，備妥題目就能遊玩。支援的單人玩法可儲存進度，依自己的步調完成",
      ],
      [
        "好友遊玩",
        "分享房間代碼，一起開局",
        "沿用全域暱稱與頭像，建立房間後邀請朋友，對局中可查看彼此的作答進度",
      ],
      [
        "隨機配對",
        "找位訓練員切磋",
        "選擇開放的雙人或四人賽制，配對並確認後進入對局",
      ],
      [
        "派對模式",
        "交給主持人安排同樂賽",
        "安排隊伍、玩法與回合，搭配大螢幕分享賽況，主持權也能轉交",
      ],
    ],
    title3: "玩法圖鑑",
    games: [
      ["角色知識", "經典猜角色、適性交叉占格、檔案排序與血統"],
      ["觀察與記憶", "記憶翻牌、聽音辨角色"],
      ["歷史與關聯", "歷史賽況重現、同場之路"],
      ["策略與同樂", "公開編隊、回合策略、團隊拼圖、默契問答等"],
    ],
    availability: "可玩的項目依站點設定與題庫而定",
    title4: "接著了解",
    routes: [
      ["快速开始", "把遊戲執行起來"],
      ["配置参考", "選擇玩法與連線方式"],
      ["技术架构说明", "了解模組與資料流程"],
      ["第三方依赖与资料来源", "查看出處與授權"],
    ],
    language: "简体中文、繁體中文、日本語",
    responsive: "桌面、平板與手機",
    note: "非官方同人開源專案，由 BHCN STUDIO 維護",
  },
  ja: {
    title: "『ウマ娘』のこと、\nどれくらい知ってる？",
    lead: "九つのヒントでキャラクター当て。記憶やレースの知識も使って、友達とクイズ大会を開こう",
    play: "遊び方を見る",
    start: "ゲームを動かす",
    screenshot: "ゲームロビーのデスクトップ画面",
    mobile: "ランダム対戦のスマートフォン画面",
    artwork: "マチカネタンホイザ、スペシャルウィーク、トウカイテイオー、ゴールドシップのカードパーティー",
    title2: "自分のペースで、友達とも",
    modes: [
      [
        "一人で挑戦",
        "自分のペースで、続きから",
        "登録不要。問題を準備すれば遊べます。対応する一人用モードは進行状況を保存できます",
      ],
      [
        "フレンド",
        "ルームコードで友達を招待",
        "設定したニックネームとアイコンをそのまま使用。対戦中は相手の回答状況も確認できます",
      ],
      [
        "ランダム対戦",
        "ほかのトレーナーと腕試し",
        "公開されている二人・四人用ルールを選び、相手が見つかって全員が確認すると対戦開始",
      ],
      [
        "パーティー",
        "進行役と楽しむクイズ大会",
        "チーム、種目、ラウンドを設定して、大画面にも進行状況を表示。進行役の引き継ぎにも対応",
      ],
    ],
    title3: "遊び方ガイド",
    games: [
      ["キャラクター知識", "キャラクター当て、適性マス埋め、並べ替え、血統"],
      ["観察と記憶", "神経衰弱、声当て"],
      ["レースの歴史", "着順の再現、共通レースのつながり"],
      [
        "作戦とパーティー",
        "編成、ターン制レース、チームパズル、以心伝心クイズなど",
      ],
    ],
    availability: "利用できる種目はサイトの設定と問題データによって異なります",
    title4: "詳しく知る",
    routes: [
      ["快速开始", "ゲームを起動する"],
      ["配置参考", "種目と接続方法を設定する"],
      ["技术架构说明", "構成とデータの流れ"],
      ["第三方依赖与资料来源", "出典とライセンス"],
    ],
    language: "简体中文、繁體中文、日本語",
    responsive: "PC・タブレット・スマートフォン",
    note: "BHCN STUDIO が開発・保守する非公式の同人オープンソースゲーム",
  },
};
const t = computed(() => words[language.value]);
</script>

<template>
  <div class="project-home">
    <section class="project-hero">
      <div class="project-hero-copy">
        <div class="project-heading">
          <p class="project-name">UmaGuessingGame</p>
          <h1>{{ t.title }}</h1>
        </div>
        <div class="project-hero-intro">
          <p class="project-lead">{{ t.lead }}</p>
          <div class="project-actions">
            <a class="project-primary" :href="link('玩法与规则')">{{ t.play }}</a
            ><a class="project-secondary" :href="link('快速开始')">{{
              t.start
            }}</a>
          </div>
          <p class="project-formats">{{ t.language }}<br />{{ t.responsive }}</p>
        </div>
      </div>
      <figure class="project-scene">
        <img
          :src="withBase('/media/party-keyvisual.webp')"
          :alt="t.artwork"
          width="2048"
          height="1152"
          fetchpriority="high"
        />
      </figure>
    </section>
    <section class="project-screens" :aria-label="t.screenshot">
      <img
        class="project-screen-main"
        :src="withBase(`/media/lobby-${language}.webp`)"
        :alt="t.screenshot"
        width="1440"
        height="1040"
        loading="lazy"
      />
      <img
        class="project-screen-mobile"
        :src="withBase(`/media/mobile-${language}.webp`)"
        :alt="t.mobile"
        width="390"
        height="844"
        loading="lazy"
      />
    </section>
    <section class="project-section">
      <h2>{{ t.title2 }}</h2>
      <div class="project-modes">
        <article v-for="(mode, i) in t.modes" :key="mode[0]">
          <span class="mode-symbol" aria-hidden="true">{{
            ["◇", "↗", "⚑", "✦"][i]
          }}</span>
          <div>
            <h3>{{ mode[0] }}</h3>
            <p class="mode-intro">{{ mode[1] }}</p>
            <p>{{ mode[2] }}</p>
          </div>
        </article>
      </div>
    </section>
    <section class="project-section project-games">
      <div class="project-section-heading"><h2>{{ t.title3 }}</h2><a :href="link('玩法与规则')">{{ t.play }} <span aria-hidden="true">↗</span></a></div>
      <ModeGallery compact />
      <p class="project-footnote">{{ t.availability }}</p>
    </section>
    <section class="project-section">
      <h2>{{ t.title4 }}</h2>
      <div class="project-routes">
        <a v-for="route in t.routes" :key="route[0]" :href="link(route[0])"
          ><span>{{ route[1] }}</span
          ><span aria-hidden="true">↗</span></a
        >
      </div>
    </section>
    <p class="project-colophon">{{ t.note }}</p>
  </div>
</template>
