import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const checker = fileURLToPath(new URL("../check-repository.mjs", import.meta.url));
function repository(t, files) {
  const cwd = mkdtempSync(join(tmpdir(), "uma-repository-"));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  execFileSync("git", ["init", "-q"], { cwd });
  for (const [path, content] of Object.entries(files)) {
    const target = join(cwd, path);
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, content);
  }
  execFileSync("git", ["add", "-A"], { cwd });
  return cwd;
}
function check(cwd) {
  return spawnSync(process.execPath, [checker], { cwd, encoding: "utf8" });
}

const audioBytes = Buffer.concat([Buffer.from([0x1a, 0x45, 0xdf, 0xa3]), Buffer.from("fixture OpusHead")]);
const digest = bytes => createHash("sha256").update(bytes).digest("hex");
function manifest(bytes = audioBytes) {
  return JSON.stringify({ format: "audio/webm; codecs=opus", encoding: { codec: "opus", container: "webm", targetBitrate: 40000, vbr: true, sampleRate: 48000, channels: 1 }, clips: [{ file: "clip.webm", source: "https://example.invalid/source.mp3", sourceBytes: 100, bytes: bytes.length, sha256: digest(bytes), sourceSHA256: "a".repeat(64), durationMillis: 1000 }] });
}
function review(hash = digest(audioBytes)) {
  return JSON.stringify({ reviewer: "fixture-reviewer", reviewedAt: "2026-11-18", clips: [{ file: "clip.webm", sourceSHA256: "a".repeat(64), clipId: "fixture", sha256: hash, listened: true, speakerVerified: true, transcriptVerified: true, noNameSpoiler: true }] });
}

test("repository policy permits source, deployment files and the WebM source library", t => {
  const cwd = repository(t, {
    "Dockerfile": "FROM scratch\n",
    "docker-compose.yml": "services: {}\n",
    "web/pnpm-lock.yaml": "lockfileVersion: '9.0'\n",
    "web/src/main.ts": "export {};\n",
    "web/public/character.png": "image fixture",
    "data/assets/audio/manifest.json": manifest(),
    "data/assets/audio/library/clip.webm": audioBytes,
    "data/manual/audio-clips.review.json": review(),
    "internal/content/testdata/audio-tone.webm": audioBytes,
    "internal/spafs/dist/.gitkeep": "",
    ".env.example": "UMA_ADDR=:8080\n",
  });
  const result = check(cwd);
  assert.equal(result.status, 0, result.stderr);
});

test("repository policy stores verified source files without treating them as listened-to questions", t => {
  const cwd = repository(t, {
    "data/assets/audio/manifest.json": manifest(),
    "data/assets/audio/library/clip.webm": audioBytes,
  });
  const result = check(cwd);
  assert.equal(result.status, 0, result.stderr);
});

test("repository policy inspects staged blobs and rejects private or generated files even inside media exceptions", t => {
  const rejected = [
    ".agent/notes.md", "web/dist/index.html", "internal/spafs/dist/index.html",
    "docs/.vitepress/dist/index.html", "docs/.vitepress/cache/state.json", "docs/.vitepress/.temp/page.js",
    "web/tools/toolchain/node_modules/package/index.js", ".cache/report.json",
    "bin/uma-server", "content/pack-test/manifest.json", "runtime.db-wal",
    ".env.production", "data/assets/audio/download.webm",
    "data/assets/audio/library/legacy.mp3", "data/assets/audio/library/other.opus",
    "web/public/audio/old.wav", "docs/public/audio/old.ogg", "internal/content/testdata/old.MP3",
    "data/assets/audio/.env", "data/assets/audio/private.key",
    "data/assets/audio/nested/.agent/notes.md", "config/cloud.local.json",
    "data/assets/audio/library/oversized.webm",
  ];
  const cwd = repository(t, Object.fromEntries(rejected.map(path => [path,
    path.endsWith("oversized.webm") ? Buffer.alloc(50 * 1024 * 1024 + 1) : "fixture\n"])));
  writeFileSync(join(cwd, "data/assets/audio/library/oversized.webm"), "small\n");
  const result = check(cwd);
  assert.equal(result.status, 1);
  for (const path of rejected) assert.ok(result.stderr.includes(path), path);
  assert.match(result.stderr, /50 MiB/);
});

test("repository policy binds stored audio to the indexed source manifest", t => {
  const cwd = repository(t, {
    "data/assets/audio/manifest.json": manifest(),
    "data/assets/audio/library/clip.webm": "changed bytes",
  });
  writeFileSync(join(cwd, "data/assets/audio/library/clip.webm"), audioBytes);
  const result = check(cwd);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /do not match the source manifest/);
});

test("repository policy checks completed listening records against indexed media", t => {
  const cwd = repository(t, {
    "data/assets/audio/manifest.json": manifest(),
    "data/assets/audio/library/clip.webm": audioBytes,
    "data/manual/audio-clips.review.json": review("b".repeat(64)),
  });
  const result = check(cwd);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /completed listening review does not match/);
});

test("repository policy rejects missing library files and disguised formats", t => {
  for (const bytes of [null, Buffer.from("not Opus")]) {
    const files = { "data/assets/audio/manifest.json": manifest(bytes ?? audioBytes) };
    if (bytes) files["data/assets/audio/library/clip.webm"] = bytes;
    const result = check(repository(t, files));
    assert.equal(result.status, 1);
    assert.match(result.stderr, bytes ? /do not match the source manifest/ : /source manifest file is not indexed/);
  }
});

test("repository policy requires the fixed audio encoding settings", t => {
  for (const changed of [{ targetBitrate: 24000 }, { vbr: false }, { channels: 2 }, { sampleRate: 44100 }, { codec: "vorbis" }, { container: "ogg" }]) {
    const data = JSON.parse(manifest());
    Object.assign(data.encoding, changed);
    const cwd = repository(t, {
      "data/assets/audio/manifest.json": JSON.stringify(data),
      "data/assets/audio/library/clip.webm": audioBytes,
    });
    const result = check(cwd);
    assert.equal(result.status, 1);
    assert.match(result.stderr, /40 kbps VBR/);
  }
});
