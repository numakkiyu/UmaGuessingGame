import { getEnv } from "@/config/schema";

export function getServerConfig() {
  const env = getEnv();

  return {
    appEnv: env.APP_ENV,
    appSigningSecret:
      env.APP_SIGNING_SECRET || `${env.APP_BASE_URL}|${env.TURNSTILE_SECRET_KEY || "dev-secret"}`,
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
    localGameStoreEnabled: env.LOCAL_GAME_STORE_ENABLED,
    realtimeWsUrl: env.REALTIME_WS_URL,
    roomSyncPollIntervalMs: env.ROOM_SYNC_POLL_INTERVAL_MS,
    realtimeHeartbeatIntervalMs: env.REALTIME_HEARTBEAT_INTERVAL_MS,
  };
}
