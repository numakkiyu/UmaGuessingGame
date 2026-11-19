import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage } from "@/i18n";
import { AccountButtons, AccountGate, AccountRuntime, AccountStatus } from "./account-ui";
import { accountKey } from "./account-session";
import { account } from "@/i18n/account";
import { accountApi } from "./account-api";
import type { ReactNode } from "react";

vi.mock("./account-api", () => ({ accountApi: { getAccount: vi.fn(), startAccountLogin: vi.fn(), renewAccount: vi.fn(), deleteAccountSession: vi.fn() } }));
afterEach(async () => { cleanup(); vi.clearAllMocks(); history.replaceState(null, "", "/"); await changeLanguage("zh-Hans"); });
const access = { enabled: true, requireLogin: { friend: true }, providers: [ { id: "google" as const, available: true }, { id: "github" as const, available: false } ] };
function open(content: ReactNode, signedIn = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(accountKey, { authenticated: signedIn, ...(signedIn ? { userId: "7c4b03e5-e141-42ac-b657-27f049f32cab", csrfToken: "in-memory", expiresAt: "2030-01-01T00:00:00Z" } : {}) });
  render(<QueryClientProvider client={client}><MemoryRouter>{content}</MemoryRouter></QueryClientProvider>);
  return client;
}
it("shows the signed-out status without exposing credentials or internal wording", () => {
  open(<AccountStatus />);
  expect(screen.getByText("未登录")).toBeTruthy();
  expect(document.body.textContent).not.toMatch(/token|OIDC|OAuth|UUID/i);
  expect(accountApi.getAccount).not.toHaveBeenCalled();
});
it("replaces the signed-out status after authentication", () => {
  open(<AccountStatus />, true);
  expect(screen.queryByText("未登录")).toBeNull();
  expect(screen.getByText("已登录")).toBeTruthy();
});
it("blocks protected content and opens the global profile instead of a second identity form", () => {
  const opened = vi.fn(); window.addEventListener("umaguess:open-profile", opened);
  open(<AccountGate><div>protected game</div></AccountGate>);
  expect(screen.queryByText("protected game")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "选择登录方式" }));
  expect(opened).toHaveBeenCalledOnce();
  expect(screen.queryByRole("textbox")).toBeNull();
  window.removeEventListener("umaguess:open-profile", opened);
});
it("allows the protected game when the account is authenticated", () => {
  open(<AccountGate><div>protected game</div></AccountGate>, true);
  expect(screen.getByText("protected game")).toBeTruthy();
});
it("hides the login section when no template is available", () => {
  open(<AccountButtons config={{...access,providers:[{id:"google",available:false}]}} />);
  expect(screen.queryByRole("button")).toBeNull();
  expect(screen.queryByText("登录账户")).toBeNull();
});
it("hides unconfigured presets and uses the existing styled failure dialog", async () => {
  vi.mocked(accountApi.startAccountLogin).mockRejectedValueOnce(new Error("PRIVATE_PROVIDER_TOKEN"));
  open(<AccountButtons config={access} />);
  expect(screen.queryByRole("button", { name: /GitHub/ })).toBeNull();
  expect(accountApi.startAccountLogin).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "使用 Google 登录" }));
  await waitFor(() => expect(accountApi.startAccountLogin).toHaveBeenCalledWith("google", { returnTo: "/" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  expect(document.body.textContent).toContain("登录失败");
  expect(document.body.textContent).not.toContain("PRIVATE_PROVIDER_TOKEN");
  const logos = document.querySelectorAll<HTMLImageElement>(".uma-account-provider-mark img");
  expect(logos).toHaveLength(1);
  expect(logos.item(0)?.getAttribute("src")).toBe("/assets/ui/providers/google.svg");
});
it.each(["zh-Hans", "zh-Hant", "ja"] as const)("uses the shared template for custom providers in %s", async language => {
  await changeLanguage(language);
  const names = { "zh-Hans": "同好会", "zh-Hant": "同好會", ja: "サークル" };
  open(<AccountButtons config={{...access, providers:[{id:"club", available:true, displayNames:names, iconPath:"/assets/ui/providers/club.svg"}]}} />);
  expect(screen.getByRole("button", {name: account[language].provider.replace("{{name}}", names[language])})).toBeTruthy();
  expect(document.querySelector(".uma-account-providers")?.getAttribute("data-count")).toBe("1");
  expect(document.querySelector(".uma-account-provider-mark img")?.getAttribute("src")).toBe("/assets/ui/providers/club.svg");
  fireEvent.error(document.querySelector(".uma-account-provider-mark img")!);
  expect(document.querySelector(".uma-account-provider-mark img")).toBeNull();
  expect(document.querySelector(".uma-account-provider-mark svg")).toBeTruthy();
});
it("consumes a generic callback result without retaining it in the address bar", async () => {
  history.replaceState(null, "", "/?signin=failed");
  vi.mocked(accountApi.getAccount).mockResolvedValue({ data: { authenticated: false }, status: 200, requestId: null, location: null });
  open(<AccountRuntime />);
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  expect(location.search).toBe("");
});
it("describes logout failures without exposing provider details", async () => {
  vi.mocked(accountApi.deleteAccountSession).mockRejectedValueOnce(new Error("PRIVATE_SESSION_SECRET"));
  open(<AccountButtons config={access} />, true);
  fireEvent.click(screen.getByRole("button", { name: "退出登录" }));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();
  expect(document.body.textContent).toContain("退出失败");
  expect(document.body.textContent).not.toContain("PRIVATE_SESSION_SECRET");
});
it.each(["zh-Hans", "zh-Hant", "ja"] as const)("localizes account settings and the gate for %s", async language => {
  await changeLanguage(language);
  open(<><AccountButtons config={access} /><AccountGate>protected</AccountGate></>);
  expect(screen.getByRole("heading", { name: account[language].requiredTitle })).toBeTruthy();
  expect(screen.getByRole("button", { name: account[language].provider.replace("{{name}}", "Google") })).toBeTruthy();
  expect(Object.keys(account[language]).sort()).toEqual(Object.keys(account["zh-Hans"]).sort());
});
