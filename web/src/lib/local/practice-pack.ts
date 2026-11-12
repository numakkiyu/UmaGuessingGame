import { validSearchNames } from "@/lib/search";
import { validLocalizedNames } from "@/i18n/entities";
import { LocalizedNotice } from "@/lib/player-notice";
import type { PracticePack, PracticeCharacter } from "@/lib/domain/classic-practice";
import { sha256 } from "./checksum";

export const CLASSIC_RULES = "2.0.0-draft.1";
export const packKey = (pack: PracticePack) => `${pack.modeId}:${pack.contentPackVersion}:${pack.sha256}`;
const object = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const stringList = (v: unknown, max = 40): v is string[] => Array.isArray(v) && v.length <= max && v.every(x => typeof x === "string" && x.length <= 160);
const invalid = () => new LocalizedNotice("invalidPack");

export function readPracticePack(raw: unknown): { pack: PracticePack; characters: PracticeCharacter[] } {
  if (!object(raw) || raw.schemaVersion !== 1 || raw.modeId !== "classic" || raw.rulesVersion !== CLASSIC_RULES || raw.reviewStatus !== "source-snapshot") throw invalid();
  for (const key of ["datasetVersion","contentPackVersion"]) if (typeof raw[key] !== "string" || raw[key].length > 160 || !raw[key]) throw invalid();
  if (!stringList(raw.sourceUrls,10) || raw.sourceUrls.some(u=> !/^https:\/\//.test(u))) throw invalid();
  if (typeof raw.contentJSON !== "string" || raw.contentJSON.length > 1_000_000 || !/^[a-f0-9]{64}$/.test(String(raw.sha256))) throw invalid();
  if (new TextEncoder().encode(raw.contentJSON).length !== raw.byteLength || sha256(raw.contentJSON) !== raw.sha256) throw invalid();
  const content: unknown = JSON.parse(raw.contentJSON);
  if (!object(content) || !Array.isArray(content.characters) || content.characters.length < 2 || content.characters.length > 1000) throw invalid();
  const ids = new Set<string>();
  for (const c of content.characters as unknown[]) {
    if (!object(c)) throw invalid();
    if (typeof c.characterId !== "string" || !/^[A-Za-z0-9_-]{1,80}$/.test(c.characterId) || ids.has(c.characterId)) throw invalid();
    ids.add(c.characterId);
    if (!validLocalizedNames(c.localizedNames) || !validSearchNames(c.searchNames)) throw invalid();
    if (typeof c.displayName !== "string" || !c.displayName || c.displayName.length > 100) throw invalid();
    if (typeof c.imagePath !== "string" || (c.imagePath !== "" && !/^\/assets\/avatars\/[A-Za-z0-9_-]+\.(?:png|webp)$/.test(c.imagePath))) throw invalid();
    for (const k of ["sex","g1","g23","grade","dormitory"]) if (typeof c[k] !== "string" || c[k].length > 50) throw invalid();
    for (const k of ["starKnown","g1HasJpn","g23HasJpn","answerEligible"]) if (typeof c[k] !== "boolean") throw invalid();
    if (!Number.isInteger(c.star) || Number(c.star) < 0 || Number(c.star) > 5 || !stringList(c.aliases)) throw invalid();
    for (const k of ["surface","distance","runningStyle"]) if (!stringList(c[k],8)) throw invalid();
    for (const [k, valid] of [["surface",["草地","泥地"]],["distance",["短","英","中","长"]],["runningStyle",["逃","先","差","追"]]] as const) if ((c[k] as string[]).some(v=>!(valid as readonly string[]).includes(v))) throw invalid();
  }
  if (!(content.characters as PracticeCharacter[]).some(c=>c.answerEligible)) throw invalid();
  return {pack:raw as unknown as PracticePack, characters:content.characters as PracticeCharacter[]};
}
