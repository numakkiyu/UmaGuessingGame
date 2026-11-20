import { expect, it } from "vitest";
import i18n, { LANGUAGES } from ".";
import { modeRules } from "./mode-rules";
import { admission } from "./admission";
import { errors } from "./errors";

// Load production resources only, including catalogs that arrive with a route.
import.meta.glob(["./*.ts", "!./*.test.ts"], { eager: true });

function leaves(value: unknown, prefix = ""): Record<string, string> {
  if (typeof value === "string") return { [prefix]: value };
  if (!value || typeof value !== "object") return {};
  return Object.assign({}, ...Object.entries(value).map(([key, item]) => leaves(item, prefix ? `${prefix}.${key}` : key)));
}
const params = (value: string) => [...new Set([...value.matchAll(/{{\s*([^}]+)\s*}}/g)].map(match => match[1]!.trim()))].sort();

it("all registered catalogs have nonempty translations and matching interpolation variables", () => {
  const catalogs = i18n.services.resourceStore.data;
  for (const namespace of Object.keys(catalogs["zh-Hans"]!)) {
    const source = leaves(catalogs["zh-Hans"]![namespace]);
    for (const lng of LANGUAGES) {
      const target = leaves(catalogs[lng]?.[namespace]);
      expect(Object.keys(target).sort(), `${namespace}:${lng} keys`).toEqual(Object.keys(source).sort());
      for (const [key, text] of Object.entries(source)) {
        expect(target[key]?.trim(), `${namespace}:${lng}:${key}`).toBeTruthy();
        expect(params(target[key]!), `${namespace}:${lng}:${key} variables`).toEqual(params(text));
      }
    }
  }
});

it("fixed interface paragraphs omit final full stops while character quotations keep their punctuation", () => {
  const violations: string[] = [];
  for (const lng of LANGUAGES) {
    for (const [namespace, resource] of Object.entries(i18n.services.resourceStore.data[lng]!)) {
      for (const [key, text] of Object.entries(leaves(resource))) {
        if (namespace === "stage" && key.endsWith(".quote")) continue;
        if (/。\s*$/.test(text)) violations.push(`${lng}:${namespace}:${key}`);
      }
    }
  }
  expect(violations).toEqual([]);
});

it("rule explanations retain real text limits and emoji counting caveats in every locale", () => {
  for (const lng of LANGUAGES) {
    expect(modeRules[lng].bluff.rules.join(" ")).toMatch(/2[–～]40/);
    expect(modeRules[lng].bluff.rules.join(" ")).not.toContain("80");
    expect(modeRules[lng].creative.rules.join(" ")).toMatch(/80/);
    expect(modeRules[lng].creative.rules[1]).toMatch(/组合表情|組合表情|絵文字/);
    expect(modeRules[lng].creative.rules[1]).toMatch(/多个|多個|複数/);
  }
});

it("ambiguous submissions keep a confirmation step and admission explains optional browser signals", () => {
  for (const lng of LANGUAGES) {
    expect(errors[lng].timeout!.detail).toMatch(/查看|确认|確認/);
    expect(admission[lng].signals).toMatch(/浏览器特征|瀏覽器特徵|ブラウザーの特徴/);
    expect(admission[lng].errors.uncertain).toMatch(/确认并继续|確認並繼續|確認して続ける/);
    expect(errors[lng]["invitation-invalid"]!.detail).not.toMatch(/大小写|大小寫|大文字・小文字/);
  }
});
