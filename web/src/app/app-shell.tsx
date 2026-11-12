
import { Menu } from "lucide-react";
import { useEffect } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router";
import { SiteBrand } from "@/components/uma/site-brand";
import { SiteFooter } from "@/components/uma/site-footer";
import { Button } from "@/components/ui/button";
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";
import { LanguageSelect } from "@/i18n/language-select";
import { ConfirmationDialog } from "@/components/uma/confirmation-dialog";

const NAV_ITEMS = [
  { to: "/", label: "nav.lobby", end: true },
  { to: "/play/classic", label: "nav.classic", end: false },
  { to: "/help", label: "nav.help", end: false },
  { to: "/sources", label: "nav.sources", end: false },
] as const;

function NavLinks() {
  const { t } = useTranslation("common");
  return (
    <ul className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-1">
      {NAV_ITEMS.map((item) => (
        <li key={item.to}>
          <NavLink
            to={item.to}
            end={item.end}
            className={({ isActive }) =>
              cn(
                // 触摸区 ≥44px
                "flex min-h-11 items-center rounded-[var(--radius-control)] px-3 text-sm font-semibold",
                "transition-colors duration-[var(--duration-instant)] ease-[var(--ease-primary)]",
                isActive
                  ? // 选中态：深字 + 草绿底（ink on brand-green 7.08:1），同时有下边框
                    "bg-[var(--color-brand-green-soft)] text-[var(--color-ink)] shadow-[inset_0_-2px_0_var(--color-action-green)]"
                  : "text-[var(--color-muted)] hover:bg-[var(--color-panel-soft)] hover:text-[var(--color-ink)]",
              )
            }
          >
            {t(item.label)}
          </NavLink>
        </li>
      ))}
    </ul>
  );
}

export function AppShell() {
  const { t } = useTranslation("common");
  const location = useLocation();
  // 对局页（/rooms/*）用精简页脚；大厅、说明、来源页保留完整页脚
  const inRound = /^\/(rooms|play|practice|challenges|events|join|matchmaking)(\/|$)/.test(location.pathname);

  // 路由切换回到顶部，避免带着上页滚动位置进入新页
  useEffect(() => {
    window.scrollTo({ top: 0, behavior: "instant" });
  }, [location.pathname]);

  return (
    <div className="uma-app-shell">
      {/* 键盘用户第一站 */}
      <a href="#main-content" className="uma-skip-link">
        {t("nav.skip")}
      </a>

      <header className="uma-header">
        <div className="uma-header__inner">
          <SiteBrand compact />

          <div className="ml-auto hidden items-center gap-2 lg:flex">
            <nav aria-label={t("nav.main")}>
              <NavLinks />
            </nav>
            <LanguageSelect />
          </div>

          {/* 手机上帮助/导航收进 Sheet，主要任务不嵌多层弹窗 */}
          <div className="ml-auto lg:hidden">
            {/*
              路由切换关闭移动端菜单：用 key 让 Sheet 在路径变化时重置，
              而不是在 effect 里 setState 触发级联渲染。
            */}
            <Sheet key={location.pathname}>
              <SheetTrigger asChild>
                <Button variant="ghost" size="icon" aria-label={t("nav.open")}>
                  <Menu aria-hidden="true" className="size-5" />
                </Button>
              </SheetTrigger>
              <SheetContent side="right" closeLabel={t("nav.close")}>
                <SheetHeader>
                  <SheetTitle>{t("nav.title")}</SheetTitle>
                  <SheetDescription>{t("nav.description")}</SheetDescription>
                </SheetHeader>
                <NavLinks />
                <LanguageSelect />
                <div className="mt-6 flex flex-col gap-2">
                  <SheetClose asChild>
                    <Button asChild variant="secondary" block>
                      <Link to="/join">{t("nav.join")}</Link>
                    </Button>
                  </SheetClose>
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
      <ConfirmationDialog />
    </div>
  );
}
