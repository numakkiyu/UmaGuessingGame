
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { isChunkLoadFailure, tryRecoverChunkFailure } from "@/app/lazy-recovery";

const CHROME = "Failed to fetch dynamically imported module: https://x/assets/a-1.js";
const FIREFOX = "error loading dynamically imported module: https://x/assets/a-1.js";
const SAFARI = "Importing a module script failed.";

describe("isChunkLoadFailure", () => {
  it("识别 Chrome / Firefox / Safari 三种措辞", () => {
    expect(isChunkLoadFailure(new TypeError(CHROME))).toBe(true);
    expect(isChunkLoadFailure(new TypeError(FIREFOX))).toBe(true);
    expect(isChunkLoadFailure(new TypeError(SAFARI))).toBe(true);
  });

  it("不把普通错误当成版本不一致", () => {
    expect(isChunkLoadFailure(new Error("网络连接中断"))).toBe(false);
    expect(isChunkLoadFailure(new TypeError("x is not a function"))).toBe(false);
    expect(isChunkLoadFailure("字符串不是 Error")).toBe(false);
    expect(isChunkLoadFailure(null)).toBe(false);
  });
});

describe("tryRecoverChunkFailure", () => {
  const reload = vi.fn();

  beforeEach(() => {
    window.sessionStorage.clear();
    reload.mockReset();
    // jsdom 的 location.reload 不可直接赋值，用 defineProperty 换成可观测的桩。
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...window.location, reload },
    });
  });

  afterEach(() => {
    window.sessionStorage.clear();
  });

  it("第一次遇到版本不一致会重载一次", () => {
    expect(tryRecoverChunkFailure(new TypeError(CHROME))).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("30 秒内第二次不再重载（防无限刷新）", () => {
    expect(tryRecoverChunkFailure(new TypeError(CHROME))).toBe(true);
    // 模拟刷新后重新进入同一个页面：标记还在
    expect(tryRecoverChunkFailure(new TypeError(CHROME))).toBe(false);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("超过窗口期后允许再恢复一次", () => {
    const past = Date.now() - 31_000;
    window.sessionStorage.setItem("uma:chunk-reload-at", String(past));
    expect(tryRecoverChunkFailure(new TypeError(CHROME))).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("非版本错误不触发重载", () => {
    expect(tryRecoverChunkFailure(new Error("服务暂时不可用"))).toBe(false);
    expect(reload).not.toHaveBeenCalled();
  });
});
