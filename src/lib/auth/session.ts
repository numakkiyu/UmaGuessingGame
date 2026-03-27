import crypto from "node:crypto";
import { type NextResponse } from "next/server";
import { getEnv } from "@/config/schema";
import { getServerConfig } from "@/config/server";

export const SESSION_COOKIE_NAME = "uma_session";
export const VIEWER_TOKEN_HEADER_NAME = "x-uma-viewer-token";
export const VIEWER_TOKEN_QUERY_NAME = "viewerToken";

type HeaderReader = {
  get(name: string): string | null;
};

type ViewerTokenPayload = {
  v: 1;
  scope: string;
  subjectId: string;
  sessionId: string;
  fingerprintHash: string;
};

function isLocalHostname(hostname: string) {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "::1";
}

export function resolveSecureAuthCookie(
  appEnv: string,
  targetUrl: string,
  forwardedProto?: string | null,
) {
  if (appEnv !== "production") {
    return false;
  }

  const normalizedProto = forwardedProto?.split(",")[0]?.trim();
  if (normalizedProto) {
    return normalizedProto === "https";
  }

  try {
    const parsedUrl = new URL(targetUrl);
    if (isLocalHostname(parsedUrl.hostname)) {
      return false;
    }

    return parsedUrl.protocol === "https:";
  } catch {
    return true;
  }
}

export function shouldUseSecureAuthCookie(targetUrl: string, forwardedProto?: string | null) {
  return resolveSecureAuthCookie(getServerConfig().appEnv, targetUrl, forwardedProto);
}

function signValue(value: string, secret = getServerConfig().appSigningSecret) {
  return crypto.createHmac("sha256", secret).update(value).digest("hex");
}

function timingSafeMatch(expected: string, actual: string) {
  const expectedBuffer = Buffer.from(expected);
  const actualBuffer = Buffer.from(actual);
  if (expectedBuffer.length !== actualBuffer.length) {
    return false;
  }

  return crypto.timingSafeEqual(expectedBuffer, actualBuffer);
}

export function parseCookieValue(cookieHeader: string | null, cookieName: string) {
  return (
    cookieHeader
      ?.split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(`${cookieName}=`))
      ?.slice(cookieName.length + 1) ?? null
  );
}

export function createSessionId() {
  return `sess_${crypto.randomUUID().replace(/-/g, "")}`;
}

export function buildSessionCookieValue(sessionId: string) {
  return `${sessionId}.${signValue(`session:${sessionId}`)}`;
}

export function parseSessionCookieValue(cookieValue?: string | null) {
  if (!cookieValue) {
    return null;
  }

  const [sessionId, signature] = cookieValue.split(".", 2);
  if (!sessionId || !signature) {
    return null;
  }

  return timingSafeMatch(signValue(`session:${sessionId}`), signature) ? sessionId : null;
}

export function buildAuthFingerprintFromHeaders(headers: HeaderReader) {
  const parts = [
    headers.get("cf-connecting-ip") ?? headers.get("x-forwarded-for") ?? "local",
    headers.get("user-agent") ?? "",
    headers.get("accept-language") ?? "",
    headers.get("sec-ch-ua-platform") ?? "",
    headers.get("sec-ch-ua-mobile") ?? "",
  ];

  return crypto.createHash("sha256").update(parts.join("|")).digest("hex");
}

export function buildAuthFingerprint(request: Request) {
  return buildAuthFingerprintFromHeaders(request.headers);
}

export function getSessionIdFromRequest(request: Request) {
  return parseSessionCookieValue(parseCookieValue(request.headers.get("cookie"), SESSION_COOKIE_NAME));
}

export function getOrCreateSessionIdFromRequest(request: Request) {
  return getSessionIdFromRequest(request) ?? createSessionId();
}

export function attachSessionCookie(response: NextResponse, sessionId: string, request?: Request) {
  response.cookies.set({
    name: SESSION_COOKIE_NAME,
    value: buildSessionCookieValue(sessionId),
    httpOnly: true,
    sameSite: "lax",
    secure: shouldUseSecureAuthCookie(
      request?.url ?? getEnv().APP_BASE_URL,
      request?.headers.get("x-forwarded-proto"),
    ),
    path: "/",
  });
}

export function buildViewerToken(input: {
  scope: string;
  subjectId: string;
  sessionId: string;
  fingerprintHash: string;
}) {
  const payload: ViewerTokenPayload = {
    v: 1,
    scope: input.scope,
    subjectId: input.subjectId,
    sessionId: input.sessionId,
    fingerprintHash: input.fingerprintHash,
  };
  const encodedPayload = Buffer.from(JSON.stringify(payload)).toString("base64url");
  const signature = signValue(`viewer:${encodedPayload}`);
  return `${encodedPayload}.${signature}`;
}

export function parseViewerToken(
  token: string | null | undefined,
  options: {
    scope?: string;
    subjectId?: string;
    sessionId?: string | null;
    fingerprintHash: string;
  },
) {
  if (!token) {
    return null;
  }

  const [encodedPayload, signature] = token.split(".", 2);
  if (!encodedPayload || !signature) {
    return null;
  }

  if (!timingSafeMatch(signValue(`viewer:${encodedPayload}`), signature)) {
    return null;
  }

  try {
    const payload = JSON.parse(Buffer.from(encodedPayload, "base64url").toString("utf8")) as ViewerTokenPayload;

    if (payload.v !== 1) {
      return null;
    }
    if (options.scope && payload.scope !== options.scope) {
      return null;
    }
    if (options.subjectId && payload.subjectId !== options.subjectId) {
      return null;
    }
    if (options.sessionId && payload.sessionId !== options.sessionId) {
      return null;
    }
    if (payload.fingerprintHash !== options.fingerprintHash) {
      return null;
    }

    return payload;
  } catch {
    return null;
  }
}

export function getViewerTokenFromRequest(request: Request, cookieName?: string) {
  const headerToken = request.headers.get(VIEWER_TOKEN_HEADER_NAME);
  const searchToken = new URL(request.url).searchParams.get(VIEWER_TOKEN_QUERY_NAME);
  const cookieToken = cookieName
    ? parseCookieValue(request.headers.get("cookie"), cookieName)
    : null;

  return headerToken ?? searchToken ?? cookieToken;
}

export function validateViewerTokenFromRequest(
  request: Request,
  options: {
    scope: string;
    subjectId?: string;
    cookieName?: string;
  },
) {
  const sessionId = getSessionIdFromRequest(request);
  return parseViewerToken(getViewerTokenFromRequest(request, options.cookieName), {
    scope: options.scope,
    subjectId: options.subjectId,
    sessionId,
    fingerprintHash: buildAuthFingerprint(request),
  });
}

export function validateViewerTokenFromHeaders(
  token: string | null | undefined,
  options: {
    scope: string;
    subjectId?: string;
    sessionId?: string | null;
    headers: HeaderReader;
  },
) {
  return parseViewerToken(token, {
    scope: options.scope,
    subjectId: options.subjectId,
    sessionId: options.sessionId,
    fingerprintHash: buildAuthFingerprintFromHeaders(options.headers),
  });
}
