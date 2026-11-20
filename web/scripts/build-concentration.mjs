import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { fileURLToPath, pathToFileURL } from "node:url";
import { join } from "node:path";

const root = fileURLToPath(new URL("../..", import.meta.url));
const sha256 = value => createHash("sha256").update(value).digest("hex");
const requireValue = (condition, message) => { if (!condition) throw new Error(message); };
const text = value => typeof value === "string" && value.trim().length > 0 && value.length <= 120;
const unique = values => [...new Set(values.filter(Boolean))];
const sourceUrl = value => {
  try {
    const url = new URL(value);
    return url.protocol === "https:" && !url.username && !url.password && !url.port &&
      ["umamusume.jp", "wiki.biligame.com", "zh.moegirl.org.cn"].includes(url.hostname);
  } catch { return false; }
};

/** Standing assets use the extended WebP container, including alpha and canvas size. */
export function standingSize(image) {
  requireValue(image.length >= 30 && image.toString("ascii", 0, 4) === "RIFF" &&
    image.readUInt32LE(4) + 8 === image.length && image.toString("ascii", 8, 12) === "WEBP" &&
    image.toString("ascii", 12, 16) === "VP8X" && image.readUInt32LE(16) === 10, "Invalid standing WebP container");
  return { width: image.readUIntLE(24, 3) + 1, height: image.readUIntLE(27, 3) + 1 };
}

