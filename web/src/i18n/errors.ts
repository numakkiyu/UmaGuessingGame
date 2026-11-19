import { errorDetails } from "./error-details";

type Message = { title: string; detail: string };
type Catalog = Record<string, Message>;

// Stable HTTP codes and failure categories, never original Chinese sentences.
export const errors: Record<"zh-Hans" | "zh-Hant" | "ja", Catalog> = {
  "zh-Hans": {
    ...errorDetails["zh-Hans"],
    forbidden: { title: "暂时无法查看", detail: "你目前不能查看这部分内容。如果刚被移出房间，请向房主确认后重新加入" },
    unauthenticated: { title: "暂时无法继续", detail: "暂时无法恢复你的游玩记录。返回大厅后重新加入" },
    "not-found": { title: "找不到这个页面", detail: "链接可能已失效，或者这个房间还没有对你开放" },
    rejected: { title: "内容还需要调整", detail: "有些内容不符合本局规则，请按页面提示调整后再提交" },
    limited: { title: "请稍等再试", detail: "刚才点得有些频繁，请稍等片刻再试。已确认的作答会保留" },
    busy: { title: "现在有些拥挤", detail: "请稍等片刻，再试一次。已确认的作答会保留" },
    unavailable: { title: "暂时加载不出来", detail: "请稍后再试。如果仍无法打开，可以联系主持人" },
    timeout: { title: "正在确认刚才的操作", detail: "还没收到结果，请用下面的按钮确认进度后再继续" },
    offline: { title: "连接中断", detail: "已确认的作答会保留。这次操作是否完成还不确定，请检查网络后再确认" },
    unknown: { title: "暂时无法继续", detail: "请查看本局最新进度。若仍无法继续，可以联系主持人" },
  },
  "zh-Hant": {
    ...errorDetails["zh-Hant"],
    forbidden: { title: "目前無法查看這項內容", detail: "目前無法查看這部分內容。如果剛被移出房間，請向房主確認後重新加入" },
    unauthenticated: { title: "目前無法繼續", detail: "暫時無法找回你的遊玩紀錄，請返回大廳重新加入" },
    "not-found": { title: "找不到這個頁面", detail: "連結可能已經失效，或這個房間尚未開放給你加入" },
    rejected: { title: "內容還需要調整", detail: "填寫的內容不符合本場規則。請依照頁面提示調整後再送出，不用重複送出相同內容" },
    limited: { title: "請稍候再試", detail: "剛才操作較頻繁，請稍候再試。已確認的作答會保留" },
    busy: { title: "目前較多人遊玩", detail: "請稍等片刻再試。已確認的作答會保留" },
    unavailable: { title: "暫時無法載入", detail: "請再試一次。若仍然無法開啟，可以聯絡主持人或稍後再來" },
    timeout: { title: "正在確認剛才的操作", detail: "還沒收到結果，請用下方按鈕確認進度後再繼續" },
    offline: { title: "連線中斷", detail: "已確認的作答會保留。目前還無法確定這次操作的結果，請恢復連線後再確認" },
    unknown: { title: "暫時無法繼續", detail: "請查看本場最新進度。若仍無法繼續，可以聯絡主持人" },
  },
  ja: {
    ...errorDetails.ja,
    forbidden: { title: "この内容は表示できません", detail: "現在この内容は閲覧できません。ルームから外された場合は、ホストに確認してから参加し直してください" },
    unauthenticated: { title: "このまま続けられません", detail: "プレイ記録を読み込めませんでした。ロビーに戻り、もう一度参加してください" },
    "not-found": { title: "ページが見つかりません", detail: "リンクが無効になったか、このルームに参加できない可能性があります" },
    rejected: { title: "内容を確認してください", detail: "入力内容が今回のルールに合っていません。画面の案内に沿って修正してから送信してください" },
    limited: { title: "少し時間を置いてください", detail: "短時間に操作が続いています。少し待ってからお試しください。受け付け済みの回答は保存されています" },
    busy: { title: "ただいま混み合っています", detail: "しばらくしてから、もう一度お試しください。受け付け済みの回答は保存されています" },
    unavailable: { title: "読み込めませんでした", detail: "もう一度お試しください。改善しない場合は進行役にお知らせいただくか、時間を置いて再度お試しください" },
    timeout: { title: "直前の操作を確認しています", detail: "まだ結果が届いていません。下のボタンで進行状況を確認してから続けてください" },
    offline: { title: "接続が切れました", detail: "受け付け済みの回答は保存されています。今回の操作が完了したかはまだ分かりません。接続が戻ってから確認してください" },
    unknown: { title: "続けられませんでした", detail: "現在の進行状況を確認してください。それでも続けられない場合は、ホストにお知らせください" },
  },
};

export type ErrorMessageKey = keyof typeof errorDetails["zh-Hans"] | "forbidden" | "unauthenticated" | "not-found" | "rejected" | "limited" | "busy" | "unavailable" | "timeout" | "offline" | "unknown";

export function hasErrorMessage(key: string): key is ErrorMessageKey {
  return Object.hasOwn(errors["zh-Hans"], key);
}

export function hasDetailedErrorMessage(key: string): key is keyof typeof errorDetails["zh-Hans"] {
  return Object.hasOwn(errorDetails["zh-Hans"], key);
}
