import { getDb } from "../src/lib/db/client";
import { questionBankEntries } from "../src/lib/db/schema";
import { loadQuestionBankReady } from "../src/lib/question-bank";

async function main() {
  const db = getDb();
  const ready = await loadQuestionBankReady();
  for (const entry of ready) {
    await db
      .insert(questionBankEntries)
      .values({ characterId: entry.id, payload: entry })
      .onConflictDoUpdate({
        target: questionBankEntries.characterId,
        set: { payload: entry },
      });
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
