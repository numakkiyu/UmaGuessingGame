import i18n, { registerMessages } from ".";
export const roundUICopy = {
 "zh-Hans": { phases:{prepared:"准备中",briefing:"看题与规则说明",collecting:"作答中",locked:"已收卷",revealed:"已揭晓",settled:"已结算",cancelled:"已取消"}, notStarted:"未开局",unknown:"正在确认进度", rejoinTitle:"需要重新进入这一局", rejoinDetail:"页面刷新后需要重新进入这一局才能继续操作。你已有的进度不会丢",rejoinAction:"重新进入这一局", kept:"已保存的草稿会保留；已提交的内容不会丢" },
 "zh-Hant": { phases:{prepared:"準備中",briefing:"看題與說明",collecting:"作答中",locked:"已收卷",revealed:"已揭曉",settled:"已結算",cancelled:"已取消"}, notStarted:"尚未開始",unknown:"正在確認進度", rejoinTitle:"請重新進入這一局", rejoinDetail:"重新載入頁面後，需要再次進入這一局才能操作，原有進度會保留",rejoinAction:"重新進入這一局", kept:"已儲存的草稿與已提交的作答都會保留" },
 ja: { phases:{prepared:"準備中",briefing:"問題・ルールの確認",collecting:"回答中",locked:"回答締め切り",revealed:"結果発表",settled:"集計完了",cancelled:"中止"}, notStarted:"開始前",unknown:"進行状況を確認中", rejoinTitle:"このゲームに入り直してください", rejoinDetail:"ページの再読み込み後、操作を続けるにはゲームに入り直す必要があります。これまでの進行状況は残っています",rejoinAction:"ゲームに入り直す", kept:"保存済みの下書きと送信済みの回答は残っています" },
} as const;
registerMessages("roundUI", roundUICopy);
export function roundText(key:string) {return i18n.t(key,{ns:"roundUI"});}
