import { z } from "zod";

export const guessStatusSchema = z.enum(["correct", "near", "wrong"]);

export const assetManifestEntrySchema = z.object({
  asset_id: z.string(),
  character_id: z.string(),
  source_site: z.string(),
  source_page: z.string(),
  source_url: z.string().url(),
  local_path: z.string(),
  downloaded_at: z.string().nullable(),
  usage_scope: z.string(),
  copyright_note: z.string(),
  cache_status: z.enum(["missing", "cached", "stale", "failed"]),
  last_checked_at: z.string().nullable(),
});

export const questionBankEntrySchema = z.object({
  id: z.string().min(1),
  name_zh: z.string().min(1),
  name_tw: z.string().nullable(),
  name_jp: z.string().min(1),
  name_en: z.string().nullable(),
  aliases: z.array(z.string().min(1)).min(1),
  star: z.number().int(),
  surface_group: z.array(z.string().min(1)).min(1),
  distance_group: z.array(z.string().min(1)).min(1),
  running_style_group: z.array(z.string().min(1)).min(1),
  sex_type: z.string().min(1),
  g1_bracket: z.string().min(1),
  g23_bracket: z.string().min(1),
  g1_has_jpn: z.boolean().default(false),
  g23_has_jpn: z.boolean().default(false),
  school_grade: z.string().min(1),
  dormitory: z.string().min(1),
  image_url: z.string().url().nullable(),
  image_local_path: z.string().min(1),
  asset_id: z.string().min(1),
  source_priority: z.array(z.string().min(1)).default([]),
});

export const searchIndexEntrySchema = z.object({
  id: z.string(),
  name_zh: z.string(),
  name_tw: z.string().nullable(),
  name_jp: z.string(),
  name_en: z.string().nullable(),
  aliases: z.array(z.string()),
  image_local_path: z.string(),
  image_url: z.string().url().nullable().default(null),
});

export const guessCellSchema = z.object({
  value: z.string(),
  status: guessStatusSchema,
});

export const guessRowSchema = z.object({
  characterId: z.string(),
  displayName: z.string(),
  avatarUrl: z.string(),
  avatarFallbackUrl: z.string().url().nullable().default(null),
  cells: z.object({
    star: guessCellSchema,
    surface: guessCellSchema,
    distance: guessCellSchema,
    style: guessCellSchema,
    sex: guessCellSchema,
    g1: guessCellSchema,
    g23: guessCellSchema,
    grade: guessCellSchema,
    dormitory: guessCellSchema,
  }),
});

export const publicConfigSchema = z.object({
  siteName: z.string(),
  assetBaseUrl: z.string().url(),
  turnstileEnabled: z.boolean(),
  turnstileSiteKey: z.string(),
  featureFlags: z.object({
    enableMultiplayer: z.boolean(),
    enableFriendBattle: z.boolean(),
    enableShare: z.boolean(),
  }),
  shareBaseUrl: z.string().url(),
  maxGuesses: z.number().int(),
  assetProxyEnabled: z.boolean(),
  realtime: z.object({
    wsUrl: z.string(),
    pollIntervalMs: z.number().int(),
    heartbeatIntervalMs: z.number().int(),
  }),
  multiplayer: z.object({
    matchmakingMaxWaitSeconds: z.number().int(),
    acceptConfirmSeconds: z.number().int(),
    idleTimeoutSeconds: z.number().int(),
    pauseDurationSeconds: z.number().int(),
    rematchConfirmSeconds: z.number().int(),
  }),
});

export const roomPresenceHeartbeatRequestSchema = z.object({
  roomCode: z.string().min(1),
  role: z.enum(["host", "spectator"]),
  sessionId: z.string().min(1),
  latencyMs: z.number().int().nonnegative().nullable().optional(),
});

export const roomPresenceSummarySchema = z.object({
  roomCode: z.string(),
  spectatorCount: z.number().int().nonnegative(),
  averageLatencyMs: z.number().int().nonnegative().nullable(),
  hostOnline: z.boolean(),
  updatedAt: z.string().datetime(),
});

export const createGameRequestSchema = z.object({
  turnstileToken: z.string().optional(),
});

export const guessRequestSchema = z.object({
  gameId: z.string().min(1),
  characterId: z.string().min(1),
  turnstileToken: z.string().optional(),
});

export const endGameRequestSchema = z.object({
  gameId: z.string().min(1),
});

export const gameStatusSchema = z.enum(["playing", "won", "lost", "ended"]);

export const gameStateSchema = z.object({
  gameId: z.string(),
  roomCode: z.string(),
  status: gameStatusSchema,
  remainingGuesses: z.number().int(),
  guessRows: z.array(guessRowSchema),
  startedAt: z.string().datetime().optional(),
  finishedAt: z.string().datetime().nullable().optional(),
  updatedAt: z.string().datetime().optional(),
  answerCharacterId: z.string().nullable().optional(),
  answerDisplayName: z.string().nullable().optional(),
  canEdit: z.boolean().optional(),
  viewerToken: z.string().nullable().optional(),
});

export const battleModeSchema = z.enum(["matchmaking", "friend"]);

