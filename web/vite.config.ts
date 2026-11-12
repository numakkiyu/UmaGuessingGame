import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";
import { createHash } from "node:crypto";

// SPA 构建：产物输出 web/dist，由 Go 服务嵌入静态文件。
// 开发期 /api 通过同源代理转发，避免跨域凭证。
const apiTarget = process.env.UMAGUESS_API_TARGET ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react(), tailwindcss(), {
    name: "local-practice-shell",
    apply: "build",
    generateBundle(_options, bundle) {
      const files = Object.keys(bundle).filter(path => /\.(js|css)$/.test(path)).map(path => `/${path}`).sort();
      const key = createHash("sha256").update(files.join("\n")).digest("hex").slice(0,16);
      const assets = [...files, "/assets/ui/branding/official-logo-jp.png", "/assets/ui/scenes/lobby-classroom.jpg"];
      this.emitFile({ type: "asset", fileName: "practice-sw.js", source: `
const SHELL = "umaguess-practice-shell-${key}";
const FILES = ${JSON.stringify(assets)};
self.addEventListener("install", event => event.waitUntil((async () => {
  const cache = await caches.open(SHELL);
  try {
    await cache.addAll(FILES);
    const response = await fetch("/play/classic", {cache:"reload", credentials:"omit"});
    if (!response.ok || !response.headers.get("content-type")?.includes("text/html")) throw new Error("shell unavailable");
    await cache.put("/__practice_shell__", response);
  } catch(error) { await caches.delete(SHELL); throw error; }
})()));
self.addEventListener("activate", event => event.waitUntil((async () => {
  for (const name of await caches.keys()) if (name.startsWith("umaguess-practice-shell-") && name !== SHELL) await caches.delete(name);
  await self.clients.claim();
})()));
self.addEventListener("fetch", event => {
  const url = new URL(event.request.url);
  if (event.request.method !== "GET" || url.origin !== self.location.origin) return;
  // No API, identity, room, private projection or stream enters Cache Storage.
  if (event.request.mode === "navigate" && (/^\\/practice(?:\\/|$)/.test(url.pathname) || /^\\/play\\/(?:classic|sort|memory|aptitude-grid|draft|race-rebuild|co-race-path|strategy-race|pedigree|audio)\\/?$/.test(url.pathname))) {
    event.respondWith(caches.open(SHELL).then(cache => cache.match("/__practice_shell__")).then(response => response || fetch(event.request)));
  } else if (FILES.includes(url.pathname)) {
    event.respondWith(caches.open(SHELL).then(cache => cache.match(url.pathname)).then(response => response || fetch(event.request)));
  } else if (/^\\/assets\\/(?:avatars|faces)\\/[A-Za-z0-9_-]+\\.(?:webp|png)$/.test(url.pathname)) {
    event.respondWith(caches.open("umaguess-practice-media").then(cache => cache.match(url.pathname)).then(response => response || fetch(event.request)));
  }
});
` });
    },
  }],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: apiTarget,
        changeOrigin: false,
        ws: true,
      },
      /*
       * 角色头像由内容包提供（content/<pack>/public/assets/avatars/<id>.png），
       * 生产环境由 Go 静态服务同时提供 SPA 与内容素材。
       * 开发期只代理 /assets/avatars 到后端，本地 /assets/ui 的品牌素材仍由 Vite 提供，
       * 避免把 12MB 头像复制进 web/public 重复入库。
       */
      "/assets/faces": { target: apiTarget, changeOrigin: false },
      "/assets/avatars": {
        target: apiTarget,
        changeOrigin: false,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    // 首屏关键 JS gzip ≤220 KiB。路由级懒加载把模式页拆出首屏。
    reportCompressedSize: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) return undefined;
          // pnpm peer suffixes contain react-dom even for Radix packages.
          // Match the installed package path, not the surrounding store directory.
          const packagePath = id.slice(id.lastIndexOf("/node_modules/") + "/node_modules/".length);
          if (/^(react|react-dom)\//.test(packagePath)) return "react";
          if (packagePath.startsWith("react-router/")) return "router";
          if (packagePath.startsWith("@tanstack/")) return "query";
          if (packagePath.startsWith("@radix-ui/") || packagePath.startsWith("cmdk/")) return "ui";
          return "vendor";
        },
      },
    },
  },
});
