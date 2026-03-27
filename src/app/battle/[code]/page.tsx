import { BattleRoomShell } from "@/components/battle-room-shell";
import { getPublicConfig } from "@/config/public";
import { loadQuestionBankReady, loadSearchIndex } from "@/lib/question-bank";

type Props = {
  params: Promise<{
    code: string;
  }>;
  searchParams: Promise<{
    viewerToken?: string;
    v?: string;
  }>;
};

export default async function BattleRoomPage({ params, searchParams }: Props) {
  const { code } = await params;
  const query = await searchParams;
  const [questionBank, searchEntries] = await Promise.all([
    loadQuestionBankReady(),
    loadSearchIndex(),
  ]);

  return (
    <BattleRoomShell
      roomCode={code}
      initialViewerToken={query.viewerToken ?? query.v ?? null}
      initialConfig={getPublicConfig()}
      initialQuestionBank={questionBank}
      initialSearchEntries={searchEntries}
    />
  );
}
