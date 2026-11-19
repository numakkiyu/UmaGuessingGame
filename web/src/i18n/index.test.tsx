import { chooseSelect } from "@/test/select";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import i18n, { changeLanguage, initialLanguage, LANGUAGE_STORAGE_KEY, LANGUAGES, matchLanguage } from ".";
import { common } from "./common";
import { LanguageSelect } from "./language-select";
import { ConnectionBar } from "@/components/uma/connection-bar";
import { SiteBrand } from "@/components/uma/site-brand";
import { ApiError, describeFailure, NetworkError, TimeoutError } from "@/lib/api/client";

afterEach(async () => {
  cleanup();
  await changeLanguage("zh-Hans");
});

describe("language selection is independent of game identity", () => {
  it.each([
    ["zh-CN", "zh-Hans"], ["zh-SG", "zh-Hans"], ["zh-Hans-HK", "zh-Hans"],
    ["zh-HK", "zh-Hant"], ["zh-TW", "zh-Hant"], ["zh_Hant", "zh-Hant"], ["ja-JP", "ja"],
    ["en", undefined], ["javascript", undefined],
  ])("matches supported language %s", (input, expected) => expect(matchLanguage(input)).toBe(expected));

  it("uses a saved preference when browser languages do not match", () => {
    vi.spyOn(navigator, "languages", "get").mockReturnValue(["en-US"]);
    localStorage.setItem(LANGUAGE_STORAGE_KEY, "ja");
    expect(initialLanguage()).toBe("ja");
  });

  it.each([["ja-JP", "ja"], ["zh-CN", "zh-Hans"], ["zh-SG", "zh-Hans"], ["zh-TW", "zh-Hant"], ["zh-HK", "zh-Hant"], ["zh-MO", "zh-Hant"]])("follows browser %s before an old preference", (language, expected) => {
    vi.spyOn(navigator, "languages", "get").mockReturnValue([language]);
    localStorage.setItem(LANGUAGE_STORAGE_KEY, "ja");
    expect(initialLanguage()).toBe(expected);
  });

  it("follows a browser language change without clearing a game", async () => {
    localStorage.setItem("example-game", "unchanged");
    await changeLanguage("zh-Hans");
    vi.spyOn(navigator, "languages", "get").mockReturnValue(["ja-JP"]);
    window.dispatchEvent(new Event("languagechange"));
    expect(i18n.language).toBe("ja");
    expect(localStorage.getItem("example-game")).toBe("unchanged");
  });

  it("has the same nonempty common message keys in all three languages", () => {
    function keys(value: object, prefix = ""): string[] {
      return Object.entries(value).flatMap(([key, v]) => typeof v === "string"
        ? (expect(v.trim()).not.toBe(""), [`${prefix}${key}`]) : keys(v, `${prefix}${key}.`)).sort();
    }
    for (const language of LANGUAGES) expect(keys(common[language])).toEqual(keys(common["zh-Hans"]));
  });

  it("changes labels without changing identity, draft storage or location", async () => {
    localStorage.setItem("example-game", JSON.stringify({ id: "same", deadline: 123, draft: "原文保持" }));
    const before = localStorage.getItem("example-game");
    const url = location.href;
    render(<MemoryRouter><LanguageSelect /><SiteBrand /><ConnectionBar status="reconnecting" /></MemoryRouter>);
    await chooseSelect(screen.getByRole("combobox"),"ja");
    expect(await screen.findByText("再接続中")).toBeTruthy();
    expect(screen.getByRole("link", { name: "ウマ娘あてクイズ、ロビーへ戻る" })).toBeTruthy();
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("ja");
    expect(document.documentElement.lang).toBe("ja");
    expect(localStorage.getItem("example-game")).toBe(before);
    expect(location.href).toBe(url);
  });

  it("uses regional phrasing rather than converting simplified characters", async () => {
    await changeLanguage("zh-Hant");
    expect(i18n.t("nav.lobby")).toBe("遊戲大廳");
    expect(i18n.t("actions.reload")).toBe("重新載入");
    expect(i18n.t("nav.classic")).toBe("經典猜角色");
  });

  it("continues switching if browser storage is denied", async () => {
    vi.spyOn(localStorage, "setItem").mockImplementation(() => { throw new DOMException("private diagnostic", "SecurityError"); });
    await changeLanguage("ja");
    expect(document.documentElement.lang).toBe("ja");
    expect(i18n.t("nav.lobby")).toBe("ロビー");
  });

  it("does not expose missing internal keys", async () => {
    await changeLanguage("ja");
    expect(i18n.t("internal.missing.key")).toBe("表示する文言がありません");
  });

  it("keeps unknown submission results uncertain and raw diagnostics private", async () => {
    await changeLanguage("ja");
    const timeout = describeFailure(new TimeoutError(1));
    expect(timeout.title).toBe("直前の操作を確認しています");
    expect(timeout.retryable).toBe(true);
    expect(describeFailure(new NetworkError("secret raw diagnostic", null)).detail).not.toContain("secret");
    const error = describeFailure(new ApiError({ status: 500, code: "private-code", message: "SQL /private/file", requestId: "safe-id" }));
    expect(error.diagnosticId).toBe("safe-id");
    expect(error.detail).not.toMatch(/SQL|private/);
    await changeLanguage("zh-Hant");
    expect(describeFailure(new TimeoutError(1)).title).toBe("正在確認剛才的操作");
  });
});
