import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, it } from "vitest";
import i18n, { LANGUAGES } from ".";
import "./grid-online";
import "./audio-online";
import "./bluff-online";
import "./clue-online";
import "./path-online";
import "./creative-online";
import "./draft-online";
import "./memory-online";
import "./pedigree-online";
import "./race-online";
import "./rapport-online";
import "./sort-online";
import "./strategy-online";
import "./trade-online";
import "./classic-online";
import "./classic-start";
import "./admission";

// Equal catalogues alone cannot catch a key omitted from all three languages:
// a healthy timer used to say "page failed to load" in every locale.
const pages = import.meta.glob("../features/modes/*-round-page.tsx");
const files = [...Object.keys(pages), "../features/classic/classic-room-page.tsx", "../features/classic/classic-online-play-page.tsx", "../features/identity/entry-admission.tsx"];
it.each(files)("all literal gameplay copy resolves in three locales: %s", file => {
  const source = readFileSync(resolve(process.cwd(), "src/i18n", file), "utf8");
  const namespace = source.match(/useTranslation\("([^"]+)"\)/)?.[1];
  expect(namespace).toBeTruthy();
  const keys = new Set([...source.matchAll(/\bt\("([^"`]+)"(?:\s*[,)]|\s*$)/g)].map(match => match[1]!));
  const missing: string[] = [];
  for (const lng of LANGUAGES) for (const key of keys) {
    if (!i18n.exists(key, { ns: namespace, lng, fallbackLng: false })) missing.push(`${lng}:${key}`);
  }
  expect(missing, `${file} has unresolved player copy`).toEqual([]);
});
