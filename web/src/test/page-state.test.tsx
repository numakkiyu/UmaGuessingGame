
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { ApiError, NetworkError, TimeoutError, describeFailure } from "@/lib/api/client";
import { EmptyView, ErrorView, ForbiddenView, toErrorState } from "@/components/uma/page-state";

function apiError(status: number, code: string): ApiError {
  return new ApiError({
    status,
    code,
    message: "服务端说明",
    requestId: "req-test",
    retryable: status === 503,
  });
}

describe("describeFailure 的状态映射", () => {
  it("403 → forbidden", () => {
    expect(describeFailure(apiError(403, "FORBIDDEN")).kind).toBe("forbidden");
  });

  it("401 → unauthenticated", () => {
    expect(describeFailure(apiError(401, "UNAUTHENTICATED")).kind).toBe("unauthenticated");
  });

  it("404 → not-found，与 forbidden 区分", () => {
    expect(describeFailure(apiError(404, "NOT_FOUND")).kind).toBe("not-found");
  });

  it("超时 → timeout，标题说明「还在确认」而不是断言失败", () => {
    const failure = describeFailure(new TimeoutError(1000));
    expect(failure.kind).toBe("timeout");
    expect(failure.title).toContain("正在确认刚才的操作");
  });

  it("网络失败 → offline", () => {
    expect(describeFailure(new NetworkError("x", new Error("y"))).kind).toBe("offline");
  });

  it("未知异常 → error，且不泄露内部细节", () => {
    const failure = describeFailure(new Error("内部堆栈细节"));
    expect(failure.kind).toBe("error");
    expect(failure.detail).not.toContain("内部堆栈细节");
  });
});

describe("toErrorState：403 走 forbidden，其余走 error", () => {
  it("403 得到 forbidden 状态", () => {
    const state = toErrorState(apiError(403, "FORBIDDEN"));
    expect(state.kind).toBe("forbidden");
  });

  it("500 得到 error 状态", () => {
    const state = toErrorState(apiError(500, "INTERNAL"));
    expect(state.kind).toBe("error");
  });
});

describe("状态视图渲染", () => {
  it("ForbiddenView 显示权限说明与诊断编号，不是「暂无数据」", () => {
    render(<ForbiddenView failure={describeFailure(apiError(403, "FORBIDDEN"))} />);
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.getByText("暂时无法查看")).toBeTruthy();
    expect(screen.getByText(/req-test/)).toBeTruthy();
    expect(screen.queryByText(/暂无/)).toBeNull();
  });

  it("ErrorView 提供重试按钮", () => {
    const onRetry = vi.fn();
    render(<ErrorView failure={describeFailure(apiError(503, "BUSY"))} onRetry={onRetry} />);
    const button = screen.getByRole("button", { name: /重试/ });
    button.click();
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("超时状态用 warning 语气，提示确认进度后再继续（不暴露内部机制）", () => {
    render(<ErrorView failure={describeFailure(new TimeoutError(500))} onRetry={vi.fn()} />);
    expect(screen.getByText(/正在确认刚才的操作/)).toBeTruthy();
    expect(screen.getByText(/确认进度后再继续/)).toBeTruthy();
    // 玩家文案：不得出现幂等键等内部词
    expect(screen.queryByText(/幂等/)).toBeNull();
  });

  it("EmptyView 只在确认无内容时使用，文案不含错误语义", () => {
    render(<EmptyView title="还没有可开局的玩法" detail="稍后再试" />);
    expect(screen.getByText("还没有可开局的玩法")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
