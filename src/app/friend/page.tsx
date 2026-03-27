import { FriendBattleShell } from "@/components/friend-battle-shell";
import { getPublicConfig } from "@/config/public";

type Props = {
  searchParams: Promise<{
    code?: string;
  }>;
};

export default async function FriendBattlePage({ searchParams }: Props) {
  const { code } = await searchParams;
  return <FriendBattleShell initialConfig={getPublicConfig()} initialInviteCode={code ?? ""} />;
}
