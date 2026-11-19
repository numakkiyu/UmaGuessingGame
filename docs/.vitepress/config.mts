import games from "./theme/games.json";
import { defineConfig, type DefaultTheme } from "vitepress";

const docsBase = process.env.DOCS_BASE || "/UmaGuessingGame/";
const repo = "https://github.com/numakkiyu/UmaGuessingGame";
const paths = {
  play: "玩法与规则",
  browser: "浏览器支持",
  start: "快速开始",
  config: "配置参考",
  linux: "Linux部署指南",
  mac: "macOS部署指南",
  windows: "Windows部署指南",
  maintenance: "维护与更新指南",
  migration: "V2迁移指南",
  account: "账户登录与游玩准入",
  architecture: "技术架构说明",
  api: "API与实时通信",
  content: "内容编译",
  ui: "界面与交互规范",
  commands: "测试与质量检查",
  sources: "第三方依赖与资料来源",
  audio: "音频编码与压缩",
  scenes: "场景素材说明",
  docs: "文档维护",
  faq: "常见问题",
  index: "文档索引",
  contribute: "参与贡献",
  security: "安全策略",
  conduct: "社区准则",
};
type Key = keyof typeof paths;
type Text = {
  brand: string;
  description: string;
  groups: string[];
  nav: string[];
  labels: Record<Key, string>;
  outline: string;
  previous: string;
  next: string;
  menu: string;
  top: string;
  language: string;
  appearance: string;
  footer: string;
};
const texts: Record<string, Text> = {
  root: {
    brand: "马娘猜猜乐",
    description: "猜角色、拼记忆，与朋友相约特雷森的课间时光",
    groups: ["游玩指南", "运行与部署", "配置与账户", "开发参考", "素材与项目"],
    nav: ["玩法", "运行游戏", "开发文档", "源码"],
    labels: {
      play: "玩法与规则",
      browser: "浏览器支持",
      start: "快速开始",
      config: "配置参考",
      linux: "Linux 部署",
      mac: "macOS 部署",
      windows: "Windows 部署",
      maintenance: "维护与更新",
      migration: "V2 迁移",
      account: "实验性账户登录",
      architecture: "架构与模块",
      api: "API 与实时通信",
      content: "题库与内容编译",
      ui: "界面与交互",
      commands: "开发命令",
      sources: "资料来源与许可",
      audio: "语音素材",
      scenes: "背景素材",
      docs: "文档站维护",
      faq: "常见问题",
      index: "文档导航",
      contribute: "参与贡献",
      security: "安全策略",
      conduct: "社区准则",
    },
    outline: "本页内容",
    previous: "上一页",
    next: "下一页",
    menu: "目录",
    top: "返回顶部",
    language: "选择语言",
    appearance: "切换明暗主题",
    footer: "代码采用 MIT 许可 · 角色与素材权利归各自权利人",
  },
  "zh-Hant": {
    brand: "馬娘猜猜樂",
    description: "猜角色、拼記憶，和朋友一起度過特雷森的課間時光",
    groups: ["遊玩指南", "執行與部署", "設定與帳號", "開發文件", "素材與專案"],
    nav: ["玩法", "執行遊戲", "開發文件", "原始碼"],
    labels: {
      play: "玩法與規則",
      browser: "瀏覽器支援",
      start: "快速開始",
      config: "設定參考",
      linux: "Linux 部署",
      mac: "macOS 部署",
      windows: "Windows 部署",
      maintenance: "維護與更新",
      migration: "遷移至 V2",
      account: "實驗性帳號登入",
      architecture: "架構與模組",
      api: "API 與即時通訊",
      content: "題庫與內容編譯",
      ui: "介面與互動",
      commands: "開發指令",
      sources: "資料來源與授權",
      audio: "語音素材",
      scenes: "背景素材",
      docs: "文件站維護",
      faq: "常見問題",
      index: "文件導覽",
      contribute: "貢獻指南",
      security: "安全政策",
      conduct: "社群準則",
    },
    outline: "本頁內容",
    previous: "上一頁",
    next: "下一頁",
    menu: "目錄",
    top: "回到頂端",
    language: "選擇語言",
    appearance: "切換明暗主題",
    footer: "程式碼採 MIT 授權 · 角色與素材權利歸各權利人所有",
  },
  ja: {
    brand: "ウマ娘あてクイズ",
    description: "ウマ娘の知識と記憶で遊ぶ、みんなの放課後クイズ",
    groups: [
      "遊び方",
      "起動とデプロイ",
      "設定とアカウント",
      "開発リファレンス",
      "素材とプロジェクト",
    ],
    nav: ["遊び方", "ゲームを動かす", "開発資料", "ソース"],
    labels: {
      play: "遊び方とルール",
      browser: "対応ブラウザー",
      start: "クイックスタート",
      config: "設定リファレンス",
      linux: "Linux",
      mac: "macOS",
      windows: "Windows",
      maintenance: "運用と更新",
      migration: "V2 への移行",
      account: "実験的なログイン機能",
      architecture: "構成とモジュール",
      api: "API とリアルタイム通信",
      content: "問題データのビルド",
      ui: "画面と操作",
      commands: "開発コマンド",
      sources: "出典とライセンス",
      audio: "音声素材",
      scenes: "背景素材",
      docs: "ドキュメントサイトの管理",
      faq: "よくある質問",
      index: "ドキュメント一覧",
      contribute: "コントリビューション",
      security: "セキュリティ方針",
      conduct: "行動規範",
    },
    outline: "このページの内容",
    previous: "前のページ",
    next: "次のページ",
    menu: "メニュー",
    top: "先頭へ戻る",
    language: "言語を選ぶ",
    appearance: "配色を切り替える",
    footer:
      "コードは MIT ライセンス · キャラクターや素材の権利は各権利者に帰属します",
  },
};
const grouped: Key[][] = [
  ["play", "browser", "faq"],
  ["start", "linux", "mac", "windows", "maintenance", "migration"],
  ["config", "account"],
  ["architecture", "api", "content", "ui", "commands"],
  [
    "sources",
    "audio",
    "scenes",
    "contribute",
    "security",
    "conduct",
    "docs",
    "index",
  ],
];
function locale(key: string) {
  const t = texts[key];
  const prefix = key === "root" ? "/" : `/${key}/`;
  const link = (key: Key) => prefix + paths[key];
  return {
    label:
      key === "root" ? "简体中文" : key === "zh-Hant" ? "繁體中文" : "日本語",
    lang: key === "root" ? "zh-Hans" : key,
    title: t.brand,
    description: t.description,
    themeConfig: {
      nav: [
        { text: t.nav[0], link: link("play") },
        { text: t.nav[1], link: link("start") },
        { text: t.nav[2], link: link("architecture") },
        { text: t.nav[3], link: repo },
      ],
      sidebar: grouped.map((keys, i) => ({
        text: t.groups[i],
        collapsed: i > 0,
        items: keys.map((item) => ({ text: t.labels[item], link: link(item), ...(item === "play" ? { collapsed: true, items: games.map(game => ({ text: game.titles[key === "root" ? 0 : key === "zh-Hant" ? 1 : 2], link: `${prefix}play/${game.id}` })) } : {}) })),
      })),
      outline: { label: t.outline, level: [2, 3] },
      skipToContentLabel: key === "ja" ? "本文へ移動" : key === "zh-Hant" ? "跳至主要內容" : "跳到主要内容",
      docFooter: { prev: t.previous, next: t.next },
      sidebarMenuLabel: t.menu,
      returnToTopLabel: t.top,
      langMenuLabel: t.language,
      darkModeSwitchLabel: t.appearance,
      darkModeSwitchTitle:
        key === "ja"
          ? "ダークテーマに切り替える"
          : key === "zh-Hant"
            ? "切換深色主題"
            : "切换深色主题",
      lightModeSwitchTitle:
        key === "ja"
          ? "ライトテーマに切り替える"
          : key === "zh-Hant"
            ? "切換淺色主題"
            : "切换浅色主题",
      footer: { message: t.footer, copyright: "BHCN STUDIO · UmaGuessingGame" },
      editLink: {
        pattern: `${repo}/edit/main/docs/:path`,
        text:
          key === "ja"
            ? "このページを編集"
            : key === "zh-Hant"
              ? "編輯此頁"
              : "编辑此页",
      },
    } satisfies DefaultTheme.Config,
  };
}
export default defineConfig({
  base: docsBase,
  title: "马娘猜猜乐",
  description: texts.root.description,
  cleanUrls: true,
  markdown: {
    image: { lazyLoading: true },
    config(md) {
      md.renderer.rules.table_open = (tokens, index, options, env, renderer) => {
        const path = env.relativePath || "";
        const label = path.startsWith("ja/") ? "表" : path.startsWith("zh-Hant/") ? "資料表" : "数据表";
        return `<div class="doc-table" tabindex="0" role="region" aria-label="${label}">` + renderer.renderToken(tokens, index, options);
      };
      md.renderer.rules.table_close = () => "</table></div>\n";
    },
  },
  lastUpdated: false,
  head: [
    ["link", { rel: "stylesheet", href: `${docsBase}fonts/source-han-sans.css` }],
    ["meta", { name: "theme-color", content: "#7772af" }],
    ["link", { rel: "icon", type: "image/png", sizes: "64x64", href: `${docsBase}media/favicon-64.png` }],
    ["link", { rel: "apple-touch-icon", sizes: "180x180", href: `${docsBase}media/apple-touch-icon.png` }],
  ],
  locales: {
    root: locale("root"),
    "zh-Hant": locale("zh-Hant"),
    ja: locale("ja"),
  },
  themeConfig: {
    logo: { src: "/media/party-logo.webp", alt: "UmaGuessingGame" },
    socialLinks: [{ icon: "github", link: repo }],
    search: {
      provider: "local",
      options: {
        locales: {
          root: {
            translations: {
              button: { buttonText: "搜索文档", buttonAriaLabel: "搜索文档" },
              modal: {
                displayDetails: "显示详情",
                resetButtonTitle: "清空",
                backButtonTitle: "返回",
                noResultsText: "没有找到相关内容",
                footer: {
                  selectText: "选择",
                  selectKeyAriaLabel: "选择",
                  navigateText: "切换",
                  navigateUpKeyAriaLabel: "上一项",
                  navigateDownKeyAriaLabel: "下一项",
                  closeText: "关闭",
                  closeKeyAriaLabel: "关闭",
                },
              },
            },
          },
          "zh-Hant": {
            translations: {
              button: { buttonText: "搜尋文件", buttonAriaLabel: "搜尋文件" },
              modal: {
                displayDetails: "顯示詳情",
                resetButtonTitle: "清除",
                backButtonTitle: "返回",
                noResultsText: "找不到相關內容",
                footer: {
                  selectText: "選擇",
                  selectKeyAriaLabel: "選擇",
                  navigateText: "切換",
                  navigateUpKeyAriaLabel: "上一項",
                  navigateDownKeyAriaLabel: "下一項",
                  closeText: "關閉",
                  closeKeyAriaLabel: "關閉",
                },
              },
            },
          },
          ja: {
            translations: {
              button: {
                buttonText: "ドキュメントを検索",
                buttonAriaLabel: "ドキュメントを検索",
              },
              modal: {
                displayDetails: "詳細を表示",
                resetButtonTitle: "クリア",
                backButtonTitle: "戻る",
                noResultsText: "該当する内容が見つかりません",
                footer: {
                  selectText: "選択",
                  selectKeyAriaLabel: "選択",
                  navigateText: "移動",
                  navigateUpKeyAriaLabel: "前へ",
                  navigateDownKeyAriaLabel: "次へ",
                  closeText: "閉じる",
                  closeKeyAriaLabel: "閉じる",
                },
              },
            },
          },
        },
      },
    },
  },
});
