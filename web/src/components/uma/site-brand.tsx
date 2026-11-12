
import { Link } from "react-router";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";

export type SiteBrandProps = {
  href?: string;
  compact?: boolean;
  showMark?: boolean;
  showTitle?: boolean;
  subtitle?: string;
  className?: string;
};

function BrandInner({ compact, showMark, showTitle, subtitle }: Omit<SiteBrandProps, "href" | "className">) {
  const { t } = useTranslation("common");
  return (
    <>
      {showMark ? (
        <img
          src="/assets/ui/branding/official-logo-jp.png"
          alt=""
          width={compact ? 46 : 56}
          height={compact ? 32 : 39}
          className={cn("shrink-0 object-contain", compact ? "h-8 w-auto" : "h-10 w-auto")}
          // 页头 LOGO 属于首屏品牌元素
          loading="eager"
          decoding="async"
        />
      ) : null}
      <span className="flex min-w-0 flex-col">
        <span className="flex items-baseline gap-2">
          <span className={cn("truncate font-bold text-[var(--color-ink)]", compact ? "text-base" : "text-lg")}>
            {t("brand.name")}
          </span>
          {showTitle ? (
            <span className="hidden text-[var(--text-aux)] text-[var(--color-muted)] sm:inline">
              UmaGuessingGame
            </span>
          ) : null}
        </span>
        {subtitle ? (
          <span className="truncate text-[var(--text-aux)] text-[var(--color-muted)]">{subtitle}</span>
        ) : null}
      </span>
    </>
  );
}

export function SiteBrand({
  href = "/",
  compact = false,
  showMark = true,
  showTitle = true,
  subtitle,
  className,
}: SiteBrandProps) {
  const { t } = useTranslation("common");
  const classes = cn(
    // 触摸区 ≥44px：视觉保持紧凑，但可点区域满足最小触摸尺寸
    "inline-flex min-h-11 min-w-0 items-center gap-3 rounded-[var(--radius-control)] py-1",
    "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",
    className,
  );

  return (
    <Link to={href} className={classes} aria-label={t("brand.home")}>
      <BrandInner compact={compact} showMark={showMark} showTitle={showTitle} subtitle={subtitle} />
    </Link>
  );
}
