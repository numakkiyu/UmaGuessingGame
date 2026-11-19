export type BoardView = "entries" | "empty" | "loading" | "unavailable";

/** Public presentation data; provider subjects and account IDs never belong here. */
export type TrainerStanding = {
  publicPlayerId: string;
  displayName: string;
  avatarId: string;
  rank: number;
  score: number;
};
