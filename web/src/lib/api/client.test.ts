/**
 * API client 行为测试。
 * 覆盖：同源 cookie、CSRF header、Idempotency-Key、Problem Details 解析、
 * X-Request-Id 记录、超时、取消、403/401 区分、写请求不自动重试。
 */
import { describe, expect, it, vi } from "vitest";
import {
  AbortedError,
  ApiError,
  NetworkError,
  TimeoutError,
  createApiClient,
  createIdempotencyKey,
  describeFailure,
} from "@/lib/api/client";

/**
 * 带类型的 fetch mock。
 * 裸 `vi.fn(async () => ...)` 的调用记录会被推成空元组 `[]`，
 * 访问 mock.calls[0][1] 时类型检查会失败（也无法断言 init）。
 * 这里显式声明调用签名为 [url, init]，让断言能拿到真实类型。
 */
type FetchArgs = [url: string, init: RequestInit];
type FetchMock = ReturnType<typeof vi.fn<(...args: FetchArgs) => Promise<Response>>>;

function createFetchMock(impl: (...args: FetchArgs) => Promise<Response>): FetchMock {
  return vi.fn<(...args: FetchArgs) => Promise<Response>>(impl);
}

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

describe("createIdempotencyKey", () => {
  it("生成 16–128 位可打印字符，且每次不同", () => {
    const a = createIdempotencyKey();
    const b = createIdempotencyKey();
    expect(a).toMatch(/^[A-Za-z0-9._~-]{16,128}$/);
    expect(a).not.toBe(b);
    // 48 个十六进制字符 = 192 位熵，满足 的 128 位要求
    expect(a.length).toBe(48);
  });
});

