import { promises as fs } from "node:fs";
import path from "node:path";
import {
  questionBankEntrySchema,
  searchIndexEntrySchema,
  type QuestionBankEntry,
  type SearchIndexEntry,
} from "./validation/schemas";
import {
  assetIdFromApiPath,
  buildStaticThumbnailPath,
} from "./assets/resolve-image";

const rootDir = process.cwd();
const dataDir = path.join(rootDir, "data");
const readyPath = path.join(dataDir, "question_bank_ready.json");
const searchIndexPath = path.join(rootDir, "public", "search", "characters.json");
const thumbnailDir = path.join(rootDir, "public", "assets", "characters", "thumbnails");
const knownAssetExtensions = ["png", "jpg", "jpeg", "webp", "svg"] as const;
const resolvedAssetPathCache = new Map<string, Promise<string | null>>();

async function readJsonFile<T>(filePath: string, fallback: T): Promise<T> {
  try {
    const raw = await fs.readFile(filePath, "utf8");
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

async function findStaticAssetPath(assetId: string) {
  if (!resolvedAssetPathCache.has(assetId)) {
    resolvedAssetPathCache.set(
      assetId,
      (async () => {
        for (const extension of knownAssetExtensions) {
          const filePath = path.join(thumbnailDir, `${assetId}.${extension}`);
          try {
            await fs.access(filePath);
            return buildStaticThumbnailPath(assetId, extension);
          } catch {}
        }

        return null;
      })(),
    );
  }

  return resolvedAssetPathCache.get(assetId)!;
}

export async function loadQuestionBankReady(): Promise<QuestionBankEntry[]> {
  const raw = await readJsonFile<unknown[]>(readyPath, []);
  const parsedEntries = raw.flatMap((item) => {
    const parsed = questionBankEntrySchema.safeParse(item);
    return parsed.success ? [parsed.data] : [];
  });

  return Promise.all(
    parsedEntries.map(async (entry) => ({
      ...entry,
      image_local_path: (await findStaticAssetPath(entry.asset_id)) ?? entry.image_local_path,
    })),
  );
}

export async function loadSearchIndex(): Promise<SearchIndexEntry[]> {
  const raw = await readJsonFile<unknown[]>(searchIndexPath, []);
  const parsedEntries = raw.flatMap((item) => {
    const parsed = searchIndexEntrySchema.safeParse(item);
    return parsed.success ? [parsed.data] : [];
  });

  return Promise.all(
    parsedEntries.map(async (entry) => {
      const assetId = assetIdFromApiPath(entry.image_local_path);
      return {
        ...entry,
        image_local_path: assetId
          ? ((await findStaticAssetPath(assetId)) ?? entry.image_local_path)
          : entry.image_local_path,
        image_url: entry.image_url ?? null,
      };
    }),
  );
}

export function formatGroup(values: string[]): string {
  return values.join("/");
}
