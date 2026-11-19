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
