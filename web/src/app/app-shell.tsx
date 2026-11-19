import { MatchCurtainProvider } from "@/components/uma/match-curtain-provider";

import { BookOpen, ChevronRight, CircleHelp, Flag, Home, Languages, LibraryBig, Menu, Swords, Users } from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
import { AccountConfigContext } from "@/features/account/account-context";
const AccountRuntime = lazy(() => import("@/features/account/account-ui").then(m => ({default:m.AccountRuntime})));
import { Link, NavLink, Outlet, useLocation } from "react-router";
import { SiteBrand } from "@/components/uma/site-brand";
import { SiteFooter } from "@/components/uma/site-footer";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { usePublicConfig } from "@/lib/api/queries";
import { anyCanPlay } from "@/lib/deployment/capabilities";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";
import { LanguageSelect } from "@/i18n/language-select";
import { TrainerProfile } from "@/components/uma/trainer-profile";
import { DeferredRouteCurtain } from "@/components/uma/deferred-route-curtain";
import { pageScene } from "@/components/uma/theme-art";
const ConfirmationDialog = lazy(() => import("@/components/uma/confirmation-dialog").then(m => ({default:m.ConfirmationDialog})));

const NAV_ITEMS = [
  { to: "/", label: "nav.single", end: true, icon: Home },
  { to: "/friends", label:"nav.friends",end:false, icon: Users },
  { to: "/party", label:"nav.party",end:false, icon: Flag },
  { to: "/matchmaking", label: "nav.matchmaking",end:false, icon: Swords },
  { to: "/help", label: "nav.help", end: false, icon: BookOpen },
  { to: "/sources", label: "nav.sources", end: false, icon: LibraryBig },
] as const;

function NavLinks({ onNavigate, primary = false, menu = false }: {onNavigate?:()=>void; primary?: boolean; menu?: boolean}) {
  const config=usePublicConfig().data;
  const { t } = useTranslation("common");
  return (
    <ul className="uma-navigation-list">
      {NAV_ITEMS.filter(item => (!primary || !["/help", "/sources"].includes(item.to)) && (item.to !== "/matchmaking" || (config?.matchmakingLobbyEnabled !== false && anyCanPlay(config,"matchmaking")))).map((item) => (
        <li key={item.to}>
          <NavLink
            to={item.to}
            end={item.end}
            onClick={onNavigate}
            className={({ isActive }) =>
              cn(
                // 触摸区 ≥44px
                "uma-navigation-link flex items-center rounded-[var(--radius-control)] font-semibold",
                "transition-colors duration-[var(--duration-instant)] ease-[var(--ease-primary)]",
                isActive
                  ? "text-[var(--color-action-green)]"
                  : "text-[var(--color-muted)] hover:bg-[var(--color-panel-soft)] hover:text-[var(--color-ink)]",
              )
            }
          >
            {menu ? <item.icon aria-hidden="true" /> : null}
            <span>{t(item.label)}</span>
            {menu ? <ChevronRight aria-hidden="true" /> : null}
          </NavLink>
        </li>
      ))}
    </ul>
  );
}

