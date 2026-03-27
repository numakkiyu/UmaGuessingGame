import { NextResponse } from "next/server";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { getGameViewerFromRequest } from "@/lib/game/auth";
import { buildRequestFingerprint, rateLimitGuard } from "@/lib/game/security";
import { submitGuess } from "@/lib/game/service";
import { guessRequestSchema } from "@/lib/validation/schemas";

export async function POST(request: Request) {
  try {
    const body = guessRequestSchema.parse(await request.json());
    if (!getGameViewerFromRequest(request, body.gameId)) {
      throw new Error("这个链接只能查看，不能落猜。");
    }

    const fingerprint = buildRequestFingerprint(request);
    await rateLimitGuard("game:guess", `${fingerprint}:${body.gameId}`);

    const state = await submitGuess(body.gameId, body.characterId);
    return NextResponse.json(state);
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "提交失败了，稍后再试一次吧。") },
      { status: 400 },
    );
  }
}
