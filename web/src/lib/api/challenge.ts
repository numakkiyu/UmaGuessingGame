import type { Character, components } from "./types";

type ChallengeView = components["schemas"]["ChallengeView"];

/** Eligibility follows the frozen challenge; current names are optional enrichment. */
export function challengeGuessChoices(view: ChallengeView | undefined, directory: readonly Character[]): Character[] {
  if (!view) return [];
  const allowed = new Set(view.characterIds);
  const known = new Map(directory.map(character => [character.characterId, character]));
  // Older stored receipts have identifiers only; never widen their candidate set.
  if (!view.characters) return directory.filter(character => allowed.has(character.characterId));
  const seen = new Set<string>();
  return view.characters.flatMap(identity => {
    if (!allowed.has(identity.characterId) || seen.has(identity.characterId) || !identity.displayName.trim()) return [];
    seen.add(identity.characterId);
    return [known.get(identity.characterId) ?? {
      characterId: identity.characterId,
      displayName: identity.displayName,
      entityKind: "uma" as const,
      datasetVersion: "",
      aliases: [],
    }];
  });
}
