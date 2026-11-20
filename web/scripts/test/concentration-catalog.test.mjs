import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { buildCatalog } from "../build-concentration.mjs";

const root = fileURLToPath(new URL("../../..", import.meta.url));
const read = path => JSON.parse(readFileSync(join(root, path), "utf8"));
const wiki = read("data/wiki/v2/characters.json");
const snapshot = read("data/manual/concentration-profiles.json");
const normalization = read("data/manual/concentration-normalization.json");
const normalized = readdirSync(join(root, "data/normalized/characters")).filter(path => path.endsWith(".json"))
  .map(path => read(`data/normalized/characters/${path}`));
const images = new Map(snapshot.profiles.map(profile => [profile.standing.path, readFileSync(join(root, "web/public", profile.standing.path))]));
function build(mutate = () => {}, readImage = path => images.get(path)) {
  const data = structuredClone({ wiki, snapshot, normalization, normalized });
  mutate(data);
  return buildCatalog(data.wiki, data.snapshot, data.normalization, data.normalized, readImage);
}

test("builds all source-backed profiles and the checked-in catalog reproducibly", () => {
  const catalog = build();
  assert.equal(catalog.characters.length, 147);
  assert.deepEqual(catalog, read("web/src/features/concentration/catalog.json"));
  assert.ok(catalog.compatibleVersions.includes("1430926db96589b2"));
  assert.ok(catalog.characters.every(profile => profile.sources.every(value => {
    const url = new URL(value);
    return url.searchParams.get("redlink") !== "1" && url.searchParams.get("action") !== "edit";
  })));
});

const invalid = [
  ["duplicate roster IDs", d => { d.wiki.characters.push(d.wiki.characters[0]); }, /Duplicate or invalid character ID/],
  ["duplicate snapshot IDs", d => { d.snapshot.profiles.push(d.snapshot.profiles[0]); }, /Duplicate or invalid profile ID/],
  ["missing snapshot", d => { d.snapshot.profiles.pop(); }, /Missing or mismatched official profile/],
  ["extra snapshot", d => { d.snapshot.profiles.push({ ...d.snapshot.profiles[0], id: "extra" }); }, /Snapshot and roster differ/],
  ["missing legacy character", d => { d.snapshot.legacyRoster.push("absent"); }, /Missing or duplicate legacy/],
  ["duplicate legacy character", d => { d.snapshot.legacyRoster.push(d.snapshot.legacyRoster[0]); }, /Missing or duplicate legacy/],
  ["official name mismatch", d => { d.snapshot.profiles[0].official.name = "別人"; }, /mismatched official/],
  ["missing localized name", d => { d.wiki.characters[0].names.zhHans = ""; }, /Missing localized name/],
  ["missing English name", d => { d.snapshot.profiles[0].official.en = ""; }, /Missing localized name/],
  ["invalid date", d => { d.snapshot.profiles[0].official.birthday = "2月30日"; }, /Invalid birthday/],
  ["invalid height", d => { d.snapshot.profiles[0].official.height = "NaNcm"; }, /Invalid birthday or height/],
  ["source URL mismatch", d => { d.snapshot.profiles[0].source = "https://umamusume.jp/character/wrong"; }, /mismatched official/],
  ["untrusted reference URL", d => { d.normalized[0].sources.source_urls.push("https://example.org/invalid"); }, /Invalid profile source/],
  ["path traversal", d => { d.snapshot.profiles[0].standing.path = "/assets/ui/standing/../../secret.webp"; }, /Invalid or duplicate standing path/],
  ["duplicate illustration", d => { d.snapshot.profiles[1].standing = d.snapshot.profiles[0].standing; }, /duplicate standing path/],
  ["wrong illustration hash", d => { d.snapshot.profiles[0].standing.sha256 = "0".repeat(64); }, /hash mismatch/],
  ["wrong illustration size", d => { d.snapshot.profiles[0].standing.width++; }, /dimensions mismatch/],
  ["ambiguous character alias", d => { delete d.snapshot.profiles.find(p => p.id === "1126").reviewed; }, /Conflicting accepted name/],
  ["CV explanation instead of name", d => { delete d.snapshot.profiles.find(p => p.id === "1031").reviewed; }, /Invalid CV alias/],
  ["unreviewed override", d => { d.snapshot.profiles.find(p => p.id === "1031").reviewed.sources = []; }, /Missing reviewed source/],
  ["normalization chain", d => { d.normalization.characters["长"] = "長"; }, /Invalid normalization mapping/],
  ["incompatible saved draw", d => { d.snapshot.compatibleCatalogs[0].rosterSha256 = "0".repeat(64); }, /Incompatible saved draw order/],
];
for (const [name, mutate, error] of invalid) test(`rejects ${name}`, () => assert.throws(() => build(mutate), error));
