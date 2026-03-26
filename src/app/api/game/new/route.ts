import { NextResponse } from "next/server";
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
    return NextResponse.json(game);
  } catch (error) {
    return NextResponse.json(
      { error: error instanceof Error ? error.message : "创建新局失败。" },
      { status: 400 },
    );
  }
}
