import { type NextResponse } from "next/server";
import {
  attachSessionCookie,
  buildAuthFingerprint,
  buildViewerToken,
  getOrCreateSessionIdFromRequest,
  parseCookieValue,
  shouldUseSecureAuthCookie,
  validateViewerTokenFromHeaders,
  validateViewerTokenFromRequest,
} from "@/lib/auth/session";

export const GAME_VIEWER_SCOPE = "single-game-host";

export function getGameViewerCookieName(roomCode: string) {
  return `uma_game_viewer_${roomCode}`;
}

export function buildGameViewerToken(roomCode: string, request: Request, sessionId?: string) {
  return buildViewerToken({
    scope: GAME_VIEWER_SCOPE,
    subjectId: roomCode,
    sessionId: sessionId ?? getOrCreateSessionIdFromRequest(request),
    fingerprintHash: buildAuthFingerprint(request),
  });
}

export function getGameViewerFromRequest(request: Request, roomCode: string) {
  return validateViewerTokenFromRequest(request, {
    scope: GAME_VIEWER_SCOPE,
    subjectId: roomCode,
    cookieName: getGameViewerCookieName(roomCode),
  });
}

export function canEditGameFromHeaders(options: {
  roomCode: string;
  headers: { get(name: string): string | null };
  searchViewerToken?: string | null;
  sessionCookie?: string | null;
  viewerCookie?: string | null;
}) {
  const fallbackCookie =
    options.viewerCookie ??
    parseCookieValue(options.headers.get("cookie"), getGameViewerCookieName(options.roomCode));
  const candidates = [options.searchViewerToken, fallbackCookie].filter(
    (value): value is string => Boolean(value),
  );

  for (const candidate of candidates) {
    const viewer = validateViewerTokenFromHeaders(candidate, {
      scope: GAME_VIEWER_SCOPE,
      subjectId: options.roomCode,
      sessionId: options.sessionCookie,
      headers: options.headers,
    });
    if (viewer) {
      return viewer;
    }
  }

  return null;
}

export function attachGameViewerCookies(
  response: NextResponse,
  request: Request,
  roomCode: string,
  viewerToken: string,
  sessionId: string,
) {
  attachSessionCookie(response, sessionId, request);
  response.cookies.set({
    name: getGameViewerCookieName(roomCode),
    value: viewerToken,
    httpOnly: true,
    sameSite: "lax",
    secure: shouldUseSecureAuthCookie(request.url, request.headers.get("x-forwarded-proto")),
    path: "/",
  });
}
