import { promises as fs } from "node:fs";
import path from "node:path";
import { getRedis } from "@/lib/cache/redis";
import { isInfrastructureConnectionError } from "@/lib/game/errors";
import {
  roomPresenceSummarySchema,
  type RoomPresenceSummary,
} from "@/lib/validation/schemas";

type RoomPresenceRole = "host" | "spectator";

type PresenceEntry = {
  role: RoomPresenceRole;
  latencyMs: number | null;
  updatedAt: number;
};

type PresenceStore = Record<string, Record<string, PresenceEntry>>;

const PRESENCE_TTL_MS = 30_000;
const PRESENCE_REDIS_TTL_SECONDS = 60;
const presenceStorePath = path.join(
  process.cwd(),
  "data",
  "runtime",
  "room-presence.json",
);

async function readPresenceStore(): Promise<PresenceStore> {
  try {
    const raw = await fs.readFile(presenceStorePath, "utf8");
    return JSON.parse(raw) as PresenceStore;
  } catch {
    return {};
  }
}

async function writePresenceStore(store: PresenceStore) {
  await fs.mkdir(path.dirname(presenceStorePath), { recursive: true });
  await fs.writeFile(presenceStorePath, `${JSON.stringify(store, null, 2)}\n`, "utf8");
}

function getRoomPresenceRedisKey(roomCode: string) {
  return `presence:room:${roomCode}`;
}

function cleanupStore(store: PresenceStore, now: number) {
  for (const [roomCode, roomEntries] of Object.entries(store)) {
    for (const [sessionId, entry] of Object.entries(roomEntries)) {
      if (now - entry.updatedAt > PRESENCE_TTL_MS) {
        delete roomEntries[sessionId];
      }
    }

    if (Object.keys(roomEntries).length === 0) {
      delete store[roomCode];
    }
  }

  return store;
}

function buildSummary(roomCode: string, store: PresenceStore, now: number) {
  const activeEntries = Object.values(store[roomCode] ?? {});
  const spectatorEntries = activeEntries.filter((entry) => entry.role === "spectator");
  const validLatencies = spectatorEntries
    .map((entry) => entry.latencyMs)
    .filter((value): value is number => value != null);

  return roomPresenceSummarySchema.parse({
    roomCode,
    spectatorCount: spectatorEntries.length,
    averageLatencyMs:
      validLatencies.length > 0
        ? Math.round(validLatencies.reduce((sum, value) => sum + value, 0) / validLatencies.length)
        : null,
    hostOnline: activeEntries.some((entry) => entry.role === "host"),
    updatedAt: new Date(now).toISOString(),
  });
}

function normalizeLatency(latencyMs?: number | null) {
  return latencyMs == null || !Number.isFinite(latencyMs)
    ? null
    : Math.max(0, Math.round(latencyMs));
}

async function heartbeatRoomPresenceFromFile(input: {
  roomCode: string;
  sessionId: string;
  role: RoomPresenceRole;
  latencyMs?: number | null;
}) {
  const now = Date.now();
  const store = cleanupStore(await readPresenceStore(), now);

  if (!store[input.roomCode]) {
    store[input.roomCode] = {};
  }

  store[input.roomCode][input.sessionId] = {
    role: input.role,
    latencyMs: normalizeLatency(input.latencyMs),
    updatedAt: now,
  };

  await writePresenceStore(store);
  return buildSummary(input.roomCode, store, now);
}

async function getRoomPresenceSummaryFromFile(roomCode: string) {
  const now = Date.now();
  const store = cleanupStore(await readPresenceStore(), now);
  await writePresenceStore(store);
  return buildSummary(roomCode, store, now);
}

export async function heartbeatRoomPresence(input: {
  roomCode: string;
  sessionId: string;
  role: RoomPresenceRole;
  latencyMs?: number | null;
}): Promise<RoomPresenceSummary> {
  try {
    const redis = await getRedis();
    const now = Date.now();
    const key = getRoomPresenceRedisKey(input.roomCode);

    await redis.hSet(
      key,
      input.sessionId,
      JSON.stringify({
        role: input.role,
        latencyMs: normalizeLatency(input.latencyMs),
        updatedAt: now,
      } satisfies PresenceEntry),
    );
    await redis.expire(key, PRESENCE_REDIS_TTL_SECONDS);

    return getRoomPresenceSummary(input.roomCode);
  } catch (error) {
    if (
      !isInfrastructureConnectionError(error) &&
      !(error instanceof Error && /DATABASE_URL|REDIS_URL/.test(error.message))
    ) {
      throw error;
    }

    return heartbeatRoomPresenceFromFile(input);
  }
}

export async function getRoomPresenceSummary(roomCode: string) {
  try {
    const redis = await getRedis();
    const key = getRoomPresenceRedisKey(roomCode);
    const now = Date.now();
    const rawEntries = await redis.hGetAll(key);
    const store: PresenceStore = { [roomCode]: {} };
    const staleSessionIds: string[] = [];

    for (const [sessionId, rawEntry] of Object.entries(rawEntries)) {
      try {
        const entry = JSON.parse(rawEntry) as PresenceEntry;
        if (now - entry.updatedAt > PRESENCE_TTL_MS) {
          staleSessionIds.push(sessionId);
          continue;
        }

        store[roomCode][sessionId] = entry;
      } catch {
        staleSessionIds.push(sessionId);
      }
    }

    if (staleSessionIds.length > 0) {
      await redis.hDel(key, staleSessionIds);
    }

    return buildSummary(roomCode, store, now);
  } catch (error) {
    if (
      !isInfrastructureConnectionError(error) &&
      !(error instanceof Error && /DATABASE_URL|REDIS_URL/.test(error.message))
    ) {
      throw error;
    }

    return getRoomPresenceSummaryFromFile(roomCode);
  }
}
