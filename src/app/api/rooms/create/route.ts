import { NextResponse } from "next/server";
import { getServerConfig } from "@/config/server";
import { buildBattleViewerToken } from "@/lib/battle/identity";
import {
  attachBattlePlayerCookie,
  getOrCreateBattlePlayerIdentity,
  withBattleViewerToken,
} from "@/lib/battle/request";
import { createFriendRoom } from "@/lib/battle/service";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { buildRequestFingerprint, rateLimitGuard, verifyTurnstileToken } from "@/lib/game/security";
import { createFriendRoomRequestSchema } from "@/lib/validation/schemas";

export async function POST(request: Request) {
  try {
    if (!getServerConfig().enableFriendBattle) {
      throw new Error("好友对战暂时还没开放。");
    }

    const body = createFriendRoomRequestSchema.parse(await request.json().catch(() => ({})));
    const fingerprint = buildRequestFingerprint(request);
    await rateLimitGuard("rooms:create", fingerprint);
    await verifyTurnstileToken(body.turnstileToken);

    const { playerId, sessionId } = getOrCreateBattlePlayerIdentity(request);
    const state = await createFriendRoom(playerId);
    const viewerToken = buildBattleViewerToken(playerId, request, sessionId);
    const response = NextResponse.json(withBattleViewerToken(state, viewerToken));
    attachBattlePlayerCookie(response, playerId, request, sessionId);
    response.headers.set("Cache-Control", "no-store");
    return response;
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "这会儿还没法建房。") },
      { status: 400 },
    );
  }
}
