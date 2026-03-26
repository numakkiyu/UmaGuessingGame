import { promises as fs } from "node:fs";
import path from "node:path";
import { assetManifestEntrySchema, questionBankEntrySchema } from "../src/lib/validation/schemas";

type RawCharacter = Record<string, unknown>;
type ManualProfileValue = {
  sex_type?: string | null;
  g1_bracket?: string | null;
  g23_bracket?: string | null;
  g1_has_jpn?: boolean | null;
  g23_has_jpn?: boolean | null;
};
type ManualProfile = Record<string, ManualProfileValue>;
type FieldOverride = Record<string, Record<string, unknown>>;

type ManifestItem = {
  order: number;
  name_zh: string;
  name_jp: string;
  official_en?: string | null;
  file_name: string;
};

type FlatQuestionBankEntry = {
  id: string;
  name_zh: string;
  name_tw: string | null;
  name_jp: string;
  name_en: string | null;
  aliases: string[];
  star: number | null;
  surface_group: string[] | null;
  distance_group: string[] | null;
  running_style_group: string[] | null;
  sex_type: string | null;
  g1_bracket: string | null;
  g23_bracket: string | null;
  g1_has_jpn: boolean;
  g23_has_jpn: boolean;
  school_grade: string | null;
  dormitory: string | null;
  image_url: string | null;
  image_local_path: string;
  asset_id: string;
  source_priority: string[];
};

const rootDir = process.cwd();
const dataDir = path.join(rootDir, "data");
const charactersDir = path.join(dataDir, "characters");
const normalizedCharactersDir = path.join(dataDir, "normalized", "characters");
const manualDir = path.join(dataDir, "manual");
const assetsDir = path.join(dataDir, "assets");
const readyPath = path.join(dataDir, "question_bank_ready.json");
const candidatesPath = path.join(dataDir, "question_bank_candidates.json");
const blockedPath = path.join(dataDir, "question_bank_blocked.json");
const racingProfilePath = path.join(manualDir, "racing-profile.json");
const fieldOverridesPath = path.join(manualDir, "field-overrides.json");
const racingProfileTodoPath = path.join(manualDir, "racing-profile.todo.json");
const orderManifestPath = path.join(charactersDir, "_order_manifest.json");

const DISTANCE_ORDER = ["短", "英", "中", "长"] as const;
const STYLE_ORDER = ["逃", "先", "差", "追"] as const;
const SURFACE_ORDER = ["草地", "泥地"] as const;
const SEX_VALUES = new Set(["牡", "牝"]);
const G1_VALUES = new Set(["0", "1-2", "3-4", "5+"]);
const G23_VALUES = new Set(["0", "1-3", "4-6", "7+"]);
const SCHOOL_VALUES = new Set(["中等部", "高等部"]);
const DORM_MAP = new Map<string, string>([
  ["栗東寮", "栗东宿舍"],
  ["栗东寮", "栗东宿舍"],
  ["栗東宿舍", "栗东宿舍"],
  ["栗东宿舍", "栗东宿舍"],
  ["美浦寮", "美浦宿舍"],
  ["美浦宿舍", "美浦宿舍"],
  ["独居", "独居"],
  ["一人暮らし", "独居"],
]);

async function readJson<T>(filePath: string, fallback: T): Promise<T> {
  try {
    return JSON.parse(await fs.readFile(filePath, "utf8")) as T;
  } catch {
    return fallback;
  }
}

async function writeJson(filePath: string, value: unknown) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
  await fs.writeFile(filePath, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

async function ensureObjectFile(filePath: string) {
  try {
    await fs.access(filePath);
  } catch {
    await writeJson(filePath, {});
  }
}

function cleanString(value: unknown): string | null {
  if (typeof value !== "string") {
    return null;
  }
  const text = value.trim();
  return text.length > 0 ? text : null;
}

function cleanStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }

  const seen = new Set<string>();
  const results: string[] = [];
  for (const item of value) {
    const text = cleanString(item);
    if (!text || seen.has(text)) {
      continue;
    }
    seen.add(text);
    results.push(text);
  }
  return results;
}

