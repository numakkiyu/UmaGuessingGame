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

test("repository policy permits source, deployment files and curated media", t => {
  const cwd = repository(t, {
    "Dockerfile": "FROM scratch\n",
    "docker-compose.yml": "services: {}\n",
    "web/pnpm-lock.yaml": "lockfileVersion: '9.0'\n",
    "web/src/main.ts": "export {};\n",
    "web/public/character.png": "image fixture",
    "data/assets/audio/manifest.json": "{}\n",
    "data/assets/audio/clip.mp3": "audio fixture",
    "data/manual/audio-clips.review.json": JSON.stringify({ reviewer: "fixture-reviewer", reviewedAt: "2026-11-12", clips: [{
      clipId: "fixture", sha256: createHash("sha256").update("audio fixture").digest("hex"),
      listened: true, speakerVerified: true, transcriptVerified: true, noNameSpoiler: true,
    }] }),
    "internal/content/testdata/audio-tone.mp3": "audio fixture",
    "internal/spafs/dist/.gitkeep": "",
    ".env.example": "UMA_ADDR=:8080\n",
  });
  const result = check(cwd);
  assert.equal(result.status, 0, result.stderr);
});

test("repository policy inspects staged blobs and rejects private or generated files even inside media exceptions", t => {
  const rejected = [
    ".agent/notes.md", "web/dist/index.html", "internal/spafs/dist/index.html",
    "web/tools/toolchain/node_modules/package/index.js", ".cache/report.json",
    "bin/uma-server", "content/pack-test/manifest.json", "runtime.db-wal",
    ".env.production", "data/assets/audio/download.mp3",
    "data/assets/audio/.env", "data/assets/audio/private.key",
    "data/assets/audio/nested/.agent/notes.md", "config/cloud.local.json",
    "data/assets/audio/oversized.mp3",
  ];
  const cwd = repository(t, Object.fromEntries(rejected.map(path => [path,
    path.endsWith("oversized.mp3") ? Buffer.alloc(50 * 1024 * 1024 + 1) : "fixture\n"])));
  // The index must remain authoritative even if the working copy was replaced.
  writeFileSync(join(cwd, "data/assets/audio/oversized.mp3"), "small\n");
  const result = check(cwd);
  assert.equal(result.status, 1);
  for (const path of rejected) assert.ok(result.stderr.includes(path), path);
  assert.match(result.stderr, /50 MiB/);
});

test("repository policy binds audio approval to staged bytes and completed review flags", t => {
  for (const complete of [false, true]) {
    const cwd = repository(t, {
      "data/assets/audio/clip.mp3": "changed bytes",
      "data/manual/audio-clips.review.json": JSON.stringify({ reviewer: "fixture-reviewer", reviewedAt: "2026-11-12", clips: [{
        clipId: "fixture", sha256: createHash("sha256").update(complete ? "old bytes" : "changed bytes").digest("hex"),
        listened: complete, speakerVerified: true, transcriptVerified: true, noNameSpoiler: true,
      }] }),
    });
    const result = check(cwd);
    assert.equal(result.status, 1);
    assert.match(result.stderr, /no matching completed listening review/);
  }
});
