import crypto from "node:crypto";
import { promises as fs } from "node:fs";
import path from "node:path";
import { buildGuessRow } from "@/lib/game/compare";
import { buildBattlePlayerCookieValue } from "@/lib/battle/identity";
import { loadQuestionBankReady } from "@/lib/question-bank";
import { publishRoomEvent, publishTicketEvent } from "@/lib/battle/realtime";
import type { GuessRow, QuestionBankEntry } from "@/lib/validation/schemas";
import {
  battleRoomStateSchema,
  matchmakingStatsSchema,
  matchmakingStatusSchema,
  type BattleMode,
  type BattleRoomState,
} from "@/lib/validation/schemas";
import { getServerConfig } from "@/config/server";

type BattleSeat = "A" | "B";
type BattleRoomStatus =
  | "waiting"
  | "matched_pending_accept"
  | "ready_confirm"
  | "playing"
  | "paused"
  | "finished";
type BattlePlayerResult = "playing" | "won" | "lost" | "surrendered" | "ended";

type GuessShadowRow = {
  attempt: number;
  statuses: {
    star: GuessRow["cells"]["star"]["status"];
    surface: GuessRow["cells"]["surface"]["status"];
    distance: GuessRow["cells"]["distance"]["status"];
    style: GuessRow["cells"]["style"]["status"];
    sex: GuessRow["cells"]["sex"]["status"];
    g1: GuessRow["cells"]["g1"]["status"];
    g23: GuessRow["cells"]["g23"]["status"];
    grade: GuessRow["cells"]["grade"]["status"];
    dormitory: GuessRow["cells"]["dormitory"]["status"];
  };
  submittedAt: string;
};

type BattlePlayerRecord = {
  playerId: string;
  seat: BattleSeat;
  displayName: string;
  online: boolean;
  latencyMs: number | null;
  lastHeartbeatAt: string | null;
  remainingGuesses: number;
  result: BattlePlayerResult;
  acceptedStart: boolean;
  requestedRematch: boolean;
  guessRows: GuessRow[];
  shadowRows: GuessShadowRow[];
};

type PauseRequestRecord = {
  requesterSeat: BattleSeat;
  requestedAt: string;
  expiresAt: string;
} | null;

type BattleRoomRecord = {
  roomCode: string;
  inviteCode: string;
  mode: BattleMode;
  status: BattleRoomStatus;
  roundNumber: number;
  createdAt: string;
  updatedAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  acceptanceDeadline: string | null;
  pausedUntil: string | null;
  pauseRequest: PauseRequestRecord;
  pauseUsed: boolean;
  winnerSeat: BattleSeat | null;
  loserSeat: BattleSeat | null;
  answerCharacterId: string | null;
  players: BattlePlayerRecord[];
  ticketIds: string[];
};

type MatchmakingTicketStatus =
  | "queueing"
  | "matched_pending_accept"
  | "ready_confirm"
  | "playing"
  | "paused"
  | "finished"
  | "cancelled"
  | "expired";

type MatchmakingTicketRecord = {
  ticketId: string;
  playerId: string;
  latencyMs: number | null;
  latencyBucket: number;
  status: MatchmakingTicketStatus;
  roomCode: string | null;
  createdAt: string;
  updatedAt: string;
};

type BattleStore = {
  rooms: BattleRoomRecord[];
  tickets: MatchmakingTicketRecord[];
};

const STORE_PATH = path.join(process.cwd(), "data", "runtime", "battle-store.json");
const ROOM_CODE_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";
const ROOM_CODE_LENGTH = 8;
const ROOM_RETENTION_MS = 1000 * 60 * 60 * 12;
const TICKET_RETENTION_MS = 1000 * 60 * 30;
const ONLINE_WINDOW_MS = 20_000;

function createTimestamp(date = new Date()) {
  return date.toISOString();
}

function createCode(length = ROOM_CODE_LENGTH) {
  let value = "";
  for (let index = 0; index < length; index += 1) {
    value += ROOM_CODE_ALPHABET[crypto.randomInt(0, ROOM_CODE_ALPHABET.length)];
  }
  return value;
}

function buildSeatLabel(mode: BattleMode, seat: BattleSeat) {
  if (mode === "friend") {
    return seat === "A" ? "房主" : "来访玩家";
  }
  return seat === "A" ? "一号玩家" : "二号玩家";
}

