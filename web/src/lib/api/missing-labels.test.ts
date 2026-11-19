import { expect, it } from "vitest";
import { normalizeMemoryRound } from "./memory";
import { normalizeTradeRound } from "./trade";
import type { components } from "./types";

it("preserves missing memory and trade labels for localized view fallbacks", () => {
  const memory = normalizeMemoryRound({ publicState: { answer: { pairs: [
    { pairId: "unknown", cards: [{ cardId: "one", imagePath: "/one.webp" }] },
    { pairId: "known", label: "資料原名", cards: [{ cardId: "two", imagePath: "/two.webp" }] },
  ] } } } as unknown as components["schemas"]["RoundView"]);
  expect(memory?.revealedPairs.map(pair => pair.label)).toEqual(["", "資料原名"]);
  const trade = normalizeTradeRound({ publicState: {
    instances: [{ instanceId: "unknown", cardId: "one" }, { instanceId: "known", cardId: "two", label: "資料原名" }],
    cardKinds: [{ cardId: "one" }, { cardId: "two", label: "資料原名" }],
  } });
  expect(trade.instances.map(card => card.label)).toEqual(["", "資料原名"]);
  expect(trade.cardKinds.map(card => card.label)).toEqual(["", "資料原名"]);
});
