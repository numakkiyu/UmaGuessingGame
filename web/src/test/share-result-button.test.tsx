
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ShareResultButton } from "@/components/uma/share-result-button";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("ShareResultButton", () => {
  it("点击后把给定文本写进剪贴板并显示已复制", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });

    render(<ShareResultButton text="我在马娘猜猜乐里猜中了，用了 3 次。" />);
    await userEvent.click(screen.getByRole("button", { name: /复制战绩/ }));

    expect(writeText).toHaveBeenCalledWith("我在马娘猜猜乐里猜中了，用了 3 次。");
    await waitFor(() => {
      expect(screen.getByRole("button").textContent).toContain("已复制");
    });
    expect(screen.getByRole("status").textContent).toContain("战绩已复制");
  });

  it("剪贴板不可用时说明下一步，而不是假装成功", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });

    render(<ShareResultButton text="x" />);
    await userEvent.click(screen.getByRole("button", { name: /复制战绩/ }));

    await waitFor(() => {
      expect(screen.getByRole("status").textContent).toContain("手动复制");
    });
    // 不能显示「已复制」——那是假的成功
    expect(screen.getByRole("button").textContent).not.toContain("已复制");
  });

  it("结果通过 role=status 播报，不只靠颜色", () => {
    render(<ShareResultButton text="x" />);
    const status = screen.getByRole("status");
    expect(status.getAttribute("aria-live")).toBe("polite");
  });
});
