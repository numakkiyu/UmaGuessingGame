import catalog from "./catalog.json";
import type { Language } from "@/i18n";

export type Profile = {
  id: string;
  imagePath: string;
  imageWidth: number;
  imageHeight: number;
  names: Record<Language, string>;
  chineseNames: string[];
  english: string | null;
  cv: string[];
  birthday: { month: number; day: number };
  height: number;
  sources: string[];
};
export const profiles: readonly Profile[] = catalog.characters;
export const CATALOG_VERSION = catalog.version;
export const QUESTION_COUNT = 20;
export const FIELDS = ["name", "translation", "japanese", "english", "cv", "birthday", "height"] as const;
export type Field = typeof FIELDS[number];
export type Answer = Record<Field, string>;
export const emptyAnswer = (): Answer => Object.fromEntries(FIELDS.map(key => [key, ""])) as Answer;

const variants: Readonly<Record<string, string>> = catalog.normalization;
export function normalize(value: string): string {
  return Array.from(value.normalize("NFKC").toLocaleLowerCase("en").replace(/\s+/gu, ""))
    .map(char => variants[char] ?? char).join("");
}
function matches(value: string, names: readonly string[]): boolean {
  const key = normalize(value);
  return key.length > 0 && names.some(name => normalize(name) === key);
}
export function translations(profile: Profile): string[] {
  return [...new Map(profile.chineseNames.map(name => [normalize(name), name])).values()];
}
export function weights(profile: Profile): Record<Field, number> {
  const hasTranslations = translations(profile).length > 1;
  return { name: (hasTranslations ? 6 : 12) + (profile.english ? 0 : 2), translation: hasTranslations ? 6 : 0, japanese: 4, english: profile.english ? 2 : 0, cv: 1, birthday: .5, height: .5 };
}
export function parseBirthday(value: string): { month: number; day: number } | null {
  const match = normalize(value).match(/^(\d{1,2})(?:月|[-/.])(\d{1,2})日?$/);
  if (!match) return null;
  const month = Number(match[1]), day = Number(match[2]);
  const days = [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1]! ? { month, day } : null;
}
export function scoreAnswer(profile: Profile, answer: Answer) {
  const weight = weights(profile), birthday = parseBirthday(answer.birthday);
  const otherNames = answer.translation.split(/[,，、;；\n]+/u).map(s => s.trim()).filter(Boolean);
  // A single, different translation earns this field once; repeated script variants do not.
  const other = otherNames.some(name => normalize(name) !== normalize(answer.name) && matches(name, profile.chineseNames));
  const correct: Record<Field, boolean> = {
    name: matches(answer.name, [...profile.chineseNames, profile.names.ja]),
    translation: other,
    japanese: matches(answer.japanese, [profile.names.ja]),
    english: profile.english !== null && matches(answer.english, [profile.english]),
    cv: matches(answer.cv, profile.cv),
    birthday: birthday?.month === profile.birthday.month && birthday?.day === profile.birthday.day,
    height: /^\d{2,3}(?:cm)?$/.test(normalize(answer.height)) && Number(normalize(answer.height).replace(/cm$/, "")) === profile.height,
  };
  const points = Object.fromEntries(FIELDS.map(key => [key, correct[key] ? weight[key] : 0])) as Record<Field, number>;
  return { points, weight, total: FIELDS.reduce((sum, key) => sum + points[key], 0) };
}

/** Mulberry32 uses explicit 32-bit arithmetic, with an unbiased Fisher–Yates draw. */
export function questions(seed: number, count = QUESTION_COUNT, pool: readonly Profile[] = profiles): Profile[] {
  if (!Number.isInteger(seed) || seed < 0 || seed > 0xffffffff || !Number.isInteger(count) || count < 1 || count > pool.length) throw new Error("Invalid concentration draw");
  if (new Set(pool.map(p => p.id)).size !== pool.length) throw new Error("Duplicate concentration profile");
  let state = seed >>> 0;
  const next = () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let n = Math.imul(state ^ (state >>> 15), 1 | state);
    n ^= n + Math.imul(n ^ (n >>> 7), 61 | n);
    return (n ^ (n >>> 14)) >>> 0;
  };
  const below = (bound: number) => {
    const limit = 0x100000000 - (0x100000000 % bound);
    let n: number;
    do { n = next(); } while (n >= limit);
    return n % bound;
  };
  const poolCopy = [...pool];
  for (let i = poolCopy.length - 1; i > 0; i--) {
    const j = below(i + 1);
    [poolCopy[i], poolCopy[j]] = [poolCopy[j]!, poolCopy[i]!];
  }
  return poolCopy.slice(0, count);
}

