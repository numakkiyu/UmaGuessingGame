import crypto from "node:crypto";
import {
  VIEWER_TOKEN_HEADER_NAME,
  buildAuthFingerprint,
  buildViewerToken,
  getOrCreateSessionIdFromRequest,
  parseViewerToken,
} from "@/lib/auth/session";
import { getServerConfig } from "@/config/server";

export const BATTLE_PLAYER_COOKIE_NAME = "uma_battle_player";
export const BATTLE_PLAYER_HEADER_NAME = VIEWER_TOKEN_HEADER_NAME;
export const BATTLE_VIEWER_SCOPE = "battle-player";

function createSignature(playerId: string, secret = getServerConfig().appSigningSecret) {
  return crypto
    .createHmac("sha256", secret)
    .update(`battle-player:${playerId}`)
    .digest("hex");
}

export function buildBattlePlayerCookieValue(playerId: string, secret?: string) {
  const signature = createSignature(playerId, secret);
  return `${playerId}.${signature}`;
}

export function parseBattlePlayerCookieValue(cookieValue?: string | null, secret?: string) {
  if (!cookieValue) {
    return null;
  }

  const [playerId, signature] = cookieValue.split(".", 2);
  if (!playerId || !signature) {
    return null;
  }

  const expected = createSignature(playerId, secret);
  const expectedBuffer = Buffer.from(expected);
  const actualBuffer = Buffer.from(signature);

  if (expectedBuffer.length !== actualBuffer.length) {
    return null;
  }

  return crypto.timingSafeEqual(expectedBuffer, actualBuffer) ? playerId : null;
}

export function createBattlePlayerId() {
  return `player_${crypto.randomUUID().replace(/-/g, "")}`;
}

export function buildBattleViewerToken(
  playerId: string,
  request: Request,
  sessionId = getOrCreateSessionIdFromRequest(request),
) {
  return buildViewerToken({
    scope: BATTLE_VIEWER_SCOPE,
    subjectId: playerId,
    sessionId,
    fingerprintHash: buildAuthFingerprint(request),
  });
}

export function parseBattleViewerToken(
  token: string | null | undefined,
  request: Request,
  sessionId?: string | null,
) {
  return parseViewerToken(token, {
    scope: BATTLE_VIEWER_SCOPE,
    sessionId,
    fingerprintHash: buildAuthFingerprint(request),
  });
}
