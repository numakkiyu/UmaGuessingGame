import { getEnv } from "@/config/schema";

export type PublicConfig = {
  siteName: string;
  assetBaseUrl: string;
  turnstileEnabled: boolean;
  turnstileSiteKey: string;
  featureFlags: {
    enableMultiplayer: boolean;
    enableFriendBattle: boolean;
    enableShare: boolean;
  };
  shareBaseUrl: string;
  maxGuesses: number;
  assetProxyEnabled: boolean;
  realtime: {
    wsUrl: string;
    pollIntervalMs: number;
    heartbeatIntervalMs: number;
  };
  multiplayer: {
    matchmakingMaxWaitSeconds: number;
    acceptConfirmSeconds: number;
    idleTimeoutSeconds: number;
    pauseDurationSeconds: number;
    rematchConfirmSeconds: number;
  };
};

export function getPublicConfig(): PublicConfig {
  const env = getEnv();

  return {
    siteName: "赛马娘弗一把",
    assetBaseUrl: env.ASSET_BASE_URL,
    turnstileEnabled: env.TURNSTILE_ENABLED,
    turnstileSiteKey: env.TURNSTILE_SITE_KEY,
    featureFlags: {
      enableMultiplayer: env.ENABLE_MULTIPLAYER,
      enableFriendBattle: env.ENABLE_FRIEND_BATTLE,
      enableShare: env.ENABLE_SHARE,
    },
    shareBaseUrl: env.APP_BASE_URL,
    maxGuesses: env.MAX_GUESSES,
    assetProxyEnabled: env.ASSET_PROXY_ENABLED,
    realtime: {
      wsUrl: env.REALTIME_WS_URL,
      pollIntervalMs: env.ROOM_SYNC_POLL_INTERVAL_MS,
      heartbeatIntervalMs: env.REALTIME_HEARTBEAT_INTERVAL_MS,
    },
    multiplayer: {
      matchmakingMaxWaitSeconds: env.MATCHMAKING_MAX_WAIT_SECONDS,
      acceptConfirmSeconds: env.MATCH_ACCEPT_CONFIRM_SECONDS,
      idleTimeoutSeconds: env.MATCH_BATTLE_IDLE_TIMEOUT_SECONDS,
      pauseDurationSeconds: env.MATCH_PAUSE_DURATION_SECONDS,
      rematchConfirmSeconds: env.ROOM_REMATCH_CONFIRM_TIMEOUT_SECONDS,
    },
  };
}
