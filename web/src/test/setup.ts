// Use jsdom-compatible storage and media-query shims; Node's experimental localStorage may otherwise shadow the DOM implementation
import { afterEach, beforeEach } from "vitest";

class MemoryStorage implements Storage {
  private store = new Map<string, string>();

  get length(): number {
    return this.store.size;
  }

  clear(): void {
    this.store.clear();
  }

  getItem(key: string): string | null {
    return this.store.get(key) ?? null;
  }

  key(index: number): string | null {
    return [...this.store.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.store.delete(key);
  }

  setItem(key: string, value: string): void {
    this.store.set(key, value);
  }
}

function installStorage(): void {
  if (typeof window === "undefined") return;
  const storage = new MemoryStorage();
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    writable: true,
    value: storage,
  });
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    writable: true,
    value: storage,
  });
}

function installMatchMedia(): void {
  if (typeof window === "undefined" || "matchMedia" in window) return;
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  });
}

installStorage();
installMatchMedia();

// Radix's keyboard list uses browser scrolling; jsdom has no layout engine.
if (!Element.prototype.scrollIntoView) Element.prototype.scrollIntoView = () => {};

beforeEach(() => {
  installStorage();
  installMatchMedia();
});

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.removeAttribute("data-motion");
});
