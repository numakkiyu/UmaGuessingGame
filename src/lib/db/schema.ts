import {
  integer,
  jsonb,
  pgTable,
  text,
  timestamp,
  varchar,
} from "drizzle-orm/pg-core";

export const questionBankEntries = pgTable("question_bank_entries", {
  characterId: varchar("character_id", { length: 128 }).primaryKey(),
  payload: jsonb("payload").notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
});

export const games = pgTable("games", {
  id: varchar("id", { length: 128 }).primaryKey(),
  answerCharacterId: varchar("answer_character_id", { length: 128 }).notNull(),
  status: varchar("status", { length: 32 }).notNull(),
  remainingGuesses: integer("remaining_guesses").notNull(),
  maxGuesses: integer("max_guesses").notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
  updatedAt: timestamp("updated_at", { withTimezone: true }).defaultNow().notNull(),
});

export const gameGuesses = pgTable("game_guesses", {
  id: integer("id").primaryKey().generatedAlwaysAsIdentity(),
  gameId: varchar("game_id", { length: 128 }).notNull(),
  guessIndex: integer("guess_index").notNull(),
  characterId: varchar("character_id", { length: 128 }).notNull(),
  guessRow: jsonb("guess_row").notNull(),
  createdAt: timestamp("created_at", { withTimezone: true }).defaultNow().notNull(),
});

export const assetRecords = pgTable("asset_records", {
  assetId: varchar("asset_id", { length: 128 }).primaryKey(),
  characterId: varchar("character_id", { length: 128 }).notNull(),
  sourceSite: varchar("source_site", { length: 64 }).notNull(),
  sourceUrl: text("source_url").notNull(),
  cacheKey: varchar("cache_key", { length: 256 }).notNull(),
  localPath: text("local_path").notNull(),
  status: varchar("status", { length: 32 }).notNull(),
  etag: varchar("etag", { length: 256 }),
  lastFetchedAt: timestamp("last_fetched_at", { withTimezone: true }),
  lastServedAt: timestamp("last_served_at", { withTimezone: true }),
});
