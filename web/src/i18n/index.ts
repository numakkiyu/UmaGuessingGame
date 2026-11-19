import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { common } from "./common";
import { errors } from "./errors";
import { modes } from "./modes";
import { lobby } from "./lobby";
import { theme } from "./theme";
export { useTranslation } from "react-i18next";

export const LANGUAGES = ["zh-Hans", "zh-Hant", "ja"] as const;
export type Language = (typeof LANGUAGES)[number];
export const LANGUAGE_STORAGE_KEY = "umaguess:language";

export function matchLanguage(value: string): Language | undefined {
  const tag = value.trim().replaceAll("_", "-").toLowerCase();
  if (/^ja(?:-|$)/.test(tag)) return "ja";
  if (!/^zh(?:-|$)/.test(tag)) return undefined;
  const parts = tag.split("-");
  if (parts.includes("hans")) return "zh-Hans";
  if (parts.some(p => ["hant", "tw", "hk", "mo"].includes(p))) return "zh-Hant";
  return "zh-Hans";
}

export function initialLanguage(): Language {
  if (typeof navigator !== "undefined") {
    for (const language of navigator.languages?.length ? navigator.languages : [navigator.language]) {
      const supported = matchLanguage(language);
      if (supported) return supported;
    }
  }
  try {
    const saved = localStorage.getItem(LANGUAGE_STORAGE_KEY);
    if (saved && LANGUAGES.includes(saved as Language)) return saved as Language;
  } catch { /* A denied preference store must not block play. */ }
  return "zh-Hans";
}

function applyLanguage(language: string) {
  const supported = matchLanguage(language) ?? "zh-Hans";
  if (typeof document !== "undefined") document.documentElement.lang = supported;
  try { localStorage.setItem(LANGUAGE_STORAGE_KEY, supported); } catch { /* Keep the in-memory preference. */ }
}

// Common navigation is synchronous and bundled: switching language needs no
// network and never remounts the router/providers or mutates a game's state.
void i18n.use(initReactI18next).init({
  lng: initialLanguage(),
  fallbackLng: "zh-Hans",
  supportedLngs: [...LANGUAGES],
  load: "currentOnly",
  initAsync: false,
  resources: Object.fromEntries(LANGUAGES.map(language => [language, { common: common[language], errors: errors[language], modes: modes[language], lobby: lobby[language], theme: theme[language] }])),
  ns: ["common", "errors", "modes", "lobby", "theme"],
  defaultNS: "common",
  interpolation: { escapeValue: false }, // React renders variables as text.
  returnNull: false,
  parseMissingKeyHandler: () => common[matchLanguage(i18n.language) ?? "zh-Hans"].state.textUnavailable,
  react: { useSuspense: false },
});
i18n.on("languageChanged", applyLanguage);
applyLanguage(i18n.language);
window.addEventListener("languagechange", () => { void i18n.changeLanguage(initialLanguage()); });

export function changeLanguage(language: Language) {
  return i18n.changeLanguage(language);
}

export default i18n;

/** Feature resources travel with their route chunk; all three locales stay available offline after that route is cached. */
export function registerMessages(namespace: string, catalog: Record<Language, object>): void {
  for (const language of LANGUAGES) i18n.addResourceBundle(language, namespace, catalog[language], true, true);
}
