import type { NextConfig } from "next";

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
};

export default nextConfig;
