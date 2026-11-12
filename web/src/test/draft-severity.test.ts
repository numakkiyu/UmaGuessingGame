
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { TaskCard } from "@/features/modes/draft-task-card";
import { SortBoard } from "@/features/modes/sort-board";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { validateLineup, type DraftCard, type DraftTask } from "@/lib/api/draft";

const tasks: DraftTask[] = [
  { taskId: "t1", label: "任务 1", field: "ground", need: "草地", role: "先" },
  { taskId: "t2", label: "任务 2", field: "ground", need: "草地", role: "差" },
] as unknown as DraftTask[];

const cards: DraftCard[] = [
  { id: "c1", name: "角色甲", price: 3, grade: "A" },
  { id: "c2", name: "角色乙", price: 2, grade: "B" },
] as unknown as DraftCard[];

const emptyDraft = { assignments: {} } as never;

describe("validateLineup 的 severity", () => {
  it("空槽位是 pending，不是 error", () => {
    const problems = validateLineup(emptyDraft, tasks, cards, 10);
    expect(problems.length).toBeGreaterThan(0);
    for (const p of problems) {
      expect(p.severity, `「${p.message}」不该是 error`).toBe("pending");
    }
  });

  it("同一角色重复使用是 error", () => {
    const draft = { assignments: { t1: "c1", t2: "c1" } } as never;
    const problems = validateLineup(draft, tasks, cards, 10);
    const dup = problems.find((p) => p.message.includes("重复使用"));
    expect(dup, "没有报出重复使用").toBeTruthy();
    expect(dup?.severity).toBe("error");
  });

  it("超预算是 error", () => {
    const draft = { assignments: { t1: "c1", t2: "c2" } } as never;
    const problems = validateLineup(draft, tasks, cards, 1);
    const over = problems.find((p) => p.message.includes("超过预算"));
    expect(over, "没有报出超预算").toBeTruthy();
    expect(over?.severity).toBe("error");
  });

  it("全部填好且合规时没有任何 problem", () => {
    const draft = { assignments: { t1: "c1", t2: "c2" } } as never;
    expect(validateLineup(draft, tasks, cards, 10)).toEqual([]);
  });
});

describe("待填状态不用危险色（跨玩法）", () => {
  const read = (rel: string) =>
    readFileSync(resolve(process.cwd(), "..", rel), "utf8");

  it("draft 任务卡的待填提示不再硬编码 danger", () => {
    const markup = renderToStaticMarkup(createElement(TaskCard, {
      task: { taskId: "t1", surface: "草地", distance: "短", style: "逃" },
      index: 0, card: null, problem: "这个任务还没选角色", problemSeverity: "pending",
      disabled: false, onOpen: () => {}, onClear: () => {},
    }));
    const wrapper = document.createElement("div"); wrapper.innerHTML = markup;
    const notice = [...wrapper.querySelectorAll("p")].find(p => p.textContent?.includes("这个任务还没选角色"));
    expect(notice).toBeTruthy();
    expect(notice?.className).not.toContain("color-danger");
    expect(wrapper.querySelector("li")?.className).not.toContain("color-danger");
  });

  it("倒计时归零用 danger 是允许的（时间到确实要警示）", () => {
    // 记录这条边界：countdown.expired → danger 不属于「待填误判」
    for (const f of ["aptitude-grid", "memory", "race-rebuild", "co-race-path"]) {
      const src = read(`web/src/features/modes/${f}-round-page.tsx`);
      expect(src, `${f} 的倒计时应该用 expired 判断`).toContain("expired");
    }
  });
});

describe("键盘可达与焦点可见", () => {
  const read = (rel: string) => readFileSync(resolve(process.cwd(), "..", rel), "utf8");

  it("全局有 focus-visible 轮廓，且不是 none", () => {
    // 实测：对局页 Tab 走 14 个元素，全部有可见焦点。
    // 这条不能靠「浏览器默认」维持——项目里大量 reset，一旦被清掉就无提示。
    const css = read("web/src/styles/base.css");
    const idx = css.indexOf(":focus-visible");
    expect(idx, "base.css 里没有 :focus-visible").toBeGreaterThan(-1);
    const block = css.slice(idx, idx + 160);
    expect(block).toMatch(/outline:\s*2px\s+solid/);
    expect(block, "outline 不能是 none").not.toMatch(/outline:\s*none/);
  });

  it("排序页的移动/并列按钮都有 aria-label（不靠图标猜）", () => {
    const html = renderToStaticMarkup(createElement(SortBoard, {
      draft: [["a", "b"], ["c"], ["d"]], cards: [], locked: false,
      onChange: () => {}, onMoveUp: () => {}, onMoveDown: () => {}, onMerge: () => {}, onSplit: () => {},
    }));
    const doc = new DOMParser().parseFromString(html, "text/html");
    const labels = [...doc.querySelectorAll("button")].map(button => button.getAttribute("aria-label"));
    for (const text of ["上移", "下移", "合并为并列", "拆成单独名次"]) {
      expect(labels.some(label => label?.includes(text)), `缺少操作名称「${text}」`).toBe(true);
    }
  });
});

