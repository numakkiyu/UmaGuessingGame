import { withSessionLock } from "@/lib/api/session-lock";
import { SessionStateError } from "@/lib/api/session-errors";
import { EntryAdmission } from "@/features/identity/entry-admission";
// Query retries apply to reads; write retries retain their original request identity at the call site
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ApiContext, api, getCsrfToken, getSessionId, setSessionCredentials, setCsrfRecoveryHandler, type ApiContextValue, type SessionState } from "@/lib/api";
import { readTrainerProfile } from "@/lib/trainer-profile";
import { ApiError, describeFailure, type Failure } from "@/lib/api/client";
import { TooltipProvider } from "@/components/ui/tooltip";
import { useReducedMotion } from "@/lib/hooks/use-reduced-motion";

function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // 私有 API 一律 no-store，缓存只作页内复用
        staleTime: 15_000,
        gcTime: 5 * 60_000,
        refetchOnWindowFocus: true,
        refetchOnReconnect: true,
        retry: (failureCount, error) => {
          if (failureCount >= 2) return false;
          // 只在可重试的失败上重试 GET
          if (error instanceof ApiError) return error.isTransient;
          return true;
        },
        retryDelay: (attempt) => Math.min(500 * 2 ** attempt, 4000),
      },
      mutations: {
        // 写请求绝不自动重试：必须由调用方复用同一幂等键
        retry: false,
      },
    },
  });
}

export type AppProvidersProps = {
  children: ReactNode;
};

