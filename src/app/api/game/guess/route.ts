import { NextResponse } from "next/server";
import { buildRequestFingerprint, rateLimitGuard, verifyTurnstileToken } from "@/lib/game/security";
import { submitGuess } from "@/lib/game/service";
import { guessRequestSchema } from "@/lib/validation/schemas";

export async function POST(request: Request) {
  try {
    const body = guessRequestSchema.parse(await request.json());
    const fingerprint = buildRequestFingerprint(request);
    await rateLimitGuard("game:guess", `${fingerprint}:${body.gameId}`);
    await verifyTurnstileToken(body.turnstileToken);

    const state = await submitGuess(body.gameId, body.characterId);
    return NextResponse.json(state);
  } catch (error) {
    return NextResponse.json(
      { error: error instanceof Error ? error.message : "提交猜测失败。" },
      { status: 400 },
    );
  }
}
