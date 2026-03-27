import { BATTLE_PLAYER_HEADER_NAME } from "@/lib/battle/identity";

function canUseSessionStorage() {
  return typeof window !== "undefined" && typeof window.sessionStorage !== "undefined";
}

function buildRoomTokenKey(roomCode: string) {
  return `uma:battle:room:${roomCode}`;
}

function buildTicketTokenKey(ticketId: string) {
  return `uma:battle:ticket:${ticketId}`;
}

export function readRoomViewerToken(roomCode: string) {
  if (!canUseSessionStorage()) {
    return null;
  }

  return window.sessionStorage.getItem(buildRoomTokenKey(roomCode));
}

export function writeRoomViewerToken(roomCode: string, viewerToken: string) {
  if (!canUseSessionStorage()) {
    return;
  }

  window.sessionStorage.setItem(buildRoomTokenKey(roomCode), viewerToken);
}

export function readTicketViewerToken(ticketId: string) {
  if (!canUseSessionStorage()) {
    return null;
  }

  return window.sessionStorage.getItem(buildTicketTokenKey(ticketId));
}

export function writeTicketViewerToken(ticketId: string, viewerToken: string) {
  if (!canUseSessionStorage()) {
    return;
  }

  window.sessionStorage.setItem(buildTicketTokenKey(ticketId), viewerToken);
}

export function withViewerTokenQuery(path: string, viewerToken: string | null) {
  if (!viewerToken) {
    return path;
  }

  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}viewerToken=${encodeURIComponent(viewerToken)}`;
}

export function buildViewerTokenHeaders(
  viewerToken: string | null,
  headers?: HeadersInit,
): HeadersInit | undefined {
  if (!viewerToken) {
    return headers;
  }

  const nextHeaders = new Headers(headers);
  nextHeaders.set(BATTLE_PLAYER_HEADER_NAME, viewerToken);
  return nextHeaders;
}
