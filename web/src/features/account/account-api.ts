import { createApiClient } from "@/lib/api/client";
import type { components } from "@/lib/api/schema";

type Schemas = components["schemas"];
const client = createApiClient();
export const accountApi = {
  getAccount: (signal?: AbortSignal) => client.request<Schemas["AccountStatus"]>("/account", { method: "GET", signal, headers: { "X-Operation-Id": "getAccount" } }),
  startAccountLogin: (id: Schemas["AccountProviderId"], body: Schemas["AccountLoginStart"], signal?: AbortSignal) => client.request<Schemas["AccountLoginRedirect"]>(`/account/logins/${encodeURIComponent(id)}`, { method: "POST", body, signal, noCsrf: true, headers: { "X-Operation-Id": "startAccountLogin" } }),
  renewAccount: (csrf: string, signal?: AbortSignal) => client.request<Schemas["AccountStatus"]>("/account/renew", { method: "POST", signal, noCsrf: true, headers: { "X-Operation-Id": "renewAccount", "X-Account-CSRF": csrf } }),
  deleteAccountSession: (csrf: string, signal?: AbortSignal) => client.request<null>("/account/session", { method: "DELETE", signal, noCsrf: true, headers: { "X-Operation-Id": "deleteAccountSession", "X-Account-CSRF": csrf } }),
};
