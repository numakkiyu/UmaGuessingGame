import { useCallback, useState } from "react";
import i18n, { useTranslation } from ".";
import { ApiError, describeFailure, localizeFailure, NetworkError, TimeoutError, type Failure } from "@/lib/api/client";
import { PlayerNotice } from "@/lib/player-notice";

type Values = Record<string, string | number>;
export type PlayerText = { ns: string; key: string; values?: Values } | { failure: Failure } | { reviewedText: string };
export function resolvePlayerText(message: PlayerText | null): string {
  if (!message) return "";
  if ("failure" in message) return localizeFailure(message.failure).detail;
  if ("reviewedText" in message) return message.reviewedText;
  return i18n.t(message.key, { ns: message.ns, ...message.values });
}
export function errorPlayerText(error: unknown, ns: string, fallback: string): PlayerText {
  if (error instanceof PlayerNotice) return error.messageKey
    ? { ns: error.messageNamespace, key: error.messageKey, values: error.messageValues } : { reviewedText: error.message };
  if (error instanceof ApiError || error instanceof NetworkError || error instanceof TimeoutError) return { failure: describeFailure(error) };
  return { ns, key: fallback };
}
/** Store message meaning rather than translated text or a raw exception. */
export function usePlayerText(namespace: string, initialKey = "") {
  useTranslation(namespace);
  const [state, setState] = useState<PlayerText | null>(() => initialKey ? { ns: namespace, key: initialKey } : null);
  const setMessage = useCallback((key: string, values?: Values) => setState(key ? { ns: namespace, key, values } : null), [namespace]);
  const setError = useCallback((error: unknown, fallback: string) => setState(errorPlayerText(error, namespace, fallback)), [namespace]);
  return [resolvePlayerText(state), setMessage, setError] as const;
}
