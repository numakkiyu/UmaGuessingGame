import { registerMessages } from ".";

export const sharingCopy = {
  "zh-Hans": {
    copy: "复制战绩", copied: "已复制", success: "战绩已复制，可以粘贴到聊天里",
    failed: "暂时不能自动复制。可以选中下方的分享文字，再手动复制", text: "分享文字",
  },
  "zh-Hant": {
    copy: "複製遊玩結果", copied: "已複製", success: "已複製遊玩結果，可以貼到聊天裡",
    failed: "目前無法自動複製。請選取下方的分享文字，再自行複製", text: "分享文字",
  },
  ja: {
    copy: "結果をコピー", copied: "コピーしました", success: "結果をコピーしました。チャットなどに貼り付けられます",
    failed: "自動でコピーできませんでした。下の共有用テキストを選択してコピーしてください", text: "共有用テキスト",
  },
} as const;

registerMessages("sharing", sharingCopy);
