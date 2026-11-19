import { afterEach, expect, it } from "vitest";
import i18n, { changeLanguage, LANGUAGES } from "@/i18n";
import { roomDisplayName } from "./room-display-name";

afterEach(async () => { await changeLanguage("zh-Hans"); });

it("localizes stored matchmaking names on language change while preserving player room names", async () => {
  const matched = { kind: "matchmaking", name: "随机对战" };
  const friend = { kind: "friend", name: "随机对战" };
  for (const language of LANGUAGES) {
    await changeLanguage(language);
    const fallback = i18n.t("nav.friends");
    expect(roomDisplayName(matched, fallback)).toBe(i18n.t("nav.matchmaking"));
    expect(roomDisplayName(friend, fallback)).toBe(friend.name);
    expect(roomDisplayName(undefined, fallback)).toBe(fallback);
    expect(roomDisplayName({ kind: "friend", name: "" }, fallback)).toBe(fallback);
  }
  expect(matched.name).toBe("随机对战");
});
