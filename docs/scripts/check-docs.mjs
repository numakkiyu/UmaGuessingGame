import { readFile, readdir, access } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createMarkdownRenderer } from "vitepress";

const docs = fileURLToPath(new URL("..", import.meta.url));
const root = path.dirname(docs);
const locales = ["", "zh-Hant", "ja"];
const errors = [];
const names = (await readdir(docs))
  .filter((name) => name.endsWith(".md"))
  .concat((await readdir(path.join(docs, "play"))).filter(name => name.endsWith(".md")).map(name => `play/${name}`))
  .sort();
const markdown = await createMarkdownRenderer(docs, { html: true }, "/");
const rendered = new Map();
const repository = "https://github.com/numakkiyu/UmaGuessingGame/blob/main/";

async function exists(file) {
  try {
    await access(file);
    return true;
  } catch {
    return false;
  }
}
async function render(file) {
  if (!rendered.has(file))
    rendered.set(file, markdown.render(await readFile(file, "utf8")));
  return rendered.get(file);
}
function report(file, message) {
  errors.push(`${path.relative(root, file)}: ${message}`);
}

for (const locale of locales.slice(1)) {
  const translated = (await readdir(path.join(docs, locale)))
    .filter((name) => name.endsWith(".md"))
    .concat((await readdir(path.join(docs, locale, "play"))).filter(name => name.endsWith(".md")).map(name => `play/${name}`))
    .sort();
  if (JSON.stringify(names) !== JSON.stringify(translated))
    errors.push(
      `${locale}: document filenames differ from the source language`,
    );
}
const files = locales.flatMap((locale) =>
  names.map((name) => path.join(docs, locale, name)),
);
files.push(
  ...[
    "README.md",
    "README.zh-Hant.md",
    "README.ja.md",
    "CONTRIBUTING.md",
    "SECURITY.md",
    "CODE_OF_CONDUCT.md",
  ].map((name) => path.join(root, name)),
);

for (const file of files) {
  const source = await readFile(file, "utf8");
  if (source.trim().length < 80)
    report(file, "document is empty or incomplete");
  if (
    /(?:\.agent\/|\/Users\/|codex-clipboard-|接入验收|接入驗收|运行前检查|執行前檢查|验收清单|驗收清單|原片)/u.test(
      source,
    )
  )
    report(file, "private reference or obsolete process section");
  const html = await render(file);
  for (const match of html.matchAll(
    /<(?:a|img|source)\b[^>]*\b(?:href|src)="([^"]+)"/gu,
  )) {
    let url = match[1].replaceAll("&amp;", "&");
    if (url.startsWith(repository))
      url = path.relative(
        path.dirname(file),
        path.join(root, decodeURIComponent(url.slice(repository.length))),
      );
    if (/^(?:https?:|mailto:|data:|\/\/)/u.test(url)) continue;
    const [pathname, anchor] = url.split("#");
    const clean = decodeURIComponent(pathname.split("?")[0]);
    let target = clean
      ? path.resolve(
          clean.startsWith("/")
            ? path.join(docs, "public")
            : path.dirname(file),
          clean.replace(/^\//u, ""),
        )
      : file;
    if (!(await exists(target)) && target.endsWith(".html")) {
      const stem = target.slice(0, -5);
      target = (await exists(stem)) ? stem : stem + ".md";
    }
    if (!(await exists(target))) {
      report(file, `missing link: ${match[1]}`);
      continue;
    }
    if (anchor && target.endsWith(".md")) {
      const id = decodeURIComponent(anchor);
      const page = await render(target);
      if (
        ![...page.matchAll(/\bid="([^"]+)"/gu)].some((item) => item[1] === id)
      )
        report(file, `missing section: ${match[1]}`);
    }
  }
}
if (errors.length) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log(
    `${files.length} documents: language coverage, public references, local links and sections passed`,
  );
}
