import { type NextResponse } from "next/server";
import {
  BATTLE_PLAYER_HEADER_NAME,
  BATTLE_PLAYER_COOKIE_NAME,
  buildBattlePlayerCookieValue,
  buildBattleViewerToken,
  createBattlePlayerId,
  parseBattleViewerToken,
  parseBattlePlayerCookieValue,
} from "@/lib/battle/identity";
import {
  attachSessionCookie,
  getOrCreateSessionIdFromRequest,
  getSessionIdFromRequest,
  getViewerTokenFromRequest,
  shouldUseSecureAuthCookie,
} from "@/lib/auth/session";

function parseCookieValue(cookieHeader: string | null, cookieName: string) {
  return (
    cookieHeader
      ?.split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(`${cookieName}=`))
      ?.slice(cookieName.length + 1) ?? null
  );
}

export function getBattlePlayerFromRequest(request: Request) {
  const sessionId = getSessionIdFromRequest(request);
  const viewerPayload = parseBattleViewerToken(
    getViewerTokenFromRequest(request, BATTLE_PLAYER_COOKIE_NAME),
    request,
    sessionId,
  );
  if (viewerPayload) {
    return viewerPayload.subjectId;
  }

  const cookieValue = parseCookieValue(request.headers.get("cookie"), BATTLE_PLAYER_COOKIE_NAME);
  const headerValue = request.headers.get(BATTLE_PLAYER_HEADER_NAME);
  const searchParamValue = new URL(request.url).searchParams.get("viewerToken");

  return (
    parseBattlePlayerCookieValue(cookieValue) ??
    parseBattlePlayerCookieValue(headerValue) ??
    parseBattlePlayerCookieValue(searchParamValue)
  );
}

export function getOrCreateBattlePlayerIdentity(request: Request) {
  const sessionId = getOrCreateSessionIdFromRequest(request);
  return {
    sessionId,
    playerId: getBattlePlayerFromRequest(request) ?? createBattlePlayerId(),
  };
}

export function attachBattlePlayerCookie(
  response: NextResponse,
  playerId: string,
  request?: Request,
  sessionId?: string,
) {
  if (request && sessionId) {
    attachSessionCookie(response, sessionId, request);
  }

  const viewerToken =
    request && sessionId
      ? buildBattleViewerToken(playerId, request, sessionId)
      : buildBattlePlayerCookieValue(playerId);

  response.cookies.set({
    name: BATTLE_PLAYER_COOKIE_NAME,
    value: viewerToken,
    httpOnly: true,
    sameSite: "lax",
    secure: shouldUseSecureAuthCookie(
      request?.url ?? "http://localhost:3000",
      request?.headers.get("x-forwarded-proto"),
    ),
    path: "/",
  });

  return viewerToken;
}

export function withBattleViewerToken<T extends { viewerToken?: string | null }>(
  payload: T,
  viewerToken: string,
) {
  return {
    ...payload,
    viewerToken,
  };
}
