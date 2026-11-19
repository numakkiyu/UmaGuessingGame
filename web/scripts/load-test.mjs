#!/usr/bin/env node
/** Measures REST latency, stream delivery and resource usage with independent sessions. Results apply to the observed machine, workload and duration. */
import { mkdir, writeFile } from "node:fs/promises";
import { assessCapacity } from "./load-verdict.mjs";
import {cpus,totalmem,platform,release,arch} from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = resolve(HERE, "..");
const EVIDENCE = resolve(WEB_ROOT, "../.cache/reports");

/* Resolve the base URL after parsing arguments so --api controls the target server. */
let apiBaseCache = null;
function apiBase() {
  if (apiBaseCache === null) {
    apiBaseCache = (args.api || process.env.UMA_API_BASE || "http://127.0.0.1:8080").replace(/\/$/, "");
  }
  return apiBaseCache;
}

/* ---------------- 参数 ---------------- */
function parseArgs(argv) {
  const args = { players: 100, spectators: 100, rooms: 10, minutes: 60, quick: false, full: false };
  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (token === "--quick") args.quick = true;
    else if (token === "--full") args.full = true;
    else if (token === "--players") args.players = Number(argv[++index]);
    else if (token === "--spectators") args.spectators = Number(argv[++index]);
    else if (token === "--rooms") args.rooms = Number(argv[++index]);
    else if (token === "--minutes") args.minutes = Number(argv[++index]);
    else if (token === "--out") args.out = argv[++index];
    else if (token === "--api") args.api = argv[++index];
    else {
      /* Reject unknown flags to avoid silently testing the wrong server. */
      throw new Error(
        `load-test: unknown argument ${JSON.stringify(token)}. ` +
          `Supported: --quick --full --players N --spectators N --rooms N --minutes N --out PATH --api URL`,
      );
    }
  }
  if (args.quick) {
    // 快速档：只验证压测链路能跑通，不代表容量结论
    Object.assign(args, { players: 20, spectators: 20, rooms: 2, minutes: 1 });
  }
  return args;
}

const args = parseArgs(process.argv.slice(2));
for(const name of ["players","rooms"]){if(!Number.isSafeInteger(args[name])||args[name]<1)throw new Error(`invalid ${name}`);}
if(!Number.isSafeInteger(args.spectators)||args.spectators<0||!Number.isFinite(args.minutes)||args.minutes<=0||args.minutes>120)throw new Error("invalid spectators or minutes");
if(args.players%args.rooms || args.spectators%args.rooms)throw new Error("participant totals must divide evenly across rooms");
const retryObservations=[];
let activeReport;
const allOpenedStreams=[];

/* ---------------- 延迟统计 ---------------- */
/**
 * 压测延迟记录器。
 *
 * 把「失败」分成两类，否则会把正常的业务校验当成服务故障：
 * - 业务拒绝（business）：服务端按规则正确拒绝，例如重复猜同一角色
 * （duplicate_guess）、额度用尽（round_finished）。压测时这类会自然发生，
 * 不计入错误率，但要单独记录数量——它们能反映「客户端请求模式是否合理」。
 * - 真失败（error）：网络错误、5xx、鉴权失败等。
 */
class LatencyRecorder {
  constructor(name) {
    this.name = name;
    this.samples = [];
    this.errors = 0;
    this.businessRejections = 0;
    this.total = 0;
    this.errorCodes = new Map();
    this.businessCodes = new Map();
    /** 分位数排序结果的惰性缓存（样本量大时避免重复排序） */
    this.sortedCache = null;
  }

  record(ms, errorCode) {
    this.total += 1;
    this.sortedCache = null;
    if (!errorCode) {
      this.samples.push(ms);
      return;
    }
    // 业务拒绝单独统计，不算失败
    if (isBusinessRejection(errorCode)) {
      this.businessRejections += 1;
      this.businessCodes.set(errorCode, (this.businessCodes.get(errorCode) ?? 0) + 1);
      this.samples.push(ms);
      return;
    }
    this.errors += 1;
    this.errorCodes.set(errorCode, (this.errorCodes.get(errorCode) ?? 0) + 1);
  }

  /**
   * 分位数。
   *
   * 排序结果缓存一次：60 分钟压测每类操作十几万样本，
   * 四个分位各排一次是纯浪费（而且 spread 拷贝也吃内存）。
   */
  percentile(p) {
    if (this.samples.length === 0) return null;
    if (!this.sortedCache) {
      this.sortedCache = Float64Array.from(this.samples).sort();
    }
    const sorted = this.sortedCache;
    const index = Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length));
    return Math.round(sorted[index]);
  }

  summary() {
    return {
      operation: this.name,
      total: this.total,
      ok: this.samples.length,
      errors: this.errors,
      businessRejections: this.businessRejections,
      errorRate: this.total > 0 ? Number((this.errors / this.total).toFixed(4)) : 0,
      errorCodes: Object.fromEntries(this.errorCodes),
      businessCodes: Object.fromEntries(this.businessCodes),
      p50: this.percentile(50),
      p95: this.percentile(95),
      p99: this.percentile(99),
      /*
       * 不能用 Math.max(...samples)：60 分钟压测的样本量可达十几万，
       * 展开成函数参数会超过调用栈上限（实测 RangeError:
       * Maximum call stack size exceeded），整个汇总阶段崩掉、报告写不出来。
       * 用 reduce 累加，与样本量无关。
       */
      max: this.samples.length > 0 ? Math.round(this.samples.reduce((a, b) => (a > b ? a : b), 0)) : null,
    };
  }
}

