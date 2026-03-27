const FALLBACK_AVATAR_SRC = "/assets/characters/avatar-fallback.svg";

const KNOWN_IMAGE_EXTENSIONS = ["png", "jpg", "jpeg", "webp", "svg"] as const;

type BuildImageSourceOptions = {
  primarySrc?: string | null;
  proxySrc?: string | null;
  remoteSrc?: string | null;
};

export function getFallbackAvatarSrc() {
  return FALLBACK_AVATAR_SRC;
}

export function assetIdFromApiPath(imagePath: string) {
  const match = imagePath.match(/^\/api\/assets\/([^/]+)$/);
  return match?.[1] ?? null;
}

export function inferImageExtension(imageUrl: string) {
  const normalized = imageUrl.toLowerCase();

  for (const extension of KNOWN_IMAGE_EXTENSIONS) {
    if (normalized.includes(`.${extension}`)) {
      return extension;
    }
  }

  return null;
}

export function buildStaticThumbnailPath(assetId: string, extension: string) {
  return `/assets/characters/thumbnails/${assetId}.${extension}`;
}

export function buildCachedThumbnailLocalPath(assetId: string, extension: string) {
  return `public/assets/characters/thumbnails/${assetId}.${extension}`;
}

export function buildImageSourceCandidates({
  primarySrc,
  proxySrc,
  remoteSrc,
}: BuildImageSourceOptions) {
  const candidates = [primarySrc, proxySrc, remoteSrc, FALLBACK_AVATAR_SRC].filter(
    (value): value is string => Boolean(value),
  );

  return [...new Set(candidates)];
}

