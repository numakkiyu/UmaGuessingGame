import { NextResponse } from "next/server";
import { getBattlePlayerFromRequest } from "@/lib/battle/request";
import { buildBattleViewerToken } from "@/lib/battle/identity";
import { subscribeRoomEvent } from "@/lib/battle/realtime";
import { getBattleRoomState } from "@/lib/battle/service";
import { getOrCreateSessionIdFromRequest } from "@/lib/auth/session";
import { toPlayerFacingGameError } from "@/lib/game/errors";

function buildSseHeaders() {
  return {
    "Content-Type": "text/event-stream",
    "Cache-Control": "no-cache, no-transform",
    Connection: "keep-alive",
  };
}

function encodeSseData(data: unknown) {
  return `data: ${JSON.stringify(data)}\n\n`;
}

export async function GET(
  request: Request,
  { params }: { params: Promise<{ code: string }> },
) {
  try {
    const playerId = getBattlePlayerFromRequest(request);
    const { code } = await params;
    if (!playerId) {
      throw new Error("请先通过房间码或匹配进入这一局。");
    }

    const stream = new ReadableStream({
      async start(controller) {
        const encoder = new TextEncoder();

        const pushState = async () => {
          try {
            const state = await getBattleRoomState(code, playerId);
            controller.enqueue(
              encoder.encode(
                encodeSseData({
                  ...state,
                  viewerToken: buildBattleViewerToken(
                    playerId,
                    request,
                    getOrCreateSessionIdFromRequest(request),
                  ),
                }),
              ),
            );
          } catch (error) {
            controller.enqueue(
              encoder.encode(
                encodeSseData({
                  error: toPlayerFacingGameError(error, "这会儿还读不到房间。"),
                }),
              ),
            );
          }
        };

        await pushState();
        const unsubscribe = subscribeRoomEvent(code, () => {
          void pushState();
        });
        const heartbeat = setInterval(() => {
          controller.enqueue(encoder.encode(": keep-alive\n\n"));
        }, 15000);

        request.signal.addEventListener("abort", () => {
          clearInterval(heartbeat);
          unsubscribe();
          controller.close();
        });
      },
    });

    return new NextResponse(stream, { headers: buildSseHeaders() });
  } catch (error) {
    return NextResponse.json(
      { error: toPlayerFacingGameError(error, "这会儿还接不上实时房间。") },
      { status: 400 },
    );
  }
}