export const battleRoomStatusSchema = z.enum([
  "waiting",
  "matched_pending_accept",
  "ready_confirm",
  "playing",
  "paused",
  "finished",
]);

export const battleSeatSchema = z.enum(["A", "B"]);

export const battlePlayerResultSchema = z.enum([
  "playing",
  "won",
  "lost",
  "surrendered",
  "ended",
]);

export const guessShadowRowSchema = z.object({
  attempt: z.number().int().positive(),
  statuses: z.object({
    star: guessStatusSchema,
    surface: guessStatusSchema,
    distance: guessStatusSchema,
    style: guessStatusSchema,
    sex: guessStatusSchema,
    g1: guessStatusSchema,
    g23: guessStatusSchema,
    grade: guessStatusSchema,
    dormitory: guessStatusSchema,
  }),
  submittedAt: z.string().datetime(),
});

export const battlePlayerPublicSchema = z.object({
  playerId: z.string(),
  seat: battleSeatSchema,
  displayName: z.string(),
  online: z.boolean(),
  latencyMs: z.number().int().nonnegative().nullable(),
  remainingGuesses: z.number().int().nonnegative(),
  result: battlePlayerResultSchema,
  acceptedStart: z.boolean(),
  requestedRematch: z.boolean(),
  guessRows: z.array(guessRowSchema),
  shadowRows: z.array(guessShadowRowSchema),
});

export const battlePauseRequestSchema = z
  .object({
    requesterSeat: battleSeatSchema,
    requestedAt: z.string().datetime(),
    expiresAt: z.string().datetime(),
  })
  .nullable();

export const battleRoomStateSchema = z.object({
  roomCode: z.string(),
  inviteCode: z.string(),
  viewerToken: z.string(),
  mode: battleModeSchema,
  status: battleRoomStatusSchema,
  roundNumber: z.number().int().positive(),
  viewerSeat: battleSeatSchema,
  canEdit: z.boolean(),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  startedAt: z.string().datetime().nullable(),
  finishedAt: z.string().datetime().nullable(),
  acceptanceDeadline: z.string().datetime().nullable(),
  pausedUntil: z.string().datetime().nullable(),
  pauseRequest: battlePauseRequestSchema,
  pauseUsed: z.boolean(),
  winnerSeat: battleSeatSchema.nullable(),
  loserSeat: battleSeatSchema.nullable(),
  answerCharacterId: z.string().nullable(),
  answerDisplayName: z.string().nullable(),
  self: battlePlayerPublicSchema,
  opponent: battlePlayerPublicSchema,
});

export const matchmakingJoinRequestSchema = z.object({
  turnstileToken: z.string().optional(),
  latencyMs: z.number().int().nonnegative().nullable().optional(),
});

export const matchmakingStatusSchema = z.object({
  ticketId: z.string(),
  viewerToken: z.string(),
  status: z.enum([
    "queueing",
    "matched_pending_accept",
    "ready_confirm",
    "playing",
    "paused",
    "finished",
    "cancelled",
    "expired",
  ]),
  queueSize: z.number().int().nonnegative(),
  activeRooms: z.number().int().nonnegative(),
  waitSeconds: z.number().int().nonnegative(),
  roomCode: z.string().nullable(),
  acceptanceDeadline: z.string().datetime().nullable(),
});

export const matchmakingConfirmRequestSchema = z.object({
  ticketId: z.string().min(1),
});

export const matchmakingCancelRequestSchema = z.object({
  ticketId: z.string().min(1),
});

export const matchmakingStatsSchema = z.object({
  queueSize: z.number().int().nonnegative(),
  activeRooms: z.number().int().nonnegative(),
});

export const createFriendRoomRequestSchema = z.object({
  turnstileToken: z.string().optional(),
});

export const joinFriendRoomRequestSchema = z.object({
  inviteCode: z.string().min(1),
  turnstileToken: z.string().optional(),
});

export const roomActionRequestSchema = z.discriminatedUnion("type", [
  z.object({ type: z.literal("confirm-start") }),
  z.object({ type: z.literal("request-pause") }),
  z.object({ type: z.literal("respond-pause"), accept: z.boolean() }),
  z.object({ type: z.literal("resume-now") }),
  z.object({ type: z.literal("surrender") }),
  z.object({ type: z.literal("request-rematch") }),
  z.object({ type: z.literal("heartbeat"), latencyMs: z.number().int().nonnegative().nullable().optional() }),
  z.object({ type: z.literal("guess"), characterId: z.string().min(1) }),
]);

export type AssetManifestEntry = z.infer<typeof assetManifestEntrySchema>;
export type GuessRow = z.infer<typeof guessRowSchema>;
export type GameState = z.infer<typeof gameStateSchema>;
export type PublicConfigPayload = z.infer<typeof publicConfigSchema>;
export type QuestionBankEntry = z.infer<typeof questionBankEntrySchema>;
export type SearchIndexEntry = z.infer<typeof searchIndexEntrySchema>;
export type RoomPresenceSummary = z.infer<typeof roomPresenceSummarySchema>;
export type BattleMode = z.infer<typeof battleModeSchema>;
export type BattleRoomState = z.infer<typeof battleRoomStateSchema>;
export type MatchmakingStatus = z.infer<typeof matchmakingStatusSchema>;