function normalizeLatency(latencyMs?: number | null) {
  return latencyMs == null || !Number.isFinite(latencyMs)
    ? null
    : Math.max(0, Math.round(latencyMs));
}

function resolveLatencyBucket(latencyMs?: number | null) {
  const value = normalizeLatency(latencyMs);
  if (value == null) return 2;
  if (value <= 80) return 0;
  if (value <= 160) return 1;
  if (value <= 260) return 2;
  if (value <= 400) return 3;
  return 4;
}

function cloneCellStatuses(cells: GuessRow["cells"]) {
  return {
    star: cells.star.status,
    surface: cells.surface.status,
    distance: cells.distance.status,
    style: cells.style.status,
    sex: cells.sex.status,
    g1: cells.g1.status,
    g23: cells.g23.status,
    grade: cells.grade.status,
    dormitory: cells.dormitory.status,
  };
}

function toShadowRow(attempt: number, guessRow: GuessRow): GuessShadowRow {
  return {
    attempt,
    statuses: cloneCellStatuses(guessRow.cells),
    submittedAt: createTimestamp(),
  };
}

async function readStore(): Promise<BattleStore> {
  try {
    const raw = await fs.readFile(STORE_PATH, "utf8");
    const parsed = JSON.parse(raw) as BattleStore;
    return {
      rooms: parsed.rooms ?? [],
      tickets: parsed.tickets ?? [],
    };
  } catch {
    return { rooms: [], tickets: [] };
  }
}

async function writeStore(store: BattleStore) {
  await fs.mkdir(path.dirname(STORE_PATH), { recursive: true });
  await fs.writeFile(STORE_PATH, `${JSON.stringify(store, null, 2)}\n`, "utf8");
}

async function withStore<T>(handler: (store: BattleStore, now: Date) => Promise<T> | T) {
  const now = new Date();
  const store = cleanupStore(await readStore(), now);
  const result = await handler(store, now);
  await writeStore(store);
  return result;
}

function cleanupStore(store: BattleStore, now: Date) {
  const nowMs = now.getTime();
  const config = getServerConfig();

  for (const room of [...store.rooms]) {
    for (const player of room.players) {
      const lastHeartbeatMs = player.lastHeartbeatAt ? Date.parse(player.lastHeartbeatAt) : 0;
      player.online = lastHeartbeatMs > 0 && nowMs - lastHeartbeatMs <= ONLINE_WINDOW_MS;
    }

    if (
      room.status === "matched_pending_accept" &&
      room.acceptanceDeadline &&
      nowMs >= Date.parse(room.acceptanceDeadline)
    ) {
      for (const ticketId of room.ticketIds) {
        const ticket = store.tickets.find((item) => item.ticketId === ticketId);
        if (!ticket) continue;
        ticket.status = "queueing";
        ticket.roomCode = null;
        ticket.updatedAt = createTimestamp(now);
      }
      store.rooms = store.rooms.filter((item) => item.roomCode !== room.roomCode);
      continue;
    }

    if (room.status === "paused" && room.pausedUntil && nowMs >= Date.parse(room.pausedUntil)) {
      room.status = "playing";
      room.pausedUntil = null;
      room.pauseRequest = null;
      room.updatedAt = createTimestamp(now);
    }

    if (
      room.status === "playing" &&
      nowMs - Date.parse(room.updatedAt) >= config.matchBattleIdleTimeoutSeconds * 1000
    ) {
      room.status = "finished";
      room.finishedAt = createTimestamp(now);
      room.winnerSeat = null;
      room.loserSeat = null;
      for (const player of room.players) {
        if (player.result === "playing") {
          player.result = "ended";
        }
      }
      room.updatedAt = createTimestamp(now);
      syncTicketsFromRoom(store, room, "finished", now);
    }
  }

  store.rooms = store.rooms.filter((room) => {
    if (room.status !== "finished") {
      return true;
    }

    const finishedAtMs = room.finishedAt ? Date.parse(room.finishedAt) : Date.parse(room.updatedAt);
    return nowMs - finishedAtMs <= ROOM_RETENTION_MS;
  });

  store.tickets = store.tickets.filter((ticket) => {
    const updatedAtMs = Date.parse(ticket.updatedAt);
    if (ticket.status === "queueing") {
      return true;
    }

    return nowMs - updatedAtMs <= TICKET_RETENTION_MS;
  });

  maybeMatchTickets(store, now);
  return store;
}

