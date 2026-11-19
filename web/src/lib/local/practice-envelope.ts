import type { components } from "@/lib/api/schema";
import { LocalizedNotice } from "@/lib/player-notice";
import { sha256 } from "./checksum";

export const practiceObject = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
export const practiceId = (v: unknown): v is string => typeof v === "string" && /^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$/.test(v);
export const PRACTICE_RULES = "2.0.0-draft.1";
export const incompletePractice = () => new LocalizedNotice("incompletePractice");
type Envelope<M extends string> = Omit<components["schemas"]["AptitudePracticePack"], "modeId"> & { modeId: M };
export function readPracticeEnvelope<M extends string>(raw: unknown, modeId: M): { pack: Envelope<M>; content: Record<string, unknown> } {
  if (!practiceObject(raw) || raw.schemaVersion !== 1 || raw.modeId !== modeId || raw.rulesVersion !== PRACTICE_RULES || raw.reviewStatus !== "source-snapshot") throw incompletePractice();
  for (const key of ["datasetVersion", "contentPackVersion"]) if (typeof raw[key] !== "string" || !raw[key] || raw[key].length > 160) throw incompletePractice();
  if (!Array.isArray(raw.sourceUrls) || raw.sourceUrls.length > 10 || raw.sourceUrls.some(s => typeof s !== "string" || s.length > 500 || !s.startsWith("https://"))) throw incompletePractice();
  if (typeof raw.contentJSON !== "string" || raw.contentJSON.length > 1_000_000 || !/^[a-f0-9]{64}$/.test(String(raw.sha256)) || new TextEncoder().encode(raw.contentJSON).length !== raw.byteLength || sha256(raw.contentJSON) !== raw.sha256) throw incompletePractice();
  let content: unknown; try { content = JSON.parse(raw.contentJSON); } catch { throw incompletePractice(); }
  if (!practiceObject(content)) throw incompletePractice();
  const pack: Envelope<M> = { schemaVersion: 1, modeId, rulesVersion: PRACTICE_RULES, datasetVersion: raw.datasetVersion as string, contentPackVersion: raw.contentPackVersion as string, sourceUrls: raw.sourceUrls as string[], reviewStatus: "source-snapshot", contentJSON: raw.contentJSON, sha256: raw.sha256 as string, byteLength: Number(raw.byteLength) };
  return { pack, content };
}
export function readPracticeBase(raw: unknown, pack: Envelope<string>, duration: number) {
  if (!practiceObject(raw) || raw.schemaVersion !== 1 || raw.modeId !== pack.modeId || typeof raw.gameId !== "string" || !/^local-[a-f0-9]{32}$/.test(raw.gameId) || raw.packKey !== `${pack.modeId}:${pack.contentPackVersion}:${pack.sha256}` || raw.rulesVersion !== pack.rulesVersion || typeof raw.ended !== "boolean") throw incompletePractice();
  for (const key of ["createdAt", "updatedAt", "deadlineAt", "revision"]) if (!Number.isSafeInteger(raw[key]) || Number(raw[key]) < 0 || Number(raw[key]) > 8_640_000_000_000_000) throw incompletePractice();
  if (Number(raw.createdAt) <= 0 || Number(raw.updatedAt) < Number(raw.createdAt) || raw.deadlineAt !== Number(raw.createdAt) + duration || Number(raw.revision) > 100000) throw incompletePractice();
  return { schemaVersion: 1 as const, modeId: pack.modeId, gameId: raw.gameId, packKey: raw.packKey as string, rulesVersion: pack.rulesVersion, createdAt: Number(raw.createdAt), updatedAt: Number(raw.updatedAt), deadlineAt: Number(raw.deadlineAt), revision: Number(raw.revision), ended: raw.ended };
}
