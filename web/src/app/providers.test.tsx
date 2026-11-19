import { useEffect } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AppProviders } from "./providers";
import { api, getCsrfToken, getSessionId, setSessionCredentials, useApi, type ApiContextValue, type SessionState } from "@/lib/api";
import { ApiError } from "@/lib/api/client";
import { readTrainerProfile, saveTrainerProfile } from "@/lib/trainer-profile";
import { usePlayerName } from "@/lib/player-name";
import { changeLanguage } from "@/i18n";

vi.mock("@/features/identity/entry-admission", () => ({ EntryAdmission: () => null }));
vi.mock("@/lib/api/session-lock", () => ({ withSessionLock: (work: () => Promise<unknown>) => work() }));
let context: ApiContextValue;
const original: SessionState & { serverTime: string } = { serverTime: "2026-11-15T00:00:00Z", sessionId: "original-player", displayName: "以前的名字", csrfToken: "original-token", expiresAt: "2030-01-01" };
function Probe() {
  const value = useApi();
  useEffect(() => { context = value; }, [value]);
  return <span data-testid="name">{usePlayerName()}</span>;
}
function show() { return render(<AppProviders><Probe /></AppProviders>); }
beforeEach(() => {
  window.history.replaceState(null, "", "/play/classic");
  setSessionCredentials(null);
  vi.spyOn(api, "getCurrentSession").mockResolvedValue({ data: original, requestId: null, status: 200, location: null });
  vi.spyOn(api, "createSession").mockResolvedValue({ data: { ...original, sessionId: "new-player" }, requestId: null, status: 200, location: null });
  vi.spyOn(api, "updateCurrentSession").mockResolvedValue({ data: { ...original, displayName: "统一昵称" }, requestId: null, status: 200, location: null });
});
afterEach(async () => {
  cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); setSessionCredentials(null);
  window.history.replaceState(null, "", "/");
  await changeLanguage("zh-Hans");
});

it("edits the shared local name without restoring or creating an online identity", async () => {
  show();
  act(() => saveTrainerProfile({ displayName: "统一昵称", avatarId: "1001" }));
  await act(async () => { await changeLanguage("ja"); });
  expect(screen.getByTestId("name").textContent).toBe("统一昵称");
  expect(context.session).toBeNull();
  expect(api.getCurrentSession).not.toHaveBeenCalled();
  expect(api.createSession).not.toHaveBeenCalled();
  expect(api.updateCurrentSession).not.toHaveBeenCalled();
});

it("synchronizes a restored identity once and retains a rotated credential for concurrent entries", async () => {
  saveTrainerProfile({ displayName: "统一昵称", avatarId: "1001" });
  vi.mocked(api.updateCurrentSession).mockImplementation(async () => {
    setSessionCredentials({ sessionId: original.sessionId, csrfToken: "rotated-token" });
    return { data: { ...original, displayName: "统一昵称", csrfToken: "" }, requestId: null, status: 200, location: null };
  });
  show();
  let entries!: SessionState[];
  await act(async () => { entries = await Promise.all([context.ensureSession("fallback"), context.ensureSession("fallback")]); });
  expect(entries[0]).toEqual({ ...original, displayName: "统一昵称", csrfToken: "rotated-token" });
  expect(entries[1]).toEqual(entries[0]);
  expect(getCsrfToken()).toBe("rotated-token");
  expect(getSessionId()).toBe(original.sessionId);
  expect(api.getCurrentSession).toHaveBeenCalledOnce();
  expect(api.updateCurrentSession).toHaveBeenCalledOnce();
  await act(async () => { await context.ensureSession("fallback"); });
  expect(api.updateCurrentSession).toHaveBeenCalledOnce();
  expect(api.createSession).not.toHaveBeenCalled();
});

it("rejects a failed name synchronization and permits a later retry without replacing the player", async () => {
  saveTrainerProfile({ displayName: "统一昵称", avatarId: "1001" });
  vi.mocked(api.updateCurrentSession).mockRejectedValueOnce(new Error("connection lost"));
  show();
  await act(async () => { await expect(context.ensureSession("fallback")).rejects.toThrow("connection lost"); });
  expect(context.session?.sessionId).toBe(original.sessionId);
  expect(api.createSession).not.toHaveBeenCalled();
  await act(async () => { await context.ensureSession("fallback"); });
  expect(context.session?.displayName).toBe("统一昵称");
  expect(api.updateCurrentSession).toHaveBeenCalledTimes(2);
});

it("does not overwrite another identity while a rename response is pending", async () => {
  saveTrainerProfile({ displayName: "统一昵称", avatarId: "1001" });
  let finish!: (value: Awaited<ReturnType<typeof api.updateCurrentSession>>) => void;
  vi.mocked(api.updateCurrentSession).mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  show();
  let pending!: Promise<unknown>;
  await act(async () => { pending = context.ensureSession("fallback").catch(error => error); });
  const other = { ...original, sessionId: "another-player", csrfToken: "another-token" };
  act(() => context.setSession(other));
  await act(async () => { finish({ data: { ...original, displayName: "统一昵称" }, requestId: null, status: 200, location: null }); });
  expect(await pending).toBeInstanceOf(Error);
  expect(context.session).toEqual(other);
  expect(getCsrfToken()).toBe("another-token");
  expect(api.createSession).not.toHaveBeenCalled();
});

it("uses the saved name for a new identity only after the old session is absent", async () => {
  saveTrainerProfile({ displayName: "统一昵称", avatarId: "1001" });
  vi.mocked(api.getCurrentSession).mockRejectedValue(new ApiError({ status: 401, code: "unauthenticated", message: "expired", requestId: null }));
  show();
  await act(async () => { await context.ensureSession("fallback"); });
  expect(api.createSession).toHaveBeenCalledExactlyOnceWith({ displayName: "统一昵称" });
  expect(api.updateCurrentSession).not.toHaveBeenCalled();
});

it("keeps the existing player's name when no shared preference was chosen", async () => {
  show();
  await act(async () => { await context.ensureSession("训练员"); });
  await act(async () => { await changeLanguage("ja"); });
  expect(screen.getByTestId("name").textContent).toBe(original.displayName);
  expect(readTrainerProfile().displayName).toBe("");
  expect(api.updateCurrentSession).not.toHaveBeenCalled();
  expect(api.createSession).not.toHaveBeenCalled();
});


it("keeps solo play and session recovery available when cross-page messaging is blocked", async () => {
  vi.stubGlobal("BroadcastChannel",class {constructor(){throw new DOMException("blocked","SecurityError");}});
  show();
  expect(context.session).toBeNull();
  expect(api.getCurrentSession).not.toHaveBeenCalled();
  await act(async () => {await context.ensureSession("训练员");});
  expect(getSessionId()).toBe(original.sessionId);
  expect(getCsrfToken()).toBe(original.csrfToken);
});

it("does not turn a successful identity update into a failure when notification fails", async () => {
  const close=vi.fn();
  vi.stubGlobal("BroadcastChannel",class {close=close;postMessage(){throw new Error("blocked notification");}});
  const rendered=show();
  await act(async () => {await context.ensureSession("训练员");});
  expect(context.session?.sessionId).toBe(original.sessionId);
  act(() => context.setSession({...original,csrfToken:"next-token"}));
  expect(getCsrfToken()).toBe("next-token");
  rendered.unmount();expect(close).toHaveBeenCalledOnce();
});
