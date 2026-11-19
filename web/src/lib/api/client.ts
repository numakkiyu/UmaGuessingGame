import i18n from "@/i18n";
import { hasErrorMessage, hasDetailedErrorMessage, type ErrorMessageKey } from "@/i18n/errors";

import type { Problem, ProblemFieldError } from "./types";
import { SessionStateError } from "./session-errors";

/** 契约服务器地址 */
export const API_BASE = "/api/v1";

/** 默认超时。写请求超时后结果未知，调用方必须按「正在确认提交结果」处理。 */
const DEFAULT_TIMEOUT_MS = 15_000;

export type RequestId = string;

/** API 失败的结构化表示，页面据此区分 forbidden / error / 其他状态。 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId: RequestId | null;
  readonly retryable: boolean;
  readonly fieldErrors: readonly ProblemFieldError[];
  readonly problem: Problem | null;

  constructor(init: {
    status: number;
    code: string;
    message: string;
    requestId: RequestId | null;
    retryable?: boolean;
    fieldErrors?: readonly ProblemFieldError[];
    problem?: Problem | null;
  }) {
    super(init.message);
    this.name = "ApiError";
    this.status = init.status;
    this.code = init.code;
    this.requestId = init.requestId;
    this.retryable = init.retryable ?? false;
    this.fieldErrors = init.fieldErrors ?? [];
    this.problem = init.problem ?? null;
  }

  /** 403：权限不足或被撤销（页面必须显示 forbidden，而不是「暂无数据」） */
  get isForbidden(): boolean {
    return this.status === 403;
  }

  /** 401：身份失效 */
  get isUnauthenticated(): boolean {
    return this.status === 401;
  }

  get isNotFound(): boolean {
    return this.status === 404;
  }

  /**
   * 422 是「你的请求本身有问题」，不是「服务暂时坏了」。
   *
   * 服务端用 422 + 明确 code 拒绝可纠正的业务错误（重复的马、超预算、文本不合规…）。
   * 把它说成「再试一次通常就好」会让玩家反复重试同一个错误组合，永远改不对。
   */
  get isUnprocessable(): boolean {
    return this.status === 422;
  }

  /** 409/422：状态或业务拒绝，不重试 */
  get isConflict(): boolean {
    return this.status === 409 || this.status === 422;
  }

  /** 429/503：可重试的暂忙 */
  get isTransient(): boolean {
    return this.status === 429 || this.status === 503;
  }
}

/** 网络层失败（离线、DNS、连接被拒），与 HTTP 失败区分 */
export class NetworkError extends Error {
  override readonly cause: unknown;
  constructor(message: string, cause: unknown) {
    super(message);
    this.name = "NetworkError";
    this.cause = cause;
  }
}

/** 客户端主动取消（路由切换、StrictMode 重挂载） */
export class AbortedError extends Error {
  constructor() {
    super("Request aborted");
    this.name = "AbortedError";
  }
}

/** 超时：结果未知，调用方必须提示「正在确认提交结果」而不是失败 */
export class TimeoutError extends Error {
  readonly timeoutMs: number;
  constructor(timeoutMs: number) {
    super(`Request timed out after ${timeoutMs}ms; outcome unknown`);
    this.name = "TimeoutError";
    this.timeoutMs = timeoutMs;
  }
}

type ProblemBody = Partial<Problem>;

function isProblemBody(value: unknown): value is ProblemBody {
  return typeof value === "object" && value !== null && "title" in value && "code" in value;
}

