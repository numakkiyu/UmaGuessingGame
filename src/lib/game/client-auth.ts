import { VIEWER_TOKEN_HEADER_NAME } from "@/lib/auth/session";

function canUseSessionStorage() {
  return typeof window !== "undefined" && typeof window.sessionStorage !== "undefined";
}

function buildGameTokenKey(roomCode: string) {
  return `uma:game:viewer:${roomCode}`;
}

export function readGameViewerToken(roomCode: string) {
  if (!canUseSessionStorage()) {
    return null;
  }

  return window.sessionStorage.getItem(buildGameTokenKey(roomCode));
}

export function writeGameViewerToken(roomCode: string, viewerToken: string) {
  if (!canUseSessionStorage()) {
    return;
  }

  window.sessionStorage.setItem(buildGameTokenKey(roomCode), viewerToken);
}

export function buildGameViewerHeaders(viewerToken: string | null, headers?: HeadersInit) {
  if (!viewerToken) {
    return headers;
  }

  const nextHeaders = new Headers(headers);
  nextHeaders.set(VIEWER_TOKEN_HEADER_NAME, viewerToken);
  return nextHeaders;
}

export function withGameViewerTokenQuery(path: string, viewerToken: string | null) {
  if (!viewerToken) {
    return path;
  }

  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}viewerToken=${encodeURIComponent(viewerToken)}`;
}
