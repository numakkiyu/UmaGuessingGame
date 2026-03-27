import { cookies, headers } from "next/headers";
import { redirect } from "next/navigation";
import { GameShell } from "@/components/game-shell";
import { getPublicConfig } from "@/config/public";
import {
  canEditGameFromHeaders,
  getGameViewerCookieName,
} from "@/lib/game/auth";
import { getGameState } from "@/lib/game/service";
import { parseSessionCookieValue, SESSION_COOKIE_NAME } from "@/lib/auth/session";
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

async function loadSingleRoomData(code: string, searchViewerToken?: string) {
  try {
    const cookieStore = await cookies();
    const headerStore = await headers();
    const [questionBank, searchEntries, gameState] = await Promise.all([
      loadQuestionBankReady(),
      loadSearchIndex(),
      getGameState(code),
    ]);
    const viewerCookie = cookieStore.get(getGameViewerCookieName(code))?.value ?? null;
    const sessionCookie = parseSessionCookieValue(cookieStore.get(SESSION_COOKIE_NAME)?.value ?? null);
    const viewer = canEditGameFromHeaders({
      roomCode: code,
      headers: headerStore,
      searchViewerToken,
      sessionCookie,
      viewerCookie,
    });
    const canEdit = Boolean(viewer);
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
      initialViewerToken: searchViewerToken ?? viewerCookie,
      questionBank,
      searchEntries,
      gameState: publicGameState,
    };
  } catch {
    redirect("/");
  }
}

export default async function SingleRoomPage({ params, searchParams }: Props) {
  const { code } = await params;
  const query = await searchParams;
  const viewerToken = query.viewerToken ?? query.v;
  const { canEdit, initialViewerToken, questionBank, searchEntries, gameState } = await loadSingleRoomData(code, viewerToken);

  return (
    <GameShell
      canEdit={canEdit}
      initialViewerToken={initialViewerToken}
      initialConfig={getPublicConfig()}
      initialGameState={gameState}
      initialQuestionBank={questionBank}
      initialQuestionBankSize={questionBank.length}
      initialSearchEntries={searchEntries}
    />
  );
}
