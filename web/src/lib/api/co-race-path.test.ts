/**
 * co-race-path 节点头像回退的单元测试。
 *
 * 背景（实测 2026-11-08）：内容包 co-race-path_graph.json 的节点只发布
 * characterId + name，没有图片字段，导致 publicState 的 imagePath 全是空串，
 * 起点/终点卡渲染成空白圆圈。前端按 characterId 回退到受控头像目录。
 */
import { describe, expect, it } from "vitest";
import { normalizePathRound } from "@/lib/api/co-race-path";

function makeView(publicState: Record<string, unknown>) {
  return {
    viewerRole: "player",
    round: {
      roundId: "rnd_test",
      modeId: "co-race-path",
      phase: "collecting",
      phaseVersion: 3,
      deadlineAt: null,
      paused: false,
      rulesVersion: "2.0.0-draft.1",
      datasetVersion: "ds-test",
      contentPackVersion: "pack-test",
    },
    publicState,
    serverTime: "2026-11-08T00:00:00Z",
    roomVersion: 1,
    privateState: {},
    controlGeneration: 0,
    submissionRevision: 0,
  } as unknown as Parameters<typeof normalizePathRound>[0];
}

const BASE = {
  modeId: "co-race-path",
  rulesVersion: "2.0.0-draft.1",
  phase: "collecting",
  config: { maxEdges: 6, questionSeconds: 180, openBook: false },
  shortestEdges: 4,
  subjects: [],
};

describe("normalizePathRound 节点头像", () => {
  it("服务端 imagePath 为空时按 characterId 回退到受控头像", () => {
    const round = normalizePathRound(
      makeView({
        ...BASE,
        start: { characterId: "1016", name: "成田白仁", imagePath: "" },
        goal: { characterId: "1034", name: "稻荷一", imagePath: "" },
        nodes: [{ characterId: "1001", name: "特别周", imagePath: "" }],
      }),
    );
    expect(round?.start?.imagePath).toBe("/assets/avatars/1016.webp");
    expect(round?.goal?.imagePath).toBe("/assets/avatars/1034.webp");
    expect(round?.nodes[0]?.imagePath).toBe("/assets/avatars/1001.webp");
  });

  it("服务端给了路径时优先用服务端的", () => {
    const round = normalizePathRound(
      makeView({
        ...BASE,
        start: { characterId: "1016", name: "A", imagePath: "/assets/custom/a.webp" },
        goal: { characterId: "1034", name: "B", imagePath: "/assets/custom/b.webp" },
        nodes: [],
      }),
    );
    expect(round?.start?.imagePath).toBe("/assets/custom/a.webp");
  });

  it("characterId 含非法字符时不拼 URL（防注入）", () => {
    const round = normalizePathRound(
      makeView({
        ...BASE,
        start: { characterId: "../../etc/passwd", name: "X", imagePath: "" },
        goal: { characterId: "1034", name: "B", imagePath: "" },
        nodes: [],
      }),
    );
    expect(round?.start?.imagePath).toBeNull();
  });
});
