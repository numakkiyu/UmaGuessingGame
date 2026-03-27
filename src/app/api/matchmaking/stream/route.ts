import { NextResponse } from "next/server";
import { getBattlePlayerFromRequest } from "@/lib/battle/request";
import { buildBattleViewerToken } from "@/lib/battle/identity";
import { subscribeTicketEvent } from "@/lib/battle/realtime";
import { getMatchmakingStatus } from "@/lib/battle/service";
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

export async function GET(request: Request) {
  try {
    const playerId = getBattlePlayerFromRequest(request);
    const { searchParams } = new URL(request.url);
    const ticketId = searchParams.get("ticketId");

    if (!playerId || !ticketId) {
      throw new Error("这次匹配已经失效了。");
    }

    const stream = new ReadableStream({
      async start(controller) {
        const encoder = new TextEncoder();

        const pushStatus = async () => {
          try {
            const status = await getMatchmakingStatus(playerId, ticketId);
            controller.enqueue(
              encoder.encode(
                encodeSseData({
                  ...status,
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
                  error: toPlayerFacingGameError(error, "这会儿还读不到匹配状态。"),
                }),
              ),
            );
          }
        };

        await pushStatus();
        const unsubscribe = subscribeTicketEvent(ticketId, () => {
          void pushStatus();
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
      { error: toPlayerFacingGameError(error, "这会儿还接不上实时状态。") },
      { status: 400 },
    );
  }
}