function syncTicketsFromRoom(
  store: BattleStore,
  room: BattleRoomRecord,
  status: MatchmakingTicketStatus,
  now: Date,
) {
  for (const ticketId of room.ticketIds) {
    const ticket = store.tickets.find((item) => item.ticketId === ticketId);
    if (!ticket) continue;
    ticket.status = status;
    ticket.roomCode = room.roomCode;
    ticket.updatedAt = createTimestamp(now);
    publishTicketEvent(ticket.ticketId);
  }
}

function maybeMatchTickets(store: BattleStore, now: Date) {
  const queue = store.tickets
    .filter((ticket) => ticket.status === "queueing")
    .sort((left, right) => Date.parse(left.createdAt) - Date.parse(right.createdAt));

  const usedTicketIds = new Set<string>();

  for (const ticket of queue) {
    if (usedTicketIds.has(ticket.ticketId)) {
      continue;
    }

    const opponent = queue
      .filter(
        (candidate) =>
          !usedTicketIds.has(candidate.ticketId) &&
          candidate.ticketId !== ticket.ticketId &&
          candidate.playerId !== ticket.playerId,
      )
      .sort((left, right) => {
        const leftDistance = Math.abs(left.latencyBucket - ticket.latencyBucket);
        const rightDistance = Math.abs(right.latencyBucket - ticket.latencyBucket);
        if (leftDistance !== rightDistance) {
          return leftDistance - rightDistance;
        }
        return Date.parse(left.createdAt) - Date.parse(right.createdAt);
      })[0];

    if (!opponent) {
      continue;
    }

    const roomCode = createUniqueRoomCode(store);
    const acceptanceDeadline = new Date(
      now.getTime() + getServerConfig().matchAcceptConfirmSeconds * 1000,
    );
    const room: BattleRoomRecord = {
      roomCode,
      inviteCode: roomCode,
      mode: "matchmaking",
      status: "matched_pending_accept",
      roundNumber: 1,
      createdAt: createTimestamp(now),
      updatedAt: createTimestamp(now),
      startedAt: null,
      finishedAt: null,
      acceptanceDeadline: createTimestamp(acceptanceDeadline),
      pausedUntil: null,
      pauseRequest: null,
      pauseUsed: false,
      winnerSeat: null,
      loserSeat: null,
      answerCharacterId: null,
      ticketIds: [ticket.ticketId, opponent.ticketId],
      players: [
        createPlayerRecord(ticket.playerId, "matchmaking", "A"),
        createPlayerRecord(opponent.playerId, "matchmaking", "B"),
      ],
    };

    store.rooms.push(room);
    ticket.status = "matched_pending_accept";
    ticket.roomCode = roomCode;
    ticket.updatedAt = createTimestamp(now);
    publishTicketEvent(ticket.ticketId);
    opponent.status = "matched_pending_accept";
    opponent.roomCode = roomCode;
    opponent.updatedAt = createTimestamp(now);
    publishTicketEvent(opponent.ticketId);
    usedTicketIds.add(ticket.ticketId);
    usedTicketIds.add(opponent.ticketId);
    publishRoomEvent(roomCode);
  }
}

function createUniqueRoomCode(store: BattleStore) {
  for (let attempt = 0; attempt < 12; attempt += 1) {
    const candidate = createCode();
    if (!store.rooms.some((room) => room.roomCode === candidate)) {
      return candidate;
    }
  }

  throw new Error("现在房间太多了，稍后再试一次吧。");
}

function createTicketId() {
  return `ticket_${crypto.randomUUID().replace(/-/g, "")}`;
}

function createPlayerRecord(
  playerId: string,
  mode: BattleMode,
  seat: BattleSeat,
): BattlePlayerRecord {
  return {
    playerId,
    seat,
    displayName: buildSeatLabel(mode, seat),
    online: false,
    latencyMs: null,
    lastHeartbeatAt: null,
    remainingGuesses: getServerConfig().maxGuesses,
    result: "playing",
    acceptedStart: false,
    requestedRematch: false,
    guessRows: [],
    shadowRows: [],
  };
}

