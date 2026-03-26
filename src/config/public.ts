import { getEnv } from "@/config/schema";

export type PublicConfig = {
  siteName: string;
  assetBaseUrl: string;
  turnstileEnabled: boolean;
  turnstileSiteKey: string;
  featureFlags: {
    enableMultiplayer: boolean;
    enableShare: boolean;
  };
  shareBaseUrl: string;
  maxGuesses: number;
  assetProxyEnabled: boolean;
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
      enableShare: env.ENABLE_SHARE,
    },
    shareBaseUrl: env.APP_BASE_URL,
    maxGuesses: env.MAX_GUESSES,
    assetProxyEnabled: env.ASSET_PROXY_ENABLED,
  };
}
