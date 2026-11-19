/**
 * 契约操作的强类型封装。
 * 每个方法对应 openapi.json 的一个 operationId，路径与响应类型都从生成类型推导，
 * 避免手写重复 API 类型。
 */
import { createApiClient, createIdempotencyKey, type ApiClient, type ApiClientOptions, type ApiResponse } from "./client";
import type { operations } from "./types";

type Op = operations;

/**
 * 从 operationId 推导成功响应体类型。
 * 只取数字状态码（2xx/304）里带 application/json 的响应体；
 * 204/304 没有响应体，其分支为 never，union 后自然消失。
 */
type OkBody<Id extends keyof Op> = Op[Id] extends { responses: infer R }
  ? {
      [K in keyof R]: K extends number
        ? R[K] extends { content: { "application/json": infer B } }
          ? B
          : never
        : never;
    }[keyof R]
  : null;

/** 取 operationId 的请求体类型 */
type ReqBody<Id extends keyof Op> = Op[Id] extends { requestBody: { content: { "application/json": infer B } } }
  ? B
  : never;

/** 取 operationId 的路径参数类型 */
type PathParams<Id extends keyof Op> = Op[Id] extends { parameters: { path: infer P } } ? P : never;

export type Api = ReturnType<typeof createContractApi>;

export function createContractApi(clientOptions: ApiClientOptions = {}) {
  const client: ApiClient = createApiClient(clientOptions);

  async function call<Id extends keyof Op & string>(
    operationId: Id,
    path: string,
    init: {
      method: "GET" | "POST" | "PATCH" | "DELETE";
      body?: unknown;
      query?: Record<string, string | number | boolean | null | undefined>;
      /** 写请求：true 表示自动生成幂等键，字符串表示复用调用方持有的键 */
      idempotency?: boolean | string;
      signal?: AbortSignal;
      /** 该操作不需要 CSRF（仅创建会话等少数情况） */
      noCsrf?: boolean;
      authScope?: "display";
    },
  ): Promise<ApiResponse<OkBody<Id>>> {
    const idempotencyKey =
      init.idempotency === undefined || init.idempotency === false
        ? undefined
        : init.idempotency === true
          ? createIdempotencyKey()
          : init.idempotency;

    return client.request<OkBody<Id>>(path, {
      method: init.method,
      body: init.body,
      query: init.query,
      signal: init.signal,
      ...(idempotencyKey === undefined ? {} : { idempotencyKey }),
      headers: { "X-Operation-Id": operationId },
      noCsrf: init.noCsrf,
      authScope: init.authScope,
    });
  }

  return {
    client,

    // ---- 会话 ----
    /*
     * 创建会话不需要 CSRF。
     * 因此显式标记 noCsrf，否则会被 requireCsrf 提前拦截，
     * 导致刷新后无法重新建立可用的会话。
     */
    createSession: (body: ReqBody<"createSession">, signal?: AbortSignal) =>
      call("createSession", "/sessions", { method: "POST", body, signal, noCsrf: true }),

    getAdmissionStatus: (signal?: AbortSignal) =>
      call("getAdmissionStatus", "/sessions/current/admission", { method: "GET", signal }),

    verifyAdmission: (body: ReqBody<"verifyAdmission">, signal?: AbortSignal) =>
      call("verifyAdmission", "/sessions/current/admission", { method:"POST",body,signal }),

    getCurrentSession: (signal?: AbortSignal) =>
      call("getCurrentSession", "/sessions/current", { method: "GET", signal }),

    updateCurrentSession: (body: ReqBody<"updateCurrentSession">, signal?: AbortSignal) =>
      call("updateCurrentSession", "/sessions/current", { method: "PATCH", body, idempotency: true, signal }),

    deleteCurrentSession: (signal?: AbortSignal) =>
      call("deleteCurrentSession", "/sessions/current", { method: "DELETE", idempotency: true, signal }),

    /* 用现有会话 cookie 与 Origin 轮换 CSRF，不创建新身份；无需旧 token 或幂等键 */
    rotateSessionCsrf: (signal?: AbortSignal) =>
      call("rotateSessionCsrf", "/sessions/current/csrf", { method: "POST", signal, noCsrf: true }),

    createChallenge: (body: ReqBody<"createChallenge">, key: string, signal?: AbortSignal) =>
      call("createChallenge", "/challenges", {method:"POST",body,idempotency:key,signal}),
    getChallenge: (id: string, signal?: AbortSignal) =>
      call("getChallenge", `/challenges/${encodeURIComponent(id)}`, {method:"GET",signal}),
    guessChallenge: (id: string, body: ReqBody<"guessChallenge">, key: string, signal?: AbortSignal) =>
      call("guessChallenge", `/challenges/${encodeURIComponent(id)}/guesses`, {method:"POST",body,idempotency:key,signal}),
    finishChallenge: (id: string, body: ReqBody<"finishChallenge">, key: string, signal?: AbortSignal) =>
      call("finishChallenge", `/challenges/${encodeURIComponent(id)}/finish`, {method:"POST",body,idempotency:key,signal}),

    // ---- 配置与内容 ----
    getPublicConfig: (signal?: AbortSignal) => call("getPublicConfig", "/config", { method: "GET", signal }),
    getAptitudePracticePack: (signal?: AbortSignal) => call("getAptitudePracticePack", "/practice-packs/aptitude-grid", {method:"GET",signal}),
    getAudioPracticePack: (signal?: AbortSignal) => call("getAudioPracticePack", "/practice-packs/audio", {method:"GET",signal}),
    getPedigreePracticePack: (signal?: AbortSignal) => call("getPedigreePracticePack", "/practice-packs/pedigree", {method:"GET",signal}),
    getStrategyPracticePack: (signal?: AbortSignal) => call("getStrategyPracticePack", "/practice-packs/strategy-race", {method:"GET",signal}),
    getPathPracticePack: (signal?: AbortSignal) => call("getPathPracticePack", "/practice-packs/co-race-path", {method:"GET",signal}),
    getRacePracticePack: (signal?: AbortSignal) => call("getRacePracticePack", "/practice-packs/race-rebuild", {method:"GET",signal}),
    getDraftPracticePack: (signal?: AbortSignal) => call("getDraftPracticePack", "/practice-packs/draft", {method:"GET",signal}),
    getMemoryPracticePack: (signal?: AbortSignal) => call("getMemoryPracticePack", "/practice-packs/memory", {method:"GET",signal}),
    getSortPracticePack: (signal?: AbortSignal) => call("getSortPracticePack", "/practice-packs/sort", {method:"GET",signal}),

    getClassicPracticePack: (signal?: AbortSignal) => call("getClassicPracticePack", "/practice-packs/classic", { method: "GET", signal }),

    listModes: (signal?: AbortSignal) => call("listModes", "/modes", { method: "GET", signal }),

    listCharacters: (
      query: Op["listCharacters"] extends { parameters: { query?: infer Q } } ? Q : never,
      signal?: AbortSignal,
    ) => call("listCharacters", "/characters", { method: "GET", query, signal }),

    getCharacter: (characterId: string, signal?: AbortSignal) =>
      call("getCharacter", `/characters/${encodeURIComponent(characterId)}`, { method: "GET", signal }),

    // ---- 邀请码解析 ----
    resolveRoomInvitation: (body: ReqBody<"resolveRoomInvitation">, signal?: AbortSignal) =>
      call("resolveRoomInvitation", "/room-invitations/resolutions", {
        method: "POST",
        body,
        // Stateless public lookup: no cookie authority or mutation to protect.
        noCsrf: true,
        signal,
      }),

    // ---- 健康 ----
    getLiveness: (signal?: AbortSignal) => call("getLiveness", "/health/live", { method: "GET", signal }),

    getReadiness: (signal?: AbortSignal) => call("getReadiness", "/health/ready", { method: "GET", signal }),

    // ---- 房间 ----
    createRoom: (body: ReqBody<"createRoom">, idempotencyKey?: string, signal?: AbortSignal) =>
      call("createRoom", "/rooms", {
        method: "POST",
        body,
        idempotency: idempotencyKey ?? true,
        signal,
      }),

    getRoom: (roomId: string, signal?: AbortSignal, displayOnly = false) =>
      call("getRoom", `/rooms/${encodeURIComponent(roomId)}${displayOnly ? "?view=display" : ""}`, { method: "GET", signal, authScope: displayOnly ? "display" : undefined }),

    updateRoom: (roomId: string, body: ReqBody<"updateRoom">, signal?: AbortSignal) =>
      call("updateRoom", `/rooms/${encodeURIComponent(roomId)}`, {
        method: "PATCH",
        body,
        idempotency: true,
        signal,
      }),

    joinRoom: (roomId: string, body: ReqBody<"joinRoom">, signal?: AbortSignal) =>
      call("joinRoom", `/rooms/${encodeURIComponent(roomId)}/memberships`, {
        method: "POST",
        body,
        idempotency: true,
        signal,
      }),

    /*
     * requestedRole 的契约 enum 已包含 team-captain（host/display 刻意排除，
     * 因为它们通过各自端点授予，角色变更不能成为取得控制权的路径）。
     * 因此直接用契约生成的类型，不再需要前端扩展。
     */
    updateMembership: (roomId: string, membershipId: string, body: ReqBody<"updateMembership">, signal?: AbortSignal) =>
      call(
        "updateMembership",
        `/rooms/${encodeURIComponent(roomId)}/memberships/${encodeURIComponent(membershipId)}`,
        { method: "PATCH", body, idempotency: true, signal },
      ),

    // ---- 队伍（G03 teams 变体 / 活动分队） ----
    /*
     * 队伍聚合的关键在房间层：成员 role=team-captain 且 teamId 非空时，
     * 回合主体用队伍 ID 而不是成员 ID（internal/room/rounds.go:82）。
     * 所以建队 + 设队长就能让聚合生效，不需要改模式计分。
     */
    createTeam: (roomId: string, body: ReqBody<"createTeam">, signal?: AbortSignal) =>
      call("createTeam", `/rooms/${encodeURIComponent(roomId)}/teams`, {
        method: "POST",
        body,
        idempotency: true,
        signal,
      }),

    issueControlLease: (roomId: string, body: ReqBody<"issueControlLease">, key: string, signal?: AbortSignal) =>
      call("issueControlLease", `/rooms/${encodeURIComponent(roomId)}/control-leases`, { method:"POST", body, idempotency:key, signal }),

    transferRoomHost: (roomId: string, body: ReqBody<"transferRoomHost">, key: string, signal?: AbortSignal) =>
      call("transferRoomHost", `/rooms/${encodeURIComponent(roomId)}/host-transfers`, { method: "POST", body, idempotency: key, signal }),

    leaveOrRemoveMembership: (roomId: string, membershipId: string, signal?: AbortSignal, key?: string) =>
      call(
        "leaveOrRemoveMembership",
        `/rooms/${encodeURIComponent(roomId)}/memberships/${encodeURIComponent(membershipId)}`,
        { method: "DELETE", idempotency: key ?? true, signal },
      ),

    // ---- 回合 ----
    createRound: (roomId: string, body: ReqBody<"createRound">, signal?: AbortSignal, idempotencyKey?: string) =>
      call("createRound", `/rooms/${encodeURIComponent(roomId)}/rounds`, {
        method: "POST",
        body,
        idempotency: idempotencyKey ?? true,
        signal,
      }),

    getRound: (roundId: string, signal?: AbortSignal, displayOnly = false) =>
      call("getRound", `/rounds/${encodeURIComponent(roundId)}${displayOnly ? "?view=display" : ""}`, { method: "GET", signal, authScope: displayOnly ? "display" : undefined }),

    createTransition: (roundId: string, body: ReqBody<"createTransition">, signal?: AbortSignal, idempotencyKey?: string) =>
      call("createTransition", `/rounds/${encodeURIComponent(roundId)}/transitions`, {
        method: "POST",
        body,
        idempotency: idempotencyKey ?? true,
        signal,
      }),

    // 写请求的幂等键由调用方持有：同一逻辑动作重试必须复用同一个键
    createSubmission: (
      roundId: string,
      body: ReqBody<"createSubmission">,
      idempotencyKey: string,
      signal?: AbortSignal,
    ) =>
      call("createSubmission", `/rounds/${encodeURIComponent(roundId)}/submissions`, {
        method: "POST",
        body,
        idempotency: idempotencyKey,
        signal,
      }),

    /*
     * 交易（trade 模式）走 room 层专用端点，不走 modeAction。
     *
     * trade 模式不实现 ContractKinds()，注释明确写「声明这些 kind 会让契约对齐
     * 测试通过而映射里没有它们，正是那个测试要抓的漂移」。
     * 所以交易必须用这两个 REST 端点，不能拼 modeAction。
     */
    createTrade: (roundId: string, body: ReqBody<"createTrade">, idempotencyKey?: string, signal?: AbortSignal) =>
      call("createTrade", `/rounds/${encodeURIComponent(roundId)}/trades`, {
        method: "POST",
        body,
        idempotency: idempotencyKey ?? true,
        signal,
      }),

    respondToTrade: (
      roundId: string,
      tradeId: string,
      body: ReqBody<"respondToTrade">,
      idempotencyKey?: string,
      signal?: AbortSignal,
    ) =>
      call(
        "respondToTrade",
        `/rounds/${encodeURIComponent(roundId)}/trades/${encodeURIComponent(tradeId)}/responses`,
        { method: "POST", body, idempotency: idempotencyKey ?? true, signal },
      ),

    createSurrender: (roundId: string, body: ReqBody<"createSurrender">, signal?: AbortSignal) =>
      call("createSurrender", `/rounds/${encodeURIComponent(roundId)}/surrenders`, {
        method: "POST",
        body,
        idempotency: true,
        signal,
      }),

    // ---- 房间事件流（实时降级用） ----
    listRoomEvents: (
      roomId: string,
      query: Op["listRoomEvents"] extends { parameters: { query?: infer Q } } ? Q : never,
      signal?: AbortSignal,
    ) => call("listRoomEvents", `/rooms/${encodeURIComponent(roomId)}/events`, { method: "GET", query, signal }),

    // ---- G05 co-race-path：邻居查找（逐边证据由服务端给出） ----
    getPathNeighbors: (roundId: string, characterId: string, signal?: AbortSignal) =>
      call("getPathNeighbors", `/rounds/${encodeURIComponent(roundId)}/path-neighbors`, {
        method: "GET",
        query: { characterId },
        signal,
      }),

    getMatchmakingLobby: (signal?:AbortSignal) => call("getMatchmakingLobby","/matchmaking-lobby",{method:"GET",signal}),
    getActiveMatchmakingTicket: (signal?:AbortSignal) => call("getActiveMatchmakingTicket","/matchmaking-tickets/current",{method:"GET",signal}),
    createMatchmakingProof: (body:ReqBody<"createMatchmakingProof">,signal?:AbortSignal) => call("createMatchmakingProof","/matchmaking-proofs",{method:"POST",body,idempotency:true,signal}),
    heartbeatMatchmakingTicket: (id:string,signal?:AbortSignal) => call("heartbeatMatchmakingTicket",`/matchmaking-tickets/${encodeURIComponent(id)}/heartbeat`,{method:"POST",body:{},signal}),

    // ---- Matchmaking ----
    createMatchmakingTicket: (body: ReqBody<"createMatchmakingTicket">, idempotencyKey?: string, signal?: AbortSignal) =>
      call("createMatchmakingTicket", "/matchmaking-tickets", {
        method: "POST",
        body,
        idempotency: idempotencyKey ?? true,
        signal,
      }),

    getMatchmakingTicket: (ticketId: string, signal?: AbortSignal) =>
      call("getMatchmakingTicket", `/matchmaking-tickets/${encodeURIComponent(ticketId)}`, { method: "GET", signal }),

    cancelMatchmakingTicket: (ticketId: string, signal?: AbortSignal,key?:string) =>
      call("cancelMatchmakingTicket", `/matchmaking-tickets/${encodeURIComponent(ticketId)}`, {
        method: "DELETE",
        idempotency: key ?? true,
        signal,
      }),

    respondToMatch: (ticketId: string, body: ReqBody<"respondToMatch">, signal?: AbortSignal, key?:string) =>
      call("respondToMatch", `/matchmaking-tickets/${encodeURIComponent(ticketId)}/responses`, {
        method: "POST",
        body,
        idempotency: key ?? true,
        signal,
      }),

    // ---- 活动（娱乐杯） ----
    createEvent: (body: ReqBody<"createEvent">, idempotencyKey?: string, signal?: AbortSignal) =>
      call("createEvent", "/events", { method: "POST", body, idempotency: idempotencyKey ?? true, signal }),

    getEvent: (eventId: string, signal?: AbortSignal, displayOnly = false) =>
      call("getEvent", `/events/${encodeURIComponent(eventId)}${displayOnly ? "?view=display" : ""}`, { method: "GET", signal, authScope: displayOnly ? "display" : undefined }),

    createEventTransition: (eventId: string, body: ReqBody<"createEventTransition">, signal?: AbortSignal, key?: string) =>
      call("createEventTransition", `/events/${encodeURIComponent(eventId)}/transitions`, {
        method: "POST",
        body,
        idempotency: key ?? true,
        signal,
      }),

    createSegment: (eventId: string, body: ReqBody<"createSegment">, signal?: AbortSignal, key?: string) =>
      call("createSegment", `/events/${encodeURIComponent(eventId)}/segments`, {
        method: "POST",
        body,
        idempotency: key ?? true,
        signal,
      }),

    updateSegment: (
      eventId: string,
      segmentId: string,
      body: ReqBody<"updateSegment">,
      signal?: AbortSignal,
    ) =>
      call(
        "updateSegment",
        `/events/${encodeURIComponent(eventId)}/segments/${encodeURIComponent(segmentId)}`,
        { method: "PATCH", body, idempotency: true, signal },
      ),

    getStandings: (eventId: string, signal?: AbortSignal, displayOnly = false) =>
      call("getStandings", `/events/${encodeURIComponent(eventId)}/standings${displayOnly ? "?view=display" : ""}`, { method: "GET", signal, authScope: displayOnly ? "display" : undefined }),

    createScoreCorrection: (eventId: string, body: ReqBody<"createScoreCorrection">, signal?: AbortSignal, key?: string) =>
      call("createScoreCorrection", `/events/${encodeURIComponent(eventId)}/score-corrections`, {
        method: "POST",
        body,
        idempotency: key ?? true,
        signal,
      }),

    createDisplayGrant: (eventId: string, signal?: AbortSignal, key?: string) =>
      call("createDisplayGrant", `/events/${encodeURIComponent(eventId)}/display-grants`, {
        method: "POST",
        idempotency: key ?? true,
        signal,
      }),

    /* 大屏配对发生在尚无会话的浏览器中，兑换请求显式跳过 CSRF 检查 */
    redeemDisplayGrant: (body: ReqBody<"redeemDisplayGrant">, signal?: AbortSignal) =>
      call("redeemDisplayGrant", "/display-sessions", {
        method: "POST",
        body,
        idempotency: true,
        signal,
        noCsrf: true,
      }),
  };
}

/** 路径参数工具：用于需要拼接 ID 的页面（导出以便测试） */
export type { PathParams };