export function AppShell() {
  const config = usePublicConfig().data;
  const [menuOpen,setMenuOpen]=useState(false);
  const { t } = useTranslation("common");
  const location = useLocation();
  const { t: themeT } = useTranslation("theme");
  // 对局页（/rooms/*）用精简页脚；大厅、说明、来源页保留完整页脚
  const inRound = /^\/(rooms|play|practice|challenges|events|join|matchmaking)(\/|$)/.test(location.pathname);

  // 路由切换回到顶部，避免带着上页滚动位置进入新页
  useEffect(() => {
    window.scrollTo({ top: 0, behavior: "instant" });
  }, [location.pathname]);

  useEffect(() => {
    const desktop = window.matchMedia("(min-width: 1024px)");
    const closeDesktopMenu = () => { if (desktop.matches) setMenuOpen(false); };
    desktop.addEventListener("change", closeDesktopMenu);
    return () => desktop.removeEventListener("change", closeDesktopMenu);
  }, []);

  return (
    <AccountConfigContext.Provider value={config?.accounts}><MatchCurtainProvider><div className="uma-app-shell">
      <div className="uma-page-backdrop" style={{ backgroundImage: `url("${pageScene(location.pathname)}")` }} aria-hidden="true" />
      {/* 键盘用户第一站 */}
      <a href="#main-content" className="uma-skip-link">
        {t("nav.skip")}
      </a>

      <header className="uma-header">
        <div className="uma-header__inner">
          <SiteBrand compact showTitle={false} accentName subtitle={themeT("subtitle")} className="uma-header-brand" />

          <nav className="uma-desktop-navigation" aria-label={t("nav.main")}>
            <NavLinks primary />
          </nav>
          <div className="uma-header-actions">
            <LanguageSelect compact />
            <Button asChild variant="ghost" size="icon" className="uma-header-help rounded-full"><Link to="/help" aria-label={t("nav.help")}><CircleHelp aria-hidden="true" className="size-5" /></Link></Button>
            <TrainerProfile compact />
          </div>

          {/* 手机上帮助/导航收进 Sheet，主要任务不嵌多层弹窗 */}
          <div className="uma-header-mobile-actions">
            <TrainerProfile compact />
            {/* 导航后关闭菜单，避免浮层遮住新页面 */}
            <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
              <SheetTrigger asChild>
                <Button variant="ghost" size="icon" aria-label={t("nav.open")}>
                  <Menu aria-hidden="true" className="size-5" />
                </Button>
              </SheetTrigger>
              <SheetContent side="right" className="uma-mobile-menu w-[min(90vw,24rem)]" closeLabel={t("nav.close")}>
                <SheetHeader>
                  <SheetTitle>{t("nav.title")}</SheetTitle>
                  <SheetDescription>{t("nav.description")}</SheetDescription>
                </SheetHeader>
                <div className="uma-mobile-menu-body">
                  <section className="uma-mobile-menu-language" aria-labelledby="menu-language">
                    <h3 id="menu-language"><Languages aria-hidden="true" />{t("language.label")}</h3>
                    <LanguageSelect fullWidth />
                  </section>
                  <nav className="uma-mobile-menu-play" aria-label={t("nav.main")}>
                    <NavLinks primary menu onNavigate={() => setMenuOpen(false)} />
                  </nav>
                  <nav className="uma-mobile-menu-links" aria-label={t("nav.title")}>
                    <Link to="/help" onClick={() => setMenuOpen(false)}><BookOpen aria-hidden="true" /><span>{t("nav.help")}</span><ChevronRight aria-hidden="true" /></Link>
                    <Link to="/sources" onClick={() => setMenuOpen(false)}><LibraryBig aria-hidden="true" /><span>{t("nav.sources")}</span><ChevronRight aria-hidden="true" /></Link>
                  </nav>
                </div>
              </SheetContent>
            </Sheet>
          </div>
        </div>
      </header>

      <main id="main-content" className="uma-main" tabIndex={-1}>
        <Outlet />
      </main>

      {/*
        对局页用精简页脚：完整页脚在手机上 665px，比一屏还高，会把「猜一次」之后的结果
        推到很远。信息没有删除——精简页脚保留版权声明和两个入口，完整内容在 /sources 与
        /help 里。
      */}
      <SiteFooter compact={inRound} />
      <Suspense fallback={null}><ConfirmationDialog /></Suspense>
      {config?.accounts?.enabled ? <Suspense fallback={null}><AccountRuntime /></Suspense> : null}
      <nav className="uma-bottom-nav" aria-label={t("nav.main")}>
        {[["/", "nav.single", Home], ["/matchmaking", "nav.matchmaking", Swords], ["/friends", "nav.friends", Users], ["/party", "nav.party", Flag]].filter(([to]) => to !== "/matchmaking" || (config?.matchmakingLobbyEnabled !== false && anyCanPlay(config,"matchmaking"))).map(([to, label, Icon]) => { const Mark = Icon as typeof Home; return <NavLink key={String(to)} to={String(to)} end={to === "/"}><Mark aria-hidden="true" /><span>{t(String(label))}</span></NavLink>; })}
      </nav>
      <DeferredRouteCurtain />
    </div></MatchCurtainProvider></AccountConfigContext.Provider>
  );
}