describe("API client", () => {
  it("GET 携带同源 cookie 且不设置 CSRF", async () => {
    const fetchImpl = createFetchMock(async () => jsonResponse({ ok: true }));
    const client = createApiClient({ fetchImpl: fetchImpl as unknown as typeof fetch });

    await client.get("/config");

    const [, init] = fetchImpl.mock.calls[0]!;
    expect(init.credentials).toBe("same-origin");
    const headers = init.headers as Record<string, string>;
    expect(headers["X-CSRF-Token"]).toBeUndefined();
    expect(init.cache).toBe("no-store");
  });

  it("写请求带 CSRF 与 Idempotency-Key", async () => {
    const fetchImpl = createFetchMock(async () => jsonResponse({ ok: true }, { status: 201 }));
    const client = createApiClient({
      getCsrfToken: () => "csrf-token-abc",
      fetchImpl: fetchImpl as unknown as typeof fetch,
    });

    await client.post("/rooms", { body: { name: "x" }, idempotencyKey: "idem-1234567890abcd" });

    const [url, init] = fetchImpl.mock.calls[0]!;
    expect(url).toBe("/api/v1/rooms");
    // init 可能被类型推成 undefined（mock 调用记录是元组数组），显式断言再访问
    expect(init).toBeDefined();
    const headers = init?.headers as Record<string, string>;
    expect(headers["X-CSRF-Token"]).toBe("csrf-token-abc");
    expect(headers["Idempotency-Key"]).toBe("idem-1234567890abcd");
    expect(headers["Content-Type"]).toBe("application/json");
    expect(init?.body).toBe(JSON.stringify({ name: "x" }));
  });

  it("记录 X-Request-Id 供诊断使用", async () => {
    const seen: string[] = [];
    const fetchImpl = vi.fn(async () =>
      jsonResponse({ ok: true }, { headers: { "Content-Type": "application/json", "X-Request-Id": "req-42" } }),
    );
    const client = createApiClient({
      onRequestId: (id) => seen.push(id),
      fetchImpl: fetchImpl as unknown as typeof fetch,
    });

    const response = await client.get("/config");
    expect(response.requestId).toBe("req-42");
    expect(seen).toEqual(["req-42"]);
  });

  it("解析 application/problem+json 并保留 code/requestId/retryable", async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(
        JSON.stringify({
          type: "https://umaguess.invalid/problems/phase-closed",
          title: "本轮已收卷",
          status: 409,
          code: "PHASE_CLOSED",
          requestId: "request-fixture-01",
          retryable: false,
          fieldErrors: [{ field: "phaseVersion", message: "已过期" }],
        }),
        { status: 409, headers: { "Content-Type": "application/problem+json" } },
      ),
    );
    const client = createApiClient({ fetchImpl: fetchImpl as unknown as typeof fetch });

    await expect(client.post("/rounds/1/submissions", { body: {} })).rejects.toMatchObject({
      name: "ApiError",
      status: 409,
      code: "PHASE_CLOSED",
      requestId: "request-fixture-01",
      retryable: false,
    });

    try {
      await client.post("/rounds/1/submissions", { body: {} });
    } catch (error) {
      const apiError = error as ApiError;
      expect(apiError.isConflict).toBe(true);
      expect(apiError.fieldErrors).toHaveLength(1);
      expect(apiError.fieldErrors[0]?.field).toBe("phaseVersion");
    }
  });

  it("403 被识别为 forbidden，不被当成空数据", async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(
        JSON.stringify({
          type: "about:blank",
          title: "无权访问",
          status: 403,
          code: "FORBIDDEN",
          requestId: "req-403",
        }),
        { status: 403, headers: { "Content-Type": "application/problem+json" } },
      ),
    );
    const client = createApiClient({ fetchImpl: fetchImpl as unknown as typeof fetch });

    try {
      await client.get("/rooms/x");
      throw new Error("应当抛出");
    } catch (error) {
      const failure = describeFailure(error);
      expect(failure.kind).toBe("forbidden");
      expect(failure.diagnosticId).toBe("req-403");
      expect(failure.retryable).toBe(false);
    }
  });

  it("401 触发 onUnauthenticated（清私有缓存）", async () => {
    const onUnauthenticated = vi.fn();
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ type: "about:blank", title: "失效", status: 401, code: "UNAUTHENTICATED", requestId: "r" }), {
        status: 401,
        headers: { "Content-Type": "application/problem+json" },
      }),
    );
    const client = createApiClient({ onUnauthenticated, fetchImpl: fetchImpl as unknown as typeof fetch });

    await expect(client.get("/sessions/current")).rejects.toBeInstanceOf(ApiError);
    expect(onUnauthenticated).toHaveBeenCalledTimes(1);
  });

  it("超时抛出 TimeoutError 并提示结果未知，而不是失败", async () => {
    const fetchImpl = vi.fn(
      (_url: string, init: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init.signal?.addEventListener("abort", () => {
            reject(new DOMException("aborted", "AbortError"));
          });
        }),
    );
    const client = createApiClient({ fetchImpl: fetchImpl as unknown as typeof fetch });

    await expect(client.post("/rounds/1/submissions", { body: {}, timeoutMs: 20 })).rejects.toBeInstanceOf(
      TimeoutError,
    );

    try {
      await client.post("/rounds/1/submissions", { body: {}, timeoutMs: 20 });
    } catch (error) {
      const failure = describeFailure(error);
      expect(failure.kind).toBe("timeout");
      // 玩家文案：不写「确认提交结果」这类内部口径，改为玩家能理解的说法
      expect(failure.title).toContain("还在确认这次答案是否提交成功");
      expect(failure.detail).toContain("先别重复作答");
      expect(failure.retryable).toBe(true);
    }
  });

  it("外部取消抛出 AbortedError（路由切换/StrictMode 卸载）", async () => {
    const controller = new AbortController();
    const fetchImpl = vi.fn(
      (_url: string, init: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
        }),
    );
    const client = createApiClient({ fetchImpl: fetchImpl as unknown as typeof fetch });

    const promise = client.get("/config", { signal: controller.signal });
    controller.abort();
    await expect(promise).rejects.toBeInstanceOf(AbortedError);
  });

  it("网络失败抛出 NetworkError 并映射为 offline", async () => {
    const fetchImpl = vi.fn(async () => {
      throw new TypeError("Failed to fetch");
    });
    const client = createApiClient({ fetchImpl: fetchImpl as unknown as typeof fetch });

    try {
      await client.get("/config");
      throw new Error("应当抛出");
    } catch (error) {
      expect(error).toBeInstanceOf(NetworkError);
      expect(describeFailure(error).kind).toBe("offline");
    }
  });

  it("200 但响应体不是 JSON 时不当成正常快照", async () => {
    const fetchImpl = vi.fn(
      async () => new Response("<html>not json</html>", { status: 200, headers: { "Content-Type": "text/html" } }),
    );
    const client = createApiClient({ fetchImpl: fetchImpl as unknown as typeof fetch });

    await expect(client.get("/config")).rejects.toMatchObject({ code: "INVALID_RESPONSE" });
  });

  it("204 返回 null 数据且不解析响应体", async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 204 }));
    const client = createApiClient({ fetchImpl: fetchImpl as unknown as typeof fetch });

    const response = await client.del("/sessions/current");
    expect(response.status).toBe(204);
    expect(response.data).toBeNull();
  });

  it("写请求不自动重试（mutation retry 由调用方控制）", async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ type: "about:blank", title: "忙", status: 503, code: "BUSY", requestId: "r" }), {
        status: 503,
        headers: { "Content-Type": "application/problem+json" },
      }),
    );
    const client = createApiClient({ fetchImpl: fetchImpl as unknown as typeof fetch });

    await expect(client.post("/rooms", { body: {}, idempotencyKey: "k".repeat(20) })).rejects.toBeInstanceOf(ApiError);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });
});

