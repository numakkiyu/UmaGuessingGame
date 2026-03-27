import { NextResponse } from "next/server";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { canEditRoom, getOwnerCookieName } from "@/lib/game/ownership";
import { endGame } from "@/lib/game/service";
import { endGameRequestSchema } from "@/lib/validation/schemas";

export async function POST(request: Request) {
  try {
    const body = endGameRequestSchema.parse(await request.json());
    const ownerCookie = request.headers
      .get("cookie")
      ?.split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(`${getOwnerCookieName(body.gameId)}=`))
      ?.split("=")
      .slice(1)
      .join("=") ?? null;

    if (!canEditRoom(body.gameId, ownerCookie)) {
      throw new Error("这个链接只能查看，不能结束这一局。");
    }

    const state = await endGame(body.gameId);
    return NextResponse.json(state);
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "这会儿还没法结束这一局。") },
      { status: 400 },
    );
  }
}
