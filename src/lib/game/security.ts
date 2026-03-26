import crypto from "node:crypto";
import { getServerConfig } from "@/config/server";
import { getRedis } from "@/lib/cache/redis";

export async function rateLimitGuard(scope: string, identifier: string) {
  if (!getServerConfig().rateLimitEnabled) return;

  const redis = await getRedis();
  const key = `ratelimit:${scope}:${identifier}`;
  const current = await redis.incr(key);
  if (current === 1) {
    await redis.expire(key, 60);
  }
  if (current > 60) {
    throw new Error("请求过于频繁，请稍后再试。");
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