describe("bounded recovery after a definite CSRF refusal", () => {
  const refusal = () => jsonResponse({title:"Forbidden",code:"csrf-token-invalid"},{status:403});
  it("recovers once and preserves the exact action and request key", async () => {
    let token = "old";
    const fetchImpl = createFetchMock(async () => token === "old" ? refusal() : jsonResponse({revision:1}));
    const recover = vi.fn(async () => { token = "fresh"; return true; });
    const client = createApiClient({getCsrfToken:()=>token,onCsrfInvalid:recover,fetchImpl:fetchImpl as unknown as typeof fetch});
    const result = await client.post("/challenges/example/guesses",{body:{characterId:"1001",expectedRevision:0},idempotencyKey:"original-action-key"});
    expect(result.data).toEqual({revision:1});
    expect(recover).toHaveBeenCalledTimes(1);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    const first = fetchImpl.mock.calls[0]![1], second = fetchImpl.mock.calls[1]![1];
    expect(second.body).toBe(first.body);
    expect((second.headers as Record<string,string>)["Idempotency-Key"]).toBe("original-action-key");
    expect((second.headers as Record<string,string>)["X-CSRF-Token"]).toBe("fresh");
  });
  it("stops after one recovery when another tab rotates again", async () => {
    const recover = vi.fn(async()=>true);
    const fetchImpl = createFetchMock(async()=>refusal());
    const client = createApiClient({getCsrfToken:()=>"old",onCsrfInvalid:recover,fetchImpl:fetchImpl as unknown as typeof fetch});
    await expect(client.post("/rooms",{body:{name:"x"},idempotencyKey:"same-action-key"})).rejects.toMatchObject({code:"csrf-token-invalid"});
    expect(fetchImpl).toHaveBeenCalledTimes(2);
    expect(recover).toHaveBeenCalledTimes(1);
  });
  it.each(["origin-not-allowed","forbidden"])("never retries unrelated refusal %s", async code => {
    const recover = vi.fn(async()=>true);
    const fetchImpl = createFetchMock(async()=>jsonResponse({title:"Forbidden",code},{status:403}));
    const client = createApiClient({onCsrfInvalid:recover,fetchImpl:fetchImpl as unknown as typeof fetch});
    await expect(client.post("/rooms",{body:{name:"x"},idempotencyKey:"same-action-key"})).rejects.toBeInstanceOf(ApiError);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(recover).not.toHaveBeenCalled();
  });
});

describe("入场确认只恢复明确被拒绝的操作", () => {
  const refusal = () => jsonResponse({ title:"Forbidden",code:"admission-required" }, {status:403});
  it("通过后保留原正文与原操作键，仅补发一次", async () => {
    const fetchImpl=createFetchMock(async()=>refusal());
    fetchImpl.mockResolvedValueOnce(refusal()).mockResolvedValueOnce(jsonResponse({ok:true},{status:201}));
    const ask=vi.fn(async()=>true);
    const client=createApiClient({fetchImpl:fetchImpl as unknown as typeof fetch,getCsrfToken:()=>"csrf",onAdmissionRequired:ask});
    await client.post("/rooms",{body:{name:"一起开跑"},idempotencyKey:"one-original-action"});
    expect(ask).toHaveBeenCalledTimes(1);expect(fetchImpl).toHaveBeenCalledTimes(2);
    expect(fetchImpl.mock.calls[0]?.[1].body).toBe(fetchImpl.mock.calls[1]?.[1].body);
    expect(fetchImpl.mock.calls[1]?.[1].headers).toMatchObject({"Idempotency-Key":"one-original-action"});
  });
  it("持续拒绝也不反复打开面板",async()=>{
    const fetchImpl=createFetchMock(async()=>refusal()),ask=vi.fn(async()=>true);
    const client=createApiClient({fetchImpl:fetchImpl as unknown as typeof fetch,onAdmissionRequired:ask});
    await expect(client.post("/rooms",{body:{},idempotencyKey:"same-original-action"})).rejects.toMatchObject({code:"admission-required"});
    expect(ask).toHaveBeenCalledTimes(1);expect(fetchImpl).toHaveBeenCalledTimes(2);
  });
  it("取消和网络未知结果均不重放创建",async()=>{
    const ask=vi.fn(async()=>false),fetchImpl=createFetchMock(async()=>refusal());
    const client=createApiClient({fetchImpl:fetchImpl as unknown as typeof fetch,onAdmissionRequired:ask});
    await expect(client.post("/rooms",{body:{},idempotencyKey:"same-original-action"})).rejects.toBeInstanceOf(ApiError);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    fetchImpl.mockRejectedValueOnce(new TypeError("offline"));ask.mockClear();
    await expect(client.post("/rooms",{body:{},idempotencyKey:"same-original-action"})).rejects.toBeInstanceOf(NetworkError);
    expect(ask).not.toHaveBeenCalled();expect(fetchImpl).toHaveBeenCalledTimes(2);
  });
  it("页面取消后即使验证通过也不发送旧操作",async()=>{
    let finish:(ok:boolean)=>void=()=>undefined;
    const ask=vi.fn(()=>new Promise<boolean>(resolve=>{finish=resolve}));
    const fetchImpl=createFetchMock(async()=>refusal());
    const client=createApiClient({fetchImpl:fetchImpl as unknown as typeof fetch,onAdmissionRequired:ask});
    const controller=new AbortController();
    const result=client.post("/rooms",{body:{},idempotencyKey:"same-original-action",signal:controller.signal});
    await vi.waitFor(()=>expect(ask).toHaveBeenCalledOnce());
    controller.abort();finish(true);
    await expect(result).rejects.toBeInstanceOf(AbortedError);expect(fetchImpl).toHaveBeenCalledTimes(1);
  });
});
