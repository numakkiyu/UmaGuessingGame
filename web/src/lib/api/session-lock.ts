/** Only coordination metadata is persisted. Credentials never enter storage. */
const LOCK = "uma:anonymous-session";
const WAIT_MS = 15_000;
const LEASE_MS = 90_000;
let database: Promise<IDBDatabase> | undefined;
function openDatabase(): Promise<IDBDatabase> {
  database ??= new Promise((resolve, reject) => {
    const request = indexedDB.open("uma:session-coordination", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("locks");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => { database = undefined; reject(new Error("暂时无法保留你的座位，请检查浏览器是否允许保存网站数据")); };
    request.onblocked = () => reject(new Error("请先关闭旧的游戏页面，再回来试一次"));
  });
  return database;
}
async function lease(db: IDBDatabase, owner: string, release = false): Promise<boolean> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction("locks", "readwrite"), store = tx.objectStore("locks");
    const request = store.get(LOCK);
    let acquired = false;
    request.onsuccess = () => {
      const current = request.result as { owner: string; until: number } | undefined;
      if (release) { if (current?.owner === owner) store.delete(LOCK); return; }
      if (!current || current.until < Date.now()) {
        acquired = true;
        store.put({ owner, until: Date.now() + LEASE_MS }, LOCK);
      }
    };
    tx.oncomplete = () => resolve(acquired);
    tx.onerror = tx.onabort = () => reject(new Error("暂时无法保留你的座位，请稍后再试"));
  });
}
export async function withSessionLock<T>(work: () => Promise<T>): Promise<T> {
  if (navigator.locks) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), WAIT_MS);
    try {
      return await navigator.locks.request(LOCK, { signal: controller.signal }, async () => {
        clearTimeout(timer);
        return work();
      });
    } catch (error) {
      if (controller.signal.aborted) throw new Error("另一个游戏页面正在为你保留座位，请稍后再试", {cause:error});
      throw error;
    } finally { clearTimeout(timer); }
  }
  // Web Locks needs HTTPS. IndexedDB's read-write transaction also serialises
  // tabs on ordinary HTTP LAN sites, without a racy localStorage check-and-set.
  const db = await openDatabase();
  const bytes = new Uint8Array(16); crypto.getRandomValues(bytes);
  const owner = [...bytes].map(value => value.toString(16).padStart(2, "0")).join("");
  const deadline = Date.now() + WAIT_MS;
  while (Date.now() < deadline) {
    if (await lease(db, owner)) {
      try { return await work(); }
      finally { await lease(db, owner, true).catch(() => undefined); }
    }
    await new Promise(resolve => setTimeout(resolve, 80));
  }
  throw new Error("另一个游戏页面正在为你保留座位，请稍后再试");
}
