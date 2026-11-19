import "@/i18n/account";
import "@/styles/account.css";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router";
import { LoaderCircle, LogIn, LogOut, ShieldCheck } from "lucide-react";
import { matchLanguage, useTranslation } from "@/i18n";
import { accountApi } from "./account-api";
import type { components, PublicConfig } from "@/lib/api/types";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { accountKey, useAccountSession } from "./account-session";

type Access = NonNullable<PublicConfig["accounts"]>;
type ProviderId = components["schemas"]["AccountProviderId"];
const providerIcons: Partial<Record<ProviderId, string>> = {
  qq: "/assets/ui/providers/qq.png",
  google: "/assets/ui/providers/google.svg",
  x: "/assets/ui/providers/x.svg",
  microsoft: "/assets/ui/providers/microsoft.svg",
  github: "/assets/ui/providers/github.svg",
  facebook: "/assets/ui/providers/facebook.png",
};
function providerIcon(provider: Access["providers"][number]) {
  if (provider.iconPath && /^\/assets\/ui\/providers\/[a-z0-9][a-z0-9._-]{0,63}\.(svg|png|webp)$/.test(provider.iconPath) && !provider.iconPath.includes("..")) return provider.iconPath;
  return Object.hasOwn(providerIcons, provider.id) ? providerIcons[provider.id] : undefined;
}
function ProviderMark({ provider }: { provider: Access["providers"][number] }) {
  const [failed, setFailed] = useState<string | null>(null);
  const source = providerIcon(provider);
  return <span className="uma-account-provider-mark" aria-hidden="true">{source && failed !== source ? <img src={source} width={20} height={20} alt="" decoding="async" onError={() => setFailed(source)} /> : <LogIn />}</span>;
}
export function AccountStatus() {
  const { t } = useTranslation("account");
  const state = useAccountSession();
  const status = state.isPending ? "checking" : state.isError ? "loadFailed" : state.data.authenticated ? "signedIn" : "signedOut";
  return <span className="uma-account-status" data-state={status}>{t(status)}</span>;
}
export function AccountButtons({ config }: { config: Access }) {
  const { t, i18n } = useTranslation("account");
  const state = useAccountSession();
  const [busy, setBusy] = useState<ProviderId | "__logout__" | null>(null);
  const [failed, setFailed] = useState<"login" | "logout" | null>(null);
  const inFlight = useRef(false);
  const providers = config.providers.filter(provider => provider.available);
  const language = matchLanguage(i18n.resolvedLanguage ?? i18n.language) ?? "zh-Hans";
  const providerName = (provider: Access["providers"][number]) => provider.displayNames?.[language] ?? t(`providers.${Object.hasOwn(providerIcons, provider.id) ? provider.id : "oidc"}`);
  async function login(id: ProviderId) {
    if (inFlight.current) return;
    inFlight.current = true; setBusy(id); setFailed(null);
    try {
      const returnTo = (["/", "/friends", "/party", "/matchmaking"] as const).find(path => path === location.pathname) ?? "/";
      const response = await accountApi.startAccountLogin(id, { returnTo });
      if (!response.data) throw new Error("Missing login redirect");
      const target = new URL(response.data.authorizationUrl);
      if (target.protocol !== "https:" || target.username || target.password) throw new Error("Invalid login redirect");
      location.assign(target.href);
    } catch { setFailed("login"); inFlight.current = false; setBusy(null); }
  }
  async function logout() {
    if (inFlight.current || !state.data?.csrfToken) return;
    inFlight.current = true; setBusy("__logout__"); setFailed(null);
    try { await accountApi.deleteAccountSession(state.data.csrfToken); location.assign("/"); }
    catch { setFailed("logout"); inFlight.current = false; setBusy(null); }
  }
  if (providers.length === 0 && !state.data?.authenticated) return null;
  return <section className="uma-account-settings" aria-label={t("title")}>
    <div className="uma-account-setting-title"><LogIn aria-hidden="true" /><strong>{t("title")}</strong></div>
    {state.isPending ? <p role="status">{t("checking")}</p> : state.isError ? <div className="uma-account-retry"><p>{t("loadFailed")}</p><Button type="button" variant="ghost" onClick={() => void state.refetch()}>{t("retry")}</Button></div> : state.data.authenticated ? <div className="uma-account-connected"><span><ShieldCheck aria-hidden="true" />{t("connected")}</span><Button type="button" variant="ghost" disabled={busy !== null} aria-busy={busy === "__logout__"} onClick={() => void logout()}>{busy === "__logout__" ? <LoaderCircle className="uma-account-spinner" aria-hidden="true" /> : <LogOut aria-hidden="true" />}{t("signOut")}</Button></div> : <>
      <p className="uma-account-prompt" role="status">{t(busy ? "busy" : "choose")}</p><div className="uma-account-providers" data-count={providers.length} aria-busy={busy !== null}>{providers.map(provider => <Button type="button" variant="secondary" key={provider.id} disabled={busy !== null} data-busy={busy === provider.id} aria-busy={busy === provider.id} onClick={() => void login(provider.id)} aria-label={t("provider", { name: providerName(provider) })}>
        {busy === provider.id ? <span className="uma-account-provider-mark" aria-hidden="true"><LoaderCircle className="uma-account-spinner" /></span> : <ProviderMark provider={provider} />}<span>{providerName(provider)}</span>
      </Button>)}</div>
    </>}
    {!state.isPending && !state.isError ? <p className="mt-3">{t("identityChange")}</p> : null}
    {failed ? <Alert tone="danger" title={t(failed === "logout" ? "signOutFailedTitle" : "failedTitle")} noticeKey={`account-${failed}-failed`}>{t(failed === "logout" ? "signOutFailed" : "failed")}</Alert> : null}
  </section>;
}
export function AccountGate({ children }: { children: ReactNode }) {
  const { t } = useTranslation("account");
  const state = useAccountSession();
  if (state.isPending) return <p role="status">{t("checking")}</p>;
  if (state.isError) return <div className="uma-account-entry"><p>{t("loadFailed")}</p><Button onClick={() => void state.refetch()}>{t("retry")}</Button></div>;
  if (state.data.authenticated) return children;
  return <section className="uma-account-entry"><div className="uma-account-entry-icon"><LogIn aria-hidden="true" /></div><h1>{t("requiredTitle")}</h1><p>{t("required")}</p><div><Button variant="primary" onClick={() => window.dispatchEvent(new Event("umaguess:open-profile"))}>{t("open")}</Button><Button asChild variant="ghost"><Link to="/">{t("home")}</Link></Button></div></section>;
}
export function AccountRuntime() {
  const { t } = useTranslation("account");
  const client = useQueryClient();
  const state = useAccountSession();
  const [failed] = useState(() => new URL(location.href).searchParams.get("signin") === "failed");
  useEffect(() => {
    const show = () => {
      const trigger = [...document.querySelectorAll<HTMLButtonElement>("[data-trainer-settings]")].find(button => button.getClientRects().length > 0);
      trigger?.click();
    };
    window.addEventListener("umaguess:open-profile",show);
    return () => window.removeEventListener("umaguess:open-profile",show);
  }, []);
  useEffect(() => {
    const url = new URL(location.href);
    const result = url.searchParams.get("signin");
    if (!result) return;
    url.searchParams.delete("signin");
    history.replaceState(history.state, "", url.pathname + url.search + url.hash);
    void client.invalidateQueries({ queryKey: accountKey });
  }, [client]);
  useEffect(() => {
    if (!state.data?.authenticated || !state.data.csrfToken) return;
    let renewing = false;
    const renew = async () => {
      if (document.visibilityState === "hidden" || renewing) return;
      renewing = true;
      try { const result = await accountApi.renewAccount(state.data.csrfToken!); if (result.data) client.setQueryData(accountKey, result.data); }
      catch { void client.invalidateQueries({ queryKey: accountKey }); }
      finally { renewing = false; }
    };
    const timer = window.setInterval(() => void renew(), Math.min(10 * 60_000, Math.max(15_000, (Date.parse(state.data.expiresAt ?? "") - Date.now()) / 2 || 60_000)));
    const focus = () => { void client.invalidateQueries({ queryKey: accountKey }); };
    window.addEventListener("focus", focus);
    return () => { clearInterval(timer); window.removeEventListener("focus", focus); };
  }, [client, state.data?.authenticated, state.data?.csrfToken, state.data?.expiresAt]);
  return failed ? <Alert tone="danger" title={t("failedTitle")} noticeKey="account-callback-failed">{t("failed")}</Alert> : null;
}
