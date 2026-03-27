import { NextResponse } from "next/server";
import { buildBattleViewerToken } from "@/lib/battle/identity";
import { joinMatchmaking } from "@/lib/battle/service";
import {
  attachBattlePlayerCookie,
  getOrCreateBattlePlayerIdentity,
  withBattleViewerToken,
} from "@/lib/battle/request";
import { getServerConfig } from "@/config/server";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { buildRequestFingerprint, rateLimitGuard, verifyTurnstileToken } from "@/lib/game/security";
import { matchmakingJoinRequestSchema } from "@/lib/validation/schemas";

export async function POST(request: Request) {
  try {
    if (!getServerConfig().enableMultiplayer) {
      throw new Error("多人对战暂时还没开放。");
    }

    const body = matchmakingJoinRequestSchema.parse(await request.json().catch(() => ({})));
    const fingerprint = buildRequestFingerprint(request);
    await rateLimitGuard("matchmaking:join", fingerprint);
    await verifyTurnstileToken(body.turnstileToken);

    const { playerId, sessionId } = getOrCreateBattlePlayerIdentity(request);
    const status = await joinMatchmaking(playerId, body.latencyMs);
    const viewerToken = buildBattleViewerToken(playerId, request, sessionId);
    const response = NextResponse.json(withBattleViewerToken(status, viewerToken));
    attachBattlePlayerCookie(response, playerId, request, sessionId);
    response.headers.set("Cache-Control", "no-store");
    return response;
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "这会儿还没法进入匹配。") },
      { status: 400 },
    );
  }
}