function findRoomOrThrow(store: BattleStore, roomCode: string) {
  const room = store.rooms.find((item) => item.roomCode === roomCode);
  if (!room) {
    throw new Error("这个房间已经不存在了。");
  }
  return room;
}

function findPlayerOrThrow(room: BattleRoomRecord, playerId: string) {
  const player = room.players.find((item) => item.playerId === playerId);
  if (!player) {
    throw new Error("你现在不在这个房间里。");
  }
  return player;
}

function getOpponent(room: BattleRoomRecord, seat: BattleSeat) {
  return room.players.find((item) => item.seat !== seat) ?? null;
}

async function getQuestionBankMap() {
  const bank = await loadQuestionBankReady();
  return new Map(bank.map((entry) => [entry.id, entry]));
}

function pickAnswer(questionBank: QuestionBankEntry[]) {
  return questionBank[Math.floor(Math.random() * questionBank.length)];
}

async function startRoomRound(room: BattleRoomRecord, now: Date) {
  const questionBank = await loadQuestionBankReady();
  if (questionBank.length === 0) {
    throw new Error("question_bank_ready.json 为空，当前无法开始对局。");
  }

  const answer = pickAnswer(questionBank);
  room.status = "playing";
  room.answerCharacterId = answer.id;
  room.startedAt = createTimestamp(now);
  room.finishedAt = null;
  room.pausedUntil = null;
  room.pauseRequest = null;
  room.pauseUsed = false;
  room.winnerSeat = null;
  room.loserSeat = null;
  room.acceptanceDeadline = null;
  room.updatedAt = createTimestamp(now);

  for (const player of room.players) {
    player.acceptedStart = false;
    player.requestedRematch = false;
    player.remainingGuesses = getServerConfig().maxGuesses;
    player.result = "playing";
    player.guessRows = [];
    player.shadowRows = [];
  }
}

function finishRoom(room: BattleRoomRecord, now: Date, options: {
  winnerSeat: BattleSeat | null;
  loserSeat: BattleSeat | null;
  loserResult?: BattlePlayerResult;
}) {
  room.status = "finished";
  room.finishedAt = createTimestamp(now);
  room.updatedAt = createTimestamp(now);
  room.pausedUntil = null;
  room.pauseRequest = null;
  room.winnerSeat = options.winnerSeat;
  room.loserSeat = options.loserSeat;

  for (const player of room.players) {
    if (player.seat === options.winnerSeat) {
      player.result = "won";
    } else if (player.seat === options.loserSeat) {
      player.result = options.loserResult ?? "lost";
    } else if (player.result === "playing") {
      player.result = "ended";
    }
  }
}

async function buildRoomState(room: BattleRoomRecord, playerId: string): Promise<BattleRoomState> {
  const self = findPlayerOrThrow(room, playerId);
  const opponent = getOpponent(room, self.seat);

  const bank = room.answerCharacterId ? await getQuestionBankMap() : null;
  const answerEntry = room.answerCharacterId ? bank?.get(room.answerCharacterId) ?? null : null;
  const opponentRecord =
    opponent ??
    {
      ...createPlayerRecord(
        "__pending__",
        room.mode,
        self.seat === "A" ? "B" : "A",
      ),
      displayName: room.mode === "friend" ? "等待朋友加入" : "等待对手出现",
    };

  return battleRoomStateSchema.parse({
    roomCode: room.roomCode,
    inviteCode: room.inviteCode,
    viewerToken: buildBattlePlayerCookieValue(playerId),
    mode: room.mode,
    status: room.status,
    roundNumber: room.roundNumber,
    viewerSeat: self.seat,
    canEdit: true,
    createdAt: room.createdAt,
    updatedAt: room.updatedAt,
    startedAt: room.startedAt,
    finishedAt: room.finishedAt,
    acceptanceDeadline: room.acceptanceDeadline,
    pausedUntil: room.pausedUntil,
    pauseRequest: room.pauseRequest,
    pauseUsed: room.pauseUsed,
    winnerSeat: room.winnerSeat,
    loserSeat: room.loserSeat,
    answerCharacterId: room.status === "finished" ? room.answerCharacterId : null,
    answerDisplayName: room.status === "finished" ? answerEntry?.name_zh ?? null : null,
    self: self,
    opponent: {
      ...opponentRecord,
      guessRows: [],
      shadowRows: opponent?.shadowRows ?? [],
    },
  });
}

