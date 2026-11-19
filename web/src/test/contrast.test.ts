
import { describe, expect, it } from "vitest";

function srgbToLinear(channel: number): number {
  const c = channel / 255;
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

function relativeLuminance(hex: string): number {
  const value = hex.replace("#", "");
  const r = parseInt(value.slice(0, 2), 16);
  const g = parseInt(value.slice(2, 4), 16);
  const b = parseInt(value.slice(4, 6), 16);
  return 0.2126 * srgbToLinear(r) + 0.7152 * srgbToLinear(g) + 0.0722 * srgbToLinear(b);
}

function contrast(foreground: string, background: string): number {
  const a = relativeLuminance(foreground);
  const b = relativeLuminance(background);
  const lighter = Math.max(a, b);
  const darker = Math.min(a, b);
  return (lighter + 0.05) / (darker + 0.05);
}

/** 令牌（与 tokens.css 一致） */
const T = {
  page: "#EDF7FF",
  pageTint: "#E5F4FF",
  panel: "#FFFFFF",
  panelSoft: "#F5FBFF",
  ink: "#1F3655",
  inkStrong: "#12233A",
  muted: "#526B87",
  brandGreen: "#97D81C",
  brandGreenSoft: "#DCF6A3",
  actionGreen: "#367A16",
  actionGreenHover: "#2C6212",
  blue: "#3F88F7",
  blueDeep: "#255FCB",
  gold: "#F3C754",
  correct: "#4FB85F",
  near: "#F1C550",
  wrong: "#8D98AB",
  correctEdge: "#358546",
  nearEdge: "#B0851A",
  wrongEdge: "#6B7789",
  lineStrong: "#6B89AC",
  danger: "#B3261E",
  dangerSoft: "#FDECEA",
} as const;

const AA_TEXT = 4.5;
const AA_LARGE_OR_UI = 3;

describe("普通文字对比度 ≥ 4.5:1", () => {
  const cases: [string, string, string][] = [
    ["ink on panel", T.ink, T.panel],
    ["ink on page", T.ink, T.page],
    ["ink on page-tint", T.ink, T.pageTint],
    ["ink on panel-soft", T.ink, T.panelSoft],
    ["muted on panel", T.muted, T.panel],
    ["muted on page", T.muted, T.page],
    ["muted on panel-soft", T.muted, T.panelSoft],
    ["white on action-green", T.panel, T.actionGreen],
    ["white on action-green-hover", T.panel, T.actionGreenHover],
    ["ink on brand-green（浅绿上不默认白字）", T.ink, T.brandGreen],
    ["ink on brand-green-soft", T.ink, T.brandGreenSoft],
    ["ink on gold", T.ink, T.gold],
    ["blue-deep on panel（正文链接）", T.blueDeep, T.panel],
    ["blue-deep on page", T.blueDeep, T.page],
    ["white on blue-deep", T.panel, T.blueDeep],
    ["white on danger", T.panel, T.danger],
    ["ink on danger-soft", T.ink, T.dangerSoft],
    ["panel text on ink（Tooltip）", T.panel, T.ink],
  ];

  for (const [name, fg, bg] of cases) {
    it(`${name} = ${contrast(fg, bg).toFixed(2)}:1`, () => {
      expect(contrast(fg, bg)).toBeGreaterThanOrEqual(AA_TEXT);
    });
  }
});

describe("三态单元：状态底上的文字 ≥ 4.5:1", () => {
  const cases: [string, string, string][] = [
    ["ink-strong on correct", T.inkStrong, T.correct],
    ["ink-strong on near", T.inkStrong, T.near],
    ["ink-strong on wrong", T.inkStrong, T.wrong],
  ];

  for (const [name, fg, bg] of cases) {
    it(`${name} = ${contrast(fg, bg).toFixed(2)}:1`, () => {
      expect(contrast(fg, bg)).toBeGreaterThanOrEqual(AA_TEXT);
    });
  }

  it("反例：白字在三态底上都不达标，所以禁止使用", () => {
    // 这三条断言记录的是「为什么不能用白字」，防止以后有人改回去
    expect(contrast(T.panel, T.correct)).toBeLessThan(AA_TEXT);
    expect(contrast(T.panel, T.near)).toBeLessThan(AA_TEXT);
    expect(contrast(T.panel, T.wrong)).toBeLessThan(AA_TEXT);
  });
});

describe("非文本关键边界 ≥ 3:1", () => {
  const cases: [string, string, string][] = [
    ["correct 边界 on panel", T.correctEdge, T.panel],
    ["near 边界 on panel", T.nearEdge, T.panel],
    ["wrong 边界 on panel", T.wrongEdge, T.panel],
    ["输入框/控件边界 on panel", T.lineStrong, T.panel],
    ["输入框/控件边界 on page", T.lineStrong, T.page],
    ["焦点环 blue-deep on panel", T.blueDeep, T.panel],
    ["焦点环 blue-deep on page", T.blueDeep, T.page],
    ["danger 边界 on panel", T.danger, T.panel],
  ];

  for (const [name, fg, bg] of cases) {
    it(`${name} = ${contrast(fg, bg).toFixed(2)}:1`, () => {
      expect(contrast(fg, bg)).toBeGreaterThanOrEqual(AA_LARGE_OR_UI);
    });
  }

  it("装饰性分隔线 line 只作视觉分区，不承担语义边界", () => {
    // 记录它确实低于 3:1，因此不能用于关键边界
    expect(contrast("#C3D6EA", T.panel)).toBeLessThan(AA_LARGE_OR_UI);
  });

  it("禁用态按钮用 panel-soft 底 + muted 字，仍满足 4.5:1", () => {
    // 反例：opacity 整体变淡会让白字 on action-green 掉到约 2.1:1
    const fadedGreen = "#9DBD8F"; // action-green 60% 混白后的近似值
    expect(contrast(T.panel, fadedGreen)).toBeLessThan(AA_TEXT);
    // 实际采用：浅底深字
    expect(contrast(T.muted, T.panelSoft)).toBeGreaterThanOrEqual(AA_TEXT);
  });

  it("blue #3F88F7 对白不足 4.5:1，只可用于非文本强调", () => {
    expect(contrast(T.blue, T.panel)).toBeLessThan(AA_TEXT);
    expect(contrast(T.blue, T.panel)).toBeGreaterThanOrEqual(AA_LARGE_OR_UI);
  });
});