const recorders = {
  createSession: new LatencyRecorder("createSession"),
  createRoom: new LatencyRecorder("createRoom"),
  joinRoom: new LatencyRecorder("joinRoom"),
  createRound: new LatencyRecorder("createRound"),
  getRoom: new LatencyRecorder("getRoom"),
  getRound: new LatencyRecorder("getRound"),
  submitGuess: new LatencyRecorder("submitGuess"),
  getHostRoom: new LatencyRecorder("getHostRoom"),
  streamConnect: new LatencyRecorder("streamConnect"),
  submissionVisible: new LatencyRecorder("submissionVisible"),
};

/* ---------------- 独立 cookie jar 的客户端 ---------------- */
function makeClient() {
  let cookie = "";
  let csrf = null;
  return {
    get cookie() {
      return cookie;
    },
    async call(method, path, body, idempotencyKey, csrfRetried = false, rateRetries = 0) {
      const startedAt = performance.now();
      const headers = { Accept: "application/json, application/problem+json", Origin: apiBase() };
      if (body !== undefined) headers["Content-Type"] = "application/json";
      if (cookie) headers["Cookie"] = cookie;
      if (csrf && method !== "GET") headers["X-CSRF-Token"] = csrf;
      if (idempotencyKey) headers["Idempotency-Key"] = idempotencyKey;
      let res;
      try {
        res = await fetch(`${apiBase()}/api/v1${path}`, {
          method,
          headers,
          body: body === undefined ? undefined : JSON.stringify(body),
          signal: AbortSignal.timeout(30_000),
        });
      } catch (error) {
        return { status: 0, json: null, ms: Math.round(performance.now() - startedAt), networkError: String(error) };
      }
      for (const c of res.headers.getSetCookie?.() ?? []) {
        const kv = c.split(";")[0];
        if (kv.startsWith("uma_session")) cookie = kv;
      }
      const text = await res.text();
      let json = null;
      try {
        json = JSON.parse(text);
      } catch {
        /* 非 JSON */
      }
      const ms = Math.round(performance.now() - startedAt);
      if(res.status===429 && rateRetries<3){
        const raw=res.headers.get("Retry-After"),seconds=raw===null?NaN:Number(raw);
        if(Number.isFinite(seconds)&&seconds>0&&seconds<=60){
          const delay=seconds*1000+Math.floor(Math.random()*200);
          retryObservations.push({path,attempt:rateRetries+1,waitMs:delay});
          await new Promise(r=>setTimeout(r,delay));
          return this.call(method,path,body,idempotencyKey,csrfRetried,rateRetries+1);
        }
      }
      // CSRF 自愈（后端只有一份 hash）
      if (!csrfRetried && res.status === 403 && /csrf-token-invalid/.test(text) && method !== "GET") {
        const rotated = await fetch(`${apiBase()}/api/v1/sessions/current/csrf`, {
          method: "POST",
          headers: { Accept: "application/json", Origin: apiBase(), Cookie: cookie },
          cache: "no-store",
        }).catch(() => null);
        if (rotated && rotated.ok) {
          const bodyJson = await rotated.json().catch(() => null);
          if (bodyJson?.csrfToken) {
            csrf = bodyJson.csrfToken;
            return this.call(method, path, body, idempotencyKey, true, rateRetries);
          }
        }
      }
      return { status: res.status, json, ms };
    },
    async newSession(name) {
      const s = await this.call("POST", "/sessions", { displayName: name });
      csrf = s.json?.csrfToken ?? null;
      return s;
    },
    /* Hold the room event stream open alongside REST traffic and record time to first frame. */
    async openStream(roomId, onFrame, rateRetries=0) {
      const startedAt = performance.now();
      const controller = new AbortController();
      const headers = { Accept: "text/event-stream", Origin: apiBase() };
      if (cookie) headers["Cookie"] = cookie;
      const res = await fetch(`${apiBase()}/api/v1/rooms/${roomId}/stream`, {
        headers,
        signal: controller.signal,
      });
      const ttfb = performance.now() - startedAt;
      const state = {intentional:false, premature:false, lastVersion:0, frames:0};
      if (!res.ok) {
        const retry=Number(res.headers.get("Retry-After"));await res.body?.cancel();controller.abort();
        if(res.status===429 && rateRetries<3 && retry>0 && retry<=60){
          const delay=retry*1000+Math.floor(Math.random()*200);retryObservations.push({path:"stream",attempt:rateRetries+1,waitMs:delay});await new Promise(r=>setTimeout(r,delay));return this.openStream(roomId,onFrame,rateRetries+1);
        }
        return { ok: false, status: res.status, ttfb, close: () => controller.abort() };
      }
      // Drain in the background: an unread body stalls the server's write side and the
      // measurement would be meaningless.
      (async () => {
        const reader = res.body?.getReader();
        if (!reader) return;
        const decoder = new TextDecoder();
        let buffer="";
        try {
          for (;;) {
            const { done, value } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, {stream:true}).replaceAll("\r\n","\n");
            let boundary;
            while ((boundary=buffer.indexOf("\n\n"))>=0) {
              const frame=buffer.slice(0,boundary);buffer=buffer.slice(boundary+2);
              const data=frame.split("\n").filter(l=>l.startsWith("data:")).map(l=>l.slice(5).trimStart()).join("\n");
              if(data){try{const message=JSON.parse(data);state.lastVersion=Math.max(state.lastVersion,Number(message.roomVersion)||0);state.frames++;onFrame?.(message);}catch{/* Heartbeat or non-JSON frame. */}}
            }
          }
        } catch {
          /* Outcome is recorded below; planned close is explicitly marked. */
        } finally { if(!state.intentional)state.premature=true; }
      })();
      const stream={ ok: true, status: res.status, ttfb, state, close: () => {state.intentional=true;controller.abort();} };
      allOpenedStreams.push(stream);return stream;
    },
  };
}