function safeParseJson(text: string): unknown {
  if (!text) return null;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

/**
 * 生成 Idempotency-Key。
 * 至少 128 位随机性、16–128 个可打印安全字符。
 */
export function createIdempotencyKey(): string {
  const bytes = new Uint8Array(24);
  crypto.getRandomValues(bytes);
  let out = "";
  for (const byte of bytes) out += byte.toString(16).padStart(2, "0");
  return out;
}

/** 幂等键作用域：session + operationId + 资源 ID */
export function idempotencyScope(parts: {
  sessionId?: string | null;
  operationId: string;
  resourceId?: string | null;
}): string {
  return [parts.sessionId ?? "anon", parts.operationId, parts.resourceId ?? "-"].join(":");
}

export type ApiClientOptions = {
  /** 返回当前 CSRF token；写操作需要 */
  getCsrfToken?: () => string | null;
  /**
   * 写操作缺少 CSRF 时是否在客户端提前失败。
   * 打开后错误信息更准确（说明是刷新导致的凭据失效），
   * 关闭时交给服务端判定（创建会话、未登录探测需要）。
   */
  requireCsrf?: boolean;
  /** 记录每个响应的 X-Request-Id，供错误提示与诊断编号使用 */
  onRequestId?: (requestId: RequestId, info: { method: string; path: string }) => void;
  /** 请求被 401 时的回调（清私有缓存、断开订阅） */
  onUnauthenticated?: () => void;
  /** Recover only after a definite CSRF refusal, never after an unknown write. */
  onCsrfInvalid?: () => Promise<boolean>;
  onAdmissionRequired?: (reason?: "entry" | "traffic") => Promise<boolean>;
  fetchImpl?: typeof fetch;
};

export type RequestOptions = {
  /** Restricted display reads must not clear the independent player identity. */
  authScope?: "display";
  method?: "GET" | "POST" | "PATCH" | "DELETE" | "PUT";
  /** 查询参数；null/undefined 会被忽略 */
  query?: Record<string, string | number | boolean | null | undefined>;
  body?: unknown;
  /** 写请求的幂等键；同一逻辑动作重试必须复用同一个键 */
  idempotencyKey?: string;
  /** 覆盖默认超时 */
  timeoutMs?: number;
  signal?: AbortSignal;
  headers?: Record<string, string>;
  /** 该请求不需要 CSRF（仅创建会话等少数情况，见 ） */
  noCsrf?: boolean;
};

export type ApiResponse<T> = {
  data: T;
  status: number;
  requestId: RequestId | null;
  /** Location 头（201 创建） */
  location: string | null;
};

export type ApiClient = {
  request<T>(path: string, options?: RequestOptions): Promise<ApiResponse<T>>;
  get<T>(path: string, options?: Omit<RequestOptions, "method" | "body">): Promise<ApiResponse<T>>;
  post<T>(path: string, options?: Omit<RequestOptions, "method">): Promise<ApiResponse<T>>;
  patch<T>(path: string, options?: Omit<RequestOptions, "method">): Promise<ApiResponse<T>>;
  del<T>(path: string, options?: Omit<RequestOptions, "method" | "body">): Promise<ApiResponse<T>>;
};

function buildUrl(path: string, query?: RequestOptions["query"]): string {
  const normalized = path.startsWith("/") ? path : `/${path}`;
  const base = `${API_BASE}${normalized}`;
  if (!query) return base;
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === null || value === undefined) continue;
    params.append(key, String(value));
  }
  const qs = params.toString();
  return qs ? `${base}?${qs}` : base;
}

