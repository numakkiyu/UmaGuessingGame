// Source checks cover SQL and entry-policy invariants that are independent of rendering
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const read = (p: string) => readFileSync(resolve(process.cwd(), "..", p), "utf8");

describe("重赛：旧回合必须先收尾再插新回合", () => {
  const src = read("internal/room/negotiation.go");

  it("插入新回合之前把旧回合推进到 settled", () => {
    const start = src.indexOf("func (s *Service) createRematchRoundQ");
    expect(start, "找不到 createRematchRoundQ").toBeGreaterThan(-1);
    const block = src.slice(start, start + 2500);

    const settleAt = block.indexOf("SET status = 'settled'");
    const insertAt = block.indexOf("INSERT INTO rounds");
    expect(settleAt, "缺少把旧回合收尾的 UPDATE").toBeGreaterThan(-1);
    expect(insertAt, "缺少插入新回合").toBeGreaterThan(-1);
    // 顺序很重要：先 settle，再 insert，否则撞 idx_rounds_room_current
    expect(settleAt).toBeLessThan(insertAt);
  });

  it("收尾 UPDATE 对已结算回合幂等", () => {
    const start = src.indexOf("func (s *Service) createRematchRoundQ");
    const block = src.slice(start, start + 2500);
    expect(block).toMatch(/status NOT IN \('settled', 'cancelled'\)/);
  });

  it("新回合 id 被带回给调用方，不再用 _ 丢弃", () => {
    const start = src.indexOf("func (s *Service) RequestRematch");
    const block = src.slice(start, start + 6000);
    expect(block).not.toMatch(/if _, err := s\.createRematchRoundQ/);
    expect(block).toMatch(/newRound = /);
  });
});

// 多人玩法不在单人入口页白点 now uses rendered behavior assertions in i18n/entry.test.tsx.

// Sort controls, complete names and skipped ranks are exercised by
// i18n/sort.test.tsx and i18n/sort-online.test.tsx. Actual table/card density
// requires browser geometry, not matching an old class string in TSX.
// The always-visible pedigree disclaimer is asserted by the rendered region in
// i18n/pedigree-online.test.tsx for all three languages.

describe("大屏字号按视口伸缩", () => {
  const src = read("web/src/styles/components.css");

  it("舞台字号用 clamp + vw，不按物理像素写死", () => {
    // 支持 1280×720 与 3840×2160，字体按 CSS 视口设置，
    // 不单按物理像素翻倍。写死 rem 会在 4K 投影上显得很小。
    expect(src).toMatch(/--display-hero:\s*clamp\([^)]*vw/);
    expect(src).toMatch(/clamp\([^)]*vw[^)]*clamp\(|clamp\([^)]*vw/);
  });

  it("至少三个档位都按 vw 伸缩（主标题/正文/辅助）", () => {
    const clamps = src.match(/clamp\([^)]*vw[^)]*\)/g) ?? [];
    expect(clamps.length).toBeGreaterThanOrEqual(3);
  });
});

// 大厅首屏：新用户四条路径都在 now uses rendered behavior assertions in i18n/entry.test.tsx.

describe("揭晓展示不直出内部 ID", () => {
  const src = read("web/src/features/classic/classic-room-page.tsx");

  it("结算面板不显示内部角色标识", () => {
    // Shared results must not expose internal answer identifiers
    const forbiddenPatterns = [/\u7b54\u6848\u89d2\u8272\s*ID/, /\u89d2\u8272\s*ID\uff1a/];
    for (const re of forbiddenPatterns) {
      expect(re.test(src), `结算面板不应出现 ${re}`).toBe(false);
    }
  });

  it("揭晓后显示角色名或中性措辞", () => {
    expect(src).toMatch(/revealedAnswerName/);
    expect(src).toMatch(/答案已揭晓/);
  });
});

