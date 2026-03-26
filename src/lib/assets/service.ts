import { promises as fs } from "node:fs";
import path from "node:path";
import { getServerConfig } from "@/config/server";
import {
  assetManifestEntrySchema,
  type AssetManifestEntry,
} from "@/lib/validation/schemas";

const assetManifestPath = path.join(
  /* turbopackIgnore: true */ process.cwd(),
  "data",
  "assets",
  "asset-manifest.json",
);
const cacheIndexPath = path.join(
  /* turbopackIgnore: true */ process.cwd(),
  "data",
  "assets",
  "cache-index.json",
);
const allowedHosts = new Set([
  "umamusume.jp",
  "wiki.biligame.com",
  "patchwiki.biligame.com",
  "storage.moegirl.org.cn",
  "zh.moegirl.org.cn",
]);

const inflight = new Map<string, Promise<AssetManifestEntry>>();

async function ensureDir(filePath: string) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
}

async function readJson<T>(filePath: string, fallback: T): Promise<T> {
  try {
    return JSON.parse(await fs.readFile(filePath, "utf8")) as T;
  } catch {
    return fallback;
  }
}

async function writeJson(filePath: string, value: unknown) {
  await ensureDir(filePath);
  await fs.writeFile(filePath, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

function toAbsoluteLocalPath(localPath: string) {
  return path.join(
    /* turbopackIgnore: true */ process.cwd(),
    localPath.replaceAll("/", path.sep),
  );
}

export async function loadAssetManifest() {
  const raw = await readJson<unknown[]>(assetManifestPath, []);
  return raw.flatMap((item) => {
    const parsed = assetManifestEntrySchema.safeParse(item);
    return parsed.success ? [parsed.data] : [];
  });
}

async function updateAssetManifestEntry(entry: AssetManifestEntry) {
  const manifest = await loadAssetManifest();
  const next = manifest.filter((item) => item.asset_id !== entry.asset_id);
  next.push(entry);
  next.sort((left, right) => left.asset_id.localeCompare(right.asset_id));
  await writeJson(assetManifestPath, next);
}

export async function getAssetEntry(assetId: string) {
  const manifest = await loadAssetManifest();
  return manifest.find((entry) => entry.asset_id === assetId) ?? null;
}

export async function ensureAssetCached(assetId: string) {
  if (inflight.has(assetId)) {
    return inflight.get(assetId)!;
  }

  const promise = (async () => {
    const entry = await getAssetEntry(assetId);
    if (!entry) {
      throw new Error("资源不存在。");
    }

    const localPath = toAbsoluteLocalPath(entry.local_path);
    try {
      await fs.access(localPath);
      return entry;
    } catch {}

    const sourceUrl = new URL(entry.source_url);
    if (!allowedHosts.has(sourceUrl.hostname)) {
      throw new Error("资源来源不在允许白名单内。");
    }

    const controller = new AbortController();
    const timer = setTimeout(
      () => controller.abort(),
      getServerConfig().assetProxyTimeoutMs,
    );
    try {
      const response = await fetch(entry.source_url, {
        signal: controller.signal,
        headers: { "User-Agent": "UmaGuessingGameAssetProxy/0.1" },
      });
      if (!response.ok) {
        throw new Error(`上游响应失败: ${response.status}`);
      }

      const bytes = new Uint8Array(await response.arrayBuffer());
      await ensureDir(localPath);
      await fs.writeFile(localPath, bytes);

      const nextEntry: AssetManifestEntry = {
        ...entry,
        cache_status: "cached",
        downloaded_at: new Date().toISOString(),
        last_checked_at: new Date().toISOString(),
      };
      await updateAssetManifestEntry(nextEntry);

      const cacheIndex = await readJson<Record<string, string>>(cacheIndexPath, {});
      cacheIndex[assetId] = nextEntry.local_path;
      await writeJson(cacheIndexPath, cacheIndex);
      return nextEntry;
    } catch (error) {
      const staleEntry: AssetManifestEntry = {
        ...entry,
        cache_status: "failed",
        last_checked_at: new Date().toISOString(),
      };
      await updateAssetManifestEntry(staleEntry);
      throw error;
    } finally {
      clearTimeout(timer);
      inflight.delete(assetId);
    }
  })();

  inflight.set(assetId, promise);
  return promise;
}

export async function readAssetBytes(assetId: string) {
  const entry = await ensureAssetCached(assetId);
  const localPath = toAbsoluteLocalPath(entry.local_path);
  return {
    entry,
    bytes: await fs.readFile(localPath),
  };
}
