export const errorDetails = {
  "zh-Hans": {
    "account-seat-active": { title: "你已经在房间里了", detail: "请回到原来的席位，或先离开那边的房间" },
    "account-queue-active": { title: "你已经在匹配或对局中了", detail: "请先完成当前一场，再来寻找新的对手" },
    "account-required": { title: "此玩法需要登录", detail: "点右上角的头像，选择登录方式" },
    "account-changed": { title: "登录的账户变了", detail: "请回到大厅，重新进入这场游玩" },
    "account-csrf-invalid": { title: "请刷新页面后再试", detail: "登录状态需要重新确认，刷新后就能继续" },
    "login-unavailable": { title: "这个登录方式暂时用不了", detail: "换一个登录方式，或稍后再试一次" },
    "account-disabled": { title: "账户登录暂未开放", detail: "你仍可体验已经开放的游玩" },

    "pause-counterparty-required": { title:"请等对方确认暂停", detail:"申请人和同队伙伴不能替对方确认" },
    "pause-request-closed": { title:"这次暂停已有结果", detail:"请查看最新对局进度" },
    "pause-request-stale": { title:"这次暂停已失效", detail:"对局已进入下一阶段，请查看最新进度" },
    "participant-surrendered": { title:"你已认输", detail:"这局不能继续作答，请等待结果，或回大厅再开一场" },
    "matchmaking-idle": { title:"这局已经结束", detail:"开始后一直没有作答，可以重新匹配" },

    "matchmaking-proof-required": { title: "这次匹配还没准备好", detail: "请再试一次，准备好就能开始寻找对手" },
    "matchmaking-proof-invalid": { title: "这次匹配还没准备好", detail: "请再试一次，准备好就能开始寻找对手" },
    "ticket-already-active": { title: "你已经在排队或对局中", detail: "正在找回当前匹配，请稍等" },
    "match-already-started": { title: "这场已经开跑了", detail: "请进入当前对局，或在房间里选择离开" },
    "matchmaking-rules-fixed": { title: "本场规则已经定好", detail: "回到匹配大厅选择一场，再来比一比" },
    "matchmaking-reserved": { title: "本场规则已经定好", detail: "回到匹配大厅选择一场，再来比一比" },
    "matchmaking-pause-disabled": { title: "本场规则已经定好", detail: "回到匹配大厅选择一场，再来比一比" },
    "matchmaking-rematch-disabled": { title: "本场规则已经定好", detail: "回到匹配大厅选择一场，再来比一比" },
    "matchmaking-mode-unavailable": { title: "这次匹配还没准备好", detail: "请再试一次，准备好就能开始寻找对手" },


    unknown_character: { title: "这局不能选择这位角色", detail: "这次没有扣除猜测机会，请从本局可选角色中重新选择" },
    "session-rejoin-required": {"title": "需要重新进入这一局", "detail": "请重新打开本局，恢复后再继续作答"},
    "session-storage-unavailable": {"title": "无法保存游玩身份", "detail": "请检查浏览器是否允许保存网站数据，再试一次"},
    "session-storage-blocked": {"title": "请先关闭旧的游戏页面", "detail": "还有旧页面开着，请关闭后再试一次"},
    "session-busy": {"title": "另一个页面正在准备加入", "detail": "请稍等片刻再试，无需重复开启游戏页面"},
    "session-restore-failed": {"title": "无法恢复游玩身份", "detail": "请保持原来的游戏页面，稍后再试一次"},
    "session-create-failed": {"title": "暂时无法加入", "detail": "请稍等片刻，再试着加入"},
    "solo-room": {
      "title": "这是一个人的挑战",
      "detail": "想和朋友一起玩，可以在大厅选择「好友游玩」，另开一个房间"
    },
    "host-changed": {
      "title": "主持人已更换",
      "detail": "请查看最新房间安排，再继续游玩"
    },
    "host-required": {
      "title": "这一步交给主持人",
      "detail": "只有当前主持人可以转交房间安排"
    },
    "invalid-host-target": {
      "title": "这位伙伴暂时不能接任",
      "detail": "请查看最新成员，选择仍在房内的另一位伙伴"
    },
    "room-closed": {
      "title": "这场游玩已结束",
      "detail": "可以回大厅，重新约伙伴开一场"
    },
    "individual-controls": {
      "title": "这局各自操作",
      "detail": "每位玩家各自作答，不需要安排队伍代表"
    },
    "admission-cancelled": {
      "title": "已返回，尚未开始",
      "detail": "刚才填写的内容还在。准备好后，再点一次开始就行"
    },
    "admission-risk-required": { title: "训练员，稍等一下", detail: "请稍后再试，完成入场确认后就能继续游玩" },
    "admission-required": {
      "title": "开始前还需确认一下",
      "detail": "请重新点一次开始，完成入场确认后继续"
    },
    "team-controller-required": {
      "title": "这次由队友确认",
      "detail": "可以继续和队友讨论，由当前代表确认全队的作答"
    },
    "control-generation-conflict": {
      "title": "作答代表已更换",
      "detail": "请刷新页面，确认当前作答代表后继续"
    },
    "preview-in-progress": {
      "title": "先记住牌面",
      "detail": "观看结束后会一起盖牌，到时再开始翻牌"
    },
    "team-locked": {
      "title": "本局已开始",
      "detail": "这一局的队伍和参赛身份保持不变，下一局开始前再调整"
    },
    "practice-unavailable": {
      "title": "这个玩法暂时不能开局",
      "detail": "这次还没有足够的题目。先回大厅看看其他玩法吧"
    },
    "audio-playback-review-required": {
      "title": "这轮有音频播放失败",
      "detail": "这轮暂不计成绩，请主持作废本轮后重新开题"
    },
    "play-disabled": {
      "title": "这种玩法暂未开放",
      "detail": "可以返回大厅，选择其他已开放的游玩方式"
    },
    "challenge-expired": {
      "title": "本轮时间到了",
      "detail": "本局已停止作答，可以查看结果，再挑战下一局"
    },
    "challenge-ended": {
      "title": "本局已经结束",
      "detail": "已确认的作答会保留，可以查看结果，再挑战下一局"
    },
    "challenge-revision-conflict": {
      "title": "进度刚刚更新",
      "detail": "已确认的作答会保留，请载入最新进度后继续"
    },
    "challenge-ticket-invalid": {
      "title": "暂时无法继续这局",
      "detail": "请重新打开当前挑战，找回进度后再试"
    },
    "device-clock-mismatch": {
      "title": "设备时间似乎不准确",
      "detail": "开启设备的自动日期与时间，再试着开始挑战"
    },
    "duplicate-guess": {
      "title": "已经猜过她",
      "detail": "这次没有扣除机会，换一位赛马娘试试吧"
    },
    "rapport-roster-invalid": {
      "title": "先安排好默契搭档",
      "detail": "每组需要 2–8 位伙伴，分队后请让所有选手加入队伍，各队人数相差不超过一人；每组人数不能超过本场题数，请减少每组人数后重试"
    },
    "team-roster-invalid": {
      "title": "先安排好情报小队",
      "detail": "情报拼图需要 1–8 支队伍，每队 3–6 位选手。请在等待区展开队伍安排，为每位选手分队，并选好各队队长"
    },
    "not-enough-participants": {
      "title": "还差几位参赛伙伴",
      "detail": "请先邀请朋友加入，人数到齐后再开始"
    },
    "too-many-participants": {
      "title": "参赛人数超出了本场上限",
      "detail": "请先调整参赛名单或分队，再开始对局"
    },
    "room-version-conflict": {
      "title": "房间安排刚刚有变",
      "detail": "请载入最新成员和设置，确认后再开始"
    },
    "event-entertainment-only": {
      "title": "这个项目只作娱乐展示",
      "detail": "它的成绩不计入正式总榜，请按娱乐项目安排"
    },
    "segment-rules-mismatch": {
      "title": "本项规则刚刚有变",
      "detail": "请刷新页面，查看赛程中的最新规则后重新开局"
    },
    "segment-roster-changed": {
      "title": "本项的参赛名单变了",
      "detail": "连续回合需要保持同一组参赛选手或队伍。请恢复名单后再继续，或作废本分项"
    },
    "segment-closed": {
      "title": "这个分项已结束",
      "detail": "可以查看本项成绩，或从赛程选择其他项目"
    },
    "segment-rounds-complete": {
      "title": "本项的对局已经开齐",
      "detail": "请先完成当前对局，之后查看分项成绩"
    },
    "segment-not-settled": {
      "title": "本项还没全部完成",
      "detail": "所有对局结算后，才能更正本分项的成绩"
    },
    "event-closed": {
      "title": "这个活动已结束",
      "detail": "活动记录可以继续查看，新的安排请另开一场活动"
    },
    "segments-incomplete": {
      "title": "还有分项没完成",
      "detail": "先完成赛程中的全部对局，或说明原因作废分项，再结束活动"
    },
    "round-already-open": {
      "title": "上一局还没结束",
      "detail": "请先收卷、揭晓并结算当前对局，再开始下一局"
    },
    "room-full": {
      "title": "这个房间已经满员了",
      "detail": "可以请房主另开一个房间，或返回大厅选择其他玩法"
    },
    "room-in-progress": {
      "title": "这一局已经开始",
      "detail": "等这局结束再来，或者请房主另开一场"
    },
    "already-member": {
      "title": "你已经在房间里了",
      "detail": "回到当前房间继续就好"
    },
    "invitation-invalid": {
      "title": "房间分享码不对",
      "detail": "请检查房间分享码中的字母和数字，或请房主重新分享邀请"
    },
    "invitation-not-found": {
      "title": "找不到这个邀请",
      "detail": "邀请可能已失效，请向房主再要一次"
    },
    "not-a-participant": {
      "title": "你不在这一局的参赛名单里",
      "detail": "如果刚被调整过名单，请刷新页面看看最新安排"
    },
    "owner-cannot-leave": {
      "title": "房主不能直接离开",
      "detail": "请先把主持交给另一位伙伴，再离开房间"
    },
    "round-paused": {
      "title": "这一局暂停中",
      "detail": "等主持人恢复后再继续作答"
    },
    "round-not-collecting": {
      "title": "现在还不能提交",
      "detail": "这一局当前不在作答阶段，等页面提示可以提交时再试"
    },
    "deadline-passed": {
      "title": "作答时间到了",
      "detail": "这一局已停止收卷，可以查看结果或等下一局"
    },
    "submission-revision-conflict": {
      "title": "你的作答刚刚更新过",
      "detail": "请刷新页面，确认当前内容后再提交一次"
    },
    "phase-version-conflict": {
      "title": "房间进度刚刚变了",
      "detail": "请刷新页面，按最新状态再操作"
    },
    "team-required": {
      "title": "还没分队",
      "detail": "请先在等待区把选手分到队伍里，再开始"
    },
    "team-captain-required": {
      "title": "这一步需要队长来做",
      "detail": "请让本队队长确认"
    },
    "team-label-taken": {
      "title": "这个队名已经有人用了",
      "detail": "换一个队名再试"
    },
    "trade-closed": {
      "title": "这一轮的交易已关闭",
      "detail": "可以等下一轮再发起"
    },
    "trade-expired": {
      "title": "这个交易已过期",
      "detail": "可以重新发起一次"
    },
    "not-the-recipient": {
      "title": "这个交易不是发给你的",
      "detail": "请查看发给你的交易请求"
    },
    "self-trade": {
      "title": "不能和自己交易",
      "detail": "请选择另一位伙伴"
    },
    "card-not-held": {
      "title": "这张牌已经不在你手里了",
      "detail": "请刷新页面，按当前手牌重新选择"
    },
    "ticket-expired": {
      "title": "匹配等待已超时",
      "detail": "重新发起匹配就好"
    },
    "partner-expired": {
      "title": "对手的匹配等待已超时",
      "detail": "对方没能在时间内确认，重新发起匹配找一位新对手"
    },
    "modes-unavailable": {
      "title": "这个玩法现在打不开",
      "detail": "请回大厅换一种玩法，或稍后再试"
    },
    "content-unavailable": {
      "title": "题目资料暂时没准备好",
      "detail": "请稍后再试，或先回大厅"
    },
    "join-unavailable": {
      "title": "无法加入这个房间",
      "detail": "房间分享码可能已失效，或房间当前不接受新成员。请向房主确认后再试"
    }
  },
  "zh-Hant": {
    "account-seat-active": { title: "你已經在房間裡了", detail: "請返回原本的席位，或先離開那邊的房間" },
    "account-queue-active": { title: "你已經在配對或對局中了", detail: "請先完成目前這場，再來尋找新的對手" },
    "account-required": { title: "此玩法需要登入", detail: "點右上角的頭像，選擇登入方式" },
    "account-changed": { title: "登入的帳號變了", detail: "請返回大廳，重新加入這場遊玩" },
    "account-csrf-invalid": { title: "請重新整理頁面後再試", detail: "需要重新確認登入狀態，重新整理後就能繼續" },
    "login-unavailable": { title: "這個登入方式暫時無法使用", detail: "換個登入方式，或稍後再試一次" },
    "account-disabled": { title: "帳號登入尚未開放", detail: "你仍可體驗已經開放的遊玩" },

    "pause-counterparty-required": { title:"請等對方確認暫停", detail:"申請人和同隊夥伴無法代替對方確認" },
    "pause-request-closed": { title:"這次暫停已有結果", detail:"請查看最新遊戲進度" },
    "pause-request-stale": { title:"這次暫停已失效", detail:"遊戲已進入下一階段，請查看最新進度" },
    "participant-surrendered": { title:"你已認輸", detail:"這場無法繼續作答，請等候結果，或回大廳再開一場" },
    "matchmaking-idle": { title:"這場已經結束", detail:"開始後一直沒有作答，可以重新配對" },

    "matchmaking-proof-required": { title: "這次配對還沒準備好", detail: "請再試一次，準備好就能開始尋找對手" },
    "matchmaking-proof-invalid": { title: "這次配對還沒準備好", detail: "請再試一次，準備好就能開始尋找對手" },
    "ticket-already-active": { title: "你已經在排隊或遊玩中", detail: "正在找回目前的配對，請稍候" },
    "match-already-started": { title: "這場已經開始了", detail: "請進入目前的遊戲，或在房間裡選擇離開" },
    "matchmaking-rules-fixed": { title: "本場規則已經訂好", detail: "回到配對大廳選一場，再來比比看" },
    "matchmaking-reserved": { title: "本場規則已經訂好", detail: "回到配對大廳選一場，再來比比看" },
    "matchmaking-pause-disabled": { title: "本場規則已經訂好", detail: "回到配對大廳選一場，再來比比看" },
    "matchmaking-rematch-disabled": { title: "本場規則已經訂好", detail: "回到配對大廳選一場，再來比比看" },
    "matchmaking-mode-unavailable": { title: "這次配對還沒準備好", detail: "請再試一次，準備好就能開始尋找對手" },


    unknown_character: { title: "這局無法選擇這位角色", detail: "這次沒有扣除猜測次數，請從這局可選的角色中重新選擇" },
    "session-rejoin-required": {"title": "需要重新進入這一局", "detail": "請重新開啟本局，恢復後再繼續作答"},
    "session-storage-unavailable": {"title": "無法儲存遊玩身分", "detail": "請確認瀏覽器允許儲存網站資料，再試一次"},
    "session-storage-blocked": {"title": "請先關閉舊的遊戲頁面", "detail": "還有舊頁面開著，請關閉後再試一次"},
    "session-busy": {"title": "另一個頁面正在準備加入", "detail": "請稍候再試，不必重複開啟遊戲頁面"},
    "session-restore-failed": {"title": "無法恢復遊玩身分", "detail": "請保留原本的遊戲頁面，稍候再試一次"},
    "session-create-failed": {"title": "暫時無法加入", "detail": "請稍候再試著加入"},
    "solo-room": {
      "title": "這是一個人的挑戰",
      "detail": "想和朋友一起玩，可以在大廳選「好友同樂」，另外建立房間"
    },
    "host-changed": {
      "title": "主持人已更換",
      "detail": "請確認最新的房間安排，再繼續遊玩"
    },
    "host-required": {
      "title": "這一步請交給主持人",
      "detail": "只有目前的主持人可以轉交主持權限"
    },
    "invalid-host-target": {
      "title": "這位夥伴目前無法接任",
      "detail": "請確認最新成員名單，選擇另一位仍在房間內的夥伴"
    },
    "room-closed": {
      "title": "這場遊戲已經結束",
      "detail": "可以返回大廳，再找夥伴開一場"
    },
    "individual-controls": {
      "title": "這一局各自操作",
      "detail": "每位玩家各自作答，不必安排隊伍代表"
    },
    "admission-cancelled": {
      "title": "已返回，還沒開始遊戲",
      "detail": "剛才填寫的內容還在。準備好後，再按一次開始就可以了"
    },
    "admission-risk-required": { title: "訓練員，請稍等一下", detail: "請稍後再試，完成入場確認後即可繼續遊玩" },
    "admission-required": {
      "title": "開始前還需要確認一下",
      "detail": "請再按一次開始，完成入場確認後就能繼續"
    },
    "team-controller-required": {
      "title": "這次由隊友確認",
      "detail": "可以繼續和隊友討論，由目前的代表確認全隊答案"
    },
    "control-generation-conflict": {
      "title": "作答代表已更換",
      "detail": "請重新載入，確認現在輪到誰，再繼續作答"
    },
    "preview-in-progress": {
      "title": "先記住牌面吧",
      "detail": "觀看時間結束後，牌會一起蓋上，到時再開始翻牌"
    },
    "team-locked": {
      "title": "本場已開始",
      "detail": "這一局的隊伍與參賽身分不能變更，請在下一局開始前調整"
    },
    "practice-unavailable": {
      "title": "這個玩法目前無法開始",
      "detail": "題目數量還不足，可以先回大廳看看其他玩法"
    },
    "audio-playback-review-required": {
      "title": "這一輪有音訊無法播放",
      "detail": "這一輪暫不計分，請主持人將本輪作廢後重新出題"
    },
    "play-disabled": {
      "title": "這種玩法暫未開放",
      "detail": "可以返回大廳，選擇其他已開放的遊玩方式"
    },
    "challenge-expired": {
      "title": "這一輪時間到了",
      "detail": "本局已停止作答，可以查看結果，再挑戰下一局"
    },
    "challenge-ended": {
      "title": "這一局已經結束",
      "detail": "已確認的作答會保留，可以查看結果，再挑戰下一局"
    },
    "challenge-revision-conflict": {
      "title": "進度剛剛更新了",
      "detail": "已確認的作答會保留，請載入最新進度後繼續"
    },
    "challenge-ticket-invalid": {
      "title": "目前無法繼續這一局",
      "detail": "請重新開啟目前的挑戰，找回進度後再試"
    },
    "device-clock-mismatch": {
      "title": "裝置時間似乎不正確",
      "detail": "請開啟裝置的自動日期與時間，再試著開始挑戰"
    },
    "duplicate-guess": {
      "title": "已經猜過她",
      "detail": "這次沒有扣除次數，換一位賽馬娘猜猜看吧"
    },
    "rapport-roster-invalid": {
      "title": "先安排好默契搭檔吧",
      "detail": "每組需要 2–8 人，請將所有選手分隊，各隊人數相差不得超過一人；每組人數不能超過本場題數，請減少每組人數後重試"
    },
    "team-roster-invalid": {
      "title": "先安排好情報小隊吧",
      "detail": "情報拼圖需要 1–8 隊，每隊 3–6 人。請在等候區展開隊伍安排，為每位選手分隊，並選好各隊隊長"
    },
    "not-enough-participants": {
      "title": "還差幾位參賽夥伴",
      "detail": "請先邀請朋友加入，人數到齊後再開始"
    },
    "too-many-participants": {
      "title": "參賽人數超過本場上限",
      "detail": "請先調整參賽名單或隊伍，再開始遊戲"
    },
    "room-version-conflict": {
      "title": "房間安排剛剛有變動",
      "detail": "請載入最新的成員名單與設定，確認後再開始"
    },
    "event-entertainment-only": {
      "title": "這個項目只供同樂展示",
      "detail": "成績不會列入正式總榜，請安排為同樂項目"
    },
    "segment-rules-mismatch": {
      "title": "本項規則剛剛變更了",
      "detail": "請重新載入賽程，依最新規則重新開始"
    },
    "segment-roster-changed": {
      "title": "本項參賽名單變更了",
      "detail": "連續回合必須由相同的選手或隊伍參賽。請恢復原名單後繼續，或將本項作廢"
    },
    "segment-closed": {
      "title": "這個分項已結束",
      "detail": "可以查看本項成績，或從賽程選擇其他項目"
    },
    "segment-rounds-complete": {
      "title": "本項的對局已全部開始",
      "detail": "請先完成目前的對局，再查看分項成績"
    },
    "segment-not-settled": {
      "title": "本項還沒全部完成",
      "detail": "所有對局結算後，才能更正本項成績"
    },
    "event-closed": {
      "title": "這場活動已經結束",
      "detail": "仍可查看活動紀錄；若有新的安排，請另外建立活動"
    },
    "segments-incomplete": {
      "title": "還有分項未完成",
      "detail": "請先完成賽程中的所有對局，或註明原因將項目作廢，再結束活動"
    },
    "round-already-open": {
      "title": "上一局還沒結束",
      "detail": "請先收卷、揭曉並結算目前的對局，再開始下一局"
    },
    "room-full": {
      "title": "這個房間已經滿員",
      "detail": "可以請房主另外建立房間，或回大廳選擇其他玩法"
    },
    "room-in-progress": {
      "title": "這一局已經開始了",
      "detail": "請等本局結束後再加入，或請房主另外開一場"
    },
    "already-member": {
      "title": "你已經在房間裡",
      "detail": "回到目前的房間繼續玩就好"
    },
    "invitation-invalid": {
      "title": "房間代碼不正確",
      "detail": "請檢查房間代碼中的字母與數字，或請房主重新分享邀請"
    },
    "invitation-not-found": {
      "title": "找不到這個邀請",
      "detail": "邀請可能已經失效，請房主重新分享一次"
    },
    "not-a-participant": {
      "title": "你不在本局的參賽名單中",
      "detail": "如果剛調整過名單，請重新載入，確認最新安排"
    },
    "owner-cannot-leave": {
      "title": "房主目前無法直接離開",
      "detail": "請先將主持權限交給另一位夥伴，再離開房間"
    },
    "round-paused": {
      "title": "這一局暫停中",
      "detail": "請等主持人恢復遊戲後再繼續作答"
    },
    "round-not-collecting": {
      "title": "現在還不能送出",
      "detail": "目前不是作答階段，請等畫面提示可以作答時再送出"
    },
    "deadline-passed": {
      "title": "作答時間到了",
      "detail": "這一局已停止收卷，可以查看結果或等待下一局"
    },
    "submission-revision-conflict": {
      "title": "你的作答剛剛更新過",
      "detail": "請重新載入，確認目前內容後再送出"
    },
    "phase-version-conflict": {
      "title": "房間進度剛剛有變動",
      "detail": "請重新載入，依最新狀況繼續操作"
    },
    "team-required": {
      "title": "還沒有分隊",
      "detail": "請先在等候區將選手分隊，再開始遊戲"
    },
    "team-captain-required": {
      "title": "這一步需要隊長操作",
      "detail": "請由本隊隊長確認"
    },
    "team-label-taken": {
      "title": "這個隊名已經有人使用",
      "detail": "換個隊名再試試吧"
    },
    "trade-closed": {
      "title": "這一輪的交易已結束",
      "detail": "可以等下一輪再提出交易"
    },
    "trade-expired": {
      "title": "這筆交易已經過期",
      "detail": "可以重新提出交易"
    },
    "not-the-recipient": {
      "title": "這筆交易不是給你的",
      "detail": "請查看對方向你提出的交易"
    },
    "self-trade": {
      "title": "不能和自己交易",
      "detail": "請選擇另一位夥伴"
    },
    "card-not-held": {
      "title": "這張牌已經不在你手上",
      "detail": "請重新載入，依目前手牌重新選擇"
    },
    "ticket-expired": {
      "title": "配對等候已逾時",
      "detail": "重新尋找對手就可以了"
    },
    "partner-expired": {
      "title": "對手未能及時確認",
      "detail": "對方的配對等候已逾時，請重新尋找另一位對手"
    },
    "modes-unavailable": {
      "title": "目前無法開啟這個玩法",
      "detail": "請回大廳換一種玩法，或稍後再試"
    },
    "content-unavailable": {
      "title": "題目資料還沒準備好",
      "detail": "請稍後再試，或先返回大廳"
    },
    "join-unavailable": {
      "title": "目前無法加入這個房間",
      "detail": "房間代碼可能已失效，或房間暫不接受新成員。請向房主確認後再試"
    }
  },
  "ja": {
    "account-seat-active": { title: "このルームには参加済みです", detail: "元の席に戻るか、そちらのルームから退出してください" },
    "account-queue-active": { title: "すでにマッチング中か対戦中です", detail: "今の対戦が終わってから、新しい相手を探してください" },
    "account-required": { title: "ログインが必要です", detail: "右上のアイコンからログイン方法を選んでください" },
    "account-changed": { title: "ログイン中のアカウントが変わりました", detail: "ロビーへ戻り、もう一度参加してください" },
    "account-csrf-invalid": { title: "ページを再読み込みしてください", detail: "ログイン状態を確認してから続けられます" },
    "login-unavailable": { title: "この方法ではログインできません", detail: "別の方法を選ぶか、少し待ってからもう一度お試しください" },
    "account-disabled": { title: "ログインはまだ開放されていません", detail: "開放中のモードはそのまま遊べます" },
    "pause-counterparty-required": { title:"相手の確認を待ってください", detail:"申請した人や同じチームの仲間は、相手に代わって確認できません" },
    "pause-request-closed": { title:"この一時停止の申請は回答済みです", detail:"最新の対戦状況を確認してください" },
    "pause-request-stale": { title:"この一時停止の申請は無効です", detail:"対戦が次の段階に進みました。最新の状況を確認してください" },
    "participant-surrendered": { title:"降参済みです", detail:"このゲームではこれ以上回答できません。結果を待つか、ロビーから新しく始めてください" },
    "matchmaking-idle": { title:"今回の対戦は終了しました", detail:"開始後に回答がなかったため終了しました。もう一度マッチングできます" },
    "matchmaking-proof-required": { title: "参加の準備ができていません", detail: "もう一度試してください。準備ができたら相手を探せます" },
    "matchmaking-proof-invalid": { title: "参加の準備ができていません", detail: "もう一度試してください。準備ができたら相手を探せます" },
    "ticket-already-active": { title: "参加中のマッチングがあります", detail: "現在のマッチングを確認しています。少しお待ちください" },
    "match-already-started": { title: "この対戦は始まっています", detail: "参加中の対戦へ進むか、ルームから退出してください" },
    "matchmaking-rules-fixed": { title: "この対戦のルールは決まっています", detail: "マッチングロビーで対戦形式を選んでください" },
    "matchmaking-reserved": { title: "この対戦のルールは決まっています", detail: "マッチングロビーで対戦形式を選んでください" },
    "matchmaking-pause-disabled": { title: "この対戦のルールは決まっています", detail: "マッチングロビーで対戦形式を選んでください" },
    "matchmaking-rematch-disabled": { title: "この対戦のルールは決まっています", detail: "マッチングロビーで対戦形式を選んでください" },
    "matchmaking-mode-unavailable": { title: "参加の準備ができていません", detail: "もう一度試してください。準備ができたら相手を探せます" },

    unknown_character: { title: "このゲームでは選べないウマ娘です", detail: "回答回数は減っていません。このゲームで選べるウマ娘から選び直してください" },
    "session-rejoin-required": {"title": "このゲームを開き直してください", "detail": "ゲームを開き直し、復帰してから回答を続けてください"},
    "session-storage-unavailable": {"title": "参加情報を保存できません", "detail": "ブラウザーでサイトデータの保存を許可してから、もう一度お試しください"},
    "session-storage-blocked": {"title": "以前開いたゲームのページを閉じてください", "detail": "古いページが開いたままです。閉じてからもう一度お試しください"},
    "session-busy": {"title": "別のページで参加の準備中です", "detail": "少し待ってからお試しください。ゲームのページを増やす必要はありません"},
    "session-restore-failed": {"title": "参加情報を復元できません", "detail": "元のゲームのページを閉じずに、少し待ってからお試しください"},
    "session-create-failed": {"title": "参加の準備ができませんでした", "detail": "少し待ってから、もう一度参加してください"},
    "solo-room": {
      "title": "これは一人用のチャレンジです",
      "detail": "フレンドと遊ぶ場合は、ロビーの「フレンド」から別のルームを作ってください"
    },
    "host-changed": {
      "title": "進行役が交代しました",
      "detail": "最新のルーム情報を確認してから続けてください"
    },
    "host-required": {
      "title": "進行役が操作してください",
      "detail": "進行役の引き継ぎは、現在の進行役のみ行えます"
    },
    "invalid-host-target": {
      "title": "このメンバーには引き継げません",
      "detail": "最新の参加者一覧から、ルーム内にいる別のメンバーを選んでください"
    },
    "room-closed": {
      "title": "このルームでのプレイは終了しました",
      "detail": "ロビーに戻り、新しいルームで遊べます"
    },
    "individual-controls": {
      "title": "今回はそれぞれが操作します",
      "detail": "一人ずつ自分で回答します。チームの回答担当を決める必要はありません"
    },
    "admission-cancelled": {
      "title": "開始せずに戻りました",
      "detail": "入力した内容は残っています。準備ができたら、もう一度開始してください"
    },
    "admission-risk-required": { title: "参加前の確認が必要です", detail: "参加前の確認を済ませてから、もう一度お試しください" },
    "admission-required": {
      "title": "開始前の確認が必要です",
      "detail": "もう一度開始ボタンを押し、参加前の確認を済ませてください"
    },
    "team-controller-required": {
      "title": "今回はチームメイトが確定します",
      "detail": "相談は続けられます。今の回答担当がチームの回答を確定してください"
    },
    "control-generation-conflict": {
      "title": "操作するメンバーが変わりました",
      "detail": "最新の状況を読み込み、誰の番かを確認してから回答してください"
    },
    "preview-in-progress": {
      "title": "まずはカードを覚えよう",
      "detail": "確認時間が終わるとカードが伏せられます。それからめくり始めてください"
    },
    "team-locked": {
      "title": "ゲームは開始済みです",
      "detail": "今回のチームと参加者の役割は変更できません。次のゲームが始まる前に調整してください"
    },
    "practice-unavailable": {
      "title": "このゲームは現在遊べません",
      "detail": "問題が足りません。ロビーで別のゲームを選んでください"
    },
    "audio-playback-review-required": {
      "title": "今回、音声を再生できませんでした",
      "detail": "この回の得点は保留です。進行役がこの回を無効にして、新しい問題を開始してください"
    },
    "play-disabled": {
      "title": "この遊び方は現在利用できません",
      "detail": "ロビーで、利用できる別の遊び方を選んでください"
    },
    "challenge-expired": {
      "title": "制限時間になりました",
      "detail": "回答の受付は終了しました。結果を確認して、次のゲームに挑戦できます"
    },
    "challenge-ended": {
      "title": "このゲームは終了しました",
      "detail": "受け付け済みの回答は残っています。結果を確認し、次のゲームに挑戦できます"
    },
    "challenge-revision-conflict": {
      "title": "進行状況が更新されました",
      "detail": "受け付け済みの回答は残っています。最新の状況を読み込んでから続けてください"
    },
    "challenge-ticket-invalid": {
      "title": "このまま続けられません",
      "detail": "今のチャレンジを開き直し、進行状況を読み込んでからお試しください"
    },
    "device-clock-mismatch": {
      "title": "端末の時刻がずれているようです",
      "detail": "端末の日時の自動設定を有効にしてから、もう一度開始してください"
    },
    "duplicate-guess": {
      "title": "そのウマ娘はもう予想済みです",
      "detail": "今回は回答回数を消費していません。別のウマ娘を選んでみよう"
    },
    "rapport-roster-invalid": {
      "title": "まずはペア・チームを決めよう",
      "detail": "各チームは 2～8 人で、全員をチームに割り当ててください。人数差は 1 人まで。各チームの人数が問題数を超える場合は、チームの人数を減らしてからお試しください"
    },
    "team-roster-invalid": {
      "title": "まずは情報チームを編成しよう",
      "detail": "情報パズルは 1～8 チーム、各 3～6 人で遊びます。待機画面で全員をチームに割り当て、各チームのリーダーを選んでください"
    },
    "not-enough-participants": {
      "title": "参加者がまだ足りません",
      "detail": "フレンドを招待し、必要な人数がそろってから開始してください"
    },
    "too-many-participants": {
      "title": "参加人数が上限を超えています",
      "detail": "参加者やチームの編成を調整してから開始してください"
    },
    "room-version-conflict": {
      "title": "ルームの設定が変わりました",
      "detail": "最新の参加者一覧と設定を読み込み、確認してから開始してください"
    },
    "event-entertainment-only": {
      "title": "この種目はお楽しみ種目です",
      "detail": "得点は総合順位に反映されません。お楽しみ種目として設定してください"
    },
    "segment-rules-mismatch": {
      "title": "この種目のルールが変わりました",
      "detail": "プログラムを読み込み直し、最新のルールで開始してください"
    },
    "segment-roster-changed": {
      "title": "この種目の参加者が変わりました",
      "detail": "同じ種目の各回は、同じ選手またはチームで行います。元の編成に戻して続けるか、この種目を無効にしてください"
    },
    "segment-closed": {
      "title": "この種目は終了しました",
      "detail": "成績を確認するか、プログラムから別の種目を選べます"
    },
    "segment-rounds-complete": {
      "title": "この種目の全ゲームを開始済みです",
      "detail": "進行中のゲームを終えてから、種目の成績を確認してください"
    },
    "segment-not-settled": {
      "title": "この種目はまだ終わっていません",
      "detail": "すべてのゲームの結果が確定すると、種目の成績を修正できます"
    },
    "event-closed": {
      "title": "このイベントは終了しました",
      "detail": "記録は引き続き確認できます。新しく開催する場合は、別のイベントを作成してください"
    },
    "segments-incomplete": {
      "title": "まだ終了していない種目があります",
      "detail": "プログラムの全ゲームを終えるか、理由を記載して種目を無効にしてから、イベントを終了してください"
    },
    "round-already-open": {
      "title": "前のゲームがまだ終わっていません",
      "detail": "回答受付を締め切り、答え合わせと結果の確定を済ませてから、次のゲームを始めてください"
    },
    "room-full": {
      "title": "ルームが満員です",
      "detail": "ホストに別のルームを作ってもらうか、ロビーでほかのゲームを選んでください"
    },
    "room-in-progress": {
      "title": "ゲームはすでに始まっています",
      "detail": "終了後に参加するか、ホストに別のゲームを作ってもらってください"
    },
    "already-member": {
      "title": "すでに参加しています",
      "detail": "今のルームに戻って、そのまま遊べます"
    },
    "invitation-invalid": {
      "title": "ルームコードが違います",
      "detail": "ルームコードの英数字を確認するか、ホストに招待を送り直してもらってください"
    },
    "invitation-not-found": {
      "title": "招待が見つかりません",
      "detail": "招待が無効になった可能性があります。ホストにもう一度送ってもらってください"
    },
    "not-a-participant": {
      "title": "今回の参加者に含まれていません",
      "detail": "編成が変更された場合は、画面を読み込み直して最新の状況を確認してください"
    },
    "owner-cannot-leave": {
      "title": "今はそのまま退出できません",
      "detail": "別のメンバーに進行役を譲ってから、ルームを退出してください"
    },
    "round-paused": {
      "title": "ゲームは一時停止中です",
      "detail": "進行役が再開してから回答してください"
    },
    "round-not-collecting": {
      "title": "今は回答を送れません",
      "detail": "回答の受付時間ではありません。画面に受付の案内が出てから送信してください"
    },
    "deadline-passed": {
      "title": "回答時間が終了しました",
      "detail": "受付は締め切りました。結果を確認するか、次のゲームをお待ちください"
    },
    "submission-revision-conflict": {
      "title": "回答が更新されています",
      "detail": "画面を読み込み直し、現在の内容を確認してから送信してください"
    },
    "phase-version-conflict": {
      "title": "ルームの進行状況が変わりました",
      "detail": "画面を読み込み直し、最新の状況で操作してください"
    },
    "team-required": {
      "title": "チーム分けが必要です",
      "detail": "待機画面で参加者をチームに割り当ててから開始してください"
    },
    "team-captain-required": {
      "title": "リーダーが操作してください",
      "detail": "自分のチームのリーダーに確認してもらってください"
    },
    "team-label-taken": {
      "title": "そのチーム名は使われています",
      "detail": "別のチーム名にしてください"
    },
    "trade-closed": {
      "title": "今回のトレードは終了しました",
      "detail": "次の回で、あらためて申し込めます"
    },
    "trade-expired": {
      "title": "このトレードは期限切れです",
      "detail": "もう一度申し込めます"
    },
    "not-the-recipient": {
      "title": "あなた宛てのトレードではありません",
      "detail": "自分に届いたトレードの申し込みを確認してください"
    },
    "self-trade": {
      "title": "自分とはトレードできません",
      "detail": "別のメンバーを選んでください"
    },
    "card-not-held": {
      "title": "そのカードはもう手元にありません",
      "detail": "画面を読み込み直し、今の手札から選び直してください"
    },
    "ticket-expired": {
      "title": "マッチングの待ち時間を過ぎました",
      "detail": "もう一度、対戦相手を探してください"
    },
    "partner-expired": {
      "title": "相手の確認が間に合いませんでした",
      "detail": "相手の待ち時間を過ぎました。もう一度マッチングして、別の相手を探してください"
    },
    "modes-unavailable": {
      "title": "このゲームを開けません",
      "detail": "ロビーで別のゲームを選ぶか、時間を置いてお試しください"
    },
    "content-unavailable": {
      "title": "問題の準備ができていません",
      "detail": "時間を置いてお試しいただくか、ロビーにお戻りください"
    },
    "join-unavailable": {
      "title": "このルームには参加できません",
      "detail": "コードが無効か、新しい参加者を受け付けていません。ホストに確認してからお試しください"
    }
  }
} as const;
