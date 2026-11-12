import { expect, it, vi } from "vitest";
import { createContractApi } from "./contract";

it("sends caller-held keys and unchanged bodies on round/phase retries", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response(JSON.stringify({ roundId: "same-round", phaseVersion: 3 }), { status: 201, headers: { "Content-Type": "application/json" } }));
  const api = createContractApi({ fetchImpl: fetcher, getCsrfToken: () => "csrf-safe", requireCsrf: true });
  const config = { modeId: "race-rebuild" as const, rulesVersion: "2.0.0-draft.1", contentPackId: "", informationStyle: "open" as const, cooperative: true };
  for (let n = 0; n < 2; n++) await api.createRound("same-room", { modeConfig: config }, undefined, "same-round-key");
  for (let n = 0; n < 2; n++) await api.createTransition("same-round", { target: "briefing", expectedPhaseVersion: 2 }, undefined, "same-briefing-key");
  expect(fetcher).toHaveBeenCalledTimes(4);
  for (const [first, retry, key] of [[0, 1, "same-round-key"], [2, 3, "same-briefing-key"]] as const) {
    const init = fetcher.mock.calls[first]![1]!;
    const repeated = fetcher.mock.calls[retry]![1]!;
    expect(new Headers(init.headers).get("Idempotency-Key")).toBe(key);
    expect(new Headers(repeated.headers).get("Idempotency-Key")).toBe(key);
    expect(repeated.body).toBe(init.body);
    expect(fetcher.mock.calls[first]![0]).toBe(fetcher.mock.calls[retry]![0]);
  }
});
