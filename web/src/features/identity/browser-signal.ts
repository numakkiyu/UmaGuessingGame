/** Optional, entry-only correlation. Not a person identifier or credential. */
export async function browserSignal(): Promise<string | undefined> {
  try {
    if (!crypto.subtle) return undefined;
    const canvas = document.createElement("canvas");
    canvas.width = 160; canvas.height = 32;
    const context = canvas.getContext("2d");
    if (context) {
      context.fillStyle = "#74b83f"; context.fillRect(0, 0, 160, 32);
      context.font = "16px sans-serif"; context.fillStyle = "#183c60";
      context.fillText("UmaGuessingGame", 4, 22);
    }
    const components = [location.origin, new Date().toISOString().slice(0, 10), navigator.userAgent,
      navigator.languages.slice(0, 3).join(","), navigator.hardwareConcurrency, navigator.maxTouchPoints,
      Intl.DateTimeFormat().resolvedOptions().timeZone,
      Math.min(screen.width, screen.height), Math.max(screen.width, screen.height), screen.colorDepth,
      context ? canvas.toDataURL() : ""];
    const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(JSON.stringify(components)));
    return [...new Uint8Array(digest)].map(value => value.toString(16).padStart(2, "0")).join("");
  } catch { return undefined; }
}
