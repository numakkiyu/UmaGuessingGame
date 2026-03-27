import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { GameShell } from "@/components/game-shell";
import { getPublicConfig } from "@/config/public";
import { canEditRoom, getOwnerCookieName } from "@/lib/game/ownership";
import { getGameState } from "@/lib/game/service";
import { loadQuestionBankReady, loadSearchIndex } from "@/lib/question-bank";

type Props = {
  params: Promise<{
    code: string;
  }>;
};

async function loadSingleRoomData(code: string) {
  try {
    const cookieStore = await cookies();
    const [questionBank, searchEntries, gameState] = await Promise.all([
      loadQuestionBankReady(),
      loadSearchIndex(),
      getGameState(code),
    ]);
    const canEdit = canEditRoom(code, cookieStore.get(getOwnerCookieName(code))?.value ?? null);
    const publicGameState =
      !canEdit && gameState.status !== "playing"
        ? {
            ...gameState,
            status: "ended" as const,
            answerCharacterId: null,
            answerDisplayName: null,
          }
        : gameState;

    return {
      canEdit,
      questionBank,
      searchEntries,
      gameState: publicGameState,
    };
  } catch {
    redirect("/");
  }
}

export default async function SingleRoomPage({ params }: Props) {
  const { code } = await params;
  const { canEdit, questionBank, searchEntries, gameState } = await loadSingleRoomData(code);

  return (
    <GameShell
      canEdit={canEdit}
      initialConfig={getPublicConfig()}
      initialGameState={gameState}
      initialQuestionBank={questionBank}
      initialQuestionBankSize={questionBank.length}
      initialSearchEntries={searchEntries}
    />
  );
}
