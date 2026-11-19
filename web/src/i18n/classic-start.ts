import { registerMessages } from ".";

export const classicStart = {
  "zh-Hans": {
    title: "经典弗一把", intro: "先猜一位你熟悉的马娘，看看九项提示里哪些对上、哪些接近。猜中她就赢啦；猜错次数用完，这一局也会结束", name: "昵称", namePlaceholder: "想让大家怎么称呼你？", nameHint: "最多 24 个字符，也会用于你之后加入的房间", budget: "允许猜错的次数", budgetHint: "次（4–12 次，默认 8 次）", budgetDetail: "猜错扣一次，猜对不扣次数", steps: { session: "准备入场", room: "准备这一局", round: "准备题目", start: "准备作答" },
  },
  "zh-Hant": {
    title: "經典猜角色", intro: "先猜一位熟悉的賽馬娘，看看九項提示中哪些吻合、哪些相近。猜中她就贏囉；猜錯次數用完，這局也會結束", name: "暱稱", namePlaceholder: "希望大家怎麼稱呼你？", nameHint: "最多 24 個字元，之後加入房間也會使用這個暱稱", budget: "可以猜錯幾次？", budgetHint: "次（4–12 次，預設 8 次）", budgetDetail: "猜錯扣一次，猜對不扣次數", steps: { session: "準備入場", room: "準備這一局", round: "準備題目", start: "準備作答" },
  },
  ja: {
    title: "キャラクター当て", intro: "まずはよく知っているウマ娘を予想。九つのヒントを見ながら、少しずつ絞り込もう。正解を当てるか、間違えられる回数を使い切ると終了です", name: "ニックネーム", namePlaceholder: "みんなに呼んでほしい名前", nameHint: "24 文字まで。あとでルームに参加するときにも使います", budget: "間違えられる回数", budgetHint: "回（4～12 回、標準は 8 回）", budgetDetail: "間違えると一回減り、正解した場合は減りません", steps: { session: "参加の準備", room: "ゲームを準備", round: "問題を準備", start: "回答の準備" },
  },
} as const;

registerMessages("classicStart", classicStart);
