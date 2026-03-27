import { NextResponse } from "next/server";
import {
  getRoomPresenceSummary,
  heartbeatRoomPresence,
} from "@/lib/realtime/presence-store";
import { roomPresenceHeartbeatRequestSchema } from "@/lib/validation/schemas";

export async function POST(request: Request) {
  try {
    const body = roomPresenceHeartbeatRequestSchema.parse(await request.json());
    const summary = await heartbeatRoomPresence(body);
    return NextResponse.json(summary);
  } catch {
    return NextResponse.json(
      { error: "这会儿还没法更新观战状态。" },
      { status: 400 },
    );
  }
}

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const roomCode = searchParams.get("roomCode");

  if (!roomCode) {
    return NextResponse.json(
      { error: "缺少房间码。" },
      { status: 400 },
    );
  }

  return NextResponse.json(await getRoomPresenceSummary(roomCode));
}
