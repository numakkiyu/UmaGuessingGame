import type { LocalizedNames } from "@/i18n/entities";
import { characterName } from "@/i18n/entities";
import type { Language } from "@/i18n";
import type { components } from "@/lib/api/schema";
export type CharacterSearchNames = components["schemas"]["CharacterSearchNames"];
/**
 * 多语言别名搜索（移植自旧 src/components/search-input.tsx + src/lib/game/search.ts）。
 *
 * 移植改动：
 * - 去掉 Next 依赖、旧 API 类型（SearchIndexEntry）与旧代理图片字段。
 * - 数据源改为契约 Character 投影（characterId/displayName/aliases）。
 * - ：输入法 composition 时不抢提交；键盘上下选项、Enter 选择、Escape 关闭；
 * 屏幕阅读器可获知结果数；空输入、无结果、图片失败、请求延迟分别处理。
 * - 中文/日文/英文/审核别名统一归一化匹配。
 */

export type SearchableCharacter = {
  characterId: string;
  displayName: string;
  localizedNames?: LocalizedNames;
  searchNames?: CharacterSearchNames;
  aliases: readonly string[];
};

/** Old saved packs remain usable; unclassified aliases are never relabelled. */
export function validSearchNames(value: unknown): value is CharacterSearchNames | undefined {
  if (value === undefined) return true;
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const name = (v: unknown) => typeof v === "string" && v.trim().length > 0 && v.length <= 160;
  return Object.entries(value).every(([key, v]) => {
    if (key === "japanese" || key === "english") return name(v);
    return (key === "translations" || key === "nicknames") && Array.isArray(v) && v.length <= 40 && v.every(name);
  });
}

export function searchDisplayName(character: SearchableCharacter, language: Language): string | undefined {
  if (language === "ja") return character.searchNames?.japanese || character.localizedNames?.ja || character.searchNames?.english;
  return characterName(character, language);
}

export type SearchNameRow = { kind: "otherNames" | "japanese" | "english" | "nicknames"; names: string[]; lang?: string };
export function searchNameRows(character: SearchableCharacter, language: Language): SearchNameRow[] {
  const details = character.searchNames;
  const main = searchDisplayName(character, language);
  const seen = new Set([normalizeQuery(main ?? "")]);
  const rows: SearchNameRow[] = [];
  const add = (kind: SearchNameRow["kind"], values: readonly string[], lang?: string) => {
    const names = values.filter(value => {
      const key = normalizeQuery(value);
      if (!key || seen.has(key)) return false;
      seen.add(key);
      return true;
    });
    if (names.length) rows.push({ kind, names, ...(lang ? { lang } : {}) });
  };
  if (language !== "ja") {
    add("otherNames", details?.translations ?? [character.displayName, character.localizedNames?.["zh-Hans"] ?? "", character.localizedNames?.["zh-Hant"] ?? ""]);
    add("japanese", [details?.japanese || character.localizedNames?.ja || ""], "ja");
  }
  add("english", [details?.english ?? ""], "en");
  if (language === "zh-Hans") add("nicknames", details?.nicknames ?? []);
  return rows;
}

/**
 * 归一化：全角转半角、大小写折叠、去掉空白与常见分隔符。
 * 不做激进假名转换——日文名本身在 aliases 里，避免把不同角色归到一起。
 */
export function normalizeQuery(input: string): string {
  return input
    .normalize("NFKC")
    .toLowerCase()
    .replace(/[\s\u3000]+/g, "")
    .replace(/[·・.,、。'’"“”\-_/\\()（）[\]【】]/g, "");
}

export type SearchOptions = {
  limit?: number;
  /** 排除已猜过的角色（旧 excludeGuessedSearchEntries 行为） */
  excludeIds?: ReadonlySet<string>;
};

export type SearchHit<T extends SearchableCharacter> = {
  character: T;
  /** 命中来源：主名 / 别名；用于界面解释为什么出现 */
  matchedOn: "name" | "alias";
};

/** 多语言别名搜索：主名优先，其次别名；前缀命中排在包含命中之前。 */
export function searchCharacters<T extends SearchableCharacter>(
  items: readonly T[],
  rawQuery: string,
  options: SearchOptions = {},
): SearchHit<T>[] {
  const query = normalizeQuery(rawQuery);
  if (!query) return [];

  const limit = options.limit ?? 8;
  const exclude = options.excludeIds;
  const scored: { hit: SearchHit<T>; rank: number }[] = [];

  for (const character of items) {
    if (exclude?.has(character.characterId)) continue;

    const name = normalizeQuery(character.displayName);
    if (name.startsWith(query)) {
      scored.push({ hit: { character, matchedOn: "name" }, rank: 0 });
      continue;
    }
    if (name.includes(query)) {
      scored.push({ hit: { character, matchedOn: "name" }, rank: 1 });
      continue;
    }

    let bestAliasRank = Number.POSITIVE_INFINITY;
    const details = character.searchNames;
    for (const alias of [...character.aliases, ...Object.values(character.localizedNames ?? {}), ...(details?.translations ?? []), details?.japanese ?? "", details?.english ?? "", ...(details?.nicknames ?? [])]) {
      const normalized = normalizeQuery(alias);
      if (!normalized) continue;
      if (normalized.startsWith(query)) {
        bestAliasRank = Math.min(bestAliasRank, 2);
      } else if (normalized.includes(query)) {
        bestAliasRank = Math.min(bestAliasRank, 3);
      }
    }
    if (bestAliasRank !== Number.POSITIVE_INFINITY) {
      scored.push({ hit: { character, matchedOn: "alias" }, rank: bestAliasRank });
    }
  }

  scored.sort((a, b) => {
    if (a.rank !== b.rank) return a.rank - b.rank;
    // 稳定次序：按显示名排序，保证同分结果顺序确定（不在 render 中改服务器数组，）
    return a.hit.character.displayName.localeCompare(b.hit.character.displayName, "zh-Hans-CN");
  });

  return scored.slice(0, limit).map((entry) => entry.hit);
}
