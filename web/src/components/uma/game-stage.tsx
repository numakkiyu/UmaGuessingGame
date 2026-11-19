import { useState, type CSSProperties, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import "@/styles/gameplay.css";

const companions: Readonly<Record<string, readonly string[]>> = {
  classic: ["nature-thinking", "tannhauser-cheer", "teio-cheer", "urara-cheer"],
  memory: ["helios-cards", "mayano-memory", "rice-cards"],
  "aptitude-grid": ["ikuno-notes", "oguri-grid", "specialweek-quiz"],
  sort: ["ikuno-notes", "opera-sort", "mcqueen-order"],
  "race-rebuild": ["mcqueen-order", "opera-sort"],
  "co-race-path": ["nature-thinking", "rice-path", "teio-cheer"],
  draft: ["helios-cards", "falcon-dance", "rice-cards"],
  pedigree: ["ikuno-notes", "rice-cards", "specialweek-quiz"],
  audio: ["suzuka-run", "falcon-dance"],
  "strategy-race": ["turbo-start", "teio-cheer"],
  "clue-puzzle": ["nature-thinking", "specialweek-quiz", "urara-cheer"],
  rapport: ["tannhauser-cheer", "teio-cheer", "urara-cheer"],
  creative: ["nature-thinking", "falcon-dance", "opera-sort"],
  trade: ["helios-cards", "rice-cards", "specialweek-snack"],
  bluff: ["specialweek-snack", "rice-cards"],
};

function pickCompanion(mode: string) {
  const choices = companions[mode] ?? companions.classic!;
  return `/assets/ui/chibi/${choices[Math.floor(Math.random() * choices.length)]}.webp`;
}

/** Decorative companions are chosen independently of questions and stay fixed during play. */
export function GameStage({ mode, children, className, illustration = true }: {
  mode: string; children: ReactNode; className?: string; illustration?: boolean;
}) {
  const [art] = useState(() => pickCompanion(mode));
  return <div className={cn("uma-game-stage", className)} data-game-mode={mode} data-illustration={illustration}
    style={{ "--game-companion": `url("${art}")` } as CSSProperties}>{children}</div>;
}

export function GameCompanion({ mode, className }: { mode: string; className?: string }) {
  const [art] = useState(() => pickCompanion(mode)), [failed, setFailed] = useState(false);
  return failed ? null : <img className={cn("uma-game-companion", className)} src={art} width={180} height={220}
    alt="" aria-hidden="true" decoding="async" onError={() => setFailed(true)} />;
}
