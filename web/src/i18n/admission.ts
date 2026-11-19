import { registerMessages } from ".";

export const admission = {
  "zh-Hans": {
    title: "训练员，准备入场", trafficTitle: "训练员，稍等一下", trafficIntro: "刚才的操作有些密集，请完成下方确认，再继续游玩", successTitle: "确认好了，一起开跑", successIntro: "正在回到刚才的页面，很快就好", successAction: "准备出发", intro: "完成下方确认，就能继续开局或加入房间", purpose: "让大家安心一起玩", waiting: "正在准备，请稍候……", ready: "准备好了，确认后继续", errorTitle: "还没完成入场确认", privacy: "不用注册，确认后就能继续一起玩，有效期间内通常无需再次确认", signals: "本次确认还会使用浏览器特征协助识别异常操作", back: "先返回", retry: "重新确认", confirm: "确认并继续", busy: "正在确认……",
    errors: { sessionLost: "离开有些久了，请先返回，再重新进入房间", loadFailed: "入场确认暂时打不开，请检查网络后重试", expired: "等候有些久了，请重新完成入场确认", widgetFailed: "入场确认暂时没能完成，请重试", incomplete: "还没完成入场确认，请再试一次", invalid: "这次确认已失效，请重新完成入场确认", uncertain: "还没收到确认结果，请稍后再点一次「确认并继续」，无需重做下方验证" },
  },
  "zh-Hant": {
    title: "訓練員，準備入場", trafficTitle: "訓練員，請稍等一下", trafficIntro: "剛才的操作較頻繁，請完成下方確認，再繼續遊玩", successTitle: "確認完成，一起出發", successIntro: "馬上返回剛才的頁面，請稍候", successAction: "準備出發", intro: "完成下方確認，就能繼續開局或加入房間", purpose: "讓大家安心同樂", waiting: "正在準備，請稍候……", ready: "準備好了，確認後繼續", errorTitle: "尚未完成入場確認", privacy: "不用註冊，確認後即可繼續同樂，有效期間內通常不必再次確認", signals: "本次確認也會使用瀏覽器特徵，協助辨識異常操作", back: "先返回", retry: "重新確認", confirm: "確認並繼續", busy: "正在確認……",
    errors: { sessionLost: "離開有一段時間了，請先返回，再重新進入房間", loadFailed: "目前無法開啟入場確認，請確認網路後再試", expired: "等候時間較久，請重新完成入場確認", widgetFailed: "目前無法完成入場確認，請再試一次", incomplete: "尚未完成入場確認，請再試一次", invalid: "這次確認已失效，請重新完成入場確認", uncertain: "尚未收到確認結果，請稍後再按一次「確認並繼續」，不必重做下方驗證" },
  },
  ja: {
    title: "トレーナーさん、入場前の確認です", trafficTitle: "トレーナーさん、少しだけ待ってね", trafficIntro: "短い間に操作が続いたため、下の確認をお願いします。終わったら、続きを楽しもう", successTitle: "確認できました。さあ、出発！", successIntro: "先ほどの画面に戻ります。もう少しだけお待ちください", successAction: "出発準備OK", intro: "下の確認を済ませると、ゲームの開始やルームへの参加を続けられます", purpose: "みんなで安心して遊ぶために", waiting: "準備しています。少々お待ちください…", ready: "準備ができました。確認して次へ進めます", errorTitle: "入場の確認が完了していません", privacy: "登録は不要です。一度確認が済めば、有効な間は通常そのまま遊べます", signals: "今回はブラウザーの特徴も、不審な操作の判別に使用します", back: "戻る", retry: "確認をやり直す", confirm: "確認して続ける", busy: "確認しています…",
    errors: { sessionLost: "時間が経ったため、いったん戻ってからルームに入り直してください", loadFailed: "入場の確認を開けませんでした。接続を確認してから、もう一度お試しください", expired: "時間が経ったため、入場の確認をやり直してください", widgetFailed: "入場の確認を完了できませんでした。もう一度お試しください", incomplete: "入場の確認がまだ完了していません。もう一度お試しください", invalid: "今回の確認は無効になりました。入場の確認をやり直してください", uncertain: "確認結果を受け取れませんでした。少し待ってから「確認して続ける」を押してください。下の認証をやり直す必要はありません" },
  },
} as const;

registerMessages("admission", admission);
