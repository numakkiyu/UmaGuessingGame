import { useState } from "react";
import { cn } from "@/lib/utils";

const modeArtwork: Readonly<Record<string, string>> = {
  concentration: "ikuno-notes",
  classic: "specialweek-quiz", memory: "mayano-memory", "aptitude-grid": "oguri-grid",
  "race-rebuild": "mcqueen-order", sort: "opera-sort", "co-race-path": "rice-path",
  "strategy-race": "turbo-start", draft: "falcon-dance", pedigree: "rice-cards",
  audio: "suzuka-run", bluff: "specialweek-snack", "clue-puzzle": "urara-cheer",
  rapport: "teio-cheer", creative: "four-party", trade: "duel-teio-mcqueen",
};
const poolArtwork: Readonly<Record<string, string>> = {
  "classic-duel": "duel-special-suzuka", "classic-four": "four-classmates",
  "sort-four": "four-party", "memory-four": "four-classmates",
};
export function MenuCharacterArt({ mode, pool, className }: { mode?: string; pool?: string; className?: string }) {
  const file = pool ? poolArtwork[pool] : modeArtwork[mode ?? ""];
  return file ? <CharacterImage key={file} file={file} group={!!pool || file.startsWith("four-")} className={className} /> : null;
}
function CharacterImage({ file, group, className }: { file: string; group: boolean; className?: string }) {
  const [failed, setFailed] = useState(false);
  if (failed) return null;
  return <img className={cn("uma-menu-character-art", className)} src={`/assets/ui/chibi/${file}.webp`} width={group ? 400 : 180} height={220} alt="" aria-hidden="true" loading="lazy" decoding="async" onError={() => setFailed(true)} />;
}
