import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage, LANGUAGES } from "@/i18n";
import { errors } from "@/i18n/errors";
import { ForbiddenView, ErrorView } from "@/components/uma/page-state";
import { failureIsCredentialRefresh, isCredentialRefreshFailure, RejoinPrompt } from "@/features/modes/rejoin-prompt";
import { ApiError, createApiClient, describeFailure, localizeFailure } from "./client";
import { SessionStateError, type SessionErrorCode } from "./session-errors";
import { withSessionLock } from "./session-lock";

afterEach(async () => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); await changeLanguage("zh-Hans"); });

it("credential recovery survives language changes without treating every refusal as rejoin", async () => {
  const fetchImpl = vi.fn();
  vi.useFakeTimers();
  const client = createApiClient({ fetchImpl, requireCsrf: true });
  const error = await client.post("/rounds", { idempotencyKey: "same-action" }).catch((reason: unknown) => reason);
  expect(fetchImpl).not.toHaveBeenCalled();
  expect(vi.getTimerCount()).toBe(0);
  vi.useRealTimers();
  expect(isCredentialRefreshFailure(error)).toBe(true);
  const failure = describeFailure(error);
  const rejected = describeFailure(new ApiError({ status: 403, code: "host-required", message: "private detail", requestId: null }));
  const unknown = describeFailure(new ApiError({ status: 403, code: "unknown-refusal", message: "private detail", requestId: null }));
  for (const lng of LANGUAGES) {
    await changeLanguage(lng);
    expect(failureIsCredentialRefresh(localizeFailure(failure))).toBe(true);
    expect(failureIsCredentialRefresh(rejected)).toBe(false);
    expect(failureIsCredentialRefresh(unknown)).toBe(false);
    const view = render(<MemoryRouter><ForbiddenView failure={failure} /><RejoinPrompt roomId="same-room" /></MemoryRouter>);
    expect(screen.getByRole("link").getAttribute("href")).toBe("/rooms/same-room");
    expect(screen.getAllByRole("alert")[0]!.textContent).toContain(errors[lng]["session-rejoin-required"]!.detail);
    expect(document.body.textContent).not.toMatch(/CSRF|private detail/);
    view.unmount();
  }
  const refused = new ApiError({ status: 403, code: "csrf-token-invalid", message: "internal", requestId: null });
  expect(isCredentialRefreshFailure(refused)).toBe(true);
  expect(failureIsCredentialRefresh(describeFailure(refused))).toBe(true);
});

it("a lock timeout gives the right next step and never runs the session mutation", async () => {
  vi.useFakeTimers();
  const work = vi.fn();
  vi.stubGlobal("navigator", { locks: { request: vi.fn((_name: string, options: { signal: AbortSignal }) => new Promise((_resolve, reject) => {
    options.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true });
  })) } });
  const result = withSessionLock(work).catch((error: unknown) => error);
  await vi.advanceTimersByTimeAsync(15_000);
  const error = await result;
  expect(error).toBeInstanceOf(SessionStateError);
  expect(describeFailure(error).messageKey).toBe("session-busy");
  expect(work).not.toHaveBeenCalled();
  expect(vi.getTimerCount()).toBe(0);
});

it("a failed storage open can be retried and does not create a new identity", async () => {
  const open = vi.fn(() => { throw new DOMException("private diagnostic", "SecurityError"); });
  vi.stubGlobal("navigator", {}); vi.stubGlobal("indexedDB", { open });
  const work = vi.fn();
  for (let attempt = 0; attempt < 2; attempt++) {
    const error = await withSessionLock(work).catch((reason: unknown) => reason);
    expect(error).toBeInstanceOf(SessionStateError);
    expect(describeFailure(error).messageKey).toBe("session-storage-unavailable");
  }
  expect(open).toHaveBeenCalledTimes(2);
  expect(work).not.toHaveBeenCalled();
});

it.each<SessionErrorCode>(["session-storage-unavailable", "session-storage-blocked", "session-busy", "session-restore-failed", "session-create-failed"])("%s has specific guidance in each language", async code => {
  const failure = describeFailure(new SessionStateError(code, { cause: new Error("private diagnostic") }));
  render(<ErrorView failure={failure} />);
  for (const lng of LANGUAGES) {
    await act(async () => { await changeLanguage(lng); });
    expect(screen.getByRole("alert").textContent).toContain(errors[lng][code]!.detail);
    expect(failure.messageKey).not.toBe("unknown");
    expect(document.body.textContent).not.toMatch(/private diagnostic|主持|進行役/);
  }
});
