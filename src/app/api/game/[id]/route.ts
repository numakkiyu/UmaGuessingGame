import { NextResponse } from "next/server";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { getGameState } from "@/lib/game/service";

export async function readPublicGameState(
  id: string,
  reader: typeof getGameState = getGameState,
) {
  const state = await reader(id);
  if (state.status !== "playing") {
    return {
      ...state,
      status: "ended" as const,
      answerCharacterId: null,
      answerDisplayName: null,
    };
  }

  return state;
}

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  try {
    const { id } = await params;
    const state = await readPublicGameState(id);
    return NextResponse.json(state);
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "这局暂时读不到了。") },
      { status: 404 },
    );
  }
}
