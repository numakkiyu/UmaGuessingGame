import { promises as fs } from "node:fs";
import path from "node:path";
import {
  buildCachedThumbnailLocalPath,
  inferImageExtension,
} from "@/lib/assets/resolve-image";
import {
  assetManifestEntrySchema,
  questionBankEntrySchema,
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
const readyPath = path.join(
  /* turbopackIgnore: true */ process.cwd(),
  "data",
  "question_bank_ready.json",
);
const allowedHosts = new Set([
  "umamusume.jp",
  "wiki.biligame.com",
  "patchwiki.biligame.com",
  "storage.moegirl.org.cn",
  "zh.moegirl.org.cn",
]);

const inflight = new Map<string, Promise<AssetManifestEntry>>();
let manifestCache: AssetManifestEntry[] | null = null;
let manifestMapCache: Map<string, AssetManifestEntry> | null = null;

function getAssetProxyTimeoutMs() {
  const parsed = Number.parseInt(process.env.ASSET_PROXY_TIMEOUT_MS ?? "", 10);
  return Number.isFinite(parsed) ? parsed : 8000;
}

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

function classifySourceSite(sourceUrl: string) {
  const hostname = new URL(sourceUrl).hostname;
  if (hostname.includes("biligame.com")) {
    return "biliwiki";
  }
  if (hostname.includes("moegirl.org.cn")) {
    return "moegirl";
  }
  if (hostname.includes("umamusume.jp")) {
    return "umamusume-official";
  }

  return hostname;
}

async function discoverAssetEntry(assetId: string) {
  const raw = await readJson<unknown[]>(readyPath, []);

  for (const item of raw) {
    const parsed = questionBankEntrySchema.safeParse(item);
    if (!parsed.success) {
      continue;
    }

    if (parsed.data.asset_id !== assetId || !parsed.data.image_url) {
      continue;
    }

    const extension = inferImageExtension(parsed.data.image_url) ?? "png";

    return assetManifestEntrySchema.parse({
      asset_id: parsed.data.asset_id,
      character_id: parsed.data.id,
      source_site: classifySourceSite(parsed.data.image_url),
      source_page: parsed.data.image_url,
      source_url: parsed.data.image_url,
      local_path: buildCachedThumbnailLocalPath(parsed.data.asset_id, extension),
      downloaded_at: null,
      usage_scope: "search_and_guess_table",
      copyright_note: "Source tracked for controlled proxy and cache use.",
      cache_status: "missing",
      last_checked_at: null,
    });
  }

  return null;
}

export async function loadAssetManifest() {
  if (manifestCache) {
    return manifestCache;
  }

  const raw = await readJson<unknown[]>(assetManifestPath, []);
  manifestCache = raw.flatMap((item) => {
    const parsed = assetManifestEntrySchema.safeParse(item);
    return parsed.success ? [parsed.data] : [];
  });
  manifestMapCache = new Map(manifestCache.map((entry) => [entry.asset_id, entry]));
  return manifestCache;
}

async function updateAssetManifestEntry(entry: AssetManifestEntry) {
  const manifest = await loadAssetManifest();
  const next = manifest.filter((item) => item.asset_id !== entry.asset_id);
  next.push(entry);
  next.sort((left, right) => left.asset_id.localeCompare(right.asset_id));
  manifestCache = next;
  manifestMapCache = new Map(next.map((item) => [item.asset_id, item]));
  await writeJson(assetManifestPath, next);
}

export async function getAssetEntry(assetId: string) {
  if (!manifestMapCache) {
    await loadAssetManifest();
  }

  const existingEntry = manifestMapCache?.get(assetId) ?? null;
  if (existingEntry) {
    return existingEntry;
  }

  const discoveredEntry = await discoverAssetEntry(assetId);
  if (!discoveredEntry) {
    return null;
  }

  await updateAssetManifestEntry(discoveredEntry);
  return discoveredEntry;
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
    const timer = setTimeout(() => controller.abort(), getAssetProxyTimeoutMs());
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
