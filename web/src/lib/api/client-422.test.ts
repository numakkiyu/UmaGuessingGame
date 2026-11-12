import { describe, expect, it } from "vitest";
import { ApiError, describeFailure } from "@/lib/api/client";

const mkErr = (status: number, code: string, message: string, requestId = "req-x") =>
  new ApiError({ status, code, message, requestId });

describe("describeFailure 的 422 分支", () => {
  it("422 说「这次提交没通过」，不说「暂时加载不出来」", () => {
    const f = describeFailure(mkErr(422, "duplicate_horse", "the same horse is used twice"));
    expect(f.kind).toBe("rejected");
    expect(f.title).toBe("这次提交没通过");
    expect(f.title).not.toContain("暂时加载不出来");
    expect(f.detail).not.toContain("再试一次通常就好");
    // 422 是「改内容」，不是「重试」
    expect(f.retryable).toBe(false);
  });

  it("500 仍然是「暂时加载不出来」（真的是临时故障）", () => {
    const f = describeFailure(mkErr(500, "internal-error", "boom"));
    expect(f.kind).toBe("error");
    expect(f.title).toBe("暂时加载不出来");
  });

  it("422 不把服务端 detail 原文透给玩家", () => {
    const f = describeFailure(
      mkErr(422, "duplicate_horse", "field slots[1].characterId duplicates slots[0]"),
    );
    expect(f.detail).not.toContain("slots[1]");
    expect(f.detail).not.toContain("characterId");
  });

  it("422 保留 diagnosticId，方便玩家反馈时定位", () => {
    const f = describeFailure(mkErr(422, "duplicate_horse", "x", "req-422-abc"));
    expect(f.diagnosticId).toBe("req-422-abc");
  });
});
