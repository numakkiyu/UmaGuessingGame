import { NextResponse } from "next/server";
import { attachBattlePlayerCookie, getBattlePlayerFromRequest } from "@/lib/battle/request";
import { buildBattleViewerToken } from "@/lib/battle/identity";
import { getBattleRoomState } from "@/lib/battle/service";
import { getOrCreateSessionIdFromRequest } from "@/lib/auth/session";
import { toPlayerFacingGameError } from "@/lib/game/errors";

export async function GET(
  request: Request,
  { params }: { params: Promise<{ code: string }> },
) {
  try {
    const playerId = getBattlePlayerFromRequest(request);
    if (!playerId) {
      throw new Error("请先通过房间码或匹配进入这一局。");
    }

    const { code } = await params;
    const state = await getBattleRoomState(code, playerId);
    const sessionId = getOrCreateSessionIdFromRequest(request);
    const viewerToken = buildBattleViewerToken(playerId, request, sessionId);
    const response = NextResponse.json({
      ...state,
      viewerToken,
    });
    attachBattlePlayerCookie(response, playerId, request, sessionId);
    return response;
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "这会儿还读不到房间。") },
      { status: 400 },
    );
  }
}
