import { registerMessages } from ".";

export const concentrationEntry = {
  "zh-Hans": {
    "title": "马娘浓度测试",
    "description": "看立绘认角色，填写译名、声优和生日等资料，每局随机 20 位",
    "untimed": "不限时",
    "difficulty": "认人，也考角色档案",
    "available": "浓度测试 · 可开局"
  },
  "zh-Hant": {
    "title": "賽馬娘濃度測試",
    "description": "看立繪認角色，填寫譯名、聲優和生日等資料，每局隨機 20 位",
    "untimed": "不限時",
    "difficulty": "認人，也考角色資料",
    "available": "濃度測試 · 可開局"
  },
  "ja": {
    "title": "ウマ娘知識テスト",
    "description": "全身イラストを見て、名前や声優、誕生日を回答。毎回ランダムに 20 人を出題",
    "untimed": "時間制限なし",
    "difficulty": "顔とプロフィールの知識",
    "available": "知識テストを遊ぶ"
  }
} as const;
registerMessages("concentrationEntry", concentrationEntry);
