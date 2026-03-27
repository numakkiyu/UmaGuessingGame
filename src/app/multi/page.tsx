import { MatchmakingShell } from "@/components/matchmaking-shell";
import { getPublicConfig } from "@/config/public";

export default function MultiplayerPage() {
  return <MatchmakingShell initialConfig={getPublicConfig()} />;
}