export async function getMatchmakingStats() {
  return withStore((store) =>
    matchmakingStatsSchema.parse({
      queueSize: store.tickets.filter((ticket) => ticket.status === "queueing").length,
      activeRooms: store.rooms.filter((room) => ["playing", "paused"].includes(room.status)).length,
    }),
  );
}

export async function joinMatchmaking(playerId: string, latencyMs?: number | null) {
  return withStore((store, now) => {
    const existing = store.tickets.find(
      (ticket) =>
        ticket.playerId === playerId &&
        !["cancelled", "expired", "finished"].includes(ticket.status),
    );

    if (existing) {
      existing.latencyMs = normalizeLatency(latencyMs);
      existing.latencyBucket = resolveLatencyBucket(existing.latencyMs);
      existing.updatedAt = createTimestamp(now);
      return matchmakingStatusSchema.parse(buildTicketStatus(store, existing, now));
    }

    const ticket: MatchmakingTicketRecord = {
      ticketId: createTicketId(),
      playerId,
      latencyMs: normalizeLatency(latencyMs),
      latencyBucket: resolveLatencyBucket(latencyMs),
      status: "queueing",
      roomCode: null,
      createdAt: createTimestamp(now),
      updatedAt: createTimestamp(now),
    };

    store.tickets.push(ticket);
    maybeMatchTickets(store, now);
    publishTicketEvent(ticket.ticketId);
    return matchmakingStatusSchema.parse(buildTicketStatus(store, ticket, now));
  });
}

function buildTicketStatus(store: BattleStore, ticket: MatchmakingTicketRecord, now: Date) {
  const room = ticket.roomCode
    ? store.rooms.find((item) => item.roomCode === ticket.roomCode) ?? null
    : null;
  return {
    ticketId: ticket.ticketId,
    viewerToken: buildBattlePlayerCookieValue(ticket.playerId),
    status: room?.status ?? ticket.status,
    queueSize: store.tickets.filter((item) => item.status === "queueing").length,
    activeRooms: store.rooms.filter((item) => ["playing", "paused"].includes(item.status)).length,
    waitSeconds: Math.max(0, Math.round((now.getTime() - Date.parse(ticket.createdAt)) / 1000)),
    roomCode: room?.roomCode ?? ticket.roomCode,
    acceptanceDeadline: room?.acceptanceDeadline ?? null,
  };
}

export async function getMatchmakingStatus(playerId: string, ticketId: string) {
  return withStore((store, now) => {
    const ticket = store.tickets.find(
      (item) => item.ticketId === ticketId && item.playerId === playerId,
    );
    if (!ticket) {
      throw new Error("这次匹配已经失效了。");
    }

    return matchmakingStatusSchema.parse(buildTicketStatus(store, ticket, now));
  });
}

export async function confirmMatchmaking(playerId: string, ticketId: string) {
  return withStore(async (store, now) => {
    const ticket = store.tickets.find(
      (item) => item.ticketId === ticketId && item.playerId === playerId,
    );
    if (!ticket || !ticket.roomCode) {
      throw new Error("这次匹配已经失效了。");
    }

    const room = findRoomOrThrow(store, ticket.roomCode);
    if (room.status !== "matched_pending_accept" && room.status !== "ready_confirm") {
      return matchmakingStatusSchema.parse(buildTicketStatus(store, ticket, now));
    }

    const player = findPlayerOrThrow(room, playerId);
    player.acceptedStart = true;
    room.updatedAt = createTimestamp(now);

    if (room.players.every((entry) => entry.acceptedStart)) {
      await startRoomRound(room, now);
      syncTicketsFromRoom(store, room, "playing", now);
    }

    return matchmakingStatusSchema.parse(buildTicketStatus(store, ticket, now));
  });
}

