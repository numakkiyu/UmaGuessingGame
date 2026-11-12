import { errorDetails } from "./error-details";

type Message = { title: string; detail: string };
type Catalog = Record<string, Message>;

// Stable HTTP codes and failure categories, never original Chinese sentences.
export const errors: Record<"zh-Hans" | "zh-Hant" | "ja", Catalog> = {
  "zh-Hans": {
    ...errorDetails["zh-Hans"],
    forbidden: { title: "没有访问权限", detail: "当前身份无权查看这部分内容。如果刚被移出房间，请重新加入" },
    unauthenticated: { title: "暂时无法继续", detail: "暂时无法恢复你的游玩记录。返回大厅后重新加入" },
    "not-found": { title: "找不到这个页面", detail: "链接可能已失效，或者这个房间还没有对你开放" },
    rejected: { title: "这次提交没通过", detail: "你填的内容有一处不合规则。按页面上的提示调整后再提交一次——同样的内容再提交还是会被拒绝" },
    limited: { title: "操作有点频繁，歇一小会儿", detail: "请稍等片刻，再试一次。已确认的作答会保留" },
    busy: { title: "现在有些忙，请稍后再试", detail: "请稍等片刻，再试一次。已确认的作答会保留" },
    unavailable: { title: "暂时加载不出来", detail: "再试一次通常就好。如果一直这样，请联系主持或稍后再来" },
    timeout: { title: "还在确认这次答案是否提交成功", detail: "先别重复作答。可以用下面的按钮查看这次提交的结果" },
    offline: { title: "连接中断", detail: "已确认的作答会保留。这次操作是否完成还不确定，请检查网络后再确认" },
    unknown: { title: "暂时出了点问题", detail: "再试一次通常就好。如果一直这样，请联系主持" },
  },
  "zh-Hant": {
    ...errorDetails["zh-Hant"],
    forbidden: { title: "目前無法查看這項內容", detail: "你目前沒有查看權限。如果剛離開房間，請重新加入" },
    unauthenticated: { title: "目前無法繼續", detail: "暫時無法找回你的遊玩紀錄，請返回大廳重新加入" },
    "not-found": { title: "找不到這個頁面", detail: "連結可能已經失效，或這個房間尚未開放給你加入" },
    rejected: { title: "這次沒有送出成功", detail: "填寫的內容不符合本場規則。請依照頁面提示調整後再送出，不用重複送出相同內容" },
    limited: { title: "操作有點快，休息一下吧", detail: "請稍等片刻再試。已確認的作答會保留" },
    busy: { title: "目前比較忙碌，請稍後再試", detail: "請稍等片刻再試。已確認的作答會保留" },
    unavailable: { title: "暫時無法載入", detail: "請再試一次。若仍然無法開啟，可以聯絡主持人或稍後再來" },
    timeout: { title: "還在確認答案是否已送出", detail: "先別重複作答。請用下方按鈕確認這次送出的結果" },
    offline: { title: "連線中斷", detail: "已確認的作答會保留。目前還無法確定這次操作的結果，請恢復連線後再確認" },
    unknown: { title: "遇到一點狀況", detail: "請再試一次。如果持續發生，請聯絡主持人" },
  },
  ja: {
    ...errorDetails.ja,
    forbidden: { title: "この内容は表示できません", detail: "現在は閲覧できません。ルームから退出した場合は、もう一度参加してください" },
    unauthenticated: { title: "このまま続けられません", detail: "プレイ記録を読み込めませんでした。ロビーに戻り、もう一度参加してください" },
    "not-found": { title: "ページが見つかりません", detail: "リンクが無効になったか、このルームに参加できない可能性があります" },
    rejected: { title: "回答を受け付けられませんでした", detail: "入力内容が今回のルールに合っていません。画面の案内に沿って修正してから送信してください" },
    limited: { title: "少し時間を置いてください", detail: "しばらくしてから、もう一度お試しください。受け付け済みの回答は保存されています" },
    busy: { title: "ただいま混み合っています", detail: "しばらくしてから、もう一度お試しください。受け付け済みの回答は保存されています" },
    unavailable: { title: "読み込めませんでした", detail: "もう一度お試しください。改善しない場合は主催者にお知らせいただくか、時間を置いて再度お試しください" },
    timeout: { title: "回答が届いたか確認しています", detail: "まだ回答を送り直さず、下のボタンで今回の送信結果を確認してください" },
    offline: { title: "接続が切れました", detail: "受け付け済みの回答は保存されています。今回の操作が完了したかはまだ分かりません。接続が戻ってから確認してください" },
    unknown: { title: "うまく処理できませんでした", detail: "もう一度お試しください。改善しない場合は主催者にお知らせください" },
  },
};

export type ErrorMessageKey = keyof typeof errorDetails["zh-Hans"] | "forbidden" | "unauthenticated" | "not-found" | "rejected" | "limited" | "busy" | "unavailable" | "timeout" | "offline" | "unknown";

export function hasErrorMessage(key: string): key is ErrorMessageKey {
  return Object.hasOwn(errors["zh-Hans"], key);
}

export function hasDetailedErrorMessage(key: string): key is keyof typeof errorDetails["zh-Hans"] {
  return Object.hasOwn(errorDetails["zh-Hans"], key);
}
