import { NextResponse } from "next/server";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { canEditRoom, getOwnerCookieName } from "@/lib/game/ownership";
import { buildRequestFingerprint, rateLimitGuard, verifyTurnstileToken } from "@/lib/game/security";
import { submitGuess } from "@/lib/game/service";
import { guessRequestSchema } from "@/lib/validation/schemas";

export async function POST(request: Request) {
  try {
    const body = guessRequestSchema.parse(await request.json());
    const ownerCookie = request.headers
      .get("cookie")
      ?.split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(`${getOwnerCookieName(body.gameId)}=`))
      ?.split("=")
      .slice(1)
      .join("=") ?? null;
    if (!canEditRoom(body.gameId, ownerCookie)) {
      throw new Error("这个链接只能查看，不能落猜。");
    }

    const fingerprint = buildRequestFingerprint(request);
    await rateLimitGuard("game:guess", `${fingerprint}:${body.gameId}`);
    await verifyTurnstileToken(body.turnstileToken);

    const state = await submitGuess(body.gameId, body.characterId);
    return NextResponse.json(state);
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "提交失败了，稍后再试一次吧。") },
      { status: 400 },
    );
  }
}
