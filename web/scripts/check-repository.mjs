#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";

const MAX_BLOB_BYTES = 50 * 1024 * 1024;
const entries = execFileSync("git", ["ls-files", "--stage", "-z"], { encoding: "utf8" })
  .split("\0").filter(Boolean).map(entry => {
    const separator = entry.indexOf("\t");
    const [mode, oid, stage] = entry.slice(0, separator).split(" ");
    return { mode, oid, stage, path: entry.slice(separator + 1) };
  });

const environmentFiles = /(?:^|\/)\.env(?:\..*)?$/;
const embeddedBuild = /^internal\/spafs\/dist\//;
const audioDirectory = /^data\/assets\/audio\//;
const audioFile = /^data\/assets\/audio\/[a-zA-Z0-9_-]+\.mp3$/;
const forbidden = [
  /(?:^|\/)(?:node_modules|\.agent|\.cache|\.next|coverage|playwright-report|test-results|\.playwright|\.idea|\.vscode)(?:\/|$)/,
  /^(?:bin|build|dist|backups)(?:\/|$)/,
  /^content\/(?:pack-[^/]+(?:\/|$)|current$)/,
  /^data\/runtime\//,
  environmentFiles,
  /\.(?:db|sqlite|sqlite3)(?:-(?:wal|shm|journal))?$/,
  /\.(?:pem|key|p12|pfx|log|tsbuildinfo|swp|swo)$/,
  /(?:^|\/)(?:server\.lock|AGENTS\.md|\.DS_Store)$/,
  /^(?:server|uma-server|uma-content)$/,
  /^config\/[^/]+\.local\.json$/,
  /^web\/dist\//,
  embeddedBuild,
  audioDirectory,
];

function permitsException(pattern, path) {
  return (pattern === environmentFiles && path === ".env.example")
    || (pattern === embeddedBuild && path === "internal/spafs/dist/.gitkeep")
    || (pattern === audioDirectory && (path === "data/assets/audio/manifest.json" || audioFile.test(path)));
}

const errors = [];
for (const entry of entries) {
  if (entry.stage !== "0") errors.push(`${entry.path}: unresolved merge entry`);
  if (entry.mode === "160000") errors.push(`${entry.path}: submodule requires an explicit repository policy`);
  const matches = forbidden.filter(pattern => pattern.test(entry.path));
  // Asset exceptions only relax their directory rule, never credential or cache rules.
  if (matches.some(pattern => !permitsException(pattern, entry.path))) {
    errors.push(`${entry.path}: generated, private or runtime file`);
  }
}

const objects = [...new Set(entries.filter(entry => entry.mode !== "160000").map(entry => entry.oid))];
const sizes = objects.length ? execFileSync("git", ["cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)"], {
  input: objects.join("\n") + "\n", encoding: "utf8", maxBuffer: 8 * 1024 * 1024,
}).trim().split("\n") : [];
const byOid = new Map(sizes.map(line => {
  const [oid, type, bytes] = line.split(" ");
  return [oid, { type, bytes: Number(bytes) }];
}));
for (const entry of entries) {
  if (entry.mode === "160000") continue;
  const object = byOid.get(entry.oid);
  if (!object || object.type !== "blob" || !Number.isFinite(object.bytes)) {
    errors.push(`${entry.path}: cannot inspect indexed object`);
  } else if (object.bytes > MAX_BLOB_BYTES) {
    errors.push(`${entry.path}: exceeds the 50 MiB source-file limit; use a release asset or approved external storage`);
  }
}

const audio = entries.filter(entry => audioFile.test(entry.path));
if (audio.length) {
  const ledgerEntry = entries.find(entry => entry.path === "data/manual/audio-clips.review.json");
  let ledger;
  try {
    ledger = ledgerEntry && JSON.parse(execFileSync("git", ["cat-file", "blob", ledgerEntry.oid], { encoding: "utf8" }));
  } catch {
    errors.push("data/manual/audio-clips.review.json: cannot read indexed review ledger");
  }
  const namedReview = typeof ledger?.reviewer === "string" && ledger.reviewer.trim()
    && typeof ledger?.reviewedAt === "string" && ledger.reviewedAt.trim();
  const reviewed = new Set(namedReview && Array.isArray(ledger.clips) ? ledger.clips.filter(clip =>
    typeof clip.clipId === "string" && clip.clipId.trim()
    && clip.listened === true && clip.speakerVerified === true
    && clip.transcriptVerified === true && clip.noNameSpoiler === true
    && /^[a-f0-9]{64}$/.test(clip.sha256)).map(clip => clip.sha256) : []);
  for (const entry of audio) {
    const size = byOid.get(entry.oid)?.bytes;
    if (!Number.isFinite(size) || size > MAX_BLOB_BYTES) continue;
    const bytes = execFileSync("git", ["cat-file", "blob", entry.oid], { maxBuffer: MAX_BLOB_BYTES + 1 });
    if (!reviewed.has(createHash("sha256").update(bytes).digest("hex"))) {
      errors.push(`${entry.path}: indexed audio has no matching completed listening review`);
    }
  }
}

if (errors.length) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log(`Repository index: ${entries.length} files, no generated outputs, private files or oversized blobs`);
}
