import { describe, expect, it } from "vitest";
import { ApiError, NetworkError, describeFailure } from "./api/client";
import { PlayerNotice, playerMessage } from "./player-notice";
import { tradeFailureMessage } from "@/features/modes/trade-round-page";

describe("player error boundaries", () => {
  it("only authored notices may be shown verbatim", () => {
    expect(playerMessage(new PlayerNotice("请保留完整的练习记录"), "暂时无法读取")).toBe("请保留完整的练习记录");
    for (const error of [new Error("服务器: SQLite failed"), new TypeError("Failed to fetch"), new DOMException("IndexedDB transaction failure"), { message: "本机路径泄露" }]) {
      expect(playerMessage(error, "暂时无法读取")).toBe("暂时无法读取");
    }
  });
  it("network failure preserves uncertainty and makes no automatic recovery promise", () => {
    const failure = describeFailure(new NetworkError("Failed to fetch", null));
    expect(failure.detail).toContain("还不确定");
    expect(failure.detail).not.toContain("正在尝试恢复");
    expect(failure.detail).not.toContain("已经提交过");
  });
  it.each([429, 503])("temporary refusal %i uses neutral player copy without leaking details", status => {
    const failure = describeFailure(new ApiError({ status, code: "busy", message: "server queue full", requestId: "req-safe" }));
    expect(failure.retryable).toBe(true);
    expect(failure.detail).toContain("稍等");
    expect(failure.detail).not.toContain("server");
    expect(failure.diagnosticId).toBe("req-safe");
  });
  it("unrecognized trade errors never render raw problem details", () => {
    const failure = tradeFailureMessage(new ApiError({ status: 500, code: "internal-error", message: "服务器 internal SQLite path", requestId: "req-safe" }));
    expect(failure.detail).not.toMatch(/服务器|SQLite|internal/);
    expect(tradeFailureMessage(new ApiError({ status: 409, code: "trade-expired", message: "expired", requestId: null })).title).toBe("这条提案已经过期");
  });
});
