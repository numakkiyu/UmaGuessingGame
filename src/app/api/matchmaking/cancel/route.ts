import { NextResponse } from "next/server";
import { getBattlePlayerFromRequest } from "@/lib/battle/request";
import { buildBattleViewerToken } from "@/lib/battle/identity";
import { cancelMatchmaking } from "@/lib/battle/service";
import { getOrCreateSessionIdFromRequest } from "@/lib/auth/session";
import { attachBattlePlayerCookie } from "@/lib/battle/request";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { matchmakingCancelRequestSchema } from "@/lib/validation/schemas";

export async function POST(request: Request) {
  try {
    const playerId = getBattlePlayerFromRequest(request);
    if (!playerId) {
      throw new Error("这次匹配已经失效了。");
    }

    const body = matchmakingCancelRequestSchema.parse(await request.json().catch(() => ({})));
    const status = await cancelMatchmaking(playerId, body.ticketId);
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
      { error: toPlayerFacingGameError(error, "这会儿还没法取消匹配。") },
      { status: 400 },
    );
  }
}
