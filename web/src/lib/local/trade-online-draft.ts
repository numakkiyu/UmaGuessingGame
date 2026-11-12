const PREFIX = "umaguess:trade-draft:";
export type TradeDraft = { offeredId: string | null; recipientId: string | null; requestedId: string | null };
export const emptyTradeDraft: TradeDraft = { offeredId: null, recipientId: null, requestedId: null };
export function loadTradeDraft(scope: string): TradeDraft | null {
  try {
    const raw = localStorage.getItem(PREFIX + scope);
    if (!raw || raw.length > 2048) return null;
    const value = JSON.parse(raw);
    if (!value || !Number.isSafeInteger(value.savedAt) || value.savedAt > Date.now() + 60000 || value.savedAt <= Date.now() - 86400000) return null;
    for (const key of ["offeredId", "recipientId", "requestedId"]) if (value[key] !== null && (typeof value[key] !== "string" || !value[key] || value[key].length > 160)) return null;
    return { offeredId: value.offeredId, recipientId: value.recipientId, requestedId: value.requestedId };
  } catch { return null; }
}
export function saveTradeDraft(scope: string, value: TradeDraft): boolean {
  try { localStorage.setItem(PREFIX + scope, JSON.stringify({ ...value, savedAt: Date.now() })); return true; } catch { return false; }
}
