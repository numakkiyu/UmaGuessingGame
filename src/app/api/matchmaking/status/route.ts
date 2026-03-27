import { NextResponse } from "next/server";
import { getBattlePlayerFromRequest } from "@/lib/battle/request";
import { buildBattleViewerToken } from "@/lib/battle/identity";
import { getMatchmakingStatus } from "@/lib/battle/service";
import { getOrCreateSessionIdFromRequest } from "@/lib/auth/session";
import { attachBattlePlayerCookie } from "@/lib/battle/request";
import { toPlayerFacingGameError } from "@/lib/game/errors";

export async function GET(request: Request) {
  try {
    const playerId = getBattlePlayerFromRequest(request);
    const { searchParams } = new URL(request.url);
    const ticketId = searchParams.get("ticketId");

    if (!playerId || !ticketId) {
      throw new Error("这次匹配已经失效了。");
    }

    const status = await getMatchmakingStatus(playerId, ticketId);
    const sessionId = getOrCreateSessionIdFromRequest(request);
    const viewerToken = buildBattleViewerToken(playerId, request, sessionId);
    const response = NextResponse.json({
      ...status,
      viewerToken,
    });
    attachBattlePlayerCookie(response, playerId, request, sessionId);
    return response;
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "这会儿还读不到匹配状态。") },
      { status: 400 },
    );
  }
}
