import type { components } from "./types";

/** Settlement overrides provisional progress, including a zero after surrender. */
export function settledScore(
  view: Pick<components["schemas"]["RoundView"], "round" | "results">,
  subjectId: unknown,
): number | null {
  if (typeof subjectId !== "string" || !subjectId ||
      (view.round?.phase !== "revealed" && view.round?.phase !== "settled")) return null;
  const result = view.results?.find(entry => entry.subjectId === subjectId);
  const score = result?.scoreUnits;
  return typeof score === "number" && Number.isSafeInteger(score) && score >= 0 ? score : null;
}

export function settledSurrendered(
  view: Pick<components["schemas"]["RoundView"], "round" | "results">,
  subjectId: unknown,
): boolean {
  return settledScore(view, subjectId) !== null &&
    view.results?.find(entry => entry.subjectId === subjectId)?.outcome === "surrendered";
}
