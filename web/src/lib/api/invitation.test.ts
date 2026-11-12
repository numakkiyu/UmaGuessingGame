import { expect, it, vi } from "vitest";
import { createContractApi } from "./contract";

it("first-time visitors can resolve an invitation without identity credentials", async () => {
  const fetcher = vi.fn<typeof fetch>(async () => new Response(JSON.stringify({ roomId: "rom_safe", joinable: true }), { headers: { "Content-Type": "application/json" } }));
  const api = createContractApi({ fetchImpl: fetcher, getCsrfToken: () => null, requireCsrf: true });
  const response = await api.resolveRoomInvitation({ inviteCode: "ABCDEFGH" });
  expect(response.data?.roomId).toBe("rom_safe");
  expect(fetcher).toHaveBeenCalledOnce();
  const init = fetcher.mock.calls[0]?.[1];
  expect(new Headers(init?.headers).has("X-CSRF-Token")).toBe(false);
  expect(new Headers(init?.headers).has("Idempotency-Key")).toBe(false);
});