function extFromUrl(url: string | null) {
  if (!url) {
    return "png";
  }
  try {
    const ext = path.extname(new URL(url).pathname).replace(".", "");
    return ext || "png";
  } catch {
    return "png";
  }
}

function deriveTraditionalName(source: { aliases: string[]; nameZh: string }) {
  const preferred = source.aliases.filter(
    (alias) =>
      alias !== source.nameZh &&
      /[一-龥]/.test(alias) &&
      /[體週東滙灣廣聲處將鬥訓奧賽華貝]/.test(alias),
  );
  const rank = (alias: string) => {
    const hasBracket = alias.startsWith("【") ? 10 : 0;
    const hasParen = alias.includes("（") ? 5 : 0;
    return hasBracket + hasParen;
  };
  const sorted = [...preferred].sort((left, right) => {
    return rank(left) - rank(right) || left.length - right.length || left.localeCompare(right, "zh-Hans-CN");
  });
  return sorted[0] ?? null;
}

function normalizeSchoolGrade(value: unknown) {
  const text = cleanString(value);
  if (!text) {
    return null;
  }
  if (text.includes("中等部")) {
    return "中等部";
  }
  if (text.includes("高等部")) {
    return "高等部";
  }
  return SCHOOL_VALUES.has(text) ? text : null;
}

function normalizeDormitory(value: unknown) {
  const text = cleanString(value);
  if (!text) {
    return null;
  }
  return DORM_MAP.get(text) ?? null;
}

function normalizeNullableEnum(value: unknown, allowed: Set<string>) {
  const text = cleanString(value);
  return text && allowed.has(text) ? text : null;
}

function normalizeBoolean(value: unknown) {
  return value === true;
}

function normalizeInteger(value: unknown) {
  if (typeof value === "number" && Number.isInteger(value)) {
    return value;
  }
  const text = cleanString(value);
  if (!text) {
    return null;
  }
  const parsed = Number.parseInt(text, 10);
  return Number.isFinite(parsed) ? parsed : null;
}

function normalizeImageUrl(value: unknown) {
  const text = cleanString(value);
  if (!text) {
    return null;
  }
  try {
    return new URL(text).toString();
  } catch {
    return null;
  }
}

function normalizeUrl(value: unknown) {
  const text = cleanString(value);
  if (!text) {
    return null;
  }
  try {
    return new URL(text).toString();
  } catch {
    return null;
  }
}

function normalizeImageUrls(value: unknown) {
  const seen = new Set<string>();
  const results: string[] = [];
  for (const item of Array.isArray(value) ? value : []) {
    const url = normalizeImageUrl(item);
    if (!url || seen.has(url)) {
      continue;
    }
    seen.add(url);
    results.push(url);
  }
  return results;
}

function normalizeUrls(value: unknown) {
  const seen = new Set<string>();
  const results: string[] = [];
  for (const item of Array.isArray(value) ? value : []) {
    const url = normalizeUrl(item);
    if (!url || seen.has(url)) {
      continue;
    }
    seen.add(url);
    results.push(url);
  }
  return results;
}

function sortByOrder(values: string[], order: readonly string[]) {
  const rank = new Map(order.map((value, index) => [value, index]));
  return [...values].sort((left, right) => {
    const leftRank = rank.get(left) ?? Number.MAX_SAFE_INTEGER;
    const rightRank = rank.get(right) ?? Number.MAX_SAFE_INTEGER;
    return leftRank - rightRank || left.localeCompare(right, "zh-Hans-CN");
  });
}

function normalizeGroup(values: unknown, order: readonly string[]) {
  const cleaned = cleanStringArray(values).filter((value) => order.includes(value));
  if (cleaned.length === 0) {
    return null;
  }
  return sortByOrder([...new Set(cleaned)], order);
}

