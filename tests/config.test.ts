import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { ZodError } from "zod";
import { parseEnv } from "../src/config/schema";

describe("config schema", () => {
  it("parses defaults and required values", () => {
    const env = parseEnv({
      NODE_ENV: "test",
      DATABASE_URL: "postgresql://postgres:postgres@localhost:5432/umaguessinggame",
      REDIS_URL: "redis://localhost:6379",
    } as NodeJS.ProcessEnv);
    assert.equal(env.MAX_GUESSES, 8);
    assert.equal(env.TURNSTILE_ENABLED, false);
    assert.equal(env.ROOM_SYNC_POLL_INTERVAL_MS, 2500);
    assert.equal(env.REALTIME_HEARTBEAT_INTERVAL_MS, 15000);
  });

  it("requires both turnstile keys when turnstile is enabled", () => {
    assert.throws(
      () =>
        parseEnv({
          NODE_ENV: "test",
          DATABASE_URL: "postgresql://postgres:postgres@localhost:5432/umaguessinggame",
          REDIS_URL: "redis://localhost:6379",
          TURNSTILE_ENABLED: "true",
        } as NodeJS.ProcessEnv),
      (error) =>
        error instanceof ZodError &&
        error.issues.some((issue) => issue.message.includes("TURNSTILE_SITE_KEY")) &&
        error.issues.some((issue) => issue.message.includes("TURNSTILE_SECRET_KEY")),
    );
  });

  it("requires app signing secret in production", () => {
    assert.throws(
      () =>
        parseEnv({
          NODE_ENV: "test",
          APP_ENV: "production",
          DATABASE_URL: "postgresql://postgres:postgres@localhost:5432/umaguessinggame",
          REDIS_URL: "redis://localhost:6379",
        } as NodeJS.ProcessEnv),
      (error) =>
        error instanceof ZodError &&
        error.issues.some((issue) => issue.message.includes("APP_SIGNING_SECRET")),
    );
  });
});
