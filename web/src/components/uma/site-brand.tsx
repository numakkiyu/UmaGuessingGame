
import { Link } from "react-router";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";

export type SiteBrandProps = {
  href?: string;
  compact?: boolean;
  showMark?: boolean;
  showTitle?: boolean;
  subtitle?: string;
  accentName?: boolean;
  className?: string;
};

function BrandInner({ compact, showMark, showTitle, subtitle, accentName }: Omit<SiteBrandProps, "href" | "className">) {
  const { t } = useTranslation("common");
  return (
    <>
      {showMark ? (
        <img
          src="/assets/ui/branding/party-logo.webp"
          alt=""
          width={compact ? 46 : 56}
          height={compact ? 46 : 56}
          className={cn("shrink-0 rounded-xl object-cover", !accentName && (compact ? "size-10" : "size-14"))}
          loading="eager"
          decoding="async"
        />
      ) : null}
      <span className="flex min-w-0 flex-col">
        <span className="flex items-baseline gap-2">
          <span className={cn("truncate text-[var(--color-ink)]", !accentName && (compact ? "text-base font-bold" : "text-lg font-bold"))}>
            {accentName ? <>{t("brand.lead")}<em className="uma-brand-accent">{t("brand.accent")}</em></> : t("brand.name")}
          </span>
          {showTitle ? (
            <span className="hidden text-[var(--text-aux)] text-[var(--color-muted)] sm:inline">
              UmaGuessingGame
            </span>
          ) : null}
        </span>
        {subtitle ? (
          <span className={cn("truncate text-[var(--color-muted)]", !accentName && "text-[var(--text-aux)]")}>{subtitle}</span>
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
  accentName = false,
  className,
}: SiteBrandProps) {
  const { t } = useTranslation("common");
  const classes = cn(
    // 触摸区 ≥44px：视觉保持紧凑，但可点区域满足最小触摸尺寸
    "inline-flex min-h-11 min-w-0 items-center rounded-[var(--radius-control)] py-1",
    !accentName && "gap-3",
    "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",
    className,
  );

  return (
    <Link to={href} className={classes} aria-label={t("brand.home")}>
      <BrandInner compact={compact} showMark={showMark} showTitle={showTitle} subtitle={subtitle} accentName={accentName} />
    </Link>
  );
}
