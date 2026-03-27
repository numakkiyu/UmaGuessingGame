import crypto from "node:crypto";
import { getServerConfig } from "@/config/server";

export const OWNER_COOKIE_PREFIX = "uma_owner_room_";

function createOwnerSignature(roomCode: string, secret = getServerConfig().appSigningSecret) {
  return crypto
    .createHmac("sha256", secret)
    .update(`owner:${roomCode}`)
    .digest("hex");
}

export function getOwnerCookieName(roomCode: string) {
  return `${OWNER_COOKIE_PREFIX}${roomCode}`;
}

export function buildOwnerCookieValue(roomCode: string, secret?: string) {
  return createOwnerSignature(roomCode, secret);
}

export function canEditRoom(roomCode: string, cookieValue?: string | null, secret?: string) {
  if (!cookieValue) {
    return false;
  }

  const expected = createOwnerSignature(roomCode, secret);
  const expectedBuffer = Buffer.from(expected);
  const actualBuffer = Buffer.from(cookieValue);

  if (expectedBuffer.length !== actualBuffer.length) {
    return false;
  }

  return crypto.timingSafeEqual(expectedBuffer, actualBuffer);
}
