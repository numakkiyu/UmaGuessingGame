import { LocalizedNotice } from "@/lib/player-notice";
import type { PracticeCharacter } from "@/lib/domain/classic-practice";

export async function prepareOfflineShell(characters: readonly Pick<PracticeCharacter,"imagePath">[], signal: AbortSignal, progress: (done: number,total: number)=>void): Promise<boolean> {
  // LAN HTTP supports local play in the open page, but not offline navigation.
  if (!window.isSecureContext || !("serviceWorker" in navigator) || !("caches" in window)) return false;
  const registration=await navigator.serviceWorker.register("/practice-sw.js",{scope:"/"});
  const worker=registration.installing ?? registration.waiting ?? registration.active;
  if (!worker) throw new LocalizedNotice("shellMissing");
  if (worker.state!=="activated" && worker.state!=="installed") {
    await new Promise<void>((resolve,reject)=>{
      const timeout=window.setTimeout(()=>finish(new LocalizedNotice("shellTimeout")),30000);
      const finish=(error?:Error)=>{clearTimeout(timeout);worker.removeEventListener("statechange",check);signal.removeEventListener("abort",abort);if(error)reject(error);else resolve();};
      const check=()=>{if (worker.state==="installed"||worker.state==="activated") finish();else if(worker.state==="redundant") finish(new LocalizedNotice("shellInterrupted"));};
      const abort=()=>finish(new DOMException("Aborted","AbortError"));
      worker.addEventListener("statechange",check);signal.addEventListener("abort",abort,{once:true});check();
    });
  }
  const paths=[...new Set(characters.map(c=>c.imagePath).filter(Boolean))];
  const cache=await caches.open("umaguess-practice-media");
  let cursor=0,done=0;
  progress(done,paths.length);
  await Promise.all(Array.from({length:4},async()=>{
    while(cursor<paths.length) {
      signal.throwIfAborted();
      const path=paths[cursor++]!;
      if (!(await cache.match(path))) {
        const response=await fetch(path,{signal,credentials:"omit",cache:"no-cache"});
        if (!response.ok || !response.headers.get("content-type")?.startsWith("image/")) throw new LocalizedNotice("avatarsMissing");
        await cache.put(path,response);
      }
      progress(++done,paths.length);
    }
  }));
  return true;
}
