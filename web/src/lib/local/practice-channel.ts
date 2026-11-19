type PracticeChange = {gameId: string; writerId: string};

/** Storage revision checks remain authoritative if cross-page messaging is blocked. */
export function subscribePracticeChanges(name: string, receive: (change: PracticeChange) => void): () => void {
  try {
    if (typeof BroadcastChannel === "undefined") return () => undefined;
    const channel = new BroadcastChannel(name);
    channel.onmessage = (event: MessageEvent<unknown>) => {
      const value = event.data;
      if (!value || typeof value !== "object" || !("gameId" in value) || !("writerId" in value) || typeof value.gameId !== "string" || typeof value.writerId !== "string") return;
      receive({gameId: value.gameId, writerId: value.writerId});
    };
    return () => { try { channel.close(); } catch { /* The subscription is already unusable. */ } };
  } catch { return () => undefined; }
}

/** A failed notification must never turn a committed save into a failed save. */
export function publishPracticeChange(name: string, change: PracticeChange): void {
  try {
    if (typeof BroadcastChannel === "undefined") return;
    const channel = new BroadcastChannel(name);
    try { channel.postMessage(change); } finally { channel.close(); }
  } catch { /* The saved record can still be read and checked by revision. */ }
}