export async function cancelMatchmaking(playerId: string, ticketId: string) {
  return withStore((store, now) => {
    const ticket = store.tickets.find(
      (item) => item.ticketId === ticketId && item.playerId === playerId,
    );
    if (!ticket) {
      throw new Error("这次匹配已经失效了。");
    }

    const room = ticket.roomCode
      ? store.rooms.find((item) => item.roomCode === ticket.roomCode) ?? null
      : null;

    ticket.status = "cancelled";
    ticket.updatedAt = createTimestamp(now);
    ticket.roomCode = null;

    if (room && room.status === "matched_pending_accept") {
      for (const opponentTicketId of room.ticketIds) {
        if (opponentTicketId === ticket.ticketId) continue;
        const opponentTicket = store.tickets.find((item) => item.ticketId === opponentTicketId);
        if (!opponentTicket) continue;
        opponentTicket.status = "queueing";
        opponentTicket.roomCode = null;
        opponentTicket.updatedAt = createTimestamp(now);
        publishTicketEvent(opponentTicket.ticketId);
      }
      store.rooms = store.rooms.filter((item) => item.roomCode !== room.roomCode);
    }

    publishTicketEvent(ticket.ticketId);
    return matchmakingStatusSchema.parse(buildTicketStatus(store, ticket, now));
  });
}

export async function createFriendRoom(playerId: string) {
  return withStore(async (store, now) => {
    const roomCode = createUniqueRoomCode(store);
    const room: BattleRoomRecord = {
      roomCode,
      inviteCode: roomCode,
      mode: "friend",
      status: "waiting",
      roundNumber: 1,
      createdAt: createTimestamp(now),
      updatedAt: createTimestamp(now),
      startedAt: null,
      finishedAt: null,
      acceptanceDeadline: null,
      pausedUntil: null,
      pauseRequest: null,
      pauseUsed: false,
      winnerSeat: null,
      loserSeat: null,
      answerCharacterId: null,
      ticketIds: [],
      players: [createPlayerRecord(playerId, "friend", "A")],
    };
    store.rooms.push(room);
    publishRoomEvent(roomCode);
    return buildRoomState(room, playerId);
  });
}

export async function joinFriendRoom(playerId: string, inviteCode: string) {
  return withStore(async (store, now) => {
    const room = store.rooms.find((item) => item.inviteCode === inviteCode);
    if (!room || room.mode !== "friend") {
      throw new Error("这个房间码已经失效了。");
    }

    const existing = room.players.find((item) => item.playerId === playerId);
    if (existing) {
      return buildRoomState(room, playerId);
    }

    if (room.players.length >= 2) {
      throw new Error("这个房间已经满了。");
    }

    room.players.push(createPlayerRecord(playerId, "friend", "B"));
    room.status = "ready_confirm";
    room.updatedAt = createTimestamp(now);
    publishRoomEvent(room.roomCode);
    return buildRoomState(room, playerId);
  });
}

export async function getBattleRoomState(roomCode: string, playerId: string) {
  return withStore(async (store) => {
    const room = findRoomOrThrow(store, roomCode);
    return buildRoomState(room, playerId);
  });
}