function cleanupMaybeRecord(value: unknown) {
  return value && typeof value === "object" && !Array.isArray(value) ? value : null;
}

function hasBaseFields(entry: FlatQuestionBankEntry) {
  return Boolean(
    entry.name_zh &&
      entry.name_jp &&
      entry.aliases.length > 0 &&
      entry.star !== null &&
      entry.surface_group &&
      entry.surface_group.length > 0 &&
      entry.distance_group &&
      entry.distance_group.length > 0 &&
      entry.running_style_group &&
      entry.running_style_group.length > 0 &&
      entry.school_grade &&
      entry.dormitory &&
      entry.image_url,
  );
}

function hasProductionFields(entry: FlatQuestionBankEntry) {
  return Boolean(
    hasBaseFields(entry) &&
      entry.sex_type &&
      entry.g1_bracket &&
      entry.g23_bracket,
  );
}

function buildMissingFields(entry: FlatQuestionBankEntry) {
  const missing: string[] = [];
  if (entry.star === null) missing.push("star");
  if (!entry.surface_group?.length) missing.push("surface_group");
  if (!entry.distance_group?.length) missing.push("distance_group");
  if (!entry.running_style_group?.length) missing.push("running_style_group");
  if (!entry.school_grade) missing.push("school_grade");
  if (!entry.dormitory) missing.push("dormitory");
  if (!entry.sex_type) missing.push("sex_type");
  if (!entry.g1_bracket) missing.push("g1_bracket");
  if (!entry.g23_bracket) missing.push("g23_bracket");
  if (!entry.image_url) missing.push("image_url");
  return missing;
}

function toReadyEntry(entry: FlatQuestionBankEntry) {
  return questionBankEntrySchema.parse({
    id: entry.id,
    name_zh: entry.name_zh,
    name_tw: entry.name_tw,
    name_jp: entry.name_jp,
    name_en: entry.name_en,
    aliases: entry.aliases,
    star: entry.star,
    surface_group: entry.surface_group,
    distance_group: entry.distance_group,
    running_style_group: entry.running_style_group,
    sex_type: entry.sex_type,
    g1_bracket: entry.g1_bracket,
    g23_bracket: entry.g23_bracket,
    g1_has_jpn: entry.g1_has_jpn,
    g23_has_jpn: entry.g23_has_jpn,
    school_grade: entry.school_grade,
    dormitory: entry.dormitory,
    image_url: entry.image_url,
    image_local_path: entry.image_local_path,
    asset_id: entry.asset_id,
    source_priority: entry.source_priority,
  });
}

