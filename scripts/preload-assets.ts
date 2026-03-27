import { ensureAssetCached } from "../src/lib/assets/service";
import { loadQuestionBankReady } from "../src/lib/question-bank";

async function main() {
  const questionBank = await loadQuestionBankReady();

  for (const entry of questionBank) {
    try {
      await ensureAssetCached(entry.asset_id);
      console.log(`cached ${entry.asset_id}`);
    } catch (error) {
      console.warn(`failed ${entry.asset_id}:`, error);
    }
  }
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