export function createApiClient(options: ApiClientOptions = {}): ApiClient {
  const fetchImpl = options.fetchImpl ?? globalThis.fetch.bind(globalThis);

  async function request<T>(path: string, requestOptions: RequestOptions = {}, mayRecover = true, mayAdmit = true): Promise<ApiResponse<T>> {
    const method = requestOptions.method ?? "GET";
    const url = buildUrl(path, requestOptions.query);
    const timeoutMs = requestOptions.timeoutMs ?? DEFAULT_TIMEOUT_MS;

    const headers: Record<string, string> = {
      Accept: "application/json, application/problem+json",
      ...requestOptions.headers,
    };

    const isWrite = method !== "GET";
    if (requestOptions.body !== undefined) {
      headers["Content-Type"] = "application/json";
    }
    // noCsrf 的写操作（创建会话）跳过 CSRF 逻辑：此时还没有 CSRF 可带
    if (isWrite && !requestOptions.noCsrf) {
      // 所有 cookie 写操作带 X-CSRF-Token
      const csrf = options.getCsrfToken?.() ?? null;
      if (csrf) {
        headers["X-CSRF-Token"] = csrf;
      } else if (options.requireCsrf) {
        /*
         * 只有显式要求 CSRF 时才提前失败。
         * 默认仍让请求发出去：创建会话（POST /sessions）本身不需要已有 CSRF，
         * 单元测试与未登录探测也需要真实的服务端应答。
         * 页面级调用方通过 requireCsrf 打开，以便在刷新后给出准确提示，
         * 而不是让用户看到后端返回的笼统 403。
         */
        // describeFailure maps this code to localized recovery guidance.
        throw new ApiError({
          status: 403,
          code: "CSRF_UNAVAILABLE",
          message: "CSRF_UNAVAILABLE",
          requestId: null,
          retryable: false,
        });
      }
      if (requestOptions.idempotencyKey) {
        headers["Idempotency-Key"] = requestOptions.idempotencyKey;
      }
    }

    const controller = new AbortController();
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, timeoutMs);

    // 合并外部取消信号（路由切换 / StrictMode 卸载）
    const onExternalAbort = () => controller.abort();
    if (requestOptions.signal) {
      if (requestOptions.signal.aborted) controller.abort();
      else requestOptions.signal.addEventListener("abort", onExternalAbort, { once: true });
    }

    let response: Response;
    let text: string;
    try {
      response = await fetchImpl(url, {
        method,
        headers,
        // 同源 cookie 必须携带
        credentials: "same-origin",
        body: requestOptions.body === undefined ? undefined : JSON.stringify(requestOptions.body),
        signal: controller.signal,
        // 私有 API 一律 no-store；浏览器缓存层不参与
        cache: "no-store",
        redirect: "error",
      });
      // Keep cancellation and the deadline active until the whole body arrives
      text = response.status === 304 || response.status === 204 ? "" : await response.text();
    } catch (error) {
      if (timedOut) throw new TimeoutError(timeoutMs);
      if (controller.signal.aborted) throw new AbortedError();
      throw new NetworkError("Network request failed", error);
    } finally {
      clearTimeout(timer);
      requestOptions.signal?.removeEventListener("abort", onExternalAbort);
    }

    const requestId = response.headers.get("X-Request-Id");
    if (requestId) {
      options.onRequestId?.(requestId, { method, path: url });
    }
    const location = response.headers.get("Location");

    if (response.status === 304) {
      return { data: null as T, status: 304, requestId, location };
    }

    if (response.status === 204) {
      return { data: null as T, status: 204, requestId, location };
    }

    const parsed = safeParseJson(text);

    if (!response.ok) {

      // 错误使用 application/problem+json，不返回 200 装业务失败
      const problem = isProblemBody(parsed) ? (parsed as Problem) : null;
      if (response.status === 401 && requestOptions.authScope !== "display" && problem?.code !== "account-required") options.onUnauthenticated?.();
      if (mayAdmit && (!isWrite || requestOptions.idempotencyKey) && response.status===403 && (problem?.code==="admission-required" || problem?.code==="admission-risk-required") && options.onAdmissionRequired) {
        if(requestOptions.signal?.aborted)throw new AbortedError();
        const signal=requestOptions.signal;
        let abort:()=>void=()=>undefined;
        const cancelled=new Promise<boolean>(resolve=>{abort=()=>resolve(false);signal?.addEventListener("abort",abort,{once:true})});
        const admitted=await Promise.race([options.onAdmissionRequired(problem?.code === "admission-risk-required" ? "traffic" : "entry"),cancelled]).finally(()=>signal?.removeEventListener("abort",abort));
        if(signal?.aborted)throw new AbortedError();
        if(admitted)return request<T>(path,requestOptions,mayRecover,false);
        throw new ApiError({status:403,code:"admission-cancelled",message:"Admission cancelled",requestId:null});
      }
      if (mayRecover && isWrite && !requestOptions.noCsrf && requestOptions.idempotencyKey &&
          response.status === 403 && problem?.code === "csrf-token-invalid" && options.onCsrfInvalid) {
        // The middleware refused this write before domain mutation. Recovery
        // retains its exact body and key, and is bounded to one additional try.
        if (!requestOptions.signal?.aborted && await options.onCsrfInvalid()) {
          if (requestOptions.signal?.aborted) throw new AbortedError();
          return request<T>(path, requestOptions, false, mayAdmit);
        }
      }
      throw new ApiError({
        status: response.status,
        code: problem?.code ?? `HTTP_${response.status}`,
        message: problem?.title ?? `HTTP ${response.status}`,
        requestId: problem?.requestId ?? requestId,
        retryable: problem?.retryable ?? (response.status === 429 || response.status === 503),
        fieldErrors: problem?.fieldErrors ?? [],
        problem,
      });
    }

    if (parsed === null && text.length > 0) {
      // 200 但响应体不是 JSON：不当成正常快照
      throw new ApiError({
        status: response.status,
        code: "INVALID_RESPONSE",
        message: "Invalid JSON response",
        requestId,
      });
    }

    return { data: parsed as T, status: response.status, requestId, location };
  }

  return {
    request,
    get: (path, opts) => request(path, { ...opts, method: "GET" }),
    post: (path, opts) => request(path, { ...opts, method: "POST" }),
    patch: (path, opts) => request(path, { ...opts, method: "PATCH" }),
    del: (path, opts) => request(path, { ...opts, method: "DELETE" }),
  };
}

