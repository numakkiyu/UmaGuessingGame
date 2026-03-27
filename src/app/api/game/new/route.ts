import { NextResponse } from "next/server";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import {
  attachGameViewerCookies,
  buildGameViewerToken,
} from "@/lib/game/auth";
import { createGameRequestSchema } from "@/lib/validation/schemas";
import { buildRequestFingerprint, rateLimitGuard, verifyTurnstileToken } from "@/lib/game/security";
import { createNewGame } from "@/lib/game/service";
import { getOrCreateSessionIdFromRequest } from "@/lib/auth/session";

export async function POST(request: Request) {
  try {
    const body = createGameRequestSchema.parse(await request.json().catch(() => ({})));
    const fingerprint = buildRequestFingerprint(request);
    await rateLimitGuard("game:new", fingerprint);
    await verifyTurnstileToken(body.turnstileToken);

    const game = await createNewGame();
    const sessionId = getOrCreateSessionIdFromRequest(request);
    const viewerToken = buildGameViewerToken(game.roomCode, request, sessionId);
    const response = NextResponse.json({
      ...game,
      canEdit: true,
      viewerToken,
    });
    attachGameViewerCookies(response, request, game.roomCode, viewerToken, sessionId);
    response.headers.set("Cache-Control", "no-store");
    return response;
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "暂时还没法开始新一局。") },
      { status: 400 },
    );
  }
}
