import { LocalizedNotice } from "@/lib/player-notice";
import { PracticeConflict, practiceWriterId } from "./practice-store";

export const ACTIVITY_CHANNEL = "umaguess-activity-changes";
const unavailable = () => new LocalizedNotice("storage");
/** Shared persistence mechanics; each adapter supplies strict archive validation. */
export function activityStore<P, G extends { gameId: string; packKey: string; revision: number; updatedAt: number }>(
  name: string, validate: (raw: unknown) => { schemaVersion: 1; pack: P; game: G }, packKey: (pack: P) => string,
) {
  let pending: Promise<IDBDatabase> | undefined;
  function open() {
    return pending ??= new Promise<IDBDatabase>((resolve, reject) => {
      if (!globalThis.indexedDB) { reject(unavailable()); return; }
      const r = indexedDB.open(name, 1);
      r.onupgradeneeded = () => { r.result.createObjectStore("packs", { keyPath: "key" }); r.result.createObjectStore("games", { keyPath: "gameId" }); };
      r.onerror = () => reject(unavailable());
      r.onblocked = () => reject(new LocalizedNotice("blocked"));
      r.onsuccess = () => { const db = r.result; db.onversionchange = () => { db.close(); pending = undefined; }; resolve(db); };
    }).catch((error: unknown) => { pending = undefined; throw error; });
  }
  async function read<T>(store: string, key?: string): Promise<T> {
    const db = await open();
    return new Promise((resolve, reject) => {
      const r = key === undefined ? db.transaction(store).objectStore(store).getAll() : db.transaction(store).objectStore(store).get(key);
      r.onsuccess = () => resolve(r.result as T); r.onerror = () => reject(unavailable());
    });
  }
  function notify(gameId: string) {
    // Notifications are best-effort; a committed transaction remains successful.
    try { if (typeof BroadcastChannel !== "undefined") { const c = new BroadcastChannel(ACTIVITY_CHANNEL); try { c.postMessage({ gameId, writerId: practiceWriterId }); } finally { c.close(); } } } catch { /* CAS remains authoritative when messaging is unavailable. */ }
  }
  return {
    async latestPack() {
      const packs = await read<{ pack: P; installedAt: number }[]>("packs");
      return packs.sort((a, b) => b.installedAt - a.installedAt)[0]?.pack ?? null;
    },
    async install(pack: P) {
      const db = await open(); await new Promise<void>((resolve, reject) => {
        const tx = db.transaction("packs", "readwrite"); tx.objectStore("packs").put({ key: packKey(pack), pack, installedAt: Date.now() });
        tx.oncomplete = () => resolve(); tx.onabort = tx.onerror = () => reject(unavailable());
      });
    },
    async list(): Promise<G[]> { return (await read<G[]>("games")).sort((a, b) => b.updatedAt - a.updatedAt); },
    async load(id: string) {
      const game = await read<G | undefined>("games", id); if (!game) return null;
      const record = await read<{ pack: P } | undefined>("packs", game.packKey);
      if (!record) throw new LocalizedNotice("missingPack");
      return validate({ schemaVersion: 1, game, pack: record.pack });
    },
    async save(game: G, pack: P, expectedRevision: number | null) {
      const checked = validate({ schemaVersion: 1, game, pack });
      const db = await open(); await new Promise<void>((resolve, reject) => {
        let conflict = false; const tx = db.transaction(["packs", "games"], "readwrite"), store = tx.objectStore("games"), r = store.get(game.gameId);
        r.onsuccess = () => {
          const previous = r.result as G | undefined;
          if (expectedRevision === null ? !!previous : previous?.revision !== expectedRevision) { conflict = true; tx.abort(); return; }
          tx.objectStore("packs").put({ key: packKey(checked.pack), pack: checked.pack, installedAt: Date.now() }); store.put(checked.game);
        };
        tx.oncomplete = () => resolve(); tx.onabort = tx.onerror = () => reject(conflict ? new PracticeConflict() : unavailable());
      }); notify(game.gameId);
    },
    async remove(id: string) {
      const db = await open(); await new Promise<void>((resolve, reject) => {
        const tx = db.transaction("games", "readwrite"); tx.objectStore("games").delete(id);
        tx.oncomplete = () => resolve(); tx.onabort = tx.onerror = () => reject(unavailable());
      }); notify(id);
    },
  };
}