export type ConcentrationGame = {
  schemaVersion: 1; modeId: "concentration"; catalogVersion: string;
  seed: number; startedAt: number; finishedAt: number | null;
  activeIndex: number; answers: Answer[];
};
export function createGame(seed: number, now = Date.now()): ConcentrationGame {
  questions(seed);
  return { schemaVersion: 1, modeId: "concentration", catalogVersion: CATALOG_VERSION, seed, startedAt: now, finishedAt: null,
    activeIndex: 0, answers: Array.from({ length: QUESTION_COUNT }, emptyAnswer) };
}
export function rankFor(concentration: number) {
  return concentration >= 95 ? "SSS" : concentration >= 90 ? "SS" : concentration >= 80 ? "S" : concentration >= 60 ? "A" : concentration >= 40 ? "B" : concentration >= 30 ? "C" : "D";
}
export function gameResult(game: ConcentrationGame) {
  const items = questions(game.seed).map((profile, i) => ({ profile, answer: game.answers[i]!, score: scoreAnswer(profile, game.answers[i]!) }));
  const score = items.reduce((n, item) => n + item.score.total, 0);
  const accuracy = Math.round(items.filter(item => item.score.points.name > 0).length / items.length * 100);
  const detailMax = items.reduce((n, item) => n + 20 - item.score.weight.name, 0);
  const detailScore = items.reduce((n, item) => n + item.score.total - item.score.points.name, 0);
  const completeness = Math.round(detailScore / detailMax * 100);
  const relevance = Math.round(score / (items.length * 20) * 100);
  const concentration = Math.round((accuracy + completeness + relevance) / 3);
  const rank = rankFor(concentration);
  return { items, score, max: items.length * 20, accuracy, completeness, relevance, concentration, rank };
}
export function hasAnswer(answer: Answer): boolean { return FIELDS.some(key => answer[key].trim().length > 0); }
export const STORAGE_KEY = "umaguess:concentration:game:v1";
export function decodeGame(raw: string): ConcentrationGame | null {
  if (raw.length > 100_000) return null;
  try {
    const v: unknown = JSON.parse(raw);
    if (!v || typeof v !== "object" || Array.isArray(v)) return null;
    const g = v as Record<string, unknown>;
    if (g.schemaVersion !== 1 || g.modeId !== "concentration" ||
      !(g.catalogVersion === CATALOG_VERSION || typeof g.catalogVersion === "string" && catalog.compatibleVersions.includes(g.catalogVersion)) ||
      typeof g.seed !== "number" || !Number.isInteger(g.seed) || g.seed < 0 || g.seed > 0xffffffff ||
      typeof g.startedAt !== "number" || !Number.isSafeInteger(g.startedAt) || g.startedAt < 0 ||
      !(g.finishedAt === null || typeof g.finishedAt === "number" && Number.isSafeInteger(g.finishedAt) && g.finishedAt >= g.startedAt) ||
      typeof g.activeIndex !== "number" || !Number.isInteger(g.activeIndex) || g.activeIndex < 0 || g.activeIndex >= QUESTION_COUNT ||
      !Array.isArray(g.answers) || g.answers.length !== QUESTION_COUNT) return null;
    const answers: Answer[] = [];
    for (const answer of g.answers) {
      if (!answer || typeof answer !== "object" || Array.isArray(answer) || !FIELDS.every(key => typeof answer[key] === "string" && answer[key].length <= 120)) return null;
      answers.push(Object.fromEntries(FIELDS.map(key => [key, answer[key]])) as Answer);
    }
    return { schemaVersion: 1, modeId: "concentration", catalogVersion: CATALOG_VERSION, seed: g.seed,
      startedAt: g.startedAt, finishedAt: g.finishedAt, activeIndex: g.activeIndex, answers };
  } catch { return null; }
}