describe("rapport 的 0 表示「还没揭晓」，不是「已揭晓 0/0」", () => {
  const read = (rel: string) => readFileSync(resolve(process.cwd(), "..", rel), "utf8");

  // The zero-to-revealed UI boundary is rendered in
  // features/modes/rapport-round-page.test.tsx, rather than matching syntax.

  it("服务端字段注释仍在（防止有人把 0 改成 null 却没同步前端）", () => {
    const src = read("internal/modes/rapport/mode.go");
    expect(src).toMatch(/A zero here means/);
  });
});

describe("选秀卡片只暴露必要字段", () => {
  const read = (rel: string) => readFileSync(resolve(process.cwd(), "..", rel), "utf8");

  it("服务端卡片结构不含答案/隐藏值字段", () => {
    // 实测 GET /rounds/{id} 的 publicState.cards[0] 只有：
    // id, name, imagePath, price, aptitudes
    // aptitudes 是角色固有适性（公开资料），玩家要靠它判断能否满足任务，
    // 不是答案泄漏。真正的答案（哪个角色被指派给哪个任务）在 self/draft 里。
    const src = read("internal/modes/draft/mode.go");
    const idx = src.indexOf("type Card struct {");
    expect(idx, "找不到 Card 结构").toBeGreaterThan(-1);
    const block = src.slice(idx, idx + 500);
    for (const forbidden of ["trueValue", "isCorrect", "answerId", "solution"]) {
      expect(block, `cardView 不应含 ${forbidden}`).not.toContain(forbidden);
    }
  });

  it("前端类型与实测字段一致", () => {
    const src = read("web/src/lib/api/draft.ts");
    for (const field of ["id", "name", "imagePath", "price", "aptitudes"]) {
      expect(src, `DraftCard 缺少 ${field}`).toContain(field);
    }
  });
});

describe("排序闭卷不泄漏真值", () => {
  const read = (rel: string) => readFileSync(resolve(process.cwd(), "..", rel), "utf8");

  it("服务端按信息风格决定是否下发 display/raw", () => {
    // 实测：
    // closed → {"characterId","name","imagePath"} 无真值
    // open → {"characterId","name","imagePath","display","raw"} 有真值
    // 两种风格都必须保留 characterId，否则玩家不知道卡是谁。
    const src = read("internal/modes/sort/mode.go");
    expect(src, "缺少信息风格判断").toMatch(/InformationStyle|informationStyle/);
    // 至少要有一处按风格分支决定是否带 display
    expect(src).toMatch(/closed|Closed/);
  });

  it("前端不自己按真值预排或预合并", () => {
    // Initial drafts preserve input order without solving the task
    const src = read("web/src/features/modes/sort-round-page.tsx");
    expect(src, "不应再出现按真值预排").not.toMatch(/sortBy\(.*(?:raw|sortValue|display)/);
    expect(src, "不应出现自动合并并列").not.toMatch(/autoMerge|autoMergeTies/);
  });
});

describe("不把内部诊断显示给玩家", () => {
  const read = (rel: string) => readFileSync(resolve(process.cwd(), "..", rel), "utf8");

  it("classic 页面刻意不渲染 shapeWarnings", () => {
    // 实测：提交一次猜测后，页面上搜不到 shapeWarnings / 「契约差异」。
    // 源码里也留了注释说明这是刻意的。
    const src = read("web/src/features/classic/classic-room-page.tsx");
    expect(src, "应保留「不显示 shapeWarnings」的说明").toMatch(/不显示\s*shapeWarnings/);
    // 但不能真的把它渲染出来
    expect(src).not.toMatch(/\{shapeWarnings\}/);
    expect(src).not.toMatch(/shapeWarnings\.map/);
  });

  it("玩家可见文本里不出现内部诊断词", () => {
    // e2e-modes 的 BANNED 列表已经覆盖这些词（shapeWarnings / privateState /
    // phaseVersion / roomVersion / OpenAPI / DTO 等），这里再守一层源码侧：
    // 这些词只应出现在类型定义、注释、或 API 层，不该进 JSX 文本节点。
    const pages = [
      "web/src/features/classic/classic-room-page.tsx",
      "web/src/features/modes/sort-round-page.tsx",
      "web/src/features/modes/draft-round-page.tsx",
    ];
    for (const rel of pages) {
      const src = read(rel);
      const withoutComments = src
        .replace(/\/\*[\s\S]*?\*\//g, "")
        .replace(/^\s*\/\/.*$/gm, "");
      // 只看「会被渲染成文字」的部分：JSX 文本节点与字符串字面量。
      // 这些词出现在逻辑里（比如提交时要带 phaseVersion）是正常的、必需的，
      // 断言不该管那个——只管它们会不会被玩家看见。
      const visibleStrings = [
        ...withoutComments.matchAll(/>\s*([^<>{}\n]{4,})\s*</g),
      ].map((m) => m[1] ?? "");
      for (const word of ["shapeWarnings", "privateState", "phaseVersion", "roomVersion"]) {
        const leaked = visibleStrings.filter((t) => t.includes(word));
        expect(leaked, `${rel} 把 ${word} 显示给玩家了：${leaked.join(" / ")}`).toEqual([]);
      }
    }
  });
});
