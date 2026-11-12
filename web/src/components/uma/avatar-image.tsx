/** Tries approved image candidates in order and silently falls back on failure. Callers supply generic alternatives for hidden memory cards. */
import { useCallback, useMemo, useState } from "react";
import { cn } from "@/lib/utils";

/** Only pack assets under /assets/ are accepted. */
const ASSET_BASE = "/assets";

export type AvatarSize = "xs" | "sm" | "md" | "lg" | "xl";

/** 头像常规 40–56px，任务卡 64–96px */
const SIZE_PX: Record<AvatarSize, { w: number; h: number; className: string }> = {
  xs: { w: 32, h: 35, className: "size-8" },
  sm: { w: 40, h: 44, className: "size-10" },
  md: { w: 56, h: 61, className: "size-14" },
  lg: { w: 72, h: 79, className: "size-[72px]" },
  xl: { w: 96, h: 105, className: "size-24" },
};

export type AvatarImageProps = {
  /** 角色 ID；用于拼接受控素材地址 */
  characterId?: string | null;
  /** 完整可读名称。传空字符串表示装饰性图片 */
  alt: string;
  size?: AvatarSize;
  /** 首屏关键头像用 eager */
  loading?: "eager" | "lazy";
  className?: string;
  /**
   * 本地回退路径（后端 imagePath 字段，如 /assets/avatars/1001.webp）。
   * 必须同源受控路径；外链失败时切到这里。
   */
  src?: string | null;
  /**
   * 外链优先地址（后端 remoteImageUrl 字段）。
   * 约 20% 角色没有该字段，此时直接用 src。
   */
  remoteSrc?: string | null;
};

/** Only pack assets under /assets/ are accepted. */
function isControlledAssetPath(path: string): boolean {
  return path.startsWith(`${ASSET_BASE}/`) && !path.includes("..") && !/^[a-z][a-z0-9+.-]*:/i.test(path);
}

/** 外链校验：只接受 https，避免 http 混合内容被浏览器拦截、也避免 javascript: 等注入 */
function isAllowedRemoteUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return parsed.protocol === "https:";
  } catch {
    return false;
  }
}

function buildCandidateUrls(input: {
  characterId: string | null | undefined;
  src: string | null | undefined;
  remoteSrc: string | null | undefined;
}): { urls: string[]; remoteCount: number } {
  const urls: string[] = [];

  // 1) 外链优先（约 115/147 角色有）
  if (input.remoteSrc && isAllowedRemoteUrl(input.remoteSrc)) {
    urls.push(input.remoteSrc);
  }
  const remoteCount = urls.length;

  // 2) 本地 WebP 回退（后端 imagePath，始终存在）
  if (input.src && isControlledAssetPath(input.src)) {
    urls.push(input.src);
  } else if (input.characterId) {
    // 没有显式 src 时按 ID 拼接；仅接受受控 ID
    const safe = /^[A-Za-z0-9_-]+$/.test(input.characterId) ? input.characterId : null;
    if (safe) {
      // 新包是 .webp；.png 作为旧包兼容（后端会把 404 走到下一候选）
      urls.push(`${ASSET_BASE}/avatars/${safe}.webp`);
      urls.push(`${ASSET_BASE}/avatars/${safe}.png`);
    }
  }

  // 3) 占位图：两者都失败才显示（保持结构与品牌色，）
  urls.push(`${ASSET_BASE}/ui/branding/official-logo-jp.png`);
  return { urls, remoteCount };
}

export function AvatarImage({
  characterId,
  alt,
  size = "md",
  loading = "lazy",
  className,
  src,
  remoteSrc,
}: AvatarImageProps) {
  const { urls: candidates, remoteCount } = useMemo(
    () => buildCandidateUrls({ characterId, src, remoteSrc }),
    [characterId, src, remoteSrc],
  );
  return <AvatarCandidates key={candidates.join("\n")} candidates={candidates} remoteCount={remoteCount} alt={alt} size={size} loading={loading} className={className} />;
}

/** A different character starts a fresh, bounded fallback chain. */
function AvatarCandidates({ candidates, remoteCount, alt, size, loading, className }: {
  candidates: string[];
  remoteCount: number;
  alt: string;
  size: AvatarSize;
  loading: "eager" | "lazy";
  className?: string;
}) {
  const [index, setIndex] = useState(0);
  // Mark missing images only after both remote and local candidates fail
  const usingRemote = remoteCount > 0 && index < remoteCount;
  const dims = SIZE_PX[size];
  const currentSrc = candidates[Math.min(index, candidates.length - 1)] ?? candidates[0]!;
  const isFallback = index >= candidates.length - 1;

  const onError = useCallback(() => {
    // 静默前进到下一个候选；外链 → 本地 → 占位，不抛错、不提示
    setIndex((prev) => (prev < candidates.length - 1 ? prev + 1 : prev));
  }, [candidates.length]);

  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center justify-center",
        // 兜底图标时给一个浅底圆，保持布局稳定
        isFallback && "rounded-full bg-[var(--color-panel-soft)]",
        dims.className,
        className,
      )}
      // 占位与最终尺寸一致，避免 CLS
      style={{ aspectRatio: `${dims.w} / ${dims.h}` }}
    >
      <img
        // 头像图内已是圆形，不加 rounded-full，用 object-contain 保留耳朵
        src={currentSrc}
        alt={alt}
        width={dims.w}
        height={dims.h}
        loading={loading}
        decoding="async"
        onError={onError}
        className={cn("size-full object-contain", isFallback && "size-2/3")}
        // 外链跨源时降低 referrer 泄露，同时多数图床不需要 referrer
        {...(usingRemote ? { referrerPolicy: "no-referrer" as const } : {})}
        // 答案/记忆题由调用方传空 alt；此处不额外推断，避免泄露
      />
    </span>
  );
}
