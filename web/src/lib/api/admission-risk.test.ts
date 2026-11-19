import { describe, expect, it, vi } from "vitest";
import { ApiError, createApiClient } from "./client";

const denied = () => new Response(JSON.stringify({ title: "Forbidden", code: "admission-risk-required" }), { status: 403, headers: { "Content-Type": "application/problem+json" } });
describe("异常入场的有界恢复", () => {
 it("只在确定尚未执行时确认一次，保留原请求和提交凭据", async () => {
  const fetchImpl = vi.fn().mockResolvedValueOnce(denied()).mockResolvedValueOnce(new Response('{"ok":true}'));
  const admit = vi.fn().mockResolvedValue(true);
  const client = createApiClient({ fetchImpl, getCsrfToken: () => "csrf", onAdmissionRequired: admit });
  await client.post("/rooms", { body: { mode: "classic" }, idempotencyKey: "same-key" });
  expect(admit).toHaveBeenCalledExactlyOnceWith("traffic");
  expect(fetchImpl).toHaveBeenCalledTimes(2);
  expect(fetchImpl.mock.calls[0]?.[1]).toMatchObject({ body: '{"mode":"classic"}', headers: { "Idempotency-Key": "same-key" } });
  expect(fetchImpl.mock.calls[1]?.[1]).toMatchObject({ body: '{"mode":"classic"}', headers: { "Idempotency-Key": "same-key" } });
 });
 it("再次被拒绝也不会陷入确认或请求循环", async () => {
  const fetchImpl = vi.fn().mockImplementation(async () => denied());
  const admit = vi.fn().mockResolvedValue(true);
  const client = createApiClient({ fetchImpl, onAdmissionRequired: admit });
  await expect(client.post("/rooms", { idempotencyKey: "same-key" })).rejects.toBeInstanceOf(ApiError);
  expect(admit).toHaveBeenCalledOnce(); expect(fetchImpl).toHaveBeenCalledTimes(2);
 });
 it("缺少提交凭据的写入不会自动重放", async () => {
  const fetchImpl = vi.fn().mockResolvedValue(denied()); const admit = vi.fn();
  const client = createApiClient({ fetchImpl, onAdmissionRequired: admit });
  await expect(client.post("/rooms")).rejects.toBeInstanceOf(ApiError);
  expect(admit).not.toHaveBeenCalled(); expect(fetchImpl).toHaveBeenCalledOnce();
 });
});