export function AppProviders({ children }: AppProvidersProps) {
  const [queryClient] = useState(createQueryClient);
  const sessionRef = useRef<SessionState | null>(null);
  const [session, setSessionState] = useState<SessionState | null>(null);
  const [sessionLoading, setSessionLoading] = useState(() => !/^\/(practice(?:\/|$)|play\/(?:classic|sort|memory|aptitude-grid|draft|race-rebuild|co-race-path|strategy-race|pedigree|audio)\/?$)/.test(window.location.pathname));
  // 减少动态：在 Provider 层激活，保证任意页面都生效
  useReducedMotion();

  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const sessionChannel=useRef<BroadcastChannel|null>(null);
  const setSession = useCallback((next: SessionState | null) => {
    sessionRef.current = next;
    setSessionState(next);
    setSessionCredentials(next ? { sessionId: next.sessionId, csrfToken: next.csrfToken } : null);
    if(next)try{sessionChannel.current?.postMessage(next);}catch{/* Identity remains usable when cross-page messaging is blocked. */}
  }, []);

  useEffect(()=>{
    if(typeof BroadcastChannel==="undefined")return;
    let channel:BroadcastChannel;
    try{channel=new BroadcastChannel("uma:session-updates");}catch{return;}
    sessionChannel.current=channel;
    channel.onmessage=(event:MessageEvent<SessionState>)=>{
      const next=event.data;
      if(!next||next.sessionId!==sessionRef.current?.sessionId||typeof next.csrfToken!=="string"||!next.csrfToken)return;
      sessionRef.current=next;setSessionState(next);setSessionCredentials({sessionId:next.sessionId,csrfToken:next.csrfToken});
    };
    return()=>{sessionChannel.current=null;try{channel.close();}catch{/* The channel is already unavailable. */}};
  },[]);

  // Recovery is demand-driven. Local practice never creates or restores a cloud identity.
  const recoveryRef = useRef<Promise<SessionState | null> | null>(null);
  const restoreSession = useCallback((): Promise<SessionState | null> => {
    if (sessionRef.current?.csrfToken) return Promise.resolve(sessionRef.current);
    if (recoveryRef.current) return recoveryRef.current;
    setSessionLoading(true);
    const promise = (async () => {
      try {
        const response = await api.getCurrentSession();
        const body = response.data;
        if (!body) throw new SessionStateError("session-restore-failed");
        let token = body.csrfToken;
        if (!token) token = (await api.rotateSessionCsrf()).data?.csrfToken ?? "";
        if (!token) throw new SessionStateError("session-restore-failed");
        const next: SessionState = { ...body, csrfToken: token };
        sessionRef.current = next;
        if (mountedRef.current) setSession(next);
        return next;
      } catch (error) {
        if (error instanceof ApiError && (error.status === 401 || error.status === 404)) return null;
        throw error;
      } finally {
        recoveryRef.current = null;
        if (mountedRef.current) setSessionLoading(false);
      }
    })();
    recoveryRef.current = promise;
    return promise;
  }, [setSession]);

  /* 刷新后用现有会话恢复 CSRF，保留房间成员身份 */
  const rotateCsrf = useCallback(async (): Promise<SessionState | null> => {
    const previous = sessionRef.current;
    if (!previous) return null;
    try {
      const response = await api.rotateSessionCsrf();
      const token = response.data?.csrfToken;
      if (!token || response.data?.sessionId !== previous.sessionId || sessionRef.current?.sessionId !== previous.sessionId) return null;
      const next: SessionState = { ...previous, csrfToken: token };
      sessionRef.current = next;
      setSession(next);
      return next;
    } catch {
      // 端点不可用或会话已失效：返回 null，由调用方给出明确提示，不新建身份
      return null;
    }
  }, [setSession]);

  useEffect(() => {
    setCsrfRecoveryHandler(async () => (await rotateCsrf()) !== null);
    return () => setCsrfRecoveryHandler(null);
  }, [rotateCsrf]);

  const creatingSession = useRef<Promise<SessionState>|null>(null);
  const ensureSession = useCallback(
    async (displayName: string): Promise<SessionState> => {
      const current = sessionRef.current;
      const preferred = readTrainerProfile().displayName;
      if (current?.csrfToken && (!preferred || preferred === current.displayName)) return current;
      if (creatingSession.current) return creatingSession.current;
      const pending=withSessionLock(async()=>{
      const preferredName = readTrainerProfile().displayName;
      const restored = sessionRef.current?.csrfToken ? sessionRef.current : await restoreSession();
      if (restored) {
        if (!preferredName || preferredName === restored.displayName) return restored;
        const response = await api.updateCurrentSession({ displayName: preferredName });
        const latest = sessionRef.current;
        const token = getCsrfToken();
        if (!response.data?.displayName || response.data.sessionId !== restored.sessionId || !latest || latest.sessionId !== restored.sessionId || getSessionId() !== restored.sessionId || !token) throw new SessionStateError("session-restore-failed");
        const next = { ...latest, displayName: response.data.displayName, csrfToken: token };
        setSession(next);
        return next;
      }
      const response = await api.createSession({ displayName: preferredName || displayName });
      const body = response.data;
      if (!body) {
        throw new SessionStateError("session-create-failed");
      }
      const next: SessionState = {
        sessionId: body.sessionId,
        displayName: body.displayName,
        csrfToken: body.csrfToken,
        expiresAt: body.expiresAt,
      };
      sessionRef.current = next;
      setSession(next);
      return next;
      }).finally(()=>{creatingSession.current=null});
      creatingSession.current=pending;
      return pending;
    },
    [setSession, restoreSession],
  );

  const value = useMemo<ApiContextValue>(
    () => ({ api, session, sessionLoading, setSession, ensureSession, rotateCsrf, restoreSession }),
    [session, sessionLoading, setSession, ensureSession, rotateCsrf, restoreSession],
  );

  return (
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <ApiContext.Provider value={value}>
          <TooltipProvider delayDuration={300} skipDelayDuration={200}>
            {children}
            <EntryAdmission prepareSession={restoreSession} />
          </TooltipProvider>
        </ApiContext.Provider>
      </QueryClientProvider>
    </StrictMode>
  );
}

/** 供错误边界使用：把异常转成可展示的 Failure */
export function toFailure(error: unknown): Failure {
  return describeFailure(error);
}
