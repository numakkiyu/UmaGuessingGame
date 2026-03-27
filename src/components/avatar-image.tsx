"use client";

import { useMemo, useState } from "react";
import {
  buildImageSourceCandidates,
  getFallbackAvatarSrc,
} from "@/lib/assets/resolve-image";

type Props = {
  alt: string;
  className?: string;
  loading?: "eager" | "lazy";
  decoding?: "async" | "sync" | "auto";
  primarySrc?: string | null;
  proxySrc?: string | null;
  remoteSrc?: string | null;
};

export function AvatarImage({
  alt,
  className,
  loading = "lazy",
  decoding = "async",
  primarySrc,
  proxySrc,
  remoteSrc,
}: Props) {
  const sources = useMemo(
    () => buildImageSourceCandidates({ primarySrc, proxySrc, remoteSrc }),
    [primarySrc, proxySrc, remoteSrc],
  );
  const [sourceIndex, setSourceIndex] = useState(0);

  const currentSrc = sources[sourceIndex] ?? getFallbackAvatarSrc();

  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={currentSrc}
      alt={alt}
      loading={loading}
      decoding={decoding}
      onError={() => {
        setSourceIndex((current) =>
          current < sources.length - 1 ? current + 1 : current,
        );
      }}
      className={className}
    />
  );
}
