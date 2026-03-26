import { getEnv } from "@/config/schema";

export function getServerConfig() {
  const env = getEnv();

  return {
    databaseUrl: env.DATABASE_URL,
    redisUrl: env.REDIS_URL,
    turnstileEnabled: env.TURNSTILE_ENABLED,
    turnstileSecretKey: env.TURNSTILE_SECRET_KEY,
    rateLimitEnabled: env.RATE_LIMIT_ENABLED,
    maxGuesses: env.MAX_GUESSES,
    assetProxyEnabled: env.ASSET_PROXY_ENABLED,
    assetCacheDir: env.ASSET_CACHE_DIR,
    assetCacheTtlSeconds: env.ASSET_CACHE_TTL_SECONDS,
    assetProxyTimeoutMs: env.ASSET_PROXY_TIMEOUT_MS,
  };
}