async function main() {
  await ensureObjectFile(racingProfilePath);
  await ensureObjectFile(fieldOverridesPath);

  const manifest = await readJson<{ items?: ManifestItem[] }>(orderManifestPath, { items: [] });
  const manifestByFile = new Map((manifest.items ?? []).map((item) => [item.file_name, item]));
  const manualProfiles = await readJson<ManualProfile>(racingProfilePath, {});
  const fieldOverrides = await readJson<FieldOverride>(fieldOverridesPath, {});

  const fileNames = (await fs.readdir(charactersDir))
    .filter((fileName) => fileName.endsWith(".json") && fileName !== "_order_manifest.json")
    .sort();

  await fs.rm(normalizedCharactersDir, { recursive: true, force: true });

  const candidates: FlatQuestionBankEntry[] = [];
  const ready: ReturnType<typeof questionBankEntrySchema.parse>[] = [];
  const blocked: Array<Record<string, unknown>> = [];
  const assetManifest: unknown[] = [];
  const racingProfileTodo: Array<Record<string, unknown>> = [];

  for (const fileName of fileNames) {
    const original = await readJson<RawCharacter>(path.join(charactersDir, fileName), {});
    const characterId = cleanString(original.id);
    const character = {
      ...original,
      ...(characterId ? fieldOverrides[characterId] ?? {} : {}),
    } as RawCharacter;

    const manifestItem = manifestByFile.get(fileName);
    const order = manifestItem?.order ?? Number.parseInt(fileName.slice(0, 3), 10);
    const releaseStatus = order <= 133 ? "released" : "unreleased";
    const aliases = cleanStringArray(character.aliases);
    const nameZh = cleanString(character.name_zh) ?? manifestItem?.name_zh ?? fileName.replace(/\.json$/u, "");
    const nameJp = cleanString(character.name_jp) ?? manifestItem?.name_jp ?? "";
    const nameEn = cleanString(manifestItem?.official_en ?? null);
    const nameTw = deriveTraditionalName({ aliases, nameZh });
    const mergedAliases = cleanStringArray([nameZh, nameTw, nameJp, nameEn, ...aliases]);
    const imageUrl = normalizeImageUrl(character.image_url);
    const imageUrls = normalizeImageUrls([imageUrl, ...(Array.isArray(character.image_urls) ? character.image_urls : [])]);
    const profile = manualProfiles[characterId ?? ""] ?? {};
    const assetId = `${characterId ?? fileName.replace(/\.json$/u, "")}-thumb`;
    const imageLocalPath = `/api/assets/${assetId}`;

    const flatEntry: FlatQuestionBankEntry = {
      id: characterId ?? fileName.replace(/\.json$/u, ""),
      name_zh: nameZh,
      name_tw: nameTw,
      name_jp: nameJp,
      name_en: nameEn,
      aliases: mergedAliases,
      star: normalizeInteger(character.star),
      surface_group: normalizeGroup(character.surface_group, SURFACE_ORDER),
      distance_group: normalizeGroup(character.distance_group, DISTANCE_ORDER),
      running_style_group: normalizeGroup(character.running_style_group, STYLE_ORDER),
      sex_type: normalizeNullableEnum(profile.sex_type, SEX_VALUES),
      g1_bracket: normalizeNullableEnum(profile.g1_bracket, G1_VALUES),
      g23_bracket: normalizeNullableEnum(profile.g23_bracket, G23_VALUES),
      g1_has_jpn: normalizeBoolean(profile.g1_has_jpn),
      g23_has_jpn: normalizeBoolean(profile.g23_has_jpn),
      school_grade: normalizeSchoolGrade(character.grade),
      dormitory: normalizeDormitory(character.dormitory),
      image_url: imageUrl,
      image_local_path: imageLocalPath,
      asset_id: assetId,
      source_priority: cleanStringArray(character.source_priority),
    };

    const normalizedCharacter = {
      id: flatEntry.id,
      order,
      file_name: fileName,
      release_status: releaseStatus,
      names: {
        zh_cn: flatEntry.name_zh,
        zh_tw: flatEntry.name_tw,
        jp: flatEntry.name_jp,
        en: flatEntry.name_en,
      },
      aliases: flatEntry.aliases,
      profile: {
        birthday: cleanString(character.birthday),
        birthday_raw: cleanString(character.birthday_raw),
        birthday_day_of_year: typeof character.birthday_day_of_year === "number" ? character.birthday_day_of_year : null,
        height_cm: typeof character.height_cm === "number" ? character.height_cm : normalizeInteger(character.height_cm),
        height_raw: cleanString(character.height_raw),
        cv: cleanString(character.cv),
        cv_raw: cleanString(character.cv_raw),
        school_grade: flatEntry.school_grade,
        grade_raw: cleanString(character.grade_raw ?? character.grade),
        dormitory: flatEntry.dormitory,
        dorm_raw: cleanString(character.dorm_raw ?? character.dormitory),
        star: flatEntry.star,
        star_raw: cleanString(character.star_raw ?? character.star),
        sex_type: flatEntry.sex_type,
      },
      racing: {
        surface_group: flatEntry.surface_group,
        distance_group: flatEntry.distance_group,
        running_style_group: flatEntry.running_style_group,
        aptitudes: cleanupMaybeRecord(character.aptitudes),
        g1_bracket: flatEntry.g1_bracket,
        g23_bracket: flatEntry.g23_bracket,
        g1_has_jpn: flatEntry.g1_has_jpn,
        g23_has_jpn: flatEntry.g23_has_jpn,
      },
      media: {
        image_url: flatEntry.image_url,
        image_urls: imageUrls,
        image_local_path: flatEntry.image_local_path,
        asset_id: flatEntry.asset_id,
      },
      sources: {
        source_pages: cleanStringArray(character.source_pages),
        source_urls: normalizeUrls(character.source_urls),
        source_priority: flatEntry.source_priority,
        image_sources: cleanupMaybeRecord(character.image_sources),
      },
      extra: {
        introduction: cleanString(character.introduction),
        intro_paragraphs: cleanStringArray(character.intro_paragraphs),
        quote_raw: cleanString(character.quote_raw),
        weight_desc_raw: cleanString(character.weight_desc_raw),
        bust_raw: cleanString(character.bust_raw),
        waist_raw: cleanString(character.waist_raw),
        hip_raw: cleanString(character.hip_raw),
      },
      raw: character.raw ?? null,
    };

    await writeJson(path.join(normalizedCharactersDir, fileName), normalizedCharacter);

    assetManifest.push(
      assetManifestEntrySchema.parse({
        asset_id: assetId,
        character_id: flatEntry.id,
        source_site: flatEntry.source_priority[0] ?? "unknown",
        source_page: "character_avatar",
        source_url: flatEntry.image_url ?? "https://example.com/missing-asset",
        local_path: `public/assets/characters/thumbnails/${assetId}.${extFromUrl(flatEntry.image_url)}`,
        downloaded_at: null,
        usage_scope: "search_and_guess_table",
        copyright_note: "Source tracked for controlled proxy and cache use.",
        cache_status: "missing",
        last_checked_at: null,
      }),
    );

    if (releaseStatus === "released" && hasBaseFields(flatEntry)) {
      candidates.push(flatEntry);
      if (!flatEntry.sex_type || !flatEntry.g1_bracket || !flatEntry.g23_bracket) {
        racingProfileTodo.push({
          id: flatEntry.id,
          order,
          file_name: fileName,
          name_zh: flatEntry.name_zh,
          name_jp: flatEntry.name_jp,
          sex_type: flatEntry.sex_type,
          g1_bracket: flatEntry.g1_bracket,
          g23_bracket: flatEntry.g23_bracket,
          source_urls: cleanStringArray(character.source_urls),
        });
      }

      if (hasProductionFields(flatEntry)) {
        ready.push(toReadyEntry(flatEntry));
      } else {
        blocked.push({
          file_name: fileName,
          id: flatEntry.id,
          name_zh: flatEntry.name_zh,
          order,
          release_status: releaseStatus,
          reason: "missing_production_fields",
          missing_fields: buildMissingFields(flatEntry),
        });
      }
      continue;
    }

    blocked.push({
      file_name: fileName,
      id: flatEntry.id,
      name_zh: flatEntry.name_zh,
      order,
      release_status: releaseStatus,
      reason: releaseStatus === "unreleased" ? "unreleased" : "missing_base_fields",
      missing_fields: buildMissingFields(flatEntry).filter((field) =>
        ["star", "surface_group", "distance_group", "running_style_group", "school_grade", "dormitory", "image_url"].includes(field),
      ),
    });
  }

  await writeJson(candidatesPath, candidates);
  await writeJson(readyPath, ready);
  await writeJson(blockedPath, blocked);
  await writeJson(racingProfileTodoPath, racingProfileTodo);
  await writeJson(path.join(assetsDir, "asset-manifest.json"), assetManifest);
  await writeJson(path.join(assetsDir, "cache-index.json"), {});

  console.log(`normalized characters written: ${fileNames.length}`);
  console.log(`candidate entries: ${candidates.length}`);
  console.log(`ready entries: ${ready.length}`);
  console.log(`blocked entries: ${blocked.length}`);
  console.log(`racing profile todo entries: ${racingProfileTodo.length}`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
