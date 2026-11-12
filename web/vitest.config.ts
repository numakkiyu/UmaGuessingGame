import { defineConfig } from "vitest/config";
import { fileURLToPath, URL } from "node:url";


export default defineConfig({
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    environment: "jsdom",
    // jsdom 默认 about:blank 是 opaque origin，localStorage 会不可用；
    // 给一个真实 origin 才能测到偏好持久化行为。
    environmentOptions: {
      jsdom: { url: "http://localhost:3000/" },
    },
    globals: true,
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test/setup.ts"],
    css: false,
    restoreMocks: true,
  },
});
