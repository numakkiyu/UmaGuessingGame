export type NetworkQuality = "excellent" | "good" | "fair" | "poor" | "bad";

/** A small rolling window dampens one-off stalls without hiding repeated failure. */
export function networkQuality(samples: readonly number[], failures = 0): NetworkQuality | null {
  if (failures >= 2) return "bad";
  if (failures === 1) return "poor";
  if (!samples.length) return null;
  const recent = samples.slice(-5).filter(value => Number.isFinite(value) && value >= 0).sort((a,b)=>a-b);
  if (!recent.length) return null;
  const median = recent[Math.floor(recent.length/2)]!;
  const jitter = recent.at(-1)! - recent[0]!;
  const effective = median + jitter/2;
  return effective < 150 ? "excellent" : effective < 350 ? "good" : effective < 800 ? "fair" : effective < 1600 ? "poor" : "bad";
}
