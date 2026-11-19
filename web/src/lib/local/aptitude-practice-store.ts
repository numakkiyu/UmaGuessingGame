import type { components } from "@/lib/api/schema";
import type { DraftAptitudes, DraftTask } from "@/lib/api/draft";
import { claimGrid, DRAFT_DURATION, draftFeasibleSets, GRID_DURATION, gridSolution, lineupScore, publishedPracticePrice, SURFACES, DISTANCES, STYLES, type AptitudeCard, type PricedCard, type GridGame, type DraftGame } from "@/lib/domain/aptitude-practice";
import { AptitudeNotice } from "@/i18n/aptitude-notices";
import { validLocalizedNames } from "@/i18n/entities";
import { activityStore } from "./activity-store";
import { incompletePractice as invalid, practiceId, practiceObject as object, readPracticeBase, readPracticeEnvelope } from "./practice-envelope";
import { newPracticeId } from "./practice-store";
import { shufflePractice } from "./random-draw";

export type GridPack = components["schemas"]["AptitudePracticePack"];
export type DraftPack = components["schemas"]["DraftPracticePack"];
export type GridArchive = { schemaVersion: 1; game: GridGame; pack: GridPack };
export type DraftArchive = { schemaVersion: 1; game: DraftGame; pack: DraftPack };
export const aptitudePackKey = (p: GridPack | DraftPack) => `${p.modeId}:${p.contentPackVersion}:${p.sha256}`;
function readGrades(raw: unknown): DraftAptitudes {
  if (!object(raw)) throw invalid();
  const out: DraftAptitudes = { surface: {}, distance: {}, style: {} };
  for (const [axis, keys] of [["surface", SURFACES], ["distance", DISTANCES], ["style", STYLES]] as const) {
    const table = raw[axis]; if (!object(table)) throw invalid();
    for (const [key, value] of Object.entries(table)) {
      if (!keys.includes(key) || typeof value !== "string" || !/^[A-GS]$/.test(value)) throw invalid();
    }
    for (const key of keys) if (typeof table[key] === "string") out[axis][key] = table[key];
  }
  return out;
}
function readCard(raw: unknown): AptitudeCard {
  if (!object(raw) || !practiceId(raw.id) || typeof raw.name !== "string" || !raw.name || raw.name.length > 100 || typeof raw.imagePath !== "string" || !/^$|^\/assets\/avatars\/[A-Za-z0-9_-]+\.(png|webp)$/.test(raw.imagePath)) throw invalid();
  if (!validLocalizedNames(raw.localizedNames)) throw invalid();
  return { id: raw.id, name: raw.name, ...(raw.localizedNames ? { localizedNames: raw.localizedNames } : {}), imagePath: raw.imagePath, aptitudes: readGrades(raw.aptitudes) };
}
function uniqueIds(cards: readonly { id: string }[], min: number) { if (cards.length < min || cards.length > 1000 || new Set(cards.map(c => c.id)).size !== cards.length) throw invalid(); }
export function readGridPack(raw: unknown) {
  const { pack, content } = readPracticeEnvelope(raw, "aptitude-grid");
  if (!Array.isArray(content.cards)) throw invalid();
  const cards = content.cards.map(readCard); uniqueIds(cards, 9);
  if (!gridSolution(cards, "distance")) throw new AptitudeNotice("noGrid");
  return { pack, cards };
}
export function readDraftPack(raw: unknown) {
  const { pack, content } = readPracticeEnvelope(raw, "draft");
  if (!Array.isArray(content.cards) || !Array.isArray(content.tasks)) throw invalid();
  const cards: PricedCard[] = content.cards.map(rawCard => {
    const card = readCard(rawCard);
    if (!object(rawCard) || rawCard.price !== publishedPracticePrice(card.aptitudes)) throw invalid();
    return { ...card, price: rawCard.price as number };
  }); uniqueIds(cards, 12);
  const tasks: DraftTask[] = content.tasks.map(rawTask => {
    if (!object(rawTask) || (typeof rawTask.taskId !== "string" || !/^[\p{L}\p{N}][\p{L}\p{N}_-]{0,99}$/u.test(rawTask.taskId)) || typeof rawTask.surface !== "string" || !SURFACES.includes(rawTask.surface) || typeof rawTask.distance !== "string" || !DISTANCES.includes(rawTask.distance) || typeof rawTask.style !== "string" || !STYLES.includes(rawTask.style)) throw invalid();
    return { taskId: rawTask.taskId, surface: rawTask.surface, distance: rawTask.distance, style: rawTask.style };
  });
  if (tasks.length < 4 || tasks.length > 64 || new Set(tasks.map(t => t.taskId)).size !== tasks.length) throw invalid();
  return { pack, cards, tasks };
}
function baseGame(pack: GridPack | DraftPack, duration: number, now: number) { return { schemaVersion: 1 as const, gameId: newPracticeId(), packKey: aptitudePackKey(pack), rulesVersion: pack.rulesVersion, createdAt: now, updatedAt: now, deadlineAt: now + duration, ended: false, revision: 0 }; }
export function createGridGame(pack: GridPack, now = Date.now()): GridGame {
  const { cards } = readGridPack(pack);
  return { ...baseGame(pack, GRID_DURATION, now), modeId: "aptitude-grid", rowAxis: shufflePractice(["distance", "style"] as const, now)[0]!, cardIds: shufflePractice(cards.map(c => c.id), now), claims: [] };
}
export function createDraftGame(pack: DraftPack, now = Date.now()): DraftGame {
  const { cards, tasks } = readDraftPack(pack), market = shufflePractice(cards, now).slice(0, 12);
  if (draftFeasibleSets(market, 2) < 2) throw new AptitudeNotice("noDraft");
  return { ...baseGame(pack, DRAFT_DURATION, now), modeId: "draft", marketIds: market.map(c => c.id), taskIds: shufflePractice(tasks, now).slice(0, 4).map(t => t.taskId), draft: {}, submitted: null, submittedAt: null };
}
function readIds(raw: unknown, ids: string[], count: number): string[] {
  if (!Array.isArray(raw) || raw.length !== count || new Set(raw).size !== count || raw.some(id => typeof id !== "string" || !ids.includes(id))) throw invalid(); return [...raw] as string[];
}
export function validateGridArchive(raw: unknown): GridArchive {
  if (!object(raw) || raw.schemaVersion !== 1 || !object(raw.game)) throw invalid();
  const { pack, cards } = readGridPack(raw.pack), g = raw.game, base = readPracticeBase(g, pack, GRID_DURATION);
  if (g.rowAxis !== "distance" && g.rowAxis !== "style" || !Array.isArray(g.claims) || g.claims.length > 9) throw invalid();
  const cardIds = readIds(g.cardIds, cards.map(c => c.id), cards.length);
  const claims = g.claims.map(c => { if (!object(c) || !Number.isInteger(c.cellIndex) || typeof c.characterId !== "string" || !Number.isSafeInteger(c.at) || Number(c.at) < base.createdAt || Number(c.at) > base.updatedAt) throw invalid(); return { cellIndex: Number(c.cellIndex), characterId: c.characterId, at: Number(c.at) }; });
  let replay: GridGame = { ...base, modeId: "aptitude-grid", rowAxis: g.rowAxis, cardIds, claims: [], ended: false };
  let lastAt = base.createdAt;
  for (const c of claims) { if (c.at < lastAt) throw invalid(); replay = claimGrid(replay, cards, c.cellIndex, c.characterId, c.at); lastAt = c.at; }
  const game: GridGame = { ...base, modeId: "aptitude-grid", rowAxis: g.rowAxis, cardIds, claims };
  return { schemaVersion: 1, game, pack };
}
function readAssignments(raw: unknown, ids: string[], tasks: string[]): Record<string, string> {
  if (!object(raw) || Object.keys(raw).length > 4 || Object.entries(raw).some(([task, id]) => !tasks.includes(task) || typeof id !== "string" || !ids.includes(id))) throw invalid(); return Object.fromEntries(Object.entries(raw)) as Record<string, string>;
}
export function validateDraftArchive(raw: unknown): DraftArchive {
  if (!object(raw) || raw.schemaVersion !== 1 || !object(raw.game)) throw invalid();
  const { pack, cards, tasks } = readDraftPack(raw.pack), g = raw.game, base = readPracticeBase(g, pack, DRAFT_DURATION);
  const marketIds = readIds(g.marketIds, cards.map(c => c.id), 12), taskIds = readIds(g.taskIds, tasks.map(t => t.taskId), 4), market = cards.filter(c => marketIds.includes(c.id));
  if (draftFeasibleSets(market, 2) < 2) throw invalid();
  const draft = readAssignments(g.draft, marketIds, taskIds), submitted = g.submitted === null ? null : readAssignments(g.submitted, marketIds, taskIds);
  if (submitted) {
    if (!base.ended || !Number.isSafeInteger(g.submittedAt) || Number(g.submittedAt) < base.createdAt || Number(g.submittedAt) > base.updatedAt || Number(g.submittedAt) >= base.deadlineAt || taskIds.some(id => draft[id] !== submitted[id])) throw invalid();
    lineupScore(submitted, market, tasks.filter(t => taskIds.includes(t.taskId)));
  } else if (g.submittedAt !== null) throw invalid();
  const game: DraftGame = { ...base, modeId: "draft", marketIds, taskIds, draft, submitted, submittedAt: g.submittedAt as number | null };
  return { schemaVersion: 1, pack, game };
}
export const gridStore = activityStore<GridPack, GridGame>("umaguess-grid-practice", validateGridArchive, aptitudePackKey);
export const draftStore = activityStore<DraftPack, DraftGame>("umaguess-draft-practice", validateDraftArchive, aptitudePackKey);
export const volatileGridGames = new Map<string, GridArchive & { savedRevision?: number | null }>(), volatileDraftGames = new Map<string, DraftArchive & { savedRevision?: number | null }>();
