import path from "node:path";
import { fileURLToPath } from "node:url";
import type { NextConfig } from "next";

const rootDir = path.dirname(fileURLToPath(import.meta.url));

const nextConfig: NextConfig = {
  images: {
    remotePatterns: [
      { protocol: "https", hostname: "umamusume.jp" },
      { protocol: "https", hostname: "patchwiki.biligame.com" },
      { protocol: "https", hostname: "wiki.biligame.com" },
      { protocol: "https", hostname: "storage.moegirl.org.cn" },
      { protocol: "https", hostname: "zh.moegirl.org.cn" },
    ],
  },
  turbopack: {
    root: rootDir,
  },
};

export default nextConfig;
