import { NextResponse } from "next/server";
import { toPlayerFacingGameError } from "@/lib/game/errors";
import { getMatchmakingStats } from "@/lib/battle/service";

export async function GET() {
  try {
    return NextResponse.json(await getMatchmakingStats());
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "这会儿还读不到匹配情况。") },
      { status: 400 },
    );
  }
}
