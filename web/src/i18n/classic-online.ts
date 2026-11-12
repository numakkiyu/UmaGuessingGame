import { registerMessages } from ".";

export const classicOnlineCopy = {
  "zh-Hans": {
    paused:"暂停中",pausedDetail:"已确认的猜测都还在，等主持人继续后再猜",
    missingTitle:"缺少房间号",missingDetail:"链接不完整，请从大厅重新进入",loadingRoom:"正在读取房间",missingRoom:"房间不存在",missingRoomDetail:"链接可能已失效，或者房间已被清理",
    notStarted:"尚未开局",refresh:"刷新本局状态",waitingTitle:"这一局还没开始",waitingDetail:"房主开始后，就能在这里搜索角色、查看猜测记录",waitingBack:"返回房间等待区",loadingRound:"正在读取本局状态",
    lockedTitle:"本题作答结束，等待揭晓",lockedDetail:"已确认的猜测会保留，结果整理好后会在这里显示",selected:"已选 {{name}}",searchHint:"中、日、英文名字或常见外号都可以",submitting:"提交中…",confirming:"确认中…",notAccepting:"当前不能猜测",guessPlaceholder:"输入一位马娘开始猜",notice:"提示",refreshState:"刷新状态",problemId:"问题编号",
    rejectedTitle:"还没确认这次猜测",rejectedDetail:"还没确认这次猜测是否记下，请刷新查看本局状态，再决定下一步",confirmTitle:"正在确认上次猜测",confirmDetail:"上次结果还不确定，先核对这一猜，确认前不能猜其他角色",confirmAction:"确认上次猜测",expiredTitle:"需要查看最新进度",expiredDetail:"这次待确认的猜测已过了可核对时间。先查看本局最新记录，再继续",latest:"查看最新记录",limited:"暂时无法保留待确认的操作。请保持此页打开，先核对结果再离开",
    history:"猜测记录",total:"共 {{count}} 次",noGuess:"还没有猜测",noGuessDetail:"选中一位角色并点“猜一次”，这里会显示九项提示的对上、接近或不符",view:"猜测记录视图",info:"本局信息",used:"已猜 / 错误额度",attempts:"{{count}} 次",budget:"{{count}} / {{budget}} 次",room:"房间",shadows:"对手影子",shadowAttempt:"对手第 {{count}} 次",unknownField:"其他提示",
    rules:"规则速查",correctRule:"✓ 对上：这一项与答案一致",nearRule:"≈ 接近：按本项规则，与你要找的角色较接近",wrongRule:"× 不符：这一项不同",guessRule:"猜对不扣额度；不能重复猜同一位",fullRules:"完整规则",more:"更多操作",surrendered:"已认输",surrenderedDetail:"你所代表的一方已结束作答。请等待本局揭晓，其他玩家按既定规则继续",surrenderDetail:"认输后，你所代表的一方不能继续作答，不能撤销",surrendering:"正在认输…",surrender:"认输",remaining:"剩余猜错次数",
    solved:"好耶，猜中了！",failed:"这次没猜中",ended:"本局已结束",solvedDetail:"你用了 {{count}} 次找到了她，漂亮！",failedDetail:"这局共猜了 {{count}} 次，再来挑战一局吧",endedDetail:"本局已结束，不能再猜测",answer:"答案：",answerRevealed:"答案已揭晓",home:"返回大厅",seeRules:"查看规则",shareSurrendered:"我在赛马娘猜猜乐里猜了 {{count}} 次，已结束本局作答",shareSolved:"我在赛马娘猜猜乐里猜中了，用了 {{count}} 次",shareFailed:"我在赛马娘猜猜乐里猜了 {{count}} 次，这次没猜中",shareEnded:"我在赛马娘猜猜乐里猜了 {{count}} 次",shareAnswer:"答案是「{{name}}」",
  },
  "zh-Hant": {
    paused:"暫停中",pausedDetail:"已確認的猜測都有保留，等主持人繼續後再猜",
    missingTitle:"缺少房間資訊",missingDetail:"連結不完整，請從大廳重新加入",loadingRoom:"正在讀取房間",missingRoom:"找不到房間",missingRoomDetail:"連結可能已失效，或房間已被清除",
    notStarted:"尚未開始",refresh:"更新這局的進度",waitingTitle:"這局還沒開始",waitingDetail:"房主開始後，就能在這裡搜尋角色、查看猜測紀錄",waitingBack:"回到房間等候區",loadingRound:"正在讀取這局的進度",
    lockedTitle:"作答已截止，等候揭曉",lockedDetail:"已確認的猜測都會保留，結果整理好後會顯示在這裡",selected:"已選 {{name}}",searchHint:"中、日、英文名稱或常見暱稱都可以",submitting:"正在送出…",confirming:"正在確認…",notAccepting:"目前無法猜測",guessPlaceholder:"輸入一位賽馬娘開始猜",notice:"提醒",refreshState:"更新進度",problemId:"問題編號",
    rejectedTitle:"還沒確認這次猜測",rejectedDetail:"還不確定這次猜測是否已記下，請先更新這局的進度",confirmTitle:"正在確認上次的猜測",confirmDetail:"上次的結果還不確定，請先確認這一猜；確認前不能猜其他角色",confirmAction:"確認上次的猜測",expiredTitle:"請先查看最新進度",expiredDetail:"這次待確認的猜測已超過可查詢時間，請先查看這局的最新紀錄",latest:"查看最新紀錄",limited:"目前無法保留待確認的操作，請保持頁面開啟，確認結果後再離開",
    history:"猜測紀錄",total:"共 {{count}} 次",noGuess:"還沒有猜測",noGuessDetail:"選好一位角色並按「猜一次」，這裡就會顯示九項提示的符合、接近或不符",view:"猜測紀錄的顯示方式",info:"這局的資訊",used:"已猜 / 可猜錯次數",attempts:"{{count}} 次",budget:"{{count}} / {{budget}} 次",room:"房間",shadows:"對手的提示紀錄",shadowAttempt:"對手第 {{count}} 次",unknownField:"其他提示",
    rules:"規則速查",correctRule:"✓ 符合：這一項與答案相同",nearRule:"≈ 接近：依這一項的規則，與目標角色較接近",wrongRule:"× 不符：這一項不同",guessRule:"猜對不扣次數；不能重複猜同一位",fullRules:"完整規則",more:"更多操作",surrendered:"已認輸",surrenderedDetail:"你所代表的一方已結束作答，請等候這局揭曉，其他玩家仍依既定規則繼續",surrenderDetail:"認輸後，你所代表的一方無法繼續作答，也不能復原",surrendering:"正在認輸…",surrender:"認輸",remaining:"還能猜錯",
    solved:"太好了，猜中了！",failed:"這次沒猜中",ended:"這局已結束",solvedDetail:"用了 {{count}} 次就找到她，漂亮！",failedDetail:"這局共猜了 {{count}} 次，再挑戰一局吧",endedDetail:"這局已結束，不能繼續猜測",answer:"答案：",answerRevealed:"答案已揭曉",home:"回到大廳",seeRules:"查看規則",shareSurrendered:"我在賽馬娘猜猜樂猜了 {{count}} 次，已結束這局作答",shareSolved:"我在賽馬娘猜猜樂猜中了，用了 {{count}} 次",shareFailed:"我在賽馬娘猜猜樂猜了 {{count}} 次，這次沒猜中",shareEnded:"我在賽馬娘猜猜樂猜了 {{count}} 次",shareAnswer:"答案是「{{name}}」",
  },
  ja: {
    paused:"一時停止中",pausedDetail:"受け付け済みの予想は残っています。ホストが再開するまでお待ちください",
    missingTitle:"ルームの情報がありません",missingDetail:"リンクが途中で切れています。ロビーから入り直してください",loadingRoom:"ルームを読み込み中",missingRoom:"ルームが見つかりません",missingRoomDetail:"リンクの期限が切れたか、ルームが削除された可能性があります",
    notStarted:"開始前",refresh:"このゲームの進行状況を更新",waitingTitle:"まだ始まっていません",waitingDetail:"ホストが始めると、ここでウマ娘を検索して予想履歴を確認できます",waitingBack:"ルームの待機画面に戻る",loadingRound:"ゲームの進行状況を読み込み中",
    lockedTitle:"回答を締め切りました。結果発表をお待ちください",lockedDetail:"受け付け済みの予想は残っています。集計が終わると、ここに結果が表示されます",selected:"選択中：{{name}}",searchHint:"名前やよく使われる呼び名で探せます",submitting:"送信中…",confirming:"確認中…",notAccepting:"今は予想できません",guessPlaceholder:"ウマ娘の名前を入力",notice:"お知らせ",refreshState:"進行状況を更新",problemId:"問い合わせ番号",
    rejectedTitle:"今回の予想が届いたか確認できません",rejectedDetail:"記録されたか分からないため、進行状況を更新してから次の操作を選んでください",confirmTitle:"前回の予想が届いたか確認しています",confirmDetail:"前回の結果がまだ分からないため、先に確認してください。確認が終わるまで、ほかのウマ娘は予想できません",confirmAction:"前回の予想を確認",expiredTitle:"最新の進行状況を確認してください",expiredDetail:"送信結果を確認できる時間を過ぎています。先にこのゲームの最新履歴を確認してください",latest:"最新の履歴を見る",limited:"確認待ちの操作を保存できません。このページを開いたまま結果を確認してから閉じてください",
    history:"予想履歴",total:"合計 {{count}} 回",noGuess:"まだ予想していません",noGuessDetail:"ウマ娘を選んで「予想する」を押すと、九項目の一致・近い・不一致が表示されます",view:"予想履歴の表示方法",info:"このゲームの情報",used:"予想回数 / 間違えられる回数",attempts:"{{count}} 回",budget:"{{count}} / {{budget}} 回",room:"ルーム",shadows:"相手のヒント履歴",shadowAttempt:"相手の {{count}} 回目",unknownField:"その他のヒント",
    rules:"ルール早見表",correctRule:"✓ 一致：この項目が正解と同じです",nearRule:"≈ 近い：この項目のルールでは、正解のウマ娘に近い予想です",wrongRule:"× 不一致：この項目が正解と異なります",guessRule:"正解なら回数は減りません。同じウマ娘を繰り返し選ぶことはできません",fullRules:"詳しいルール",more:"その他の操作",surrendered:"降参しました",surrenderedDetail:"あなたの側の回答は終了しました。結果発表をお待ちください。ほかの参加者はルールに従って続けます",surrenderDetail:"降参すると、あなたの側はこれ以上回答できません。取り消せません",surrendering:"降参しています…",surrender:"降参する",remaining:"あと間違えられる回数",
    solved:"やった、正解！",failed:"今回は見つかりませんでした",ended:"ゲーム終了",solvedDetail:"{{count}} 回で見つけたね。お見事！",failedDetail:"今回は {{count}} 回予想しました。次も挑戦してみよう",endedDetail:"このゲームは終了しました。これ以上は予想できません",answer:"正解：",answerRevealed:"正解が発表されました",home:"ロビーに戻る",seeRules:"ルールを見る",shareSurrendered:"ウマ娘当てで {{count}} 回予想し、降参しました",shareSolved:"ウマ娘当てで正解！{{count}} 回で見つけました",shareFailed:"ウマ娘当てで {{count}} 回予想しました。今回は正解ならず",shareEnded:"ウマ娘当てで {{count}} 回予想しました",shareAnswer:"正解は「{{name}}」でした",
  },
} as const;

registerMessages("classicOnline", classicOnlineCopy);