/** 把任意异常转成可展示的页面状态描述（不泄露内部栈） */
export type FailureKind =
  | "forbidden"
  | "unauthenticated"
  | "not-found"
  | "offline"
  | "timeout"
  /** 请求本身不合规则（422）：需要改内容，不是重试 */
  | "rejected"
  | "error";

export type Failure = {
  kind: FailureKind;
  /** Reviewed semantic message; safe to re-render after a language change. */
  messageKey?: ErrorMessageKey;
  title: string;
  detail: string;
  /** 诊断编号：优先服务端 requestId */
  diagnosticId: string | null;
  retryable: boolean;
};

/** Resolves cached failures without retaining an exception or server details. */
export function localizeFailure(failure: Failure): Failure {
  if (!failure.messageKey || !hasErrorMessage(failure.messageKey)) return failure;
  return { ...failure,
    title: i18n.t(`${failure.messageKey}.title`, { ns: "errors" }),
    detail: i18n.t(`${failure.messageKey}.detail`, { ns: "errors" }),
  };
}

export function createFailure(messageKey: ErrorMessageKey, state: Pick<Failure, "kind" | "diagnosticId" | "retryable">): Failure {
  return localizeFailure({ ...state, messageKey, title: "", detail: "" });
}

/** Localization never exposes raw server details or changes retry semantics. */
export function describeFailure(error: unknown): Failure {
  if (error instanceof SessionStateError) return createFailure(error.code, {
    kind: "error", diagnosticId: null, retryable: true,
  });
  if (error instanceof ApiError) {
    const state = { diagnosticId: error.requestId, retryable: error.retryable };
    if (error.code === "CSRF_UNAVAILABLE" || error.code === "csrf-token-invalid") return createFailure("session-rejoin-required", {
      ...state, kind: "forbidden", retryable: false,
    });
    // HTTP codes known to this application are mapped to reviewed copy only.
    // Prototype names, new codes and raw Problem Details cannot become UI copy.
    if (hasDetailedErrorMessage(error.code)) return createFailure(error.code, {
      ...state, kind: error.isForbidden ? "forbidden" : "rejected", retryable: false,
    });
    if (error.isForbidden) return createFailure("forbidden", { ...state, kind: "forbidden", retryable: false });
    if (error.isUnauthenticated) return createFailure("unauthenticated", { ...state, kind: "unauthenticated", retryable: false });
    if (error.isNotFound) return createFailure("not-found", { ...state, kind: "not-found", retryable: false });
    if (error.isUnprocessable) return createFailure("rejected", { ...state, kind: "rejected", retryable: false });
    if (error.status === 429 || error.status === 503) return createFailure(error.status === 429 ? "limited" : "busy", { ...state, kind: "error", retryable: true });
    return createFailure("unavailable", { ...state, kind: "error" });
  }
  if (error instanceof TimeoutError) return createFailure("timeout", { kind: "timeout", diagnosticId: null, retryable: true });
  if (error instanceof NetworkError) return createFailure("offline", { kind: "offline", diagnosticId: null, retryable: true });
  return createFailure("unknown", { kind: "error", diagnosticId: null, retryable: true });
}
