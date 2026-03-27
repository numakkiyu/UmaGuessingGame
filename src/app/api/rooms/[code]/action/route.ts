import { NextResponse } from "next/server";
import { attachBattlePlayerCookie, getBattlePlayerFromRequest } from "@/lib/battle/request";
import { buildBattleViewerToken } from "@/lib/battle/identity";
import { performRoomAction } from "@/lib/battle/service";
import { getOrCreateSessionIdFromRequest } from "@/lib/auth/session";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { roomActionRequestSchema } from "@/lib/validation/schemas";

export async function POST(
  request: Request,
  { params }: { params: Promise<{ code: string }> },
) {
  try {
    const playerId = getBattlePlayerFromRequest(request);
    if (!playerId) {
      throw new Error("这个房间现在还没法操作。");
    }

    const { code } = await params;
    const body = roomActionRequestSchema.parse(await request.json().catch(() => ({})));
    const state = await performRoomAction(code, playerId, body);
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
      { error: toPlayerFacingGameError(error, "这会儿还没法操作这个房间。") },
      { status: 400 },
    );
  }
}
