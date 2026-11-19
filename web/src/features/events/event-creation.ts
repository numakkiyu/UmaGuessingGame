import type { Api } from "@/lib/api/contract";
import { createIdempotencyKey } from "@/lib/api/client";
import { packModeConfig, toKnownModeId, type components } from "@/lib/api/types";

type RoomCreate = components["schemas"]["RoomCreate"];
type EventCreate = Omit<components["schemas"]["EventCreate"], "roomId">;
export type EventCreation = {
  createdAt: number;
  sessionId: string;
  room: RoomCreate;
  event: EventCreate;
  roomKey: string;
  eventKey: string;
  roomId: string | null;
};
export const EVENT_CREATION_STORAGE = "umaguess:pending-event";
// Receipts expire after 24 hours. Stop confirmation before that boundary;
// replaying an expired receipt could otherwise create a second room.
export const EVENT_CREATION_MAX_AGE = 23 * 60 * 60 * 1000;
export type StoredCreation = { kind: "empty" | "unavailable" | "invalid" } | { kind: "ready" | "expired"; value: EventCreation };

type StorageReader = Pick<Storage, "getItem">;
function object(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}
const text = (value: unknown, max: number): value is string => typeof value === "string" && value.length > 0 && [...value].length <= max;
const integer = (value: unknown, min: number, max: number): value is number => typeof value === "number" && Number.isSafeInteger(value) && value >= min && value <= max;

function parseConfig(value: unknown): RoomCreate["modeConfig"] | null {
  const cfg = object(value);
  const modeId = toKnownModeId(typeof cfg?.modeId === "string" ? cfg.modeId : "");
  if (!cfg || !modeId) return null;
  if (modeId === "classic") {
    if (cfg.variant !== "public" || cfg.winPolicy !== "window-attempts" || !integer(cfg.maxWrongGuesses, 4, 12) || !integer(cfg.durationSeconds, 1, 3600) || typeof cfg.shadowEnabled !== "boolean") return null;
    return { modeId, variant: cfg.variant, winPolicy: cfg.winPolicy, maxWrongGuesses: cfg.maxWrongGuesses, durationSeconds: cfg.durationSeconds, shadowEnabled: cfg.shadowEnabled };
  }
  if (!text(cfg.rulesVersion, 64) || typeof cfg.contentPackId !== "string" || cfg.contentPackId.length > 160 || !["open", "closed"].includes(String(cfg.informationStyle))) return null;
  return packModeConfig(modeId, cfg.rulesVersion, cfg.informationStyle as "open" | "closed", cfg.contentPackId);
}

export function readEventCreation(storage: StorageReader, now = Date.now()): StoredCreation {
  let raw: string | null;
  try { raw = storage.getItem(EVENT_CREATION_STORAGE); } catch { return { kind: "unavailable" }; }
  if (raw === null) return { kind: "empty" };
  if (raw.length > 8192) return { kind: "invalid" };
  try {
    const value = object(JSON.parse(raw)), room = object(value?.room), event = object(value?.event);
    const config = parseConfig(room?.modeConfig);
    if (!value || !room || !event || !config || !integer(value.createdAt, 1, now + 60_000) || !text(value.sessionId, 128) || !/^[0-9a-f]{48}$/.test(String(value.roomKey)) || !/^[0-9a-f]{48}$/.test(String(value.eventKey)) || !(value.roomId === null || (typeof value.roomId === "string" && /^rom_[0-9a-f]{32}$/.test(value.roomId))) || !text(room.name, 48) || room.kind !== "event" || !integer(room.maxPlayers, 1, 200) || !integer(room.maxSpectators, 0, 300) || room.visibility !== "invite" || room.allowSpectators !== (room.maxSpectators > 0) || !text(event.name, 48) || !["shared-rank", "announced-tiebreak"].includes(String(event.tiePolicy))) return { kind: "invalid" };
    const pending: EventCreation = {
      createdAt: value.createdAt, sessionId: value.sessionId,
      room: {name: room.name, kind: "event", modeConfig: config, maxPlayers: room.maxPlayers, maxSpectators: room.maxSpectators, visibility: "invite", allowSpectators: room.allowSpectators as boolean},
      event: { name: event.name, tiePolicy: event.tiePolicy as EventCreate["tiePolicy"] },
      roomKey: value.roomKey as string, eventKey: value.eventKey as string, roomId: value.roomId as string | null,
    };
    return {kind: now - pending.createdAt >= EVENT_CREATION_MAX_AGE ? "expired" : "ready", value: pending};
  } catch { return { kind: "invalid" }; }
}

export function newEventCreation(sessionId: string, room: RoomCreate, event: EventCreate, now = Date.now()): EventCreation {
  return {createdAt: now, sessionId, room, event, roomKey: createIdempotencyKey(), eventKey: createIdempotencyKey(), roomId: null};
}

export function eventCreationExpired(pending: EventCreation, now = Date.now()): boolean {
  return now - pending.createdAt >= EVENT_CREATION_MAX_AGE;
}

/** No retries or identity changes here: each invocation confirms one attempt. */
export async function continueEventCreation(api: Pick<Api, "createRoom" | "createEvent">, pending: EventCreation, sessionId: string, onProgress: (next: EventCreation) => void, now = Date.now()): Promise<string> {
  if (pending.sessionId !== sessionId || eventCreationExpired(pending, now)) throw new Error("Event attempt cannot be confirmed with this identity or time");
  let current = pending;
  if (!current.roomId) {
    const response = await api.createRoom(current.room, current.roomKey);
    const roomId = response.data?.room?.roomId;
    if (!roomId) throw new Error("Missing created room");
    current = {...current, roomId};
    onProgress(current);
  }
  const response = await api.createEvent({...current.event, roomId: current.roomId!}, current.eventKey);
  if (!response.data?.eventId) throw new Error("Missing created event");
  return response.data.eventId;
}
