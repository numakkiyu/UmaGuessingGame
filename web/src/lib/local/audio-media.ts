import type { PracticeAudioClip } from "@/lib/domain/audio-practice";
import { AudioNotice } from "@/i18n/audio";
import { sha256 } from "./checksum";
const memory = new Map<string, Blob>();
const decodedCache = new Map<string, AudioBuffer>();
const key = (c: PracticeAudioClip) => `${c.audioPath}:${c.sha256}`;
function mediaDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("umaguess-audio-media", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("clips");
    request.onerror = () => reject(request.error);
    request.onblocked = () => reject(new Error("media storage blocked"));
    request.onsuccess = () => resolve(request.result);
  });
}
async function readMedia(c: PracticeAudioClip): Promise<Blob | null> {
  const cached = memory.get(key(c)); if (cached) return cached;
  try {
    const db = await mediaDB();
    try { return await new Promise<Blob | null>((resolve, reject) => { const tx = db.transaction("clips", "readonly"), request = tx.objectStore("clips").get(key(c)); tx.oncomplete = () => resolve(request.result instanceof Blob ? request.result : null); tx.onerror = () => reject(tx.error); tx.onabort = () => reject(tx.error); }); }
    finally { db.close(); }
  } catch { return null; }
}
async function saveMedia(c: PracticeAudioClip, blob: Blob): Promise<boolean> {
  memory.set(key(c), blob);
  try {
    const db = await mediaDB();
    try { await new Promise<void>((resolve, reject) => { const tx = db.transaction("clips", "readwrite"); tx.objectStore("clips").put(blob, key(c)); tx.oncomplete = () => resolve(); tx.onerror = () => reject(tx.error); tx.onabort = () => reject(tx.error); }); }
    finally { db.close(); }
    return true;
  } catch { return false; }
}
async function validatedBuffer(blob: Blob, clip: PracticeAudioClip, context: AudioContext) {
  const bytes = new Uint8Array(await blob.arrayBuffer());
  if (bytes.length !== clip.byteLength || sha256(bytes) !== clip.sha256) throw new AudioNotice("damaged");
  const cached = decodedCache.get(key(clip)); if (cached) return cached;
  const decoded = await context.decodeAudioData(bytes.buffer);
  if (!decoded.length || Math.abs(decoded.duration * 1000 - clip.durationMillis) > Math.max(400, clip.durationMillis * 0.05)) throw new AudioNotice("decode");
  if (decodedCache.size >= 24) decodedCache.delete(decodedCache.keys().next().value!);
  decodedCache.set(key(clip), decoded); return decoded;
}
export async function ensurePracticeAudioPresent(clips: readonly PracticeAudioClip[]) {
  const context = new AudioContext();
  try { for (const c of clips) { const blob = await readMedia(c); if (!blob) throw new AudioNotice("missing"); await validatedBuffer(blob, c, context); } }
  finally { await context.close(); }
}
/** The caller obtains explicit consent including the total byte size first. */
export async function preparePracticeAudio(clips: readonly PracticeAudioClip[], signal: AbortSignal, progress: (done: number, total: number) => void): Promise<boolean> {
  const context = new AudioContext(); let persistent = true;
  try {
    for (let i = 0; i < clips.length; i++) {
      signal.throwIfAborted(); const clip = clips[i]!;
      let blob = await readMedia(clip), cached = Boolean(blob);
      if (blob) { try { await validatedBuffer(blob, clip, context); } catch { blob = null; cached = false; } }
      if (!blob) {
        const response = await fetch(clip.audioPath, { signal, credentials: "omit", cache: "no-cache", redirect: "error" });
        if (!response.ok || !response.headers.get("content-type")?.startsWith("audio/")) throw new AudioNotice("network");
        const reader = response.body?.getReader(); if (!reader) throw new AudioNotice("read");
        const parts: Uint8Array<ArrayBuffer>[] = []; let length = 0;
        try { while (true) { const { done, value } = await reader.read(); if (done) break; length += value.length; if (length > clip.byteLength) { await reader.cancel(); throw new AudioNotice("damaged"); } parts.push(new Uint8Array(value)); } }
        finally { reader.releaseLock(); }
        blob = new Blob(parts, { type: "audio/webm; codecs=opus" }); await validatedBuffer(blob, clip, context);
      }
      signal.throwIfAborted();
      // Save again even for the memory fallback; persistence may now be available.
      persistent = await saveMedia(clip, blob) && persistent;
      if (!cached) signal.throwIfAborted(); progress(i + 1, clips.length);
    }
    return persistent;
  } finally { await context.close(); }
}
export async function loadPracticeAudio(clips: readonly PracticeAudioClip[], signal: AbortSignal) {
  const context = new AudioContext(), buffers = new Map<string, AudioBuffer>(), failed = new Set<string>();
  try {
    for (const clip of clips) {
      signal.throwIfAborted();
      try { const blob = await readMedia(clip); if (!blob) throw new Error("media missing"); buffers.set(clip.clipId, await validatedBuffer(blob, clip, context)); }
      catch { signal.throwIfAborted(); failed.add(clip.clipId); }
    }
    return { context, buffers, failed };
  } catch (error) { await context.close(); throw error; }
}
