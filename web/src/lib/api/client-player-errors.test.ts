import { describe, expect, it } from "vitest";
import { ApiError, describeFailure } from "@/lib/api/client";

const mkErr = (status: number, code: string, message: string, requestId = "req-x") =>
  new ApiError({ status, code, message, requestId });

/*
 * 玩家正常操作会撞上的错误，必须有专属文案，不能落到通用 422 兜底。
 *
 * 缺口来源（2026-11-11 实测）：加入一个已满员的房间，服务端返回
 * 422 room-full "the room has no free player slot"
 * 而 describeFailure 没有 room-full 的条目，于是玩家读到的是 422 兜底的
 * 「你填的内容有一处不合规则」
 * ——对「房间满了」来说完全是误导，玩家会去反复检查自己填了什么。
 * invitation-invalid 同理（403，落不到 422 但也没有专属文案）。
 *
 * 这条测试锁定这批文案存在且「不再等于兜底文案」，防止将来被误删或改名。
 */

// 服务端会返回、且玩家真的会遇到：(code, status, 期望标题片段)
const PLAYER_FACING: Array<[string, number, string]> = [
  ["room-full", 422, "满员"],
  ["room-in-progress", 422, "已经开始"],
  ["already-member", 422, "已经在房间"],
  ["invitation-invalid", 403, "房间分享码"],
  ["invitation-not-found", 404, "找不到这个邀请"],
  ["not-a-participant", 403, "不在这一局"],
  ["owner-cannot-leave", 422, "房主不能直接离开"],
  ["round-paused", 422, "暂停中"],
  ["round-not-collecting", 422, "现在还不能提交"],
  ["deadline-passed", 422, "作答时间到了"],
  ["submission-revision-conflict", 409, "刚刚更新过"],
  ["phase-version-conflict", 409, "进度刚刚变了"],
  ["team-required", 422, "还没分队"],
  ["team-captain-required", 403, "需要队长"],
  ["team-label-taken", 422, "队名"],
  ["trade-closed", 422, "交易已关闭"],
  ["trade-expired", 422, "交易已过期"],
  ["not-the-recipient", 403, "不是发给你的"],
  ["self-trade", 422, "不能和自己交易"],
  ["card-not-held", 422, "不在你手里"],
  ["ticket-expired", 422, "匹配等待已超时"],
  ["partner-expired", 409, "对手的匹配等待已超时"],
  ["modes-unavailable", 422, "打不开"],
  ["content-unavailable", 503, "题目资料"],
];

describe("玩家会遇到的错误都有专属文案", () => {
  it.each(PLAYER_FACING)("%s 有专属标题，不落到通用兜底", (code, status, expectTitle) => {
    const f = describeFailure(mkErr(status, code, `server detail for ${code}`));
    expect(f.title).toContain(expectTitle);
    // 关键断言：不能是 422 兜底那句「你填的内容有一处不合规则」所对应的标题
    expect(f.title).not.toBe("内容还需要调整");
    // 也不能把服务端的英文 detail 直接显示给玩家
    expect(f.detail).not.toContain("server detail");
    // 这些都需要玩家改变操作，不是「重试就好」
    expect(f.retryable).toBe(false);
  });

  it("未知错误码仍然落到兜底，且不泄露服务端 detail", () => {
    const f = describeFailure(mkErr(422, "some-brand-new-code", "internal wording leaked here"));
    expect(f.title).toBe("内容还需要调整");
    expect(f.detail).not.toContain("internal wording");
  });
});