export async function performRoomAction(
  roomCode: string,
  playerId: string,
  action:
    | { type: "confirm-start" }
    | { type: "request-pause" }
    | { type: "respond-pause"; accept: boolean }
    | { type: "resume-now" }
    | { type: "surrender" }
    | { type: "request-rematch" }
    | { type: "heartbeat"; latencyMs?: number | null }
    | { type: "guess"; characterId: string },
) {
  return withStore(async (store, now) => {
    const room = findRoomOrThrow(store, roomCode);
    const player = findPlayerOrThrow(room, playerId);
    const opponent = getOpponent(room, player.seat);

    player.lastHeartbeatAt = createTimestamp(now);
    player.online = true;
    if ("latencyMs" in action) {
      player.latencyMs = normalizeLatency(action.latencyMs);
    }

    switch (action.type) {
      case "heartbeat": {
        break;
      }
      case "confirm-start": {
        if (!["matched_pending_accept", "ready_confirm", "waiting"].includes(room.status)) {
          throw new Error("这一局现在还不能开始。");
        }
        if (room.players.length < 2) {
          throw new Error("还在等另一位玩家加入。");
        }
        player.acceptedStart = true;
        room.status = room.mode === "matchmaking" ? "matched_pending_accept" : "ready_confirm";
        room.updatedAt = createTimestamp(now);
        if (room.players.every((entry) => entry.acceptedStart)) {
          await startRoomRound(room, now);
          syncTicketsFromRoom(store, room, "playing", now);
        }
        break;
      }
      case "request-pause": {
        if (room.status !== "playing") {
          throw new Error("这一局现在还不能暂停。");
        }
        if (room.pauseUsed) {
          throw new Error("这一局的暂停已经用过了。");
        }
        room.pauseRequest = {
          requesterSeat: player.seat,
          requestedAt: createTimestamp(now),
          expiresAt: createTimestamp(
            new Date(now.getTime() + getServerConfig().matchPauseDurationSeconds * 1000),
          ),
        };
        room.updatedAt = createTimestamp(now);
        break;
      }
      case "respond-pause": {
        if (!room.pauseRequest || room.pauseRequest.requesterSeat === player.seat) {
          throw new Error("现在没有需要你确认的暂停。");
        }
        if (!action.accept) {
          room.pauseRequest = null;
          room.updatedAt = createTimestamp(now);
          break;
        }
        room.pauseUsed = true;
        room.status = "paused";
        room.pausedUntil = createTimestamp(
          new Date(now.getTime() + getServerConfig().matchPauseDurationSeconds * 1000),
        );
        room.pauseRequest = null;
        room.updatedAt = createTimestamp(now);
        syncTicketsFromRoom(store, room, "paused", now);
        break;
      }
      case "resume-now": {
        if (room.status !== "paused") {
          throw new Error("这一局现在没有在暂停。");
        }
        room.status = "playing";
        room.pausedUntil = null;
        room.pauseRequest = null;
        room.updatedAt = createTimestamp(now);
        syncTicketsFromRoom(store, room, "playing", now);
        break;
      }
      case "surrender": {
        if (!["playing", "paused"].includes(room.status)) {
          throw new Error("这一局已经结束了。");
        }
        finishRoom(room, now, {
          winnerSeat: opponent?.seat ?? null,
          loserSeat: player.seat,
          loserResult: "surrendered",
        });
        syncTicketsFromRoom(store, room, "finished", now);
        break;
      }
      case "request-rematch": {
        if (room.status !== "finished") {
          throw new Error("这一局还没结束。");
        }
        player.requestedRematch = true;
        room.updatedAt = createTimestamp(now);
        if (room.players.length === 2 && room.players.every((entry) => entry.requestedRematch)) {
          room.roundNumber += 1;
          await startRoomRound(room, now);
          syncTicketsFromRoom(store, room, "playing", now);
        }
        break;
      }
      case "guess": {
        if (room.status !== "playing") {
          throw new Error("这一局现在不能继续猜了。");
        }
        if (!room.answerCharacterId) {
          throw new Error("答案还没准备好。");
        }

        const bank = await getQuestionBankMap();
        const guess = bank.get(action.characterId);
        const answer = bank.get(room.answerCharacterId);
        if (!guess || !answer) {
          throw new Error("当前题库中找不到对应角色。");
        }

        const guessRow = buildGuessRow(guess, answer);
        const attempt = player.guessRows.length + 1;
        player.guessRows.push(guessRow);
        if (opponent) {
          opponent.shadowRows.push(toShadowRow(attempt, guessRow));
        }

        if (action.characterId === room.answerCharacterId) {
          finishRoom(room, now, {
            winnerSeat: player.seat,
            loserSeat: opponent?.seat ?? null,
          });
          syncTicketsFromRoom(store, room, "finished", now);
          break;
        }

        player.remainingGuesses = Math.max(player.remainingGuesses - 1, 0);
        room.updatedAt = createTimestamp(now);
        if (player.remainingGuesses <= 0) {
          finishRoom(room, now, {
            winnerSeat: opponent?.seat ?? null,
            loserSeat: player.seat,
          });
          syncTicketsFromRoom(store, room, "finished", now);
        }
        break;
      }
      default: {
        action satisfies never;
      }
    }

    publishRoomEvent(room.roomCode);
    return buildRoomState(room, playerId);
  });
}

export async function __resetBattleStoreForTests() {
  try {
    await fs.unlink(STORE_PATH);
  } catch {}
}

export async function __peekBattleRoomForTests(roomCode: string) {
  const store = await readStore();
  return store.rooms.find((room) => room.roomCode === roomCode) ?? null;
}
