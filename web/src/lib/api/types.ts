// API aliases come from schema.d.ts, generated from api/openapi.json
import type { components } from "./schema";

export type { components, operations, paths } from "./schema";

type Schemas = components["schemas"];

/** RFC 9457 Problem Details */
export type Problem = Schemas["Problem"];
/** Problem Details 的字段级错误 */
export type ProblemFieldError = NonNullable<Problem["fieldErrors"]>[number];

export type Session = Schemas["Session"];
export type SessionCreate = Schemas["SessionCreate"];
export type SessionUpdate = Schemas["SessionUpdate"];

export type PublicConfig = Schemas["PublicConfig"];
export type ModeId = Schemas["ModeId"];
export type Mode = Schemas["Mode"];
export type ModeList = Schemas["ModeList"];
export type ModeAvailability = Mode["availability"];
export type InformationStyle = Mode["informationStyle"];

/** Preserve the discriminant for modes with their own config schema. */
export function packModeConfig(modeId: Exclude<ModeId, "classic">, rulesVersion: string, informationStyle: InformationStyle, contentPackId = ""): Exclude<Schemas["ModeConfig"], { modeId: "classic" }> {
  const common = { rulesVersion, contentPackId, informationStyle };
  if (modeId === "race-rebuild") return { modeId, ...common };
  if (modeId === "co-race-path") return { modeId, ...common };
  return { modeId, ...common };
}

export type Character = Schemas["Character"];
export type CharacterList = Schemas["CharacterList"];
export type CharacterEntityKind = Character["entityKind"];
export type Aptitudes = Schemas["Aptitudes"];
export type AptitudeGrade = Aptitudes["turf"];

export type Health = Schemas["Health"];
export type InviteResolution = Schemas["InviteResolution"];
export type InviteResolutionCreate = Schemas["InviteResolutionCreate"];
export type RoomSnapshot = Schemas["RoomSnapshot"];
export type Membership = Schemas["Membership"];
export type RoundSummary = Schemas["RoundSummary"];

/** Known contract identifiers, used to reject unsupported modes safely. */
export const KNOWN_MODE_IDS = [
  "classic",
  "aptitude-grid",
  "draft",
  "race-rebuild",
  "co-race-path",
  "sort",
  "memory",
  "bluff",
  "clue-puzzle",
  "strategy-race",
  "rapport",
  "creative",
  "trade",
  "pedigree",
  "audio",
] as const satisfies readonly ModeId[];

const KNOWN_MODE_ID_SET: ReadonlySet<string> = new Set(KNOWN_MODE_IDS);

/**
 * 服务端可能新增未知枚举。不自行按未知模式结算，
 * 而是返回 null 让调用方显示「需要刷新/暂不支持」。
 */
export function toKnownModeId(value: string): ModeId | null {
  return KNOWN_MODE_ID_SET.has(value) ? (value as ModeId) : null;
}

/** 模式的中文显示名。契约只保证机器标识，显示名由前端提供。 */
export const MODE_DISPLAY_NAMES: Record<ModeId, string> = {
  classic: "经典弗一把",
  "aptitude-grid": "适性交叉占格",
  draft: "公开编队",
  "race-rebuild": "历史赛况复原",
  "co-race-path": "同场之路",
  sort: "排序",
  memory: "记忆配对",
  bluff: "假档案",
  "clue-puzzle": "团队情报拼图",
  "strategy-race": "回合制策略赛道",
  rapport: "默契问答",
  creative: "创意企划",
  trade: "档案交易",
  pedigree: "血统",
  audio: "听音辨马",
};

/** Modes with dedicated UI routes; server admission is checked separately. */
export const PLAYABLE_MODE_IDS = [
  "classic",
  "sort",
  "aptitude-grid",
  "draft",
  "race-rebuild",
  "co-race-path",
  "memory",
  "pedigree",
  "audio",
  "bluff",
  "clue-puzzle",
  "strategy-race",
  "rapport",
  "creative",
  "trade",
] as const satisfies readonly ModeId[];

export type PlayableModeId = (typeof PLAYABLE_MODE_IDS)[number];



/** 未知模式的安全显示名（不臆造规则） */
export function modeDisplayName(modeId: string): string {
  const known = toKnownModeId(modeId);
  return known ? MODE_DISPLAY_NAMES[known] : "未知玩法";
}
