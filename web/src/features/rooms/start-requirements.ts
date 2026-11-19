import type { Membership } from "@/lib/api/types";

type Member = Pick<Membership, "role" | "teamId">;
export type StartIssue = "clue-puzzle" | "rapport" | "trade" | "strategy-race" | "captains" | "captainTeam" | "players";
export function startIssue(modeId: string, members: readonly Member[], config?: object): StartIssue | null {
  const players = members.filter(m => m.role === "player" || m.role === "team-captain");
  const teams = new Map<string, Member[]>();
  for (const player of players) if (player.teamId) teams.set(player.teamId, [...(teams.get(player.teamId) ?? []), player]);
  if (players.some(p => p.role === "team-captain" && !p.teamId)) return "captainTeam";
  const counts = [...teams.values()].map(group => group.length);
  if (modeId === "rapport") {
    if (players.length < 2 || players.length > 64) return "rapport";
    if (teams.size === 0) return players.length <= 8 ? null : "rapport";
    if (players.some(p => !p.teamId) || teams.size > 8 || counts.some(n => n < 2 || n > 8) || Math.max(...counts) - Math.min(...counts) > 1) return "rapport";
    return null;
  }
  if (modeId === "trade") return players.length >= 3 && players.length <= 8 ? null : "trade";
  if ([...teams.values()].some(group => group.filter(p => p.role === "team-captain").length !== 1)) return "captains";
  if (modeId === "clue-puzzle") {
    const participants = config && "participants" in config ? config.participants : 0;
    return teams.size >= 1 && teams.size <= 8 && !players.some(p => !p.teamId) && counts.every(n => n >= 3 && n <= 6 && (!participants || n === participants)) ? null : "clue-puzzle";
  }
  if (modeId === "strategy-race") {
    const units = teams.size + players.filter(p => !p.teamId).length;
    const solo = config && "practice" in config && config.practice === true;
    return solo ? units === 1 ? null : "strategy-race" : units >= 2 && units <= 8 ? null : "strategy-race";
  }
  return players.length ? null : "players";
}
