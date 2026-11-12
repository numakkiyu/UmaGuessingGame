import { registerMessages } from ".";
export const localNotices = {
  "zh-Hans": {
    incompletePractice: "这份练习资料不完整，请重新准备，旧记录请先保留",
    shellMissing: "练习页面暂时没有准备好，请重试", shellTimeout: "练习页面准备超时，请重试", shellInterrupted: "练习页面准备中断，请重试", avatarsMissing: "有些头像还没准备好，请重试",
    storage: "本局可以继续，但进度暂时无法保存。请允许浏览器保存资料，并留意设备的剩余空间", conflict: "这局刚刚在另一个页面有了新进展，请载入最新记录后继续", blocked: "请先关闭其他练习页面，再重新准备资料", missingPack: "没有找到这局的练习资料，可以导入之前导出的完整记录", invalidBudget: "请选择 4 到 12 次错误机会", invalidArchive: "这不是完整的练习记录", unreadableArchive: "暂时无法读取这份记录，请保留原文件", incompatibleArchive: "这份记录与当前玩法不兼容，请保留原文件", invalidGuesses: "记录中的作答不完整，请保留原文件", invalidQuestion: "记录中的题目信息不完整", afterEnd: "记录包含结束后的作答，请保留原文件", invalidPack: "这份练习资料不完整，请重新准备后再试", missingQuestion: "这份记录缺少题目资料，请重新导入完整记录", unknownGuess: "这份记录包含无法识别的作答", ended: "这局已经结束，来开下一局吧", duplicate: "已经猜过她啦，换一位试试", unknownCharacter: "没有找到这位赛马娘", missingGame: "没有找到这份练习记录，可以导入之前保存的文件，或再开一局", largeArchive: "这份文件太大，请选择从自由练习导出的单局记录",
  },
  "zh-Hant": {
    incompletePractice: "這份練習資料不完整，請重新準備，原有紀錄請先保留",
    shellMissing: "練習頁面還沒準備好，請再試一次", shellTimeout: "準備練習頁面花了太久，請再試一次", shellInterrupted: "練習頁面的準備中斷了，請再試一次", avatarsMissing: "部分頭像還沒準備好，請再試一次",
    storage: "這一局仍可繼續，但目前無法儲存進度。請允許瀏覽器儲存資料，並確認裝置還有足夠空間", conflict: "另一個頁面剛更新了這一局，請載入最新紀錄後繼續", blocked: "請先關閉其他練習頁面，再重新準備資料", missingPack: "找不到這一局的練習資料，可以匯入之前匯出的完整紀錄", invalidBudget: "請選擇 4 到 12 次猜錯次數", invalidArchive: "這不是完整的練習紀錄", unreadableArchive: "目前無法讀取這份紀錄，請保留原始檔案", incompatibleArchive: "這份紀錄不適用目前的玩法，請保留原始檔案", invalidGuesses: "紀錄中的作答不完整，請保留原始檔案", invalidQuestion: "紀錄中的題目資料不完整", afterEnd: "紀錄包含結束後的作答，請保留原始檔案", invalidPack: "這份練習資料不完整，請重新準備後再試", missingQuestion: "紀錄缺少題目資料，請重新匯入完整紀錄", unknownGuess: "紀錄中有無法辨識的作答", ended: "這一局已經結束，來開下一局吧", duplicate: "已經猜過她囉，換一位猜猜看吧", unknownCharacter: "找不到這位賽馬娘", missingGame: "找不到這份練習紀錄，可以匯入之前儲存的檔案，或再開一局", largeArchive: "這份檔案太大，請選擇自由練習匯出的單局紀錄",
  },
  ja: {
    incompletePractice: "練習データがそろっていません。元の記録は残して、データを準備し直してください",
    shellMissing: "練習ページを準備できませんでした。もう一度お試しください", shellTimeout: "練習ページの準備に時間がかかりすぎました。もう一度お試しください", shellInterrupted: "練習ページの準備が中断されました。もう一度お試しください", avatarsMissing: "一部のアイコンを準備できませんでした。もう一度お試しください",
    storage: "プレイは続けられますが、進行状況を保存できません。ブラウザーでデータの保存を許可し、端末の空き容量も確認してください", conflict: "別のページでこのゲームが更新されました。最新の記録を読み込んでから続けてください", blocked: "ほかの練習ページを閉じてから、もう一度データを準備してください", missingPack: "このゲームの問題データが見つかりません。以前書き出した記録全体を読み込んでください", invalidBudget: "間違えられる回数は 4～12 回で選んでください", invalidArchive: "完全な練習記録ではありません", unreadableArchive: "この記録を読み込めません。元のファイルは残しておいてください", incompatibleArchive: "この記録は現在のルールに対応していません。元のファイルは残しておいてください", invalidGuesses: "記録の回答データがそろっていません。元のファイルは残しておいてください", invalidQuestion: "記録の問題データがそろっていません", afterEnd: "終了後の回答が記録に含まれています。元のファイルは残しておいてください", invalidPack: "練習データがそろっていません。もう一度準備してください", missingQuestion: "問題データがありません。記録全体を読み込み直してください", unknownGuess: "記録に読み取れない回答が含まれています", ended: "このゲームは終了しました。次の問題に挑戦しよう", duplicate: "そのウマ娘はもう予想済みです。別のウマ娘を選んでみよう", unknownCharacter: "そのウマ娘が見つかりません", missingGame: "練習記録が見つかりません。保存したファイルを読み込むか、新しく始めてください", largeArchive: "ファイルが大きすぎます。練習から書き出した一回分の記録を選んでください",
  },
} as const;
export type LocalNoticeKey = keyof typeof localNotices["zh-Hans"];
registerMessages("localNotices", localNotices);