describe("玩法页不把内部标识直出给玩家", () => {
  // 允许的例外只有「诊断编号」——文案规范明确允许在严重故障时展示
  // 经审校的安全诊断码，中文影响与下一步优先。
  const MODE_PAGES = [
    "aptitude-grid-round-page.tsx", "audio-round-page.tsx", "bluff-round-page.tsx",
    "clue-puzzle-round-page.tsx", "co-race-path-round-page.tsx", "creative-round-page.tsx",
    "draft-round-page.tsx", "memory-round-page.tsx", "pedigree-round-page.tsx",
    "race-rebuild-round-page.tsx", "rapport-round-page.tsx", "sort-round-page.tsx",
    "strategy-race-round-page.tsx",
  ];

  it("没有页面用「角色 ID / 卡 ID / 实例 ID」这类内部词直接展示", () => {
    const offenders: string[] = [];
    for (const f of MODE_PAGES) {
      const src = read(`web/src/features/modes/${f}`);
      for (const re of [/\u89d2\u8272\s*ID/, /\u5361\s*ID/, /\u5b9e\u4f8b\s*ID/]) {
        if (re.test(src)) offenders.push(`${f}: ${re}`);
      }
    }
    expect(offenders, `以下页面直出了内部标识：${offenders.join(", ")}`).toEqual([]);
  });
});

describe("前端模式清单与服务端不脱节", () => {
  const types = read("web/src/lib/api/types.ts");
  const modepages = read("web/src/features/modes/mode-play-pages.tsx");

  it("PLAYABLE_MODE_IDS 覆盖 15 个模式（全集，不是「可用子集」）", () => {
    // 这份清单是「前端认识的模式全集」，实际能否开局由服务端 availability 决定。
    // 混用两者会导致：audio/bluff 被前端当成可玩，或新玩法加进服务端后前端不认。
    const m = types.match(/PLAYABLE_MODE_IDS = \[([\s\S]*?)\]/);
    if (!m || m[1] === undefined) throw new Error("找不到 PLAYABLE_MODE_IDS");
    const ids = [...m[1].matchAll(/"([a-z-]+)"/g)].map((x) => x[1] ?? "");
    expect(ids.length).toBe(15);
    for (const must of ["classic", "sort", "trade", "clue-puzzle", "audio", "bluff"]) {
      expect(ids, `清单缺少 ${must}`).toContain(must);
    }
  });

  it("mode-play-pages 里的入口页组件都在（经典/排序/记忆/血统走独立文件）", () => {
    // 清单里有、但没有页面 = 大厅点进去 404。
    // 注意 classic / sort / memory / pedigree 有各自的独立页面文件，
    // 不在这份 mode-play-pages 里——断言要按真实分布写。
    for (const page of [
      "AptitudeGridPlayPage", "DraftPlayPage", "RaceRebuildPlayPage",
      "CoRacePathPlayPage", "CluePuzzlePlayPage", "StrategyRacePlayPage",
      "RapportPlayPage", "CreativePlayPage", "TradePlayPage",
    ]) {
      expect(modepages, `缺少入口页 ${page}`).toContain(page);
    }
  });

  it("独立入口页文件存在", () => {
    for (const f of ["sort-play-page", "memory-play-page", "mode-start-page"]) {
      const src = read(`web/src/features/modes/${f}.tsx`);
      expect(src.length, `${f}.tsx 为空`).toBeGreaterThan(500);
    }
    // classic 走 classic-room-page 的入口
    const classic = read("web/src/features/modes/mode-play-pages.tsx");
    expect(classic).toBeTruthy();
  });
});

describe("揭晓前不泄漏答案（答案隐私）", () => {
  const view = read("internal/room/view.go");

  it("revealedAnswerCharacterId 只在揭晓或结算阶段赋值", () => {
    // 实测：closed 与 open 两种信息风格在 collecting 阶段，
    // revealedAnswerCharacterId 都是 null、publicState 里没有 answer、
    // 整个响应 0 次 characterId。这条红线不能靠「碰巧没赋值」维持。
    //
    // 真实结构是「先判阶段，再赋值」：
    // if r.Status == PhaseRevealed || r.Status == PhaseSettled {
    // view.RevealedAnswerCharacterID = ...
    // 所以要在赋值点之前找那条 if。
    const assign = view.indexOf("view.RevealedAnswerCharacterID =");
    expect(assign, "找不到赋值语句").toBeGreaterThan(-1);
    const before = view.slice(Math.max(0, assign - 600), assign);
    expect(
      before,
      "赋值前必须有「已揭晓或已结算」的阶段判断，否则 collecting 阶段就会泄漏答案",
    ).toMatch(/PhaseRevealed[\s\S]{0,80}PhaseSettled/);
  });

  it("答案取自 Project 之后的 publicState，不是存储态", () => {
    // 存储态没有 answer 字段；只有 Project 产出后才可能读到。
    // 若改成读存储态，要么永远空、要么把答案提前写进存储态。
    expect(view).toMatch(/view\.PublicState/);
  });
});
