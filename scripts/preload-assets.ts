import { ensureAssetCached, loadAssetManifest } from "../src/lib/assets/service";

async function main() {
  const manifest = await loadAssetManifest();
  for (const entry of manifest) {
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
