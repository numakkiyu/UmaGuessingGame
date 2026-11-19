
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const STYLES = resolve(import.meta.dirname, "../styles");

function readStyle(name: string): string {
  return readFileSync(resolve(STYLES, name), "utf8");
}

const tokens = readStyle("tokens.css");
const base = readStyle("base.css");
const components = readStyle("components.css");

describe("颜色令牌", () => {
  const REQUIRED_COLORS: Record<string, string> = {
    "--color-page": "#f4f8ed",
    "--color-panel": "#ffffff",
    "--color-ink": "#304739",
    "--color-muted": "#5a705e",
    "--color-brand-green": "#97d81c",
    "--color-action-green": "#367a16",
    "--color-blue": "#3f88f7",
    "--color-blue-deep": "#255fcb",
    "--color-gold": "#f3c754",
    "--color-correct": "#4fb85f",
    "--color-near": "#f1c550",
    "--color-wrong": "#8d98ab",
  };

  for (const [token, value] of Object.entries(REQUIRED_COLORS)) {
    it(`${token} = ${value}`, () => {
      expect(tokens.toLowerCase()).toContain(`${token}: ${value}`);
    });
  }

  it("不引入橙色主强调（状态色与装饰色分离）", () => {
    expect(tokens.toLowerCase()).not.toContain("--um-orange");
    expect(tokens.toLowerCase()).not.toContain("--um-cream");
  });
});

describe("字体与尺寸", () => {
  it("按语言加载本地思源字体，缺失时使用系统字体", () => {
    const fonts = readFileSync(resolve(STYLES, "../../public/fonts/source-han-sans.css"), "utf8");
    for (const family of ["Uma Sans SC", "Uma Sans TC", "Uma Sans JP"]) {
      expect(fonts).toContain(family);
    }
    expect(fonts).not.toMatch(/https?:/);
    expect(fonts).toContain("sans-serif");
    expect(tokens).toContain("--font-sans: var(--uma-font)");
  });

  it("正文 16px/1.6，辅助文字最低 13px/1.5", () => {
    expect(tokens).toContain("--text-body: 1rem");
    expect(tokens).toContain("--text-body--line-height: 1.6");
    expect(tokens).toContain("--text-aux: 0.8125rem"); // 13px
    expect(tokens).toContain("--text-aux--line-height: 1.5");
    expect(base).toContain("font-size: 1rem");
    expect(base).toContain("line-height: 1.6");
  });

  it("触摸区令牌 44 / 48", () => {
    expect(tokens).toContain("--touch-min: 44px");
    expect(tokens).toContain("--touch-comfortable: 48px");
  });

  it("间距比例为 4/8/12/16/24/32/48/64 的派生值", () => {
    expect(tokens).toContain("--gap-region: 24px");
    expect(tokens).toContain("--gap-related: 12px");
  });
});

describe("断点", () => {
  it("五档断点：480 / 768 / 1024 / 1440（320 为默认）", () => {
    expect(tokens).toContain("--breakpoint-sm: 30rem"); // 480
    expect(tokens).toContain("--breakpoint-md: 48rem"); // 768
    expect(tokens).toContain("--breakpoint-lg: 64rem"); // 1024
    expect(tokens).toContain("--breakpoint-xl: 90rem"); // 1440
  });

  it("短屏（height < 600px）收紧 header 与间距", () => {
    expect(tokens).toContain("@media (height < 37.5rem)"); // 600px
    expect(tokens).toContain("--header-h: var(--header-h-short)");
  });
});

describe("全局壳与 z-index", () => {
  it("层级基线 0 / 10 / 30 / 50 / 60 / 80", () => {
    expect(tokens).toContain("--z-base: 0");
    expect(tokens).toContain("--z-sticky: 10");
    expect(tokens).toContain("--z-popover: 30");
    expect(tokens).toContain("--z-sheet: 50");
    expect(tokens).toContain("--z-toast: 60");
    expect(tokens).toContain("--z-fault: 80");
  });

  it("内容宽 min(100% - 2*gutter, 1440px)", () => {
    expect(tokens).toContain("--content-max: 1440px");
    expect(components).toContain("max-width: var(--content-max)");
    expect(components).toContain("padding-inline: var(--gutter)");
  });

  it("100dvh 有内容滚动回退", () => {
    expect(components).toContain("min-height: 100vh");
    expect(components).toContain("min-height: 100dvh");
  });
});

describe("动画时间与曲线令牌", () => {
  it("六个时间令牌", () => {
    expect(tokens).toContain("--duration-instant: 80ms");
    expect(tokens).toContain("--duration-quick: 120ms");
    expect(tokens).toContain("--duration-standard: 200ms");
    expect(tokens).toContain("--duration-reveal: 320ms");
    expect(tokens).toContain("--duration-scene: 480ms");
    expect(tokens).toContain("--duration-ceremony: 800ms");
    expect(tokens).toContain("--duration-ceremony-max: 1200ms");
  });

  it("主曲线与离场曲线", () => {
    expect(tokens).toContain("--ease-primary: cubic-bezier(0.2, 0, 0, 1)");
    expect(tokens).toContain("--ease-exit: cubic-bezier(0.3, 0, 1, 1)");
  });
});

describe("减少动态", () => {
  it("支持 prefers-reduced-motion", () => {
    expect(base).toContain("prefers-reduced-motion: reduce");
  });

  it("支持局内「减少动态」开关（data-motion=reduced）", () => {
    expect(base).toContain('[data-motion="reduced"]');
  });
});

/** 去掉 CSS 注释，只检查真实声明，避免把「严禁使用 overflow-x: hidden」这类说明文字算成违规 */
function stripCssComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\s+/g, " ");
}

describe("禁止用 overflow-x:hidden 掩盖溢出", () => {
  it("样式源中不出现 overflow-x: hidden（注释除外）", () => {
    for (const [name, source] of [
      ["tokens.css", tokens],
      ["base.css", base],
      ["components.css", components],
    ] as const) {
      const code = stripCssComments(source).toLowerCase();
      expect(code, `${name} 不应使用 overflow-x: hidden`).not.toContain("overflow-x: hidden");
      expect(code, `${name} 不应使用 overflow-x:hidden`).not.toContain("overflow-x:hidden");
    }
  });

  it("可收缩子项使用 min-width:0 真实解决溢出", () => {
    expect(components).toContain("min-width: 0");
  });

  it("长译名允许换行（overflow-wrap）", () => {
    expect(base).toContain("overflow-wrap: break-word");
  });

  it("局部横向滚动只在容器内（uma-scroll-x）", () => {
    expect(components).toContain("overflow-x: auto");
  });
});
