import { NextResponse } from "next/server";
import { getServerConfig } from "@/config/server";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { buildOwnerCookieValue, getOwnerCookieName } from "@/lib/game/ownership";
import { createGameRequestSchema } from "@/lib/validation/schemas";
import { buildRequestFingerprint, rateLimitGuard, verifyTurnstileToken } from "@/lib/game/security";
import { createNewGame } from "@/lib/game/service";

export async function POST(request: Request) {
  try {
    const body = createGameRequestSchema.parse(await request.json().catch(() => ({})));
    const fingerprint = buildRequestFingerprint(request);
    await rateLimitGuard("game:new", fingerprint);
    await verifyTurnstileToken(body.turnstileToken);

    const game = await createNewGame();
    const response = NextResponse.json(game);
    response.cookies.set({
      name: getOwnerCookieName(game.roomCode),
      value: buildOwnerCookieValue(game.roomCode),
      httpOnly: true,
      sameSite: "lax",
      secure: getServerConfig().appEnv === "production",
      path: "/",
      maxAge: 60 * 60 * 24 * 30,
    });
    return response;
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "暂时还没法开始新一局。") },
      { status: 400 },
    );
  }
}
