/**
 * 会话与 API 的单例装配。
 *
 * CSRF token 只在内存中保存（不写 localStorage： 禁止把会话凭证存本地）。
 * 页面刷新后由 GET /sessions/current 重新取回。
 */
import { createContext, useContext } from "react";
import { createContractApi, type Api } from "./contract";

export type SessionState = {
  sessionId: string;
  displayName: string;
  csrfToken: string;
  expiresAt: string;
};

/** 内存中的会话凭证持有者；刷新后为空，需要重新获取 */
let csrfToken: string | null = null;
let sessionId: string | null = null;

export function setSessionCredentials(next: { sessionId: string; csrfToken: string } | null): void {
  sessionId = next?.sessionId ?? null;
  csrfToken = next?.csrfToken ?? null;
}

export function getCsrfToken(): string | null {
  return csrfToken;
}

export function getSessionId(): string | null {
  return sessionId;
}

export type RequestIdLog = { requestId: string; method: string; path: string; at: number };

const requestIdLog: RequestIdLog[] = [];
const MAX_LOG = 30;

/** 最近请求的 X-Request-Id，供故障提示展示诊断编号 */
export function recentRequestIds(): readonly RequestIdLog[] {
  return requestIdLog;
}

let onUnauthenticatedHandler: (() => void) | null = null;
let onCsrfInvalidHandler: (() => Promise<boolean>) | null = null;
let csrfRecovery: Promise<boolean> | null = null;
export type AdmissionReason = "entry" | "traffic";
let onAdmissionHandler:((reason?: AdmissionReason)=>Promise<boolean>)|null=null;
export function setAdmissionHandler(handler:((reason?: AdmissionReason)=>Promise<boolean>)|null):void{onAdmissionHandler=handler}


export function setCsrfRecoveryHandler(handler: (() => Promise<boolean>) | null): void {
  onCsrfInvalidHandler = handler;
}

export function setUnauthenticatedHandler(handler: (() => void) | null): void {
  onUnauthenticatedHandler = handler;
}

export const api: Api = createContractApi({
  getCsrfToken,
  onAdmissionRequired:(reason)=>onAdmissionHandler?.(reason)??Promise.resolve(false),
  onCsrfInvalid: () => {
    if (csrfRecovery) return csrfRecovery;
    csrfRecovery = (onCsrfInvalidHandler?.() ?? Promise.resolve(false)).finally(() => { csrfRecovery = null; });
    return csrfRecovery;
  },
  // 页面级写操作：缺少 CSRF 时提前失败并给出「刷新导致凭据失效」的准确提示
  requireCsrf: true,
  onRequestId: (requestId, info) => {
    requestIdLog.push({ requestId, ...info, at: Date.now() });
    if (requestIdLog.length > MAX_LOG) requestIdLog.shift();
  },
  onUnauthenticated: () => {
    setSessionCredentials(null);
    onUnauthenticatedHandler?.();
  },
});

export type ApiContextValue = {
  api: Api;
  session: SessionState | null;
  /** 会话是否仍在加载（页面据此显示 loading 而不是 empty） */
  sessionLoading: boolean;
  setSession: (session: SessionState | null) => void;
  /** 确保存在匿名会话，复用已有身份并同步全局昵称 */
  ensureSession: (displayName: string) => Promise<SessionState>;
  /* 用现有会话恢复 CSRF；失败返回 null，不创建新身份 */
  rotateCsrf: () => Promise<SessionState | null>;
  restoreSession?: () => Promise<SessionState | null>;
};

export const ApiContext = createContext<ApiContextValue | null>(null);

export function useApi(): ApiContextValue {
  const value = useContext(ApiContext);
  if (!value) {
    throw new Error("useApi 必须在 ApiProvider 内使用");
  }
  return value;
}
