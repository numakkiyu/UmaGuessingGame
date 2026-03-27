import crypto from "node:crypto";
import { getServerConfig } from "@/config/server";
import { getRedis } from "@/lib/cache/redis";
import { isInfrastructureConnectionError } from "@/lib/game/errors";

const memoryRateLimitStore = new Map<string, { count: number; expiresAt: number }>();

function canAutoFallbackRateLimit() {
  const config = getServerConfig();
  return config.localGameStoreEnabled || config.appEnv !== "production";
}

function applyMemoryRateLimit(key: string) {
  const now = Date.now();
  const existing = memoryRateLimitStore.get(key);
  if (!existing || existing.expiresAt <= now) {
    memoryRateLimitStore.set(key, { count: 1, expiresAt: now + 60_000 });
    return;
  }

  existing.count += 1;
  if (existing.count > 60) {
    throw new Error("请求过于频繁，请稍后再试。");
  }
}

export async function rateLimitGuard(scope: string, identifier: string) {
  if (!getServerConfig().rateLimitEnabled) return;

  const key = `ratelimit:${scope}:${identifier}`;
  try {
    const redis = await getRedis();
    const current = await redis.incr(key);
    if (current === 1) {
      await redis.expire(key, 60);
    }
    if (current > 60) {
      throw new Error("请求过于频繁，请稍后再试。");
    }
  } catch (error) {
    if (!canAutoFallbackRateLimit() || !isInfrastructureConnectionError(error)) {
      throw error;
    }

    applyMemoryRateLimit(key);
  }
}

export async function verifyTurnstileToken(token?: string) {
  const config = getServerConfig();
  if (!config.turnstileEnabled) return true;
  if (!token) {
    throw new Error("缺少 Turnstile token。");
  }

  const response = await fetch("https://challenges.cloudflare.com/turnstile/v0/siteverify", {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      secret: config.turnstileSecretKey,
      response: token,
    }),
  });
  const data = (await response.json()) as { success?: boolean };
  if (!data.success) {
    throw new Error("Turnstile 校验失败。");
  }
  return true;
}

export function buildRequestFingerprint(request: Request) {
  const forwarded = request.headers.get("x-forwarded-for") ?? "local";
  return crypto.createHash("sha1").update(forwarded).digest("hex").slice(0, 16);
}
