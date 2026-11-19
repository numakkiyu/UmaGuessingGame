import { registerMessages } from ".";
export const fault = {
  "zh-Hans": { back: "返回当前页面", notice: "遇到一点小状况", title: "这次操作还没完成", detail: "请稍后再试，或先返回游玩大厅" },
  "zh-Hant": { back: "回到目前頁面", notice: "遇到一點小狀況", title: "這次操作還沒完成", detail: "請稍後再試，或先返回遊戲大廳" },
  ja: { back: "元の画面へ戻る", notice: "うまくいかなかったようです", title: "操作を完了できませんでした", detail: "時間をおいて試すか、ロビーへ戻ってください" },
} as const;
registerMessages("fault", fault);
