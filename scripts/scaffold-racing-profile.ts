import { promises as fs } from "node:fs";
import path from "node:path";

type CandidateEntry = {
  id?: string;
  name_zh?: string;
  name_jp?: string;
};

type CharacterSource = {
  id?: string;
  source_urls?: string[];
};

type RacingProfileEntry = {
  sex_type: string;
  g1_bracket: string;
  g23_bracket: string;
  g1_has_jpn?: boolean;
  g23_has_jpn?: boolean;
};

const rootDir = process.cwd();
const dataDir = path.join(rootDir, "data");
const candidatesPath = path.join(dataDir, "question_bank_candidates.json");
const manualPath = path.join(dataDir, "manual", "racing-profile.json");
const outputPath = path.join(dataDir, "manual", "racing-profile.todo.json");
const charactersDir = path.join(dataDir, "characters");

async function readJson<T>(filePath: string, fallback: T): Promise<T> {
  try {
    const raw = await fs.readFile(filePath, "utf8");
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

async function main() {
  const candidates = await readJson<CandidateEntry[]>(candidatesPath, []);
  const manualProfiles = await readJson<Record<string, RacingProfileEntry>>(manualPath, {});
  const characterFiles = (await fs.readdir(charactersDir))
    .filter((fileName) => fileName.endsWith(".json") && fileName !== "_order_manifest.json")
    .sort();

  const sourceUrlsById = new Map<string, string[]>();
  for (const fileName of characterFiles) {
    const character = await readJson<CharacterSource>(path.join(charactersDir, fileName), {});
    if (!character.id) {
      continue;
    }
    sourceUrlsById.set(character.id, Array.isArray(character.source_urls) ? character.source_urls : []);
  }

  const todo = candidates
    .filter((candidate) => candidate.id && candidate.name_zh && candidate.name_jp)
    .filter((candidate) => {
      const current = manualProfiles[candidate.id!];
      return !current?.sex_type || !current?.g1_bracket || !current?.g23_bracket;
    })
    .map((candidate) => ({
      id: candidate.id,
      name_zh: candidate.name_zh,
      name_jp: candidate.name_jp,
      sex_type: manualProfiles[candidate.id!]?.sex_type ?? "",
      g1_bracket: manualProfiles[candidate.id!]?.g1_bracket ?? "",
      g23_bracket: manualProfiles[candidate.id!]?.g23_bracket ?? "",
      g1_has_jpn: manualProfiles[candidate.id!]?.g1_has_jpn ?? false,
      g23_has_jpn: manualProfiles[candidate.id!]?.g23_has_jpn ?? false,
      source_urls: sourceUrlsById.get(candidate.id!) ?? [],
    }));

  await fs.mkdir(path.dirname(outputPath), { recursive: true });
  await fs.writeFile(outputPath, `${JSON.stringify(todo, null, 2)}\n`, "utf8");

  console.log(`racing profile scaffold written: ${outputPath}`);
  console.log(`entries pending completion: ${todo.length}`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
