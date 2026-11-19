import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ApiContext, getCsrfToken, setSessionCredentials, type ApiContextValue, type SessionState } from "@/lib/api";
import { changeLanguage } from "@/i18n";
import { readTrainerProfile, saveTrainerProfile } from "@/lib/trainer-profile";
import Editor from "./trainer-profile-editor";

const original: SessionState = { sessionId: "same-player", displayName: "原来的名字", csrfToken: "old-token", expiresAt: "2030-01-01" };
afterEach(async () => { cleanup(); setSessionCredentials(null); await changeLanguage("zh-Hans"); });

it("keeps the current identity and refreshed credential when saving a nickname", async () => {
  const closed = vi.fn(), committed = vi.fn();
  const update = vi.fn(async () => {
    setSessionCredentials({ sessionId: original.sessionId, csrfToken: "refreshed-token" });
    return { data: { ...original, displayName: "小特的伙伴", csrfToken: "" } };
  });
  setSessionCredentials(original);
  function Harness() {
    const [session, setSession] = useState<SessionState | null>(original);
    return <ApiContext.Provider value={{ session, api: { updateCurrentSession: update }, setSession: (next: SessionState) => { committed(next); setSession(next); setSessionCredentials(next); } } as unknown as ApiContextValue}><Editor onClose={closed} /></ApiContext.Provider>;
  }
  render(<Harness />);
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "小特的伙伴" } });
  fireEvent.click(screen.getByRole("button", { name: "就这样出发" }));
  await waitFor(() => expect(closed).toHaveBeenCalledOnce());
  expect(committed).toHaveBeenCalledWith({ ...original, displayName: "小特的伙伴", csrfToken: "refreshed-token" });
  expect(getCsrfToken()).toBe("refreshed-token");
  expect(readTrainerProfile().displayName).toBe("小特的伙伴");
});

it("refuses to overwrite an identity that changed during a pending rename", async () => {
  const closed = vi.fn(), committed = vi.fn();
  let finish!: (value: unknown) => void;
  const update = vi.fn(() => new Promise(resolve => { finish = resolve; }));
  setSessionCredentials(original);
  render(<ApiContext.Provider value={{ session: original, api: { updateCurrentSession: update }, setSession: committed } as unknown as ApiContextValue}><Editor onClose={closed} /></ApiContext.Provider>);
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "新名字" } });
  fireEvent.click(screen.getByRole("button", { name: "就这样出发" }));
  setSessionCredentials({ sessionId: "another-player", csrfToken: "another-token" });
  await act(async () => { finish({ data: { ...original, displayName: "新名字", csrfToken: "" } }); });
  expect(committed).not.toHaveBeenCalled();
  expect(closed).not.toHaveBeenCalled();
  expect(screen.getByRole("alert")).toBeTruthy();
  expect(readTrainerProfile().displayName).not.toBe("新名字");
});

it("retains appearance in memory when persistent storage rejects writes", () => {
  saveTrainerProfile({ displayName: "第一次", avatarId: "1001" });
  vi.spyOn(localStorage, "setItem").mockImplementation(() => { throw new DOMException("quota", "QuotaExceededError"); });
  saveTrainerProfile({ displayName: "第二次", avatarId: "1007" });
  expect(readTrainerProfile()).toEqual({ displayName: "第二次", avatarId: "1007" });
});
