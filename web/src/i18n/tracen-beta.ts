import { registerMessages, type Language } from ".";

const simplified = {
  beta: "beta", account: "训练员账户", challenge: "特雷森挑战赛", board: "训练员挑战榜", guest: "训练员",
  challengeMode: "经典弗一把", challengeIntro: "根据九项线索，猜出这位马娘", ruleWrong: "最多猜错 {{count}} 次", ruleTime: "每场 {{minutes}} 分钟", ruleTie: "同分并列",
  start: "开始挑战", signIn: "登录账户", loading: "正在准备题目", remaining: "还可猜错 {{count}} 次", time: "剩余时间",
  resultTitle: "本场成绩", score: "成绩", points: "{{score}} 分", resultBoard: "查看挑战榜",
  period: "榜单范围", boardScope: "经典弗一把 · 单人成绩", boardRule: "同分并列，各玩法分别排名", rank: "名次", position: "第 {{rank}} 名", trainer: "训练员", publicId: "训练员编号 {{id}}",
  empty: "暂无上榜成绩", emptyDetail: "完成挑战后可参加排名", boardLoading: "正在读取挑战榜", boardError: "挑战榜加载失败", boardErrorDetail: "请稍后重试", retry: "重试", record: "我的挑战记录", noRecord: "暂无上榜成绩",
};
type Messages = { [K in keyof typeof simplified]: string };
export const tracenBeta: Record<Language, Messages> = {
  "zh-Hans": simplified,
  "zh-Hant": {
    beta: "beta", account: "訓練員帳號", challenge: "特雷森挑戰賽", board: "訓練員挑戰榜", guest: "訓練員",
    challengeMode: "經典猜角色", challengeIntro: "跟著九項提示，猜出這位賽馬娘", ruleWrong: "最多猜錯 {{count}} 次", ruleTime: "每場 {{minutes}} 分鐘", ruleTie: "同分並列",
    start: "開始挑戰", signIn: "登入帳號", loading: "正在準備題目", remaining: "還能猜錯 {{count}} 次", time: "剩餘時間",
    resultTitle: "本場成績", score: "成績", points: "{{score}} 分", resultBoard: "查看挑戰榜",
    period: "榜單範圍", boardScope: "經典猜角色 · 個人成績", boardRule: "同分並列，各玩法分開排名", rank: "名次", position: "第 {{rank}} 名", trainer: "訓練員", publicId: "訓練員編號 {{id}}",
    empty: "尚無上榜成績", emptyDetail: "完成挑戰後即可參加排名", boardLoading: "正在讀取挑戰榜", boardError: "無法載入挑戰榜", boardErrorDetail: "請稍後再試", retry: "重試", record: "我的挑戰紀錄", noRecord: "尚無上榜成績",
  },
  ja: {
    beta: "beta", account: "トレーナーアカウント", challenge: "トレセン腕試し", board: "トレーナーチャレンジランキング", guest: "トレーナー",
    challengeMode: "キャラクター当て", challengeIntro: "九つのヒントから、お題のウマ娘を当てよう", ruleWrong: "間違えられるのは {{count}} 回まで", ruleTime: "制限時間 {{minutes}} 分", ruleTie: "同点は同順位",
    start: "チャレンジを始める", signIn: "ログイン", loading: "お題を準備中", remaining: "あと {{count}} 回まで間違えられます", time: "残り時間",
    resultTitle: "今回の結果", score: "スコア", points: "{{score}} 点", resultBoard: "ランキングを見る",
    period: "ランキングの期間", boardScope: "キャラクター当て・個人成績", boardRule: "同点は同順位、種目別に集計", rank: "順位", position: "{{rank}} 位", trainer: "トレーナー", publicId: "トレーナー番号 {{id}}",
    empty: "ランキングの記録はまだありません", emptyDetail: "チャレンジを完了するとランキングに参加できます", boardLoading: "ランキングを読み込み中", boardError: "ランキングを読み込めませんでした", boardErrorDetail: "少し待ってからお試しください", retry: "再試行", record: "自分の挑戦記録", noRecord: "ランキングの記録はまだありません",
  },
};
registerMessages("tracenBeta", tracenBeta);
