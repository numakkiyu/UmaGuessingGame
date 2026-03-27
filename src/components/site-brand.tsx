import Image from "next/image";
import Link from "next/link";

type Props = {
  className?: string;
  compact?: boolean;
  href?: string;
  showMark?: boolean;
  showMini?: boolean;
  showTitle?: boolean;
  subtitle?: string;
};

function BrandInner({
  compact,
  showMark,
  showMini,
  showTitle,
  subtitle,
}: Pick<Props, "compact" | "showMark" | "showMini" | "showTitle" | "subtitle">) {
  return (
    <>
      {showMark ? (
        <span className="uma-brand-mark" aria-hidden="true">
          <span className="uma-brand-mark-glow" />
          <span className="uma-brand-avatar">
            <Image
              src="/assets/ui/branding/official-app-icon.png"
              alt=""
              fill
              sizes={compact ? "72px" : "92px"}
              className="object-cover"
            />
          </span>
        </span>
      ) : null}

      <span className="uma-brand-copy">
        <span className="uma-brand-topline">
          <span className="uma-brand-wordmark">
            <Image
              src="/assets/ui/branding/official-logo.png"
              alt="赛马娘标识"
              width={168}
              height={70}
              className="h-auto w-auto"
            />
          </span>
          {showMini ? <span className="uma-brand-mini">UmaGuessingGame</span> : null}
        </span>
        {showTitle ? <span className="uma-brand-title">赛马娘猜猜乐</span> : null}
        <span className="uma-brand-subtitle">
          {subtitle ?? "看线索，收范围，把答案一路追到终点。"}
        </span>
      </span>
    </>
  );
}

export function SiteBrand({
  className,
  compact = false,
  href,
  showMark = true,
  showMini = true,
  showTitle = true,
  subtitle,
}: Props) {
  const brandClassName = ["uma-brand", compact ? "uma-brand--compact" : "", className ?? ""]
    .filter(Boolean)
    .join(" ");

  if (href) {
    return (
      <Link href={href} className={brandClassName}>
        <BrandInner
          compact={compact}
          showMark={showMark}
          showMini={showMini}
          showTitle={showTitle}
          subtitle={subtitle}
        />
      </Link>
    );
  }

  return (
    <div className={brandClassName}>
      <BrandInner
        compact={compact}
        showMark={showMark}
        showMini={showMini}
        showTitle={showTitle}
        subtitle={subtitle}
      />
    </div>
  );
}
