// Shared interface copy; game facts, player input and transport diagnostics are handled separately

import type { Transport } from "@/lib/realtime/use-realtime";

/* 连接补充说明只显示房间名，不展示传输协议 */
export function connectionDetail(roomName: string, _transport: Transport): string {
  // transport 保留参数以便将来按通道给出更细提示，但当前不向玩家暴露通道名
  return roomName;
}

/** Connection copy describes confirmed state without predicting success or failure. */
export const COPY = {
  /** 提交发出后结果未知 */
  confirmingSubmit: "还在确认这次提交结果，先别重复作答。",
  /** 确认按钮：确认的是同一次提交，不是新建一次 */
  confirmSubmitAction: "查看提交结果",
  /** 已确认记下，但还没结算（可以带一点轻松感，但仍说清「等揭晓」） */
  submittedPendingReveal: "答案记下了，等揭晓吧！",
  /** 提交失败但原因明确 */
  submitNotAccepted: "这次没记下，再提交一次吧。",
  /** 网络中断 */
  connectionLost: "连接中断，正在尝试恢复。",
  /** 已提交后断线 */
  connectionLostAfterSubmit: "连接中断。已提交的内容会保留。",
  /** 无权操作 */
  hostOnly: "这一步由主持完成。",
  /** 加载失败 */
  loadFailed: "内容暂时加载不出来，再试一次。",
  /** 已收卷 */
  roundLocked: "本题已收卷，不能再改答案。",
  /** 内容不可用 */
  modeUnavailable: "这个玩法暂时没有可用题目，换一个玩法试试。",
  /* 会话恢复失败时保留已输入内容，并提供重新进入入口 */
  rejoinAfterRefresh: "页面刷新后需要重新进入这一局才能继续操作。你已有的进度不会丢。",
  /** 对应的按钮文案 */
  rejoinAction: "重新进入这一局",
} as const;

/** 「问题编号」——不叫诊断编号、不叫 requestId */
export const PROBLEM_ID_LABEL = "问题编号";

/* 装饰文案应少量使用；倒计时、错误与窄屏优先呈现任务和状态 */
export const TONE = {
  /** 大厅/玩法入口：热情邀约 */
  lobbyGreeting: "训练员，来猜一把！",
  lobbySubtitle: "看线索，猜猜是哪位赛马娘。",
  /** 约局 */
  inviteFriends: "喊上朋友，一起开局吧。",
  /** 等待开场（旁边必须有真实人数与准备状态） */
  waitingForStart: "人齐就开跑！",
  /** 答对 */
  correct: "好耶，猜中了！",
  /** 全部完成 */
  allDone: "全部配对成功，好耶！",
  /** 猜错但还能继续（旁边必须显示真实剩余次数） */
  stillGuessing: "还没猜中，再看看线索。",
  /** 一局结束，可重赛 */
  playAgain: "这局过瘾吗？再来一把！",
  /** 再来一局按钮 */
  playAgainAction: "再来一局",
} as const;

/* 仅在对应玩法规则成立时使用任务提示 */
export const MODE_TONE = {
  classic: {
    beforeStart: "训练员，认得出是哪位赛马娘吗？",
    prompt: "这次猜谁？",
  },
  "aptitude-grid": {
    prompt: "这格派谁？选一位同时符合两项条件的赛马娘。",
  },
  draft: {
    prompt: "这次让谁上场？预算内排好阵容吧。",
  },
  "race-rebuild": {
    prompt: "这场比赛的顺序，你还记得吗？",
  },
  "co-race-path": {
    prompt: "从她到她，中间能接上谁？",
  },
  sort: {
    prompt: "把大家排一排。",
  },
  memory: {
    viewing: "先记住大家的位置，等会儿找出相同的两张。",
    playing: "翻开两张，找出相同的她们。",
  },
} as const;
