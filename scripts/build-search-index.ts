import { promises as fs } from "node:fs";
import path from "node:path";
import { searchIndexEntrySchema } from "../src/lib/validation/schemas";

const rootDir = process.cwd();
const readyPath = path.join(rootDir, "data", "question_bank_ready.json");
const outputPath = path.join(rootDir, "public", "search", "characters.json");

async function main() {
  const raw = JSON.parse(await fs.readFile(readyPath, "utf8")) as Array<Record<string, unknown>>;
  const index = raw.map((entry) =>
    searchIndexEntrySchema.parse({
      id: entry.id,
      name_zh: entry.name_zh,
      name_tw: entry.name_tw ?? null,
      name_jp: entry.name_jp,
      name_en: entry.name_en ?? null,
      aliases: entry.aliases,
      image_local_path: entry.image_local_path,
      image_url: entry.image_url ?? null,
    }),
  );
  await fs.mkdir(path.dirname(outputPath), { recursive: true });
  await fs.writeFile(outputPath, `${JSON.stringify(index, null, 2)}\n`, "utf8");
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
