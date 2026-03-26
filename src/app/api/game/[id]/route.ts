import { NextResponse } from "next/server";
import { getGameState } from "@/lib/game/service";

export async function readPublicGameState(
  id: string,
  reader: typeof getGameState = getGameState,
) {
  return reader(id);
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
      { error: error instanceof Error ? error.message : "读取游戏失败。" },
      { status: 404 },
    );
  }
}
