import { z } from "zod";

const booleanish = z
  .string()
  .optional()
  .transform((value) => value === "true");

const intish = (fallback: number) =>
  z
    .string()
    .optional()
    .transform((value) => {
      const parsed = Number.parseInt(value ?? "", 10);
      return Number.isFinite(parsed) ? parsed : fallback;
    });

const envSchema = z
  .object({
    APP_ENV: z.enum(["development", "preview", "production"]).default("development"),
    APP_BASE_URL: z.string().url().default("http://localhost:3000"),
    ASSET_BASE_URL: z.string().url().default("http://localhost:3000"),
    DATABASE_URL: z.string().min(1),
    REDIS_URL: z.string().min(1),
    TURNSTILE_ENABLED: booleanish.default(false),
    TURNSTILE_SITE_KEY: z.string().optional().default(""),
    TURNSTILE_SECRET_KEY: z.string().optional().default(""),
    RATE_LIMIT_ENABLED: booleanish.default(true),
    MAX_GUESSES: intish(8),
    ENABLE_MULTIPLAYER: booleanish.default(false),
    ENABLE_SHARE: booleanish.default(true),
    ASSET_PROXY_ENABLED: booleanish.default(true),
    ASSET_CACHE_DIR: z.string().default("public/assets"),
    ASSET_CACHE_TTL_SECONDS: intish(86400),
    ASSET_PROXY_TIMEOUT_MS: intish(8000),
  })
  .superRefine((env, ctx) => {
    if (!env.TURNSTILE_ENABLED) {
      return;
    }

    if (!env.TURNSTILE_SITE_KEY) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "TURNSTILE_ENABLED=true 时必须提供 TURNSTILE_SITE_KEY。",
        path: ["TURNSTILE_SITE_KEY"],
      });
    }

    if (!env.TURNSTILE_SECRET_KEY) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "TURNSTILE_ENABLED=true 时必须提供 TURNSTILE_SECRET_KEY。",
        path: ["TURNSTILE_SECRET_KEY"],
      });
    }
  });

export type AppEnv = z.infer<typeof envSchema>;

let cachedEnv: AppEnv | null = null;

export function parseEnv(source: NodeJS.ProcessEnv = process.env): AppEnv {
  return envSchema.parse(source);
}

export function getEnv(): AppEnv {
  cachedEnv ??= parseEnv();
  return cachedEnv;
}
