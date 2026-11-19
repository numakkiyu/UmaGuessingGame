import { registerMessages } from ".";

export const entry = {
  "zh-Hans": {
    friend: {
      title: "和朋友约一局", intro: "训练员，先选个项目。建好房间，把房间分享码发给朋友，等大家到齐再开场", loading: "正在查看可约的玩法", closed: "这里暂未开放好友约局", closedDetail: "可以返回大厅，试试其他游玩方式", mode: "这局玩什么？", name: "你的昵称", namePlaceholder: "朋友们怎么称呼你？", roomName: "房间名称（可不填）", roomPlaceholder: "例如：周末特雷森游玩会", defaultName: "{{name}}的约局", capacity: "最多几人参加？", seatsUnavailable: "这个玩法目前容纳不下所需人数，请换个玩法", seatsExact: "包含你自己，共 {{count}} 人", seatsRange: "包含你自己。这个项目可约 {{min}}–{{max}} 人", busy: "正在安排座位……", create: "创建好友房间", duration: "约 {{minutes}} 分钟一局", clue: "每队需要 3–6 人。邀请大家入场后，在等待区安排队伍；好友合作只需一队，也可以分成多队比一比", classic: "同一道题，各自作答；3 分钟内先猜中者获胜，每人可猜错 8 次。次数用完后等待对手，整场结束才揭晓；都没猜中则双方都未获胜，任一方主动认输则另一方获胜", other: "每位朋友各自入场。开场前确认大家看过规则、准备好了；需要分队办比赛，可以选择活动团队", invite: "怎么邀请朋友？", inviteDetail: "房间建好后，把房间分享码或邀请链接发给朋友。大家会使用已设置的昵称加入；刷新后仍能查看邀请，等房主开场就好",
    },
    join: {
      title: "加入房间", intro: "输入房主给你的房间分享码。加入后你会先进入等待区，房主开始对局前不会消耗作答次数", code: "房间分享码", codePlaceholder: "例如 ABCD1234", codeHint: "{{min}}–{{max}} 位字母或数字，不区分大小写", name: "昵称", namePlaceholder: "其他玩家看到的名字", nameHint: "最多 {{max}} 个字符。可以稍后在房间内修改", check: "请检查输入", invalidCode: "房间分享码应为 {{min}}–{{max}} 位字符", invalidName: "昵称需为 1–{{max}} 个字符", submitting: "正在查找房间……", confirming: "正在进入房间……", noCode: "没有房间分享码？", noCodeDetail: "请房主把房间分享码或邀请链接发给你，也可以先到大厅来一局单人挑战",
    },
    info: {
      missing: "没有找到这个玩法", check: "请检查游玩链接", checkDetail: "这条链接暂时无法打开。可以向分享链接的朋友确认，或返回大厅选择玩法", loading: "正在读取玩法信息", closed: "「{{name}}」暂未开放", closedDetail: "现在还不能开始这个玩法，可以返回大厅，选择其他已开放的项目", unavailable: "这个玩法暂不可开局", unavailableDetail: "游玩资料暂时没有准备好，请稍后再试", fromLobby: "请从大厅进入", fromLobbyDetail: "回到大厅，选择这个玩法就能开始",
    },
    start: {
      duration: "预计时长：{{duration}}", soloClosed: "这里暂未开放单人对局", soloClosedDetail: "你可以选择其他已开放的游玩方式", friend: "和朋友约一局", settings: "开局设置", name: "昵称", namePlaceholder: "其他玩家看到的名字", party: "这个玩法需要至少 {{count}} 人", partyDetail: "这个项目至少需要 {{count}} 人。建好好友房间后，把房间分享码发给朋友，大家到齐再开场", busy: "正在开局……", play: "开始对局", rules: "规则要点", defaultPlayer: "玩家", defaultRoom: "{{name}}的{{mode}}", classic: "去玩经典猜谜", steps: { session: "准备入场", room: "准备房间", round: "准备题目", start: "开始作答" }, state: { pending: "未开始", active: "进行中", done: "已完成", failed: "失败" },
    },
    unavailable: {
      audio: { title: "听音辨马暂时不能开局", detail: "这次还没有足够的语音题目。先来一局经典猜谜，或回大厅看看其他玩法吧" },
      bluff: { title: "真假档案的题目还在核对中", detail: "真假档案的题目还不够开一局，先回大厅玩点别的吧" },
      short: { title: "这个玩法的题目还不够开一局", detail: "符合这次规则的题目还不够，请返回大厅选择其他玩法" },
      generic: { title: "这个玩法暂时不能开局", detail: "当前这局玩法还缺少必需的内容，暂时开不了。可以先玩经典猜谜，或回大厅看其它玩法" },
    },
  },
  "zh-Hant": {
    friend: {
      title: "和朋友約一場", intro: "訓練員，先挑個項目吧。建立房間後，把房間代碼分享給朋友，等大家到齊再開始", loading: "正在確認好友同樂的項目", closed: "目前未開放好友同樂", closedDetail: "可以回大廳試試其他遊玩方式", mode: "這次玩什麼？", name: "你的暱稱", namePlaceholder: "朋友怎麼稱呼你？", roomName: "房間名稱（選填）", roomPlaceholder: "例如：週末特雷森同樂會", defaultName: "{{name}}的同樂房", capacity: "最多幾人參加？", seatsUnavailable: "這個玩法目前的名額不足，請選擇其他玩法", seatsExact: "包含你在內，共 {{count}} 人", seatsRange: "包含你在內，可供 {{min}}–{{max}} 人遊玩", busy: "正在安排座位……", create: "建立好友房間", duration: "每局約 {{minutes}} 分鐘", clue: "每隊需要 3–6 人。邀請大家加入後，在等候區安排隊伍；可以一隊合作，也能分成多隊較量", classic: "同一題，各自作答；3 分鐘內先猜中的人獲勝，每人可以猜錯 8 次。次數用完後等待對手，整場結束才揭曉；都沒猜中則雙方皆未獲勝，任一方主動認輸則另一方獲勝", other: "每位朋友各自加入。開始前請確認大家已看過玩法、準備就緒；想分隊舉辦比賽，也可以選擇活動團隊", invite: "怎麼邀請朋友？", inviteDetail: "房間建好後，把房間代碼或邀請連結傳給朋友。大家會使用已設定的暱稱加入；重新整理後仍可查看邀請，等房主開場就好",
    },
    join: {
      title: "加入房間", intro: "輸入房主分享的房間代碼。加入後會先進入等候區，開始遊戲前不會消耗作答次數", code: "房間代碼", codePlaceholder: "例如 ABCD1234", codeHint: "{{min}}–{{max}} 位英文字母或數字，不分大小寫", name: "暱稱", namePlaceholder: "讓其他玩家看見的名字", nameHint: "最多 {{max}} 個字元，之後也能在房間內修改", check: "請確認輸入內容", invalidCode: "房間代碼需為 {{min}}–{{max}} 位字元", invalidName: "暱稱需為 1–{{max}} 個字元", submitting: "正在尋找房間……", confirming: "正在加入房間……", noCode: "還沒有房間代碼？", noCodeDetail: "請房主分享房間代碼或邀請連結，也可以先到大廳來場單人挑戰",
    },
    info: {
      missing: "找不到這個玩法", check: "請確認遊玩連結", checkDetail: "目前無法開啟這個連結。可以向分享的朋友確認，或回大廳選擇玩法", loading: "正在讀取玩法資訊", closed: "「{{name}}」暫未開放", closedDetail: "目前還不能開始這個玩法，可以回大廳選擇其他已開放的項目", unavailable: "這個玩法目前無法開始", unavailableDetail: "遊玩資料還沒準備好，請稍後再試", fromLobby: "請從大廳進入", fromLobbyDetail: "請回大廳選擇這個玩法，從對應的入口開始",
    },
    start: {
      duration: "預計時間：{{duration}}", soloClosed: "目前未開放單人遊玩", soloClosedDetail: "可以選擇其他已開放的遊玩方式", friend: "和朋友約一場", settings: "開始前設定", name: "暱稱", namePlaceholder: "讓其他玩家看見的名字", party: "這個玩法至少需要 {{count}} 人", partyDetail: "本項至少需要 {{count}} 人。建立好友房間後，將房間代碼分享給朋友，等大家到齊再開始", busy: "正在準備遊戲……", play: "開始遊戲", rules: "玩法重點", defaultPlayer: "玩家", defaultRoom: "{{name}}的{{mode}}", classic: "去玩經典猜角色", steps: { session: "準備入場", room: "準備房間", round: "準備題目", start: "開始作答" }, state: { pending: "尚未開始", active: "進行中", done: "已完成", failed: "失敗" },
    },
    unavailable: {
      audio: { title: "聽聲辨角色目前無法開始", detail: "語音題目還不夠開一局，可以先玩猜角色，或回大廳看看其他玩法" },
      bluff: { title: "這個玩法的題目還在核對中", detail: "真假檔案的題目還不夠開一場，先回大廳玩點別的吧" },
      short: { title: "題目數量還不足以開始", detail: "符合這次規則的題目還不夠，請回大廳選擇其他玩法" },
      generic: { title: "這個玩法目前無法開始", detail: "遊玩所需的資料還不完整，可以先玩猜角色，或回大廳選擇其他項目" },
    },
  },
  ja: {
    friend: {
      title: "フレンドと一緒に遊ぼう", intro: "トレーナーさん、まずはゲームを選ぼう。ルームを作ったらコードをフレンドに送って、全員そろったらスタート！", loading: "フレンドと遊べるゲームを確認中", closed: "現在フレンドルームは利用できません", closedDetail: "ロビーで別の遊び方をお試しください", mode: "何で遊ぶ？", name: "あなたのニックネーム", namePlaceholder: "みんなに呼んでほしい名前", roomName: "ルーム名（省略可）", roomPlaceholder: "例：週末のトレセン交流会", defaultName: "{{name}}のルーム", capacity: "参加人数の上限", seatsUnavailable: "このゲームに必要な人数が定員を超えています。別のゲームを選んでください", seatsExact: "あなたを含めて {{count}} 人で遊びます", seatsRange: "あなたを含めて {{min}}～{{max}} 人で遊べます", busy: "ルームを準備中……", create: "フレンドルームを作成", duration: "一回約 {{minutes}} 分", clue: "各チーム 3～6 人で遊びます。全員が参加したら待機画面でチームを編成。1 チームで協力しても、複数チームで対戦しても遊べます", classic: "同じお題にそれぞれ回答し、3 分以内に先に正解した人の勝ち。間違えられるのは各自 8 回まで。回数を使い切っても相手は続けられ、正解は対戦終了後に発表します。どちらも正解できなければ勝者なし。一方が降参した場合は、もう一方の勝ちです", other: "フレンドはそれぞれルームに参加します。開始前にルールを確認し、全員の準備を整えましょう。チームを組んで大会を開く場合は、チームイベントも選べます", invite: "フレンドの招待方法", inviteDetail: "ルームを作ったら、コードか招待リンクをフレンドに送ろう。設定済みのニックネームで参加できます。再読み込みしても招待は確認でき、ホストがゲームを始めます",
    },
    join: {
      title: "ルームに参加", intro: "ホストから届いたルームコードを入力してください。参加後は待機画面に進みます。ゲームが始まるまで回答回数は減りません", code: "ルームコード", codePlaceholder: "例：ABCD1234", codeHint: "{{min}}～{{max}} 桁の英数字。大文字・小文字は区別しません", name: "ニックネーム", namePlaceholder: "ほかの参加者に表示する名前", nameHint: "{{max}} 文字まで。参加後もルーム内で変更できます", check: "入力内容を確認してください", invalidCode: "ルームコードは {{min}}～{{max}} 桁で入力してください", invalidName: "ニックネームは 1～{{max}} 文字で入力してください", submitting: "ルームを探しています…", confirming: "ルームに参加中……", noCode: "コードをお持ちでない方へ", noCodeDetail: "ルームを作ったフレンドにコードか招待リンクを送ってもらおう。ロビーでひとり用のチャレンジを遊ぶこともできます",
    },
    info: {
      missing: "ゲームが見つかりません", check: "リンクを確認してください", checkDetail: "このリンクは開けません。送ってくれたフレンドに確認するか、ロビーでゲームを選んでください", loading: "ゲーム情報を読み込み中", closed: "「{{name}}」は現在遊べません", closedDetail: "現在このゲームは利用できません。ロビーで別のゲームを選んでください", unavailable: "このゲームは現在遊べません", unavailableDetail: "プレイに必要な資料の準備ができていません。時間を置いてお試しください", fromLobby: "ロビーから開始してください", fromLobbyDetail: "ロビーに戻り、このゲームを選んで開始してください",
    },
    start: {
      duration: "目安時間：{{duration}}", soloClosed: "現在一人用プレイは利用できません", soloClosedDetail: "利用できる別の遊び方を選んでください", friend: "フレンドと遊ぶ", settings: "開始前の設定", name: "ニックネーム", namePlaceholder: "ほかの参加者に表示する名前", party: "このゲームには {{count}} 人以上必要です", partyDetail: "{{count}} 人以上で遊びます。フレンドルームを作成してコードを共有し、全員そろってから開始してください", busy: "ゲームを準備中……", play: "ゲームを開始", rules: "ルールのポイント", defaultPlayer: "プレイヤー", defaultRoom: "{{name}}の{{mode}}", classic: "キャラクター当てで遊ぶ", steps: { session: "参加の準備", room: "ルームを準備", round: "問題を準備", start: "回答を開始" }, state: { pending: "開始前", active: "進行中", done: "完了", failed: "失敗" },
    },
    unavailable: {
      audio: { title: "声当ては現在遊べません", detail: "音声の問題がまだ足りません。キャラクター当てで遊ぶか、ロビーで別のゲームを選んでください" },
      bluff: { title: "このゲームの問題は確認中です", detail: "ウソを見抜くクイズは、まだ問題が足りません。ロビーでほかのゲームを選んでください" },
      short: { title: "プレイに必要な問題が足りません", detail: "今回のルールで使える問題が足りません。ロビーで別のゲームを選んでください" },
      generic: { title: "このゲームは現在遊べません", detail: "プレイに必要な内容がそろっていません。キャラクター当てや、ロビーの別のゲームで遊べます" },
    },
  },
} as const;

registerMessages("entry", entry);
