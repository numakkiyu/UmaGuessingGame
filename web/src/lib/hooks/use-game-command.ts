import { useRef, useState } from "react";
import { ApiError, createIdempotencyKey } from "@/lib/api/client";

type Pending<T> = { key: string; body: T; createdAt: number };
type State<T> = {
  scope: string | null;
  pending: Pending<T> | null;
  busy: boolean;
  error: unknown;
  storageLimited: boolean;
  expired: boolean;
};
const MAX_AGE = 23 * 60 * 60 * 1000; // Less than the minimum 24h receipt retention.
const prefix = "uma:game-command:";

function restore<T>(scope: string | null, valid: (value: unknown) => value is T): State<T> {
  const state: State<T> = { scope, pending: null, busy: false, error: null, storageLimited: false, expired: false };
  if (!scope) return state;
  try {
    const raw = sessionStorage.getItem(prefix + scope);
    if (!raw) return state;
    if (raw.length > 8192) throw new Error("Oversized command");
    const value = JSON.parse(raw) as Partial<Pending<unknown>>;
    if (!value || typeof value.key !== "string" || !/^[a-f0-9]{48}$/.test(value.key) || typeof value.createdAt !== "number" || !Number.isFinite(value.createdAt) || value.createdAt > Date.now() + 60_000 || !valid(value.body)) throw new Error("Invalid command");
    if (Date.now() - value.createdAt >= MAX_AGE) state.expired = true;
    else state.pending = { key: value.key, body: value.body, createdAt: value.createdAt };
  } catch {
    // Invalid or inaccessible saved state never becomes an executable request.
    state.storageLimited = true;
    state.expired = true;
  }
  return state;
}

/** One explicit attempt per click. Pending writes survive refresh within the
 * same anonymous identity/round; no credentials or secret answers are stored.
 * A new snapshot, controller or phase never rewrites an uncertain command. */
export function useGameCommand<T, R>(scope: string | null, valid: (value: unknown) => value is T, execute: (body: T, key: string) => Promise<R>, afterSuccess?: () => Promise<unknown>) {
  const [state, setState] = useState(() => restore(scope, valid));
  const inFlight = useRef(false);
  if (state.scope !== scope) setState(restore(scope, valid));
  const current = state.scope === scope ? state : restore(scope, valid);

  function persist(pending: Pending<T> | null): boolean {
    if (!scope) return false;
    try {
      if (pending) sessionStorage.setItem(prefix + scope, JSON.stringify(pending));
      else sessionStorage.removeItem(prefix + scope);
      return true;
    } catch { return false; }
  }

  async function run(body?: T): Promise<{ ok: true; value: R } | { ok: false; error: unknown; uncertain: boolean } | null> {
    if (!scope || inFlight.current || current.expired) return null;
    const pending = current.pending ?? (body ? { key: createIdempotencyKey(), body, createdAt: Date.now() } : null);
    if (!pending) return null;
    if (Date.now() - pending.createdAt >= MAX_AGE) {
      setState(prev => prev.scope === scope ? { ...prev, expired: true } : prev);
      return null;
    }
    inFlight.current = true;
    const saved = persist(pending);
    setState(prev => prev.scope === scope ? { ...prev, pending, busy: true, error: null, storageLimited: prev.storageLimited || !saved } : prev);
    try {
      const value = await execute(pending.body, pending.key);
      await afterSuccess?.();
      const cleared = persist(null);
      setState(prev => prev.scope === scope ? { ...prev, pending: null, busy: false, error: null, storageLimited: prev.storageLimited || !cleared } : prev);
      return { ok: true, value };
    } catch (error) {
      // A rate limit or lost authorisation does not establish whether an earlier
      // uncertain attempt committed. Keep that original request for confirmation.
      const uncertain = !(error instanceof ApiError) || error.status >= 500 || [401, 403, 429].includes(error.status);
      if (!uncertain) persist(null);
      setState(prev => prev.scope === scope ? { ...prev, pending: uncertain ? pending : null, busy: false, error } : prev);
      return { ok: false, error, uncertain };
    } finally { inFlight.current = false; }
  }

  function acknowledgeExpired() {
    persist(null);
    setState(prev => prev.scope === scope ? { ...prev, expired: false, pending: null, error: null } : prev);
  }
  return { ...current, run, acknowledgeExpired };
}
