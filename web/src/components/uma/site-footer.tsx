
import { BookImage, CodeXml, ExternalLink, LibraryBig } from "lucide-react";
import { Link } from "react-router";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";

const SOURCE_LINKS = [
  {
    label: "footer.biliwiki",
    href: "https://wiki.biligame.com/umamusume/",
    note: "footer.characters",
  },
  {
    label: "footer.moegirl",
    href: "https://zh.moegirl.org.cn/",
    note: "footer.aliases",
  },
] as const;

const PROJECT_LINKS = [
  { label: "footer.repository", href: "https://github.com/numakkiyu/UmaGuessingGame" },
  { label: "footer.maintainer", href: "https://github.com/numakkiyu" },
] as const;

export type SiteFooterProps = {
  className?: string;
  /**
   * 精简页脚：只留版权声明与来源入口，不铺开项目说明与链接卡。
   *
   * 对局页用这个。手机实测完整页脚 665px，比一屏还高——玩家在「猜一次」下面要滚过
   * 整页项目说明才能看到结果。信息不删，只是挪到「关于/来源」页，从精简页脚可达。
   */
  compact?: boolean;
};

export function SiteFooter({ className, compact = false }: SiteFooterProps) {
  const { t } = useTranslation("common");
  if (compact) {
    return (
      <footer
        className={cn(
          "mt-6 border-t border-[var(--color-line)] bg-[var(--color-panel)]",
          className,
        )}
      >
        <div className="mx-auto flex w-full max-w-[var(--content-max)] flex-wrap items-center gap-x-4 gap-y-1 px-[var(--gutter)] py-3 text-[length:var(--text-aux)] text-[var(--color-muted)]">
          {/* 精简页脚仍保留版权声明 */}
          <span>{t("footer.copyright")}</span>
          {/*
            页脚这两个链接原本是纯文字（实测 52×21px），点起来太窄。
            加 min-h-11 + inline-flex 把它撑到 44px 触摸区，
            视觉上仍是一行小字——用 items-center 让它垂直居中，
            不会把页脚撑高太多（下方 -my-2 抵消多出来的行高）。
          */}
          <Link
            to="/sources"
            className="-my-2 inline-flex min-h-11 items-center underline underline-offset-2"
          >
            {t("nav.sources")}
          </Link>
          <Link
            to="/help"
            className="-my-2 inline-flex min-h-11 items-center underline underline-offset-2"
          >
            {t("nav.help")}
          </Link>
        </div>
      </footer>
    );
  }

  return (
    <footer className={cn("mt-[var(--gap-region)] border-t border-[var(--color-line)] bg-[var(--color-panel)]", className)}>
      <div className="mx-auto w-full max-w-[var(--content-max)] px-[var(--gutter)] py-8">
        <div className="grid gap-6 md:grid-cols-[1.3fr_1fr]">
          <section className="min-w-0">
            <p className="font-semibold text-[var(--color-ink)]">{t("brand.name")}（UmaGuessingGame）</p>
            <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
              {t("footer.about")}
            </p>

            {/* 版权声明 */}
            <p className="mt-3 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] px-3 py-2 text-sm leading-6 text-[var(--color-ink)]">
              <span className="font-semibold">{t("footer.copyright")}</span>
            </p>

            <div className="mt-4 flex flex-wrap gap-2">
              {PROJECT_LINKS.map((link) => (
                <a
                  key={link.href}
                  href={link.href}
                  target="_blank"
                  rel="noreferrer noopener"
                  className={cn(
                    "inline-flex min-h-11 items-center gap-2 rounded-full px-3 py-2",
                    "border border-[var(--color-line-strong)] bg-[var(--color-panel)]",
                    "text-sm font-medium text-[var(--color-ink)] hover:bg-[var(--color-panel-soft)]",
                  )}
                >
                  <CodeXml aria-hidden="true" className="size-4 text-[var(--color-action-green)]" />
                  {t(link.label)}
                  <ExternalLink aria-hidden="true" className="size-3 text-[var(--color-muted)]" />
                </a>
              ))}
              <Link
                to="/sources"
                className={cn(
                  "inline-flex min-h-11 items-center gap-2 rounded-full px-3 py-2",
                  "border border-[var(--color-line-strong)] bg-[var(--color-panel)]",
                  "text-sm font-medium text-[var(--color-ink)] hover:bg-[var(--color-panel-soft)]",
                )}
              >
                <LibraryBig aria-hidden="true" className="size-4 text-[var(--color-action-green)]" />
                {t("nav.sources")}
              </Link>
            </div>
          </section>

          <section className="min-w-0">
            <p className="flex items-center gap-2 font-semibold text-[var(--color-ink)]">
              <LibraryBig aria-hidden="true" className="size-4 text-[var(--color-action-green)]" />
              {t("footer.sources")}
            </p>
            <ul className="mt-3 space-y-2">
              {SOURCE_LINKS.map((link) => (
                <li key={link.href}>
                  <a
                    href={link.href}
                    target="_blank"
                    rel="noreferrer noopener"
                    className={cn(
                      "flex min-h-11 items-center justify-between gap-2 rounded-[var(--radius-control)] px-3 py-2.5",
                      "border border-[var(--color-line)] bg-[var(--color-panel-soft)]",
                      "text-sm text-[var(--color-ink)] hover:bg-[var(--color-panel)]",
                    )}
                  >
                    <span className="inline-flex min-w-0 items-center gap-2">
                      <BookImage aria-hidden="true" className="size-4 shrink-0 text-[var(--color-action-green)]" />
                      <span className="truncate">{t(link.label)}</span>
                    </span>
                    <ExternalLink aria-hidden="true" className="size-4 shrink-0 text-[var(--color-muted)]" />
                  </a>
                  <p className="mt-1 px-1 text-[var(--text-aux)] text-[var(--color-muted)]">{t(link.note)}</p>
                </li>
              ))}
            </ul>
            <p className="mt-4 text-[var(--text-aux)] leading-6 text-[var(--color-muted)]">
              {t("footer.feedback")}
            </p>
          </section>
        </div>
      </div>
    </footer>
  );
}
