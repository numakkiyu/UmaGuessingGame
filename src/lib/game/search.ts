import type { GuessRow, SearchIndexEntry } from "@/lib/validation/schemas";

export function excludeGuessedSearchEntries(
  entries: SearchIndexEntry[],
  guessRows: GuessRow[],
): SearchIndexEntry[] {
  if (guessRows.length === 0) {
    return entries;
  }

  const guessedIds = new Set(guessRows.map((row) => row.characterId));
  return entries.filter((entry) => !guessedIds.has(entry.id));
}
