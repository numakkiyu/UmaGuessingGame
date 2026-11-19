import { publishPracticeChange } from "./practice-channel";
import { LocalizedNotice } from "@/lib/player-notice";
import type { ClassicGame, PracticePack } from "@/lib/domain/classic-practice";
import { classicProgress } from "@/lib/domain/classic-practice";
import { packKey, readPracticePack, CLASSIC_RULES } from "./practice-pack";

const DB_NAME = "umaguess-local-practice";
export const practiceWriterId = newPracticeId();
export class PracticeConflict extends LocalizedNotice {
  constructor() { super("conflict"); }
}
let dbPromise: Promise<IDBDatabase> | undefined;
export function openPracticeDB(): Promise<IDBDatabase> {
  if (!dbPromise) {
    dbPromise = new Promise<IDBDatabase>((resolve, reject) => {
      if (!globalThis.indexedDB) { reject(new LocalizedNotice("storage")); return; }
      const request = indexedDB.open(DB_NAME,1);
      request.onupgradeneeded = () => {
        const db = request.result;
        db.createObjectStore("packs",{keyPath:"key"});
        db.createObjectStore("games",{keyPath:"gameId"});
      };
      request.onblocked = () => reject(new LocalizedNotice("blocked"));
      request.onerror = () => reject(new LocalizedNotice("storage"));
      request.onsuccess = () => {
        const db = request.result;
        db.onversionchange = () => {db.close(); dbPromise=undefined;};
        resolve(db);
      };
    }).catch((error: unknown) => {dbPromise=undefined; throw error;});
  }
  return dbPromise;
}
async function read<T>(store: string, key?: IDBValidKey): Promise<T> {
  const db = await openPracticeDB();
  return new Promise<T>((resolve,reject)=>{
    const tx = db.transaction(store,"readonly");
    const request = key === undefined ? tx.objectStore(store).getAll() : tx.objectStore(store).get(key);
    request.onsuccess=()=>resolve(request.result as T);
    request.onerror=()=>reject(new LocalizedNotice("storage"));
  });
}
export async function latestPracticePack(): Promise<PracticePack | null> {
  const values = await read<{pack: PracticePack; installedAt: number}[]>("packs");
  values.sort((a,b)=>b.installedAt-a.installedAt);
  for (const entry of values) {
    try { return readPracticePack(entry.pack).pack; } catch { /* Keep incompatible packs for exported old games. */ }
  }
  return null;
}
export async function savedPracticePack(key: string): Promise<PracticePack | null> {
  return (await read<{pack: PracticePack}|undefined>("packs",key))?.pack ?? null;
}
export async function installPracticePack(pack: PracticePack): Promise<void> {
  readPracticePack(pack);
  const db = await openPracticeDB();
  await new Promise<void>((resolve,reject)=>{
    const tx=db.transaction("packs","readwrite");
    tx.objectStore("packs").put({key:packKey(pack),pack,installedAt:Date.now()});
    tx.oncomplete=()=>resolve(); tx.onabort=()=>reject(new LocalizedNotice("storage")); tx.onerror=()=>reject(new LocalizedNotice("storage"));
  });
}
export async function listPracticeGames(): Promise<ClassicGame[]> {
  const games = await read<ClassicGame[]>("games");
  return games.sort((a,b)=>b.updatedAt-a.updatedAt);
}
export async function loadPracticeGame(id: string): Promise<{game:ClassicGame; pack:PracticePack} | null> {
  const game=await read<ClassicGame|undefined>("games",id);
  if (!game) return null;
  const pack=await savedPracticePack(game.packKey);
  if (!pack) throw new LocalizedNotice("missingPack");
  return validatePracticeArchive({schemaVersion:1,game,pack});
}
function notify(gameId: string) { publishPracticeChange("umaguess-practice-changes",{gameId,writerId:practiceWriterId}); }
export async function savePracticeGame(game: ClassicGame, expectedRevision: number | null): Promise<void> {
  const db=await openPracticeDB();
  await new Promise<void>((resolve,reject)=>{
    let conflict=false;
    const tx=db.transaction("games","readwrite");
    const store=tx.objectStore("games");
    const request=store.get(game.gameId);
    request.onsuccess=()=>{
      const previous=request.result as ClassicGame|undefined;
      if ((expectedRevision === null && previous) || (expectedRevision !== null && previous?.revision !== expectedRevision)) {
        conflict=true; tx.abort(); return;
      }
      store.put(game);
    };
    tx.oncomplete=()=>resolve();
    tx.onabort=()=>reject(conflict ? new PracticeConflict() : new LocalizedNotice("storage"));
    tx.onerror=()=>reject(new LocalizedNotice("storage"));
  });
  notify(game.gameId);
}
export async function removePracticeGame(gameId: string): Promise<void> {
  const db=await openPracticeDB();
  await new Promise<void>((resolve,reject)=>{
    const tx=db.transaction("games","readwrite");
    tx.objectStore("games").delete(gameId);
    tx.oncomplete=()=>resolve(); tx.onabort=()=>reject(new LocalizedNotice("storage")); tx.onerror=()=>reject(new LocalizedNotice("storage"));
  });
  notify(gameId);
}
export function newPracticeId(): string {
  // getRandomValues is available on LAN HTTP too; randomUUID need not be.
  return "local-"+Array.from(crypto.getRandomValues(new Uint8Array(16)),v=>v.toString(16).padStart(2,"0")).join("");
}
export function createClassicGame(pack: PracticePack, wrongBudget: number, now=Date.now()): ClassicGame {
  const {characters}=readPracticePack(pack);
  const answers=characters.filter(c=>c.answerEligible);
  if (!Number.isInteger(wrongBudget) || wrongBudget<4 || wrongBudget>12) throw new LocalizedNotice("invalidBudget");
  const limit=Math.floor(0x100000000/answers.length)*answers.length;
  let n: number;
  do { n=(crypto.getRandomValues(new Uint32Array(1))[0]! ^ (now>>>0))>>>0; } while(n>=limit);
  return {schemaVersion:1,modeId:"classic",gameId:newPracticeId(),packKey:packKey(pack),rulesVersion:pack.rulesVersion,answerId:answers[n%answers.length]!.characterId,wrongBudget,guesses:[],ended:false,createdAt:now,updatedAt:now,revision:0};
}
export function validatePracticeArchive(raw: unknown): {game:ClassicGame;pack:PracticePack} {
  if (!raw || typeof raw!=="object") throw new LocalizedNotice("invalidArchive");
  const value=raw as Record<string,unknown>;
  if (value.schemaVersion!==1 || !value.game || typeof value.game!=="object") throw new LocalizedNotice("unreadableArchive");
  const {pack,characters}=readPracticePack(value.pack);
  const g=value.game as ClassicGame;
  if (g.schemaVersion!==1 || g.modeId!=="classic" || !/^local-[a-f0-9]{32}$/.test(g.gameId) || g.packKey!==packKey(pack) || g.rulesVersion!==CLASSIC_RULES) throw new LocalizedNotice("incompatibleArchive");
  if (!Number.isInteger(g.wrongBudget)||g.wrongBudget<4||g.wrongBudget>12||!Array.isArray(g.guesses)||g.guesses.length>13||g.guesses.some(id=>typeof id!=="string"||!characters.some(c=>c.characterId===id))||new Set(g.guesses).size!==g.guesses.length||typeof g.ended!=="boolean"||!Number.isSafeInteger(g.revision)||g.revision<0||g.revision>100) throw new LocalizedNotice("invalidGuesses");
  if (![g.createdAt,g.updatedAt].every(n=>Number.isSafeInteger(n)&&n>0&&n<=8_640_000_000_000_000)||g.updatedAt<g.createdAt||!characters.some(c=>c.characterId===g.answerId&&c.answerEligible)) throw new LocalizedNotice("invalidQuestion");
  let previous={...g,ended:false,guesses:[] as string[]};
  for (const id of g.guesses) {
    if (classicProgress(previous,characters).finished) throw new LocalizedNotice("afterEnd");
    previous={...previous,guesses:[...previous.guesses,id]};
  }
  // Rebuild a whitelist instead of retaining arbitrary imported properties.
  const game:ClassicGame={schemaVersion:1,modeId:"classic",gameId:g.gameId,packKey:g.packKey,rulesVersion:g.rulesVersion,answerId:g.answerId,wrongBudget:g.wrongBudget,guesses:[...g.guesses],ended:g.ended,createdAt:g.createdAt,updatedAt:g.updatedAt,revision:g.revision};
  return {game,pack};
}
