
import { ModeStartPage, type ModeStartConfig } from "@/features/modes/mode-start-page";

const APTITUDE_GRID: ModeStartConfig = {
  modeId: "aptitude-grid",
};

const DRAFT: ModeStartConfig = {
  modeId: "draft",
};

const RACE_REBUILD: ModeStartConfig = {
  modeId: "race-rebuild",
  modeConfig: { cooperative: true },
};

const CO_RACE_PATH: ModeStartConfig = {
  modeId: "co-race-path",
  modeConfig: { openBook: true },
};

const MEMORY: ModeStartConfig = {
  modeId: "memory",
};

const PEDIGREE: ModeStartConfig = {
  modeId: "pedigree",
};

const AUDIO: ModeStartConfig = {
  modeId: "audio",
};

// 团队情报拼图：玩法入口
const CLUE_PUZZLE: ModeStartConfig = {
  modeId: "clue-puzzle",
};

// 回合制策略赛道：玩法入口
const STRATEGY_RACE: ModeStartConfig = {
  modeId: "strategy-race",
  modeConfig: { practice: true },
};

// 默契连线：玩法入口
const RAPPORT: ModeStartConfig = {
  modeId: "rapport",
};

// 学园企划擂台：玩法入口
const CREATIVE: ModeStartConfig = {
  modeId: "creative",
};

const BLUFF: ModeStartConfig = {
  modeId: "bluff",
};

const TRADE: ModeStartConfig = {
  modeId: "trade",
};

export function AptitudeGridPlayPage() {
  return <ModeStartPage config={APTITUDE_GRID} />;
}

export function DraftPlayPage() {
  return <ModeStartPage config={DRAFT} />;
}

export function RaceRebuildPlayPage() {
  return <ModeStartPage config={RACE_REBUILD} />;
}

export function CoRacePathPlayPage() {
  return <ModeStartPage config={CO_RACE_PATH} />;
}

export function MemoryPlayEntryPage() {
  return <ModeStartPage config={MEMORY} />;
}

export function PedigreePlayEntryPage() {
  return <ModeStartPage config={PEDIGREE} />;
}

export function AudioPlayEntryPage() {
  return <ModeStartPage config={AUDIO} />;
}

export function BluffPlayEntryPage() {
  return <ModeStartPage config={BLUFF} />;
}

export function CluePuzzlePlayPage() {
  return <ModeStartPage config={CLUE_PUZZLE} />;
}

export function StrategyRacePlayPage() {
  return <ModeStartPage config={STRATEGY_RACE} />;
}

export function RapportPlayPage() {
  return <ModeStartPage config={RAPPORT} />;
}

export function CreativePlayPage() {
  return <ModeStartPage config={CREATIVE} />;
}

export function TradePlayPage() {
  return <ModeStartPage config={TRADE} />;
}
