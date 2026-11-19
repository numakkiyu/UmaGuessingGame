/** Shared by source checks and browser checks; never filters player-authored names. */
const LOCATION_WORDS = [
  "本机", "本地", "服务器", "云端", "服务端", "客户端",
  "本機", "伺服器", "雲端", "服務端", "用戶端", "客戶端",
  "サーバー", "サーバ", "ローカル", "クラウド", "クライアント",
];
const ENGLISH_LOCATION = /\b(?:local|server|cloud|backend|frontend|client[ -]side|on[ -]device)\b/gi;

export function findExecutionLocationWords(text) {
  const words = LOCATION_WORDS.filter(word => text.includes(word));
  return [...new Set([...words, ...(text.match(ENGLISH_LOCATION) ?? []).map(word => word.toLowerCase())])];
}

/** Include assistive copy and tooltips, not just painted text. */
export async function readPlayerSurface(page) {
  return page.evaluate(() => {
    const document = globalThis.document;
    const labels = [];
    for (const element of document.body.querySelectorAll("[aria-label], [aria-description], [title], [placeholder], [alt]")) {
      if (element.closest('[hidden], [inert], [aria-hidden="true"]')) continue;
      const style = globalThis.getComputedStyle(element);
      if (style.display === "none" || style.visibility === "hidden" || !element.getClientRects().length) continue;
      for (const attribute of ["aria-label", "aria-description", "title", "placeholder", "alt"]) {
        const value = element.getAttribute(attribute);
        if (value) labels.push(value);
      }
    }
    return [document.title, document.body.innerText, ...labels].join("\n");
  });
}
