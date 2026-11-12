/** Retries stale lazy-loaded chunks with one page reload per 30-second window. Other errors and repeated failures reach the route boundary. */
import { lazy, type ComponentType, type LazyExoticComponent } from "react";

const RELOAD_MARK = "uma:chunk-reload-at";
const RELOAD_WINDOW_MS = 30_000;

/** 判断一个异常是不是「页面资源版本对不上」。 */
export function isChunkLoadFailure(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  const message = error.message ?? "";
  // Chrome/Edge: "Failed to fetch dynamically imported module: ..."
  // Firefox: "error loading dynamically imported module: ..."
  // Safari: "Importing a module script failed."
  return (
    /failed to fetch dynamically imported module/i.test(message) ||
    /error loading dynamically imported module/i.test(message) ||
    /importing a module script failed/i.test(message) ||
    /loading chunk \S+ failed/i.test(message) ||
    /chunkloaderror/i.test(message)
  );
}

/**
 * 尝试用一次整页重载修复 chunk 版本不一致。
 * 返回 true 表示已经发起重载，调用方应停止后续处理；false 表示不该重载。
 */
export function tryRecoverChunkFailure(error: unknown): boolean {
  if (!isChunkLoadFailure(error)) return false;
  if (typeof window === "undefined") return false;

  try {
    const last = Number(window.sessionStorage.getItem(RELOAD_MARK) ?? "0");
    if (Number.isFinite(last) && Date.now() - last < RELOAD_WINDOW_MS) {
      // 刚刷过一次还是失败：说明不是版本问题，交给错误边界显示。
      return false;
    }
    window.sessionStorage.setItem(RELOAD_MARK, String(Date.now()));
  } catch {
    // 隐私模式下 sessionStorage 不可用：宁可不自动刷新，也不要冒无限刷新的风险。
    return false;
  }

  window.location.reload();
  return true;
}

/**
 * 与 React.lazy 同签名，额外在「chunk 版本不一致」时自动重载一次。
 *
 * 只接受返回 `{ default: Component }` 的 import()，和 router.tsx 里的现有写法一致。
 */
export function lazyWithRecovery<T extends ComponentType<unknown>>(
  factory: () => Promise<{ default: T }>,
): LazyExoticComponent<T> {
  return lazy(async () => {
    try {
      return await factory();
    } catch (error) {
      if (tryRecoverChunkFailure(error)) {
        // 页面正在重载。返回一个永不 resolve 的 promise，避免卸载过程中
        // 错误边界闪一下「加载失败」再刷新。
        return new Promise<{ default: T }>(() => {});
      }
      throw error;
    }
  });
}
