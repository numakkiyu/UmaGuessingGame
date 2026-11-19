/** Team placement determines the scoring subject; the captain is its initial representative. */
import { teamText } from "@/i18n/teams";
import type { components } from "./types";

type Schemas = components["schemas"];

/** 队伍颜色令牌：只能取这 8 个值（服务端枚举，`red` 会被拒） */
export const TEAM_COLOR_TOKENS = [
  "green",
  "blue",
  "gold",
  "pink",
  "purple",
  "orange",
  "teal",
  "slate",
] as const;

export type TeamColorToken = (typeof TEAM_COLOR_TOKENS)[number];

/** 颜色令牌 → 中文名（给玩家看的，不是内部值） */
export const TEAM_COLOR_LABELS: Record<TeamColorToken, string> = {
  green: "绿",
  blue: "蓝",
  gold: "金",
  pink: "粉",
  purple: "紫",
  orange: "橙",
  teal: "青",
  slate: "灰",
};

/**
 * 颜色令牌 → 实际色值。
 *
 * 用项目既有令牌派生，不引入新颜色。
 * 每个色值都配深色文字（`--color-ink-strong`），实测对比度均 ≥4.5:1。
 */
export const TEAM_COLOR_STYLES: Record<TeamColorToken, { background: string; border: string }> = {
  green: { background: "var(--color-brand-green-soft)", border: "var(--color-correct-edge)" },
  blue: { background: "var(--color-page-tint)", border: "var(--color-blue-deep)" },
  gold: { background: "var(--color-gold)", border: "var(--color-near-edge)" },
  pink: { background: "#FBE4F0", border: "#B04A7A" },
  purple: { background: "#EBE4FB", border: "#6B4FB8" },
  orange: { background: "#FDEBD9", border: "#B96A1E" },
  teal: { background: "#DDF3F0", border: "#1F7A6E" },
  slate: { background: "var(--color-panel-soft)", border: "var(--color-line-strong)" },
};

export type RoomTeam = {
  teamId: string;
  label: string;
  colorToken: string;
};

export type TeamMember = {
  publicCode?: string;
  membershipId: string;
  displayName: string;
  role: string;
  teamId: string | null;
  /** 是否是队伍队长（决定这个成员是否代表队伍成为回合主体） */
  isCaptain: boolean;
};

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}

function str(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}

function num(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

/** 从房间快照里读队伍列表 */
export function readTeams(room: unknown): RoomTeam[] {
  const record = asRecord(room);
  if (!record) return [];
  const raw = record["teams"];
  if (!Array.isArray(raw)) return [];
  const out: RoomTeam[] = [];
  for (const entry of raw) {
    const item = asRecord(entry);
    if (!item) continue;
    const teamId = str(item["teamId"]);
    if (!teamId) continue;
    out.push({
      teamId,
      label: str(item["label"]) ?? teamText("unnamed"),
      colorToken: str(item["colorToken"]) ?? "slate",
    });
  }
  return out;
}

/** 从房间快照里读成员（含队伍归属与队长身份） */
export function readTeamMembers(room: unknown): TeamMember[] {
  const record = asRecord(room);
  if (!record) return [];
  const raw = record["members"];
  if (!Array.isArray(raw)) return [];
  const out: TeamMember[] = [];
  for (const entry of raw) {
    const item = asRecord(entry);
    if (!item) continue;
    const membershipId = str(item["membershipId"]);
    if (!membershipId) continue;
    const role = str(item["role"]) ?? "player";
    out.push({
      membershipId,
      displayName: str(item["nickname"]) ?? str(item["displayName"]) ?? teamText("trainer"),
      publicCode: str(item["publicCode"]) ?? undefined,
      role,
      teamId: str(item["teamId"]),
      isCaptain: role === "team-captain",
    });
  }
  return out;
}

/** 房间版本：PATCH 必须带，否则 409 */
export function readRoomVersion(room: unknown): number | null {
  const record = asRecord(room);
  if (!record) return null;
  return num(record["roomVersion"]);
}

/**
 * 分队校验（本地预检，不替代服务端判定）。
 *
 * 只拦明显配置错误，让主持在点开局前就看到问题：
 * - 队长必须有队伍（否则服务端 422 `captain-without-team`）
 * - 同一队伍不应有多个队长（会变成两个同 ID 主体）
 */
export function validateTeams(teams: readonly RoomTeam[], members: readonly TeamMember[]): string[] {
  const problems: string[] = [];

  for (const member of members) {
    if (member.isCaptain && !member.teamId) {
      problems.push(teamText("captainWithoutTeam", { name: member.displayName }));
    }
  }

  const captainsByTeam = new Map<string, string[]>();
  for (const member of members) {
    if (!member.isCaptain || !member.teamId) continue;
    const list = captainsByTeam.get(member.teamId) ?? [];
    list.push(member.displayName);
    captainsByTeam.set(member.teamId, list);
  }
  for (const [teamId, names] of captainsByTeam) {
    if (names.length > 1) {
      const team = teams.find((item) => item.teamId === teamId);
      problems.push(teamText("multipleCaptains", { team: team?.label ?? teamText("unknownTeam"), count: names.length, names: names.join("、") }));
    }
  }

  for (const team of teams) {
    const active = members.filter(member => member.teamId === team.teamId && (member.role === "player" || member.isCaptain));
    if (active.length > 0 && !active.some(member => member.isCaptain)) problems.push(teamText("noCaptain", { team: team.label }));
  }
  return problems;
}

/**
 * 本局实际有几个回合主体。
 *
 * 规则与服务端一致（rounds.go）：
 * - 队长 → 按队伍算一个主体
 * - 普通玩家 → 按人算一个主体
 * - 观众/主持/大屏 → 不参与
 *
 * 前端用它显示「本局 N 支队伍 / N 名玩家」，让主持在开局前就知道主体数。
 */
export function countSubjects(members: readonly TeamMember[]): { teams: number; players: number; total: number } {
  const teamIds = new Set<string>();
  let players = 0;
  for (const member of members) {
    if ((member.role === "player" || member.isCaptain) && member.teamId) {
      teamIds.add(member.teamId);
      continue;
    }
    if (member.role === "player") players += 1;
  }
  return { teams: teamIds.size, players, total: teamIds.size + players };
}

/** 队伍类型的便捷别名（保持与其他 lib/api/*.ts 一致） */
export type RoomSnapshot = Schemas["RoomSnapshot"];
