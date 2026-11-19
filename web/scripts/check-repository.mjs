#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";

const MAX_BLOB_BYTES = 50 * 1024 * 1024;
const entries = execFileSync("git", ["ls-files", "--stage", "-z"], { encoding: "utf8", maxBuffer: 32 * 1024 * 1024 })
  .split("\0").filter(Boolean).map(entry => {
    const separator = entry.indexOf("\t");
    const [mode, oid, stage] = entry.slice(0, separator).split(" ");
    return { mode, oid, stage, path: entry.slice(separator + 1) };
  });

const environmentFiles = /(?:^|\/)\.env(?:\..*)?$/;
const embeddedBuild = /^internal\/spafs\/dist\//;
const audioDirectory = /^data\/assets\/audio\//;
const audioFile = /^data\/assets\/audio\/library\/[a-zA-Z0-9_-]+\.webm$/;
const forbidden = [
  /(?:^|\/)(?:node_modules|\.agent|\.cache|\.next|coverage|playwright-report|test-results|\.playwright|\.idea|\.vscode)(?:\/|$)/,
  /^(?:bin|build|dist|backups)(?:\/|$)/,
  /^content\/(?:pack-[^/]+(?:\/|$)|current$)/,
  /^data\/runtime\//,
  environmentFiles,
  /\.(?:db|sqlite|sqlite3)(?:-(?:wal|shm|journal))?$/,
  /\.(?:mp3|m4a|ogg|oga|opus|wav|aac|flac|aiff?|wma)$/i,
  /\.(?:pem|key|p12|pfx|log|tsbuildinfo|swp|swo)$/,
  /(?:^|\/)(?:server\.lock|AGENTS\.md|\.DS_Store)$/,
  /^(?:server|uma-server|uma-content)$/,
  /^config\/[^/]+\.local\.json$/,
  /^web\/dist\//,
  /^docs\/\.vitepress\/(?:dist|cache|\.temp)(?:\/|$)/,
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
    errors.push(`${entry.path}: excluded by repository policy`);
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
const manifestEntry = entries.find(entry => entry.path === "data/assets/audio/manifest.json");
if (audio.length || manifestEntry) {
  let manifest;
  try {
    manifest = manifestEntry && JSON.parse(execFileSync("git", ["cat-file", "blob", manifestEntry.oid], { encoding: "utf8", maxBuffer: MAX_BLOB_BYTES + 1 }));
  } catch {
    errors.push("data/assets/audio/manifest.json: cannot read indexed source manifest");
  }
  const media = new Map();
  const encoding = manifest?.encoding;
  if (manifest?.format !== "audio/webm; codecs=opus" || !Array.isArray(manifest?.clips) || !manifest.clips.length
    || encoding?.codec !== "opus" || encoding.container !== "webm" || encoding.targetBitrate !== 40000
    || encoding.vbr !== true || encoding.sampleRate !== 48000 || encoding.channels !== 1) {
    errors.push("data/assets/audio/manifest.json: expected a 40 kbps VBR, mono 48 kHz WebM/Opus source manifest");
  } else {
    for (const clip of manifest.clips) {
      let source;
      try { source = new URL(clip.source); } catch { /* Report invalid provenance below. */ }
      if (typeof clip.file !== "string" || !/^[A-Za-z0-9_-]+\.webm$/.test(clip.file) || media.has(clip.file)
        || !/^[a-f0-9]{64}$/.test(clip.sha256) || !/^[a-f0-9]{64}$/.test(clip.sourceSHA256)
        || !source || !["https:", "http:"].includes(source.protocol) || source.username || source.password
        || !Number.isSafeInteger(clip.sourceBytes) || clip.sourceBytes < 1
        || !Number.isSafeInteger(clip.bytes) || clip.bytes < 1 || clip.bytes > 2_000_000
        || !Number.isSafeInteger(clip.durationMillis) || clip.durationMillis < 1 || clip.durationMillis > 60_000) {
        errors.push("data/assets/audio/manifest.json: invalid or duplicate media record");
        continue;
      }
      media.set(clip.file, clip);
    }
  }
  const indexedNames = new Set(audio.map(entry => entry.path.split("/").at(-1)));
  for (const name of media.keys()) {
    if (!indexedNames.has(name)) errors.push(`data/assets/audio/library/${name}: source manifest file is not indexed`);
  }

  // Read bounded groups of indexed blobs without starting one process per clip.
  const bounded = audio.filter(entry => {
    const size = byOid.get(entry.oid)?.bytes;
    if (!Number.isFinite(size) || size > 2_000_000) {
      errors.push(`${entry.path}: audio exceeds the 2 MB clip limit`);
      return false;
    }
    return true;
  });
  for (let start = 0; start < bounded.length; start += 64) {
    const group = bounded.slice(start, start + 64);
    const output = execFileSync("git", ["cat-file", "--batch"], { input: group.map(entry => entry.oid).join("\n") + "\n", maxBuffer: 128 * 1024 * 1024 });
    let cursor = 0;
    for (const entry of group) {
      const end = output.indexOf(10, cursor);
      const [oid, type, length] = output.subarray(cursor, end).toString("ascii").split(" ");
      const size = Number(length);
      if (end < cursor || oid !== entry.oid || type !== "blob" || size !== byOid.get(entry.oid)?.bytes) throw new Error("cannot read indexed audio blob");
      const bytes = output.subarray(end + 1, end + 1 + size);
      cursor = end + size + 2;
      const clip = media.get(entry.path.split("/").at(-1));
      if (!clip || size < 4 || clip.bytes !== size || clip.sha256 !== createHash("sha256").update(bytes).digest("hex")
        || bytes.readUInt32BE(0) !== 0x1a45dfa3 || !bytes.includes(Buffer.from("OpusHead"))) {
        errors.push(`${entry.path}: indexed WebM/Opus bytes do not match the source manifest`);
      }
    }
  }

  const ledgerEntry = entries.find(entry => entry.path === "data/manual/audio-clips.review.json");
  let ledger;
  try {
    ledger = ledgerEntry && JSON.parse(execFileSync("git", ["cat-file", "blob", ledgerEntry.oid], { encoding: "utf8" }));
  } catch {
    errors.push("data/manual/audio-clips.review.json: cannot read indexed review ledger");
  }
  const namedReview = typeof ledger?.reviewer === "string" && ledger.reviewer.trim()
    && typeof ledger?.reviewedAt === "string" && ledger.reviewedAt.trim();
  const reviewed = namedReview && Array.isArray(ledger.clips) ? ledger.clips.filter(clip =>
    typeof clip.clipId === "string" && clip.clipId.trim()
    && clip.listened === true && clip.speakerVerified === true
    && clip.transcriptVerified === true && clip.noNameSpoiler === true
    && /^[a-f0-9]{64}$/.test(clip.sha256)) : [];
  for (const clip of reviewed) {
    const record = media.get(clip.file);
    if (!record || record.sha256 !== clip.sha256 || record.sourceSHA256 !== clip.sourceSHA256) {
      errors.push("data/manual/audio-clips.review.json: completed listening review does not match indexed media");
    }
  }
}

if (errors.length) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log(`Repository index: ${entries.length} files, no generated outputs, private files or oversized blobs`);
}
