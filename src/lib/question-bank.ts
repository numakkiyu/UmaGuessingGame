import { promises as fs } from "node:fs";
import path from "node:path";
import {
  questionBankEntrySchema,
  searchIndexEntrySchema,
  type QuestionBankEntry,
  type SearchIndexEntry,
} from "./validation/schemas";

const rootDir = process.cwd();
const dataDir = path.join(rootDir, "data");
const readyPath = path.join(dataDir, "question_bank_ready.json");
const searchIndexPath = path.join(rootDir, "public", "search", "characters.json");

async function readJsonFile<T>(filePath: string, fallback: T): Promise<T> {
  try {
    const raw = await fs.readFile(filePath, "utf8");
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

export async function loadQuestionBankReady(): Promise<QuestionBankEntry[]> {
  const raw = await readJsonFile<unknown[]>(readyPath, []);
  return raw.flatMap((item) => {
    const parsed = questionBankEntrySchema.safeParse(item);
    return parsed.success ? [parsed.data] : [];
  });
}

export async function loadSearchIndex(): Promise<SearchIndexEntry[]> {
  const raw = await readJsonFile<unknown[]>(searchIndexPath, []);
  return raw.flatMap((item) => {
    const parsed = searchIndexEntrySchema.safeParse(item);
    return parsed.success ? [parsed.data] : [];
  });
}

export function formatGroup(values: string[]): string {
  return values.join("/");
}
