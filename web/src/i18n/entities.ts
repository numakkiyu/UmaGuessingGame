import i18n, { matchLanguage, type Language } from ".";
export type LocalizedNames = Partial<Record<Language, string>>;
export function characterName(character: { displayName: string; localizedNames?: LocalizedNames }, language = i18n.resolvedLanguage): string {
  const locale = matchLanguage(language ?? "") ?? "zh-Hans";
  return character.localizedNames?.[locale] || character.displayName;
}
export function formatDateTime(value: number | string): string {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "—";
  const locale = { "zh-Hans": "zh-CN", "zh-Hant": "zh-TW", ja: "ja-JP" }[matchLanguage(i18n.resolvedLanguage ?? "") ?? "zh-Hans"];
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
}
/** Optional metadata on older archives; no guessing or automatic conversion. */
export function validLocalizedNames(value: unknown): value is LocalizedNames | undefined {
  if (value === undefined) return true;
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  return Object.entries(value).every(([key, name]) => ["zh-Hans", "zh-Hant", "ja"].includes(key) && typeof name === "string" && name.trim().length > 0 && name.length <= 100);
}

export function cardName(card: { name: string; localizedNames?: LocalizedNames }, language = i18n.resolvedLanguage): string {
  return characterName({ displayName: card.name, ...(card.localizedNames ? { localizedNames: card.localizedNames } : {}) }, language);
}

/** Search all source-backed regional names while retaining the original card. */
export function cardMatches(card: { name: string; localizedNames?: LocalizedNames }, query: string): boolean {
  const normalize = (value: string) => value.normalize("NFKC").toLowerCase().replace(/[\s\u3000]+/g, "");
  const needle = normalize(query);
  return [card.name, ...Object.values(card.localizedNames ?? {})].some(name => name && normalize(name).includes(needle));
}