export function buildCatalog(wiki, snapshot, normalization, normalized, readImage) {
  requireValue(Array.isArray(wiki.characters) && wiki.characters.length >= 20, "Incomplete concentration roster");
  requireValue(Array.isArray(snapshot.profiles) && Array.isArray(snapshot.legacyRoster), "Missing profile snapshot");
  requireValue(normalization.characters && typeof normalization.characters === "object" && !Array.isArray(normalization.characters), "Invalid normalization map");
  const variants = normalization.characters;
  for (const [from, to] of Object.entries(variants)) {
    requireValue(Array.from(from).length === 1 && typeof to === "string" && Array.from(to).length === 1 && (!variants[to] || variants[to] === to), "Invalid normalization mapping");
  }
  const normalize = value => Array.from(value.normalize("NFKC").toLocaleLowerCase("en").replace(/\s+/gu, "")).map(char => variants[char] ?? char).join("");
  const profiles = new Map();
  for (const profile of snapshot.profiles) {
    requireValue(text(profile.id) && !profiles.has(profile.id), `Duplicate or invalid profile ID: ${profile.id}`);
    profiles.set(profile.id, profile);
  }
  const seen = new Set(), acceptedNames = new Map(), imagePaths = new Set();
  const characters = wiki.characters.map(character => {
    const id = character.id;
    requireValue(text(id) && !seen.has(id), `Duplicate or invalid character ID: ${id}`);
    seen.add(id);
    const official = profiles.get(id);
    requireValue(official?.standing && official.official?.name === character.names.ja &&
      official.source === `https://umamusume.jp/character/${official.official.id}`, `Missing or mismatched official profile: ${id}`);
    const standing = official.standing;
    requireValue(/^\/assets\/ui\/standing\/[a-f0-9]{16}\.webp$/.test(standing.path) && !imagePaths.has(standing.path), `Invalid or duplicate standing path: ${id}`);
    imagePaths.add(standing.path);
    const image = readImage(standing.path);
    requireValue(sha256(image) === standing.sha256, `Standing illustration hash mismatch: ${id}`);
    const size = standingSize(image);
    requireValue(Number.isInteger(standing.width) && Number.isInteger(standing.height) && size.width === standing.width && size.height === standing.height && size.width >= 100 && size.height >= 100, `Standing illustration dimensions mismatch: ${id}`);
    const source = normalized.find(p => p.names.jp === character.names.ja);
    const profile = official.official;
    const reviewed = official.reviewed;
    if (reviewed) requireValue(text(reviewed.reason) && Array.isArray(reviewed.sources) && reviewed.sources.length > 0 && reviewed.sources.every(sourceUrl), `Missing reviewed source: ${id}`);
    requireValue(Array.isArray(character.names.zhAll), `Missing Chinese names: ${id}`);
    const cn = reviewed?.chineseNames ?? unique([character.names.zhHans, character.names.zhHant, ...character.names.zhAll]);
    const cv = profile.cv ?? character.profile.cv;
    // Latin stage names retain their official spelling.
    const cvAliases = reviewed?.cv ?? (/[a-z]/i.test(cv) ? [cv] : unique([
      cv, character.profile.cv, source?.profile.cv,
      source?.raw.bili_detail?.cv_raw, source?.raw.moegirl_intro?.cv_raw,
    ]));
    requireValue(Array.isArray(cn) && cn.length > 0 && cn.every(text), `Invalid Chinese names: ${id}`);
    requireValue(Array.isArray(cvAliases) && cvAliases.length > 0 && cvAliases.every(name => text(name) && !/[<>[\]（）()→\d]/u.test(name)), `Invalid CV alias: ${id}`);
    const birthday = profile.birthday?.match(/^(\d+)月(\d+)日$/);
    const height = Number(profile.height?.replace(/cm$/, "") ?? character.profile.heightCm);
    const month = birthday ? Number(birthday[1]) : character.profile.birthday.month;
    const day = birthday ? Number(birthday[2]) : character.profile.birthday.day;
    const days = [31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    requireValue(Number.isInteger(height) && height >= 100 && height <= 250 && Number.isInteger(month) && month >= 1 && month <= 12 && Number.isInteger(day) && day >= 1 && day <= days[month - 1], `Invalid birthday or height: ${id}`);
    const names = { "zh-Hans": character.names.zhHans, "zh-Hant": character.names.zhHant ?? (id === "mg-駿川たづな" ? "駿川手綱" : character.names.zhHans), ja: character.names.ja };
    const english = profile.en ?? character.names.en;
    requireValue(Object.values(names).every(text) && text(english), `Missing localized name: ${id}`);
    requireValue(cn.some(name => normalize(name) === normalize(names["zh-Hans"])) && cn.some(name => normalize(name) === normalize(names["zh-Hant"])), `Display name missing from accepted names: ${id}`);
    for (const name of [...cn, names.ja]) {
      const key = normalize(name), other = acceptedNames.get(key);
      requireValue(!other || other === id, `Conflicting accepted name: ${name} (${other}, ${id})`);
      acceptedNames.set(key, id);
    }
    const candidates = unique([official.source, ...(reviewed?.sources ?? source?.sources.source_urls ?? [
      character.names.wikiTitle ? `https://wiki.biligame.com/umamusume/${encodeURIComponent(character.names.wikiTitle)}` : null,
      character.names.moegirlTitle || character.names.moegirlZh ? `https://zh.moegirl.org.cn/${encodeURIComponent(character.names.moegirlTitle ?? character.names.moegirlZh)}` : null,
    ])]);
    requireValue(candidates.length > 0 && candidates.every(sourceUrl), `Invalid profile source: ${id}`);
    const sources = candidates.filter(value => {
      const url = new URL(value);
      return url.searchParams.get("redlink") !== "1" && url.searchParams.get("action") !== "edit";
    });
    return { imagePath: standing.path, imageWidth: standing.width, imageHeight: standing.height,
      id, names, chineseNames: cn, english, cv: cvAliases, birthday: { month, day }, height, sources };
  }).sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
  requireValue(profiles.size === characters.length, "Snapshot and roster differ");
  requireValue(new Set(snapshot.legacyRoster).size === snapshot.legacyRoster.length && snapshot.legacyRoster.every(id => seen.has(id)), "Missing or duplicate legacy character");
  const compatibleVersions = [];
  const rosterHash = sha256(JSON.stringify(characters.map(character => character.id)));
  for (const previous of snapshot.compatibleCatalogs ?? []) {
    // A corrected profile may retain drafts only when the seeded draw order is unchanged.
    requireValue(/^[a-f0-9]{16}$/.test(previous.version) && previous.rosterSha256 === rosterHash, "Incompatible saved draw order");
    compatibleVersions.push(previous.version);
  }
  const payload = { characters, normalization: variants };
  return { version: sha256(JSON.stringify(payload)).slice(0, 16), compatibleVersions, ...payload };
}

function main() {
  const read = path => JSON.parse(readFileSync(join(root, path), "utf8"));
  const normalized = readdirSync(join(root, "data/normalized/characters")).filter(p => p.endsWith(".json"))
    .map(p => read(`data/normalized/characters/${p}`));
  const snapshot = read("data/manual/concentration-profiles.json");
  const catalog = buildCatalog(read("data/wiki/v2/characters.json"), snapshot, read("data/manual/concentration-normalization.json"), normalized,
    path => readFileSync(join(root, "web/public", path)));
  const output = JSON.stringify(catalog, null, 2) + "\n";
  const target = join(root, "web/src/features/concentration/catalog.json");
  if (process.argv.includes("--check")) {
    requireValue(readFileSync(target, "utf8") === output, "Concentration catalog is out of date");
  } else writeFileSync(target, output);
  console.log(`Concentration: ${catalog.characters.length} profiles, ${snapshot.legacyRoster.length} legacy profiles included, ${catalog.version}`);
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) main();