const key = () => crypto.randomUUID().replace(/-/g, "");

/**
 * 压测用角色池。
 *
 * 必须每次换角色：经典模式拒绝「同回合重复猜同一角色」（422 duplicate_guess），
 * 一直提交同一个 ID 会把正常的业务校验变成假的错误率。
 * 这些 ID 都来自内容包，保证存在。
 */
/**
 * 这些是服务端按规则正确拒绝的响应，不是故障。
 *
 * - duplicate_guess：同一回合重复猜同一角色（压测随机抽到重复）
 * - round_finished：个人可猜错次数用尽（回合仍在 collecting，其他人可继续）
 * - round-not-collecting：回合已 locked/revealed（重开局过程中的正常窗口）
 */
const BUSINESS_REJECTION_CODES = new Set([
  "duplicate_guess",
  "round_finished",
  "round-not-collecting",
  "round-paused",
  "not-your-turn",
  "out_of_turn",
  "condition_not_satisfied",
]);

function isBusinessRejection(errorCode) {
  if (!errorCode) return false;
  const code = String(errorCode);
  for (const business of BUSINESS_REJECTION_CODES) {
    if (code.includes(business)) return true;
  }
  return false;
}

const CHARACTER_POOL = [
  "1001", "1002", "1003", "1004", "1005", "1006", "1007", "1008",
  "1009", "1010", "1011", "1012", "1013", "1014", "1015", "1016",
  "1017", "1018", "1019", "1020", "1021", "1022", "1023", "1024",
];

/* ---------------- 资源采样 ---------------- */
async function sampleResources() {
  const out = { at: new Date().toISOString(), serverRssKb: null, serverThreads: null, goroutines: null, activeConnections: null };
  // 通过 ps 采样服务进程 RSS（本机单实例）
  try {
    const { execFile } = await import("node:child_process");
    const { promisify } = await import("node:util");
    const exec = promisify(execFile);
    /*
     * macOS 的 ps 没有 thcount（Linux 才有），会打印帮助文本。
     * 改用两条独立命令：rss 用 ps -o rss=，线程数用 ps -M 数行。
     */
    /*
     * 只认真正监听 8080 的进程。
     *
     * `pgrep -f 'bin/uma-server'` 会同时命中：
     * - 启动它的 shell 包装进程（RSS 只有 1.6MB，会得出荒谬的数字）
     * - 别的端口上的其它实例（本机可能有 /tmp/uma-server 跑在 18123）
     * 所以先按端口找 PID，再读它的 RSS。
     */
    /*
     * 端口必须从 apiBase() 推导，不能写死 8080。
     *
     * 写死时，只要压测跑在别的端口（例如 18772），lsof 就查不到 PID，
     * RSS 与线程数永远是 undefined，报告里显示成 `RSS ?KB threads=?`——
     * 而这份报告正是用来判断「容量有没有撑住」的，资源趋势空着等于少了一半结论。
     * 2026-11-11 实测踩到，与容量硬编码属同一类「脚本假设了某个部署参数」。
     */
    const port = (() => {
      try {
        return String(new URL(apiBase()).port || (new URL(apiBase()).protocol === "https:" ? 443 : 80));
      } catch {
        return "";
      }
    })();
    const { stdout: rssOut } = await exec("bash", [
      "-c",
      `PID=$(lsof -nP -iTCP:${port} -sTCP:LISTEN -t 2>/dev/null | head -1); ` +
        "[ -n \"$PID\" ] && ps -o rss= -p $PID 2>/dev/null || true",
    ]);
    const rss = Number(rssOut.trim());
    if (Number.isFinite(rss) && rss > 0) out.serverRssKb = rss;

    const { stdout: thOut } = await exec("bash", [
      "-c",
      `PID=$(lsof -nP -iTCP:${port} -sTCP:LISTEN -t 2>/dev/null | head -1); ` +
        "[ -n \"$PID\" ] && ps -M -p $PID 2>/dev/null | tail -n +2 | wc -l || true",
    ]);
    const threads = Number(thOut.trim());
    if (Number.isFinite(threads) && threads > 0) out.serverThreads = threads;
  } catch {
    /* 采样失败不影响压测 */
  }
  // 通过 /metrics 取 goroutine 数（若启用）
  try {
    const res = await fetch(`${apiBase()}/metrics`, { signal: AbortSignal.timeout(3000) });
    if (res.ok) {
      const text = await res.text();
      const match = text.match(/^uma_runtime_goroutines\s+(\d+)/m);
      const active = text.match(/^uma_realtime_connections\s+(\d+)/m);
      if(active)out.activeConnections=Number(active[1]);
      if (match) out.goroutines = Number(match[1]);
    }
  } catch {
    /* /metrics 可能只允许本机或未启用 */
  }
  return out;
}

/* ---------------- 主流程 ---------------- */
async function main() {
  await mkdir(EVIDENCE, { recursive: true });

  const health = await fetch(`${apiBase()}/health/ready`).catch(() => null);
  if (!health || !health.ok) {
    console.error(`[load] 后端未就绪：${apiBase()}/health/ready`);
    process.exitCode = 1;
    return;
  }

  const startedAt = Date.now();
  const report = activeReport = {
    generatedAt: new Date().toISOString(),
    /** 明确口径：本机单实例，不是生产容量承诺 */
    disclaimer:
      "本机单实例实测（localhost、同机网络栈、无真实弱网、无真实移动设备）。" +
      "结果不代表生产容量承诺，也不能替代真实 LAN/Wi-Fi 环境验证。",
    config: { ...args, apiBase: apiBase() },
    hardware:{platform:platform(),release:release(),arch:arch(),cpu:cpus()[0]?.model,cpuCount:cpus().length,totalMemoryBytes:totalmem(),node:process.version},
    /**
     * What this run does NOT cover, stated in the artifact itself.
     *
     * A capacity report is read by people who will not re-read the script. Leaving a
     * gap implicit is how "getHostRoom: 0" previously read as a measurement when it
     * was an absence. Every gap is named here so the numbers cannot be quoted as
     * broader coverage than they are.
     */
    coverageGaps: [
      "实际活动总榜未测：getHostRoom测的是主持房间快照，不是活动standings；活动混合负载另测",
      "断线重连未测：流在阶段内保持打开，未模拟中断后按 cursor 重放；该场景由 scripts/e2e-reconnect.mjs 单独覆盖",
      "主持推进（transitions）只在重开局时触发，未做持续主持负载",
      "弱网、真实移动设备、投影设备、LCP/INP 均未测",
      "单实例、同机网络栈：客户端与服务端共处一机，会低估服务端开销",
    ],
    resources: [],
    rounds: [],
    errors: [],
  };

  console.log(
    `[load] 开始：${args.players} 玩家 + ${args.spectators} 观众 / ${args.rooms} 房间 / ${args.minutes} 分钟`,
  );
  console.log(`[load] 口径：本机单实例，非生产容量承诺`);

  // 初始资源采样
  const baseline = await sampleResources();
  report.resources.push({ ...baseline, phase: "baseline" });
  console.log(`[load] 基线 RSS=${baseline.serverRssKb ?? "?"}KB threads=${baseline.serverThreads ?? "?"}`);

  const playersPerRoom = Math.max(1, Math.floor(args.players / args.rooms));
  const spectatorsPerRoom = Math.max(0, Math.floor(args.spectators / args.rooms));

  /*
   * 每房间人数必须 ≤ 模式的 MaxSubjects，否则开局 422 too-many-participants，
   * 后续所有提交都打空。实测上限：classic=16、memory/draft/co-race-path=8、
   * sort/race-rebuild=6。
   *
   * 这是玩法设计约束，不是系统容量上限——报告里必须写清这一点，
   * 否则会被误读成「系统只能撑这么多」。
   */
  const MODE_MAX_SUBJECTS = 16; // 本压测用 classic
  if (playersPerRoom > MODE_MAX_SUBJECTS) {
    console.error(
      `[load] 每房间 ${playersPerRoom} 人超过 classic 上限 ${MODE_MAX_SUBJECTS}，开局会 422。` +
        `请调整 --players/--rooms（例如 ${args.rooms} 房间 × ${MODE_MAX_SUBJECTS} 人 = ${args.rooms * MODE_MAX_SUBJECTS} 人）。`,
    );
    process.exitCode = 1;
    return;
  }

  // Read room and spectator limits from the instance before creating load
  const limitsRes = await fetch(`${apiBase()}/api/v1/config`, {
    headers: { Accept: "application/json", Origin: apiBase() },
  }).catch(() => null);
  const limitsJson = limitsRes && limitsRes.ok ? await limitsRes.json().catch(() => null) : null;
  const instanceMaxMembers = Number(limitsJson?.maxRoomMembers) || 0;
  const instanceMaxSpectators = Number(limitsJson?.maxRoomSpectators) || 0;
  if (!instanceMaxMembers) {
    console.error(
      "[load] 读不到实例的房间容量上限（/api/v1/config 的 maxRoomMembers）。" +
        "无法保证建房不被拒，停止。",
    );
    process.exitCode = 1;
    return;
  }
  // 每房实际需要的人数（含房主）不能超过实例上限。
  const neededPerRoom = playersPerRoom + 1;
  if (neededPerRoom > instanceMaxMembers) {
    console.error(
      `[load] 每房间需要 ${neededPerRoom} 人（${playersPerRoom} 选手 + 房主），` +
        `超过本实例上限 ${instanceMaxMembers}。` +
        `请减小 --players 或增大 --rooms，或在部署策略里调高 playersPerRoom。`,
    );
    process.exitCode = 1;
    return;
  }
  if (spectatorsPerRoom > instanceMaxSpectators) {
    console.error(
      `[load] 每房间需要 ${spectatorsPerRoom} 名观众，超过本实例上限 ${instanceMaxSpectators}。` +
        `请减小 --spectators 或增大 --rooms，或在部署策略里调高 spectatorsPerRoom。`,
    );
    process.exitCode = 1;
    return;
  }
  report.limits = {
    instanceMaxMembers,
    instanceMaxSpectators,
    neededMembersPerRoom: neededPerRoom,
    neededSpectatorsPerRoom: spectatorsPerRoom,
  };
  console.log(
    `[load] 实例上限：每房 ${instanceMaxMembers} 人 / ${instanceMaxSpectators} 观众；` +
      `本次每房 ${neededPerRoom} 人 / ${spectatorsPerRoom} 观众`,
  );

  const roomIds = [];

  /* ---- 阶段 1：建房 + 成员加入（集中入场） ---- */
  console.log("[load] 阶段 1/4：集中入场（建房 + 加入）");
  for (let roomIndex = 0; roomIndex < args.rooms; roomIndex += 1) {
    const host = makeClient();
    const sessionRes = await host.newSession(`房主${roomIndex + 1}`);
    recorders.createSession.record(sessionRes.ms, (sessionRes.status < 200 || sessionRes.status >= 300) ? `HTTP_${sessionRes.status}` : null);
    if (sessionRes.status !== 201) {
      report.errors.push({ phase: "session", room: roomIndex, status: sessionRes.status });
      continue;
    }

    const roomRes = await host.call(
      "POST",
      "/rooms",
      {
        name: `压测房间${roomIndex + 1}`,
        kind: "event",
        modeConfig: {
          modeId: "classic",
          maxWrongGuesses: 8,
          winPolicy: "window-attempts",
          durationSeconds: 0,
          shadowEnabled: false,
        },
        maxPlayers: Math.min(instanceMaxMembers, 200),
        maxSpectators: Math.min(instanceMaxSpectators, 300),
        visibility: "invite",
        allowSpectators: true,
      },
      key(),
    );
    recorders.createRoom.record(roomRes.ms, (roomRes.status < 200 || roomRes.status >= 300) ? `HTTP_${roomRes.status}` : null);
    if (roomRes.status !== 201) {
      // 打印出来：静默记录会让「加入完成：0 个会话」看起来像网络问题。
      console.error(
        `[load] 建房失败 room=${roomIndex + 1} status=${roomRes.status} ` +
          `detail=${roomRes.json?.detail ?? roomRes.networkError ?? ""}`,
      );
      report.errors.push({ phase: "room", room: roomIndex, status: roomRes.status });
      continue;
    }
    const roomId = roomRes.json?.room?.roomId;
    const inviteCode = roomRes.json?.room?.inviteCode;

    /*
     * 房主必须改成观众。
     *
     * classic 的 MaxSubjects=16 只数 player/team-captain（观众被跳过），
     * 但房主默认是 player —— 若每房间 16 个选手 + 1 个房主，就是 17 个 subject，
     * 开局直接 422 too-many-participants。
     *
     * 实测症状：`[load] 开局完成：0 个回合`，10 个房间全部失败，
     * 然后阶段 3 因为 rounds 为空而崩溃（Cannot read properties of undefined）。
     */
    {
      const cur = await host.call("GET", `/rooms/${roomId}`);
      const self = (cur.json?.members ?? []).find((m) => m.displayName === `房主${roomIndex + 1}`);
      if (self) {
        await host.call(
          "PATCH",
          `/rooms/${roomId}/memberships/${self.membershipId}`,
          { expectedRoomVersion: cur.json?.roomVersion, requestedRole: "spectator" },
          key(),
        );
      }
    }

    roomIds.push({ roomId, inviteCode, host });
  }

  // 并发加入（每个成员独立客户端）
  const joinTasks = [];
  for (let roomIndex = 0; roomIndex < roomIds.length; roomIndex += 1) {
    const room = roomIds[roomIndex];
    for (let memberIndex = 0; memberIndex < playersPerRoom; memberIndex += 1) {
      joinTasks.push(async () => {
        const client = makeClient();
        const s = await client.newSession(`选手${roomIndex + 1}-${memberIndex + 1}`);
        recorders.createSession.record(s.ms, (s.status < 200 || s.status >= 300) ? `HTTP_${s.status}` : null);
        if (s.status !== 201) return null;
        const join = await client.call(
          "POST",
          `/rooms/${room.roomId}/memberships`,
          { inviteCode: room.inviteCode, requestedRole: "player" },
          key(),
        );
        recorders.joinRoom.record(join.ms, (join.status < 200 || join.status >= 300) ? `HTTP_${join.status}` : null);
        if (join.status !== 201) return null;
        return { client, roomId: room.roomId, name: `选手${roomIndex + 1}-${memberIndex + 1}` };
      });
    }
    for (let memberIndex = 0; memberIndex < spectatorsPerRoom; memberIndex += 1) {
      joinTasks.push(async () => {
        const client = makeClient();
        const s = await client.newSession(`观众${roomIndex + 1}-${memberIndex + 1}`);
        recorders.createSession.record(s.ms, (s.status < 200 || s.status >= 300) ? `HTTP_${s.status}` : null);
        if (s.status !== 201) return null;
        const join = await client.call(
          "POST",
          `/rooms/${room.roomId}/memberships`,
          { inviteCode: room.inviteCode, requestedRole: "spectator" },
          key(),
        );
        recorders.joinRoom.record(join.ms, (join.status < 200 || join.status >= 300) ? `HTTP_${join.status}` : null);
        return join.status === 201 ? { client, roomId: room.roomId, name: `观众${roomIndex + 1}-${memberIndex + 1}` } : null;
      });
    }
  }

  // 有界并发，避免本机文件句柄耗尽（这不是服务端容量，是压测客户端限制）
  const CONCURRENCY = 32;
  const joined = [];
  for (let index = 0; index < joinTasks.length; index += CONCURRENCY) {
    const batch = joinTasks.slice(index, index + CONCURRENCY);
    const results = await Promise.all(batch.map((task) => task()));
    for (const result of results) if (result) joined.push(result);
  }
  const playerCount = joined.filter((j) => j.name.startsWith("选手")).length;
  console.log(`[load] 加入完成：${joined.length} 个会话（选手 ${playerCount}）`);

  report.resources.push({ ...(await sampleResources()), phase: "after-join" });

  /* ---- 阶段 2：开局 ---- */
  console.log("[load] 阶段 2/4：各房间开局");
  const rounds = [];
  for (const room of roomIds) {
    const roundRes = await room.host.call(
      "POST",
      `/rooms/${room.roomId}/rounds`,
      {
        modeConfig: {
          modeId: "classic",
          maxWrongGuesses: 8,
          winPolicy: "window-attempts",
          durationSeconds: 0,
          shadowEnabled: false,
        },
      },
      key(),
    );
    recorders.createRound.record(roundRes.ms, (roundRes.status < 200 || roundRes.status >= 300) ? `HTTP_${roundRes.status}` : null);
    if (roundRes.status !== 201) continue;
    const roundId = roundRes.json?.roundId;
    let phaseVersion = roundRes.json?.phaseVersion ?? 1;
    for (const target of ["briefing", "collecting"]) {
      const t = await room.host.call(
        "POST",
        `/rounds/${roundId}/transitions`,
        { target, expectedPhaseVersion: phaseVersion },
        key(),
      );
      phaseVersion = t.json?.phaseVersion ?? phaseVersion + 1;
    }
    rounds.push({ roomId: room.roomId, roundId, phaseVersion, host: room.host });
  }
  console.log(`[load] 开局完成：${rounds.length} 个回合`);

  /* ---- 阶段 3：持续提交（观察延迟与错误率随时间变化） ---- */
  console.log(`[load] 阶段 3/4：持续提交 ${args.minutes} 分钟`);
  let deadline;
  const sampleInterval = 60_000; // 每分钟采样一次资源
  let nextResourceSample = Date.now() + sampleInterval;
  let roundIndex = 0;
  let submitCount = 0;
  /** 因额度耗尽被拒绝的次数：这是正常业务状态，不计入错误率 */
  let exhaustedCount = 0;

  if (rounds.length === 0) {
    console.error("[load] 没有任何回合建立成功，无法进入持续提交阶段。检查开局失败原因（通常是每房间人数超过模式上限）。");
    report.errors.push({ phase: "rounds", note: "no rounds created" });
    process.exitCode = 1;
    return;
  }

  /*
   * Hold one event stream open per room for the whole submit phase.
   *
   * A capacity claim that only measures REST is incomplete: a server can answer every
   * write and still drop subscribers, and a viewer would see a frozen screen. Each
   * room's host opens the stream so the connection count matches the audience model,
   * and the first-frame latency is recorded as streamConnect.
   */
  const streams = [];
  let streamFrames = 0;
  const onFrame = () => {
    streamFrames += 1;
  };
  for (const round of rounds) {
    try {
      const stream = await round.host.openStream(round.roomId, onFrame);
      recorders.streamConnect.record(stream.ttfb, stream.ok ? null : `HTTP_${stream.status}`);
      if (stream.ok) streams.push(stream);
    } catch (err) {
      recorders.streamConnect.record(0, `ERR:${err?.name ?? "unknown"}`);
    }
  }

  /* Open a stream for each spectator in bounded batches to measure actual audience connections. */
  const spectatorStreams = [];
  const spectators = joined.filter((j) => j.name.startsWith("观众"));
  const STREAM_BATCH = 16;
  for (let index = 0; index < spectators.length; index += STREAM_BATCH) {
    const batch = spectators.slice(index, index + STREAM_BATCH);
    const opened = await Promise.all(
      batch.map(async (spectator) => {
        try {
          const stream = await spectator.client.openStream(spectator.roomId, onFrame);
          recorders.streamConnect.record(stream.ttfb, stream.ok ? null : `HTTP_${stream.status}`);
          return stream.ok ? stream : null;
        } catch (err) {
          recorders.streamConnect.record(0, `ERR:${err?.name ?? "unknown"}`);
          return null;
        }
      }),
    );
    for (const stream of opened) if (stream) spectatorStreams.push(stream);
  }
  console.log(
    `[load] 已建立 ${streams.length}/${rounds.length} 条房主流 + ${spectatorStreams.length}/${spectators.length} 条观众流`,
  );

  const playerStreams=[];
  const players=joined.filter(j=>j.name.startsWith("选手"));
  for(let i=0;i<players.length;i+=STREAM_BATCH){
    const opened=await Promise.all(players.slice(i,i+STREAM_BATCH).map(async p=>{
      try{const stream=await p.client.openStream(p.roomId,onFrame);recorders.streamConnect.record(stream.ttfb,stream.ok?null:`HTTP_${stream.status}`);return stream.ok?{...stream,roomId:p.roomId}:null;}catch(e){recorders.streamConnect.record(0,`ERR:${e.name}`);return null;}
    }));
    playerStreams.push(...opened.filter(Boolean));
  }
  console.log(`[load] 选手实时连接 ${playerStreams.length}/${players.length}`);
  if(playerCount!==args.players || spectators.length!==args.spectators || rounds.length!==args.rooms || playerStreams.length!==args.players || spectatorStreams.length!==args.spectators){throw new Error("incomplete setup: refusing a partial-capacity run");}
  const steadyStartedAt=Date.now();deadline=steadyStartedAt+args.minutes*60000;
  report.resources.push({...(await sampleResources()),phase:"all-streams-open"});
  while (Date.now() < deadline) {
    const round = rounds[roundIndex % rounds.length];
    const roomPlayers = joined.filter((j) => j.roomId === round.roomId && j.name.startsWith("选手"));
    if (roomPlayers.length === 0) {
      roundIndex += 1;
      continue;
    }

    // 每轮让该房间的选手各提交一次（并发）
    const submits = roomPlayers.map(async (player) => {
      const view = await player.client.call("GET", `/rounds/${round.roundId}`);
      recorders.getRound.record(view.ms, (view.status < 200 || view.status >= 300) ? `HTTP_${view.status}` : null);
      if (view.status !== 200) return;

      const characterId = CHARACTER_POOL[Math.floor(Math.random() * CHARACTER_POOL.length)];
      const submit = await player.client.call(
        "POST",
        `/rounds/${round.roundId}/submissions`,
        {
          phaseVersion: view.json?.round?.phaseVersion ?? round.phaseVersion,
          submissionRevision: view.json?.submissionRevision ?? 0,
          controlGeneration: view.json?.controlGeneration ?? 0,
          modeAction: { kind: "classic.guess", characterId },
        },
        key(),
      );
      const code = submit.json?.code ?? "";

      /*
       * `round_finished` 不是失败——是这个人的额度用完了（回合 phase 仍是
       * collecting，只是 self.failed=true）。压测要持续压力，所以把它单独计数，
       * 不混进错误率，并在累计到阈值时重开一局。
       */
      if (code === "round_finished") exhaustedCount += 1;
      const errorCode = (submit.status < 200 || submit.status >= 300) ? `HTTP_${submit.status}:${code || "unknown"}` : null;
      recorders.submitGuess.record(submit.ms, errorCode);
      if (submit.status >= 200 && submit.status < 300) {
        submitCount += 1;
        // Receipt time -> at least one real participant subscription sees its version.
        const start=performance.now(),version=submit.json?.roomVersion;
        const relevant=playerStreams.filter(s=>s.roomId===round.roomId);
        while(Number.isFinite(version) && !relevant.some(s=>s.state.lastVersion>=version) && performance.now()-start<3000)await new Promise(r=>setTimeout(r,5));
        recorders.submissionVisible.record(performance.now()-start,Number.isFinite(version)&&relevant.some(s=>s.state.lastVersion>=version)?null:"notification-not-visible");
      }
    });

    await Promise.all(submits);

    /* Read the room view each cycle. These rooms have no event, so event-scoped standings remain a coverage gap. */
    const standings = await round.host.call("GET", `/rooms/${round.roomId}`);
    recorders.getHostRoom.record(
      standings.ms,
      (standings.status < 200 || standings.status >= 300) ? `HTTP_${standings.status}` : null,
    );

    /*
     * 额度耗尽 → 重开一局，让压力持续。
     * 阈值设成「本轮房间选手数」，即所有人都耗尽后重开。
     */
    if (exhaustedCount >= roomPlayers.length) {
      /*
       * 重开局前必须把上一局推到 settled。
       *
       * 实测：只推到 locked 就建房，`POST /rooms/{id}/rounds` 会 409
       * （房间只允许一个未完结的回合），导致 245 次重开全部失败。
       * 必须走完 locked → revealed → settled。
       */
      let probe = await round.host.call("GET", `/rounds/${round.roundId}`);
      for (const target of ["locked", "revealed", "settled"]) {
        const phase = probe.json?.round?.phase;
        if (phase === "settled" || phase === "cancelled") break;
        const t = await round.host.call(
          "POST",
          `/rounds/${round.roundId}/transitions`,
          { target, expectedPhaseVersion: probe.json?.round?.phaseVersion ?? 1 },
          key(),
        );
        if ((t.status < 200 || t.status >= 300)) break;
        probe = await round.host.call("GET", `/rounds/${round.roundId}`);
      }

      const fresh = await round.host.call(
        "POST",
        `/rooms/${round.roomId}/rounds`,
        {
          modeConfig: {
            modeId: "classic",
            maxWrongGuesses: 8,
            winPolicy: "window-attempts",
            durationSeconds: 0,
            shadowEnabled: false,
          },
        },
        key(),
      );
      recorders.createRound.record(fresh.ms, (fresh.status < 200 || fresh.status >= 300) ? `HTTP_${fresh.status}` : null);
      if (fresh.status === 201) {
        let freshPv = fresh.json?.phaseVersion ?? 1;
        for (const target of ["briefing", "collecting"]) {
          const t = await round.host.call(
            "POST",
            `/rounds/${fresh.json.roundId}/transitions`,
            { target, expectedPhaseVersion: freshPv },
            key(),
          );
          freshPv = t.json?.phaseVersion ?? freshPv + 1;
        }
        round.roundId = fresh.json.roundId;
        round.phaseVersion = freshPv;
        report.reopenedRounds = (report.reopenedRounds ?? 0) + 1;
        exhaustedCount = 0;
        console.log(`[load] 额度耗尽，已重开第 ${report.reopenedRounds} 局`);
      }
    }

    // 顺带读房间（模拟观众/展示端刷新）
    const roomReads = joined
      .filter((j) => j.name.startsWith("观众") && j.roomId === round.roomId)
      .slice(0, 10)
      .map(async (spectator) => {
        const read = await spectator.client.call("GET", `/rooms/${round.roomId}`);
        recorders.getRoom.record(read.ms, (read.status < 200 || read.status >= 300) ? `HTTP_${read.status}` : null);
      });
    await Promise.all(roomReads);

    if (Date.now() >= nextResourceSample) {
      const sample = await sampleResources();
      report.resources.push({ ...sample, phase: "steady", submitsSoFar: submitCount });
      console.log(
        `[load] 采样 RSS=${sample.serverRssKb ?? "?"}KB threads=${sample.serverThreads ?? "?"} goroutines=${sample.goroutines ?? "?"} submits=${submitCount}`,
      );
      nextResourceSample = Date.now() + sampleInterval;
    }

    roundIndex += 1;
    await new Promise((r) => setTimeout(r, 200));
  }

  report.steadyDurationMs=Date.now()-steadyStartedAt;
  report.prematureStreamCloses=[...streams,...spectatorStreams,...playerStreams].filter(s=>s.state?.premature).length;
  for(const stream of playerStreams)stream.close();
  for (const stream of streams) stream.close();
  for (const stream of spectatorStreams) stream.close();
  report.streamFrames = streamFrames;
  report.streamsHeld = { hosts: streams.length, spectators: spectatorStreams.length,players:playerStreams.length };

  /*
   * Connection release.
   *
   * Closing a stream is only half the claim: the point is that the server lets the
   * connection go. After closing every stream, the server's own goroutine count must
   * come back down rather than staying at the peak. This is a bounded check with a
   * short grace period, not a leak proof for a 60-minute run.
   */
  let afterClose=await sampleResources();
  const releaseDeadline=Date.now()+5000;
  while(afterClose.activeConnections!==0 && Date.now()<releaseDeadline){await new Promise(r=>setTimeout(r,100));afterClose=await sampleResources();}
  report.streamRelease = {
    closed: streams.length + spectatorStreams.length + playerStreams.length,
    activeAfter:afterClose.activeConnections,
    goroutinesAtClose: afterClose.goroutines ?? null,
    note: "关闭全部实时流后采样；数值回落到接近基线表示连接被释放，单次采样不是泄漏证明",
  };

  /* ---- 阶段 4：收尾采样 ---- */
  console.log("[load] 阶段 4/4：收尾");
  const final = await sampleResources();
  report.resources.push({ ...final, phase: "final", totalSubmits: submitCount });

  /* ---- 汇总 ---- */
  const summaries = Object.values(recorders).map((r) => r.summary());
  report.summaries = summaries;
  report.durationMs = Date.now() - startedAt;
  report.joined = { total: joined.length, players: playerCount, spectators: joined.length - playerCount };
  report.roomsCreated = roomIds.length;
  report.roundsCreated = rounds.length;
  report.totalSubmits = submitCount;

  // 资源趋势：首末对比，判断是否单调增长
  const rssSamples = report.resources.map((r) => r.serverRssKb).filter((v) => typeof v === "number");
  const threadSamples = report.resources.map((r) => r.serverThreads).filter((v) => typeof v === "number");
  report.resourceTrend = {
    rssKb: {
      first: rssSamples[0] ?? null,
      last: rssSamples[rssSamples.length - 1] ?? null,
      deltaKb:
        rssSamples.length >= 2 ? (rssSamples[rssSamples.length - 1] ?? 0) - (rssSamples[0] ?? 0) : null,
      samples: rssSamples,
    },
    threads: {
      first: threadSamples[0] ?? null,
      last: threadSamples[threadSamples.length - 1] ?? null,
      delta: threadSamples.length >= 2 ? (threadSamples[threadSamples.length - 1] ?? 0) - (threadSamples[0] ?? 0) : null,
      samples: threadSamples,
    },
  };

  report.complete=true;
  report.rateLimitBackoff=retryObservations;
  report.conclusion=assessCapacity(report);
  report.conclusion.metTarget=report.conclusion.passed?"基础档达标":"未达基础档";

  // --out is honoured; the default keeps the historical name.
  //
  // It used to be recorded in the report while the write path stayed hard-coded to
  // load-test.json, so every run silently overwrote the same file. A 60-minute baseline
  // then looked like it had never been produced, because the path asked for on the
  // command line was never written.
  const out = args.out ? resolve(args.out) : resolve(EVIDENCE, "load-test.json");
  await writeFile(out, `${JSON.stringify(report, null, 2)}\n`, "utf8");

  console.log("\n[load] ==== 延迟汇总 ====");
  for (const s of summaries) {
    if (s.total === 0) continue;
    console.log(
      `  ${s.operation.padEnd(16)} n=${String(s.ok).padStart(5)} 真失败=${String(s.errors).padStart(3)} (${(s.errorRate * 100).toFixed(2)}%) 业务拒绝=${String(s.businessRejections).padStart(4)} p50=${s.p50 ?? "-"}ms p95=${s.p95 ?? "-"}ms p99=${s.p99 ?? "-"}ms`,
    );
  }
  console.log("\n[load] ==== 资源趋势 ====");
  console.log(
    `  RSS ${report.resourceTrend.rssKb.first ?? "?"}KB → ${report.resourceTrend.rssKb.last ?? "?"}KB (Δ${report.resourceTrend.rssKb.deltaKb ?? "?"}KB)`,
  );
  console.log(
    `  threads ${report.resourceTrend.threads.first ?? "?"} → ${report.resourceTrend.threads.last ?? "?"} (Δ${report.resourceTrend.threads.delta ?? "?"})`,
  );
  console.log(`\n[load] 报告：${out}`);

  // 有错误就非零退出（真实反映）
  const totalErrors = summaries.reduce((sum, s) => sum + s.errors, 0);
  if (totalErrors > 0 || report.errors.length>0 || report.prematureStreamCloses>0 || report.streamRelease.activeAfter!==0) {
    console.error(`[load] 共 ${totalErrors} 个请求失败，详见报告`);
    process.exitCode = 1;
  }
}

try {await main();}catch(error){
  process.exitCode=1;
  const report=activeReport??{errors:[]};report.errors.push({fatal:String(error)});report.rateLimitBackoff=retryObservations;report.complete=false;
  await writeFile(args.out?resolve(args.out):resolve(EVIDENCE,"load-test.json"),JSON.stringify(report,null,2));console.error(error);
}finally{for(const stream of allOpenedStreams)stream.close();}
