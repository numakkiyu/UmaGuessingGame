import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { changeLanguage, LANGUAGES } from "@/i18n";
import { concentrationCopy } from "@/i18n/concentration";
import { useReducedMotion } from "@/lib/hooks/use-reduced-motion";
import { ConcentrationResults } from "./concentration-results";
import { STORAGE_KEY, createGame, gameResult, questions, translations } from "./engine";

const game = { ...createGame(42, 1000), finishedAt: 126000 };
game.answers[0]!.name = questions(42)[0]!.chineseNames[0]!;
const result = gameResult(game);
let hidden = false, osReduced = false;
const osListeners = new Set<() => void>();
function show(reveal = false) {
  const again = vi.fn();
  return { ...render(<ConcentrationResults game={game} playerName="周末玩家" onAgain={again} disabled={false} reveal={reveal} />), again };
}
function motionSettings() {
  function Settings() {
    const { setPreference } = useReducedMotion();
    return <><button onClick={() => setPreference("reduced")}>reduce</button><button onClick={() => setPreference("full")}>full</button></>;
  }
  return render(<Settings />);
}
const sheet = () => document.querySelector(".uma-concentration-result")!;
function visibility(value: boolean) { act(() => { hidden = value; document.dispatchEvent(new Event("visibilitychange")); }); }

beforeEach(async () => {
  hidden = false; osReduced = false; osListeners.clear();
  vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden);
  vi.stubGlobal("matchMedia", () => ({ get matches() { return osReduced; }, addEventListener: (_: string, fn: () => void) => osListeners.add(fn), removeEventListener: (_: string, fn: () => void) => osListeners.delete(fn) }));
  await changeLanguage("zh-Hans");
});
afterEach(async () => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); await changeLanguage("zh-Hans"); });

it("shows exact final scores immediately and leaves replay available during the reveal", () => {
  vi.useFakeTimers();
  const { again } = show(true);
  expect(sheet().getAttribute("data-reveal")).toBe("true");
  expect(document.querySelector(".uma-concentration-value")!.textContent).toContain(`${result.concentration}%`);
  expect(document.querySelector(".uma-concentration-scoreline")!.textContent).toContain(`${result.score} / 400`);
  for (const key of ["accuracy", "completeness", "relevance"] as const) {
    expect(screen.getByRole("meter", { name: concentrationCopy["zh-Hans"][key] }).getAttribute("aria-valuenow")).toBe(String(result[key]));
  }
  fireEvent.click(screen.getByRole("button", { name: "再测一次" }));
  expect(again).toHaveBeenCalledTimes(1);
  act(() => vi.advanceTimersByTime(800));
  expect(sheet().getAttribute("data-reveal")).toBe("false");
  expect(screen.queryByRole("button", { name: "跳过动画" })).toBeNull();
  expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
});

it("skips only the presentation and keeps all scores and review answers", () => {
  vi.useFakeTimers(); show(true);
  fireEvent.click(screen.getByRole("button", { name: "跳过动画" }));
  expect(sheet().getAttribute("data-reveal")).toBe("false");
  expect(document.activeElement).toBe(screen.getByRole("heading", { name: "你的浓度测试结果" }));
  const rows = document.querySelectorAll<HTMLDetailsElement>(".uma-concentration-review-item");
  expect(rows).toHaveLength(20);
  expect([...rows].every(row => !row.open)).toBe(true);
  const row = rows[0]!;
  fireEvent.click(row.querySelector("summary")!);
  expect(row.open).toBe(true);
  expect(row.querySelector(".uma-concentration-review-fields")!.textContent).toContain(game.answers[0]!.name);
  expect(within(row).getAllByRole("link").every(link => link.getAttribute("rel") === "noopener noreferrer")).toBe(true);
  fireEvent.click(row.querySelector("summary")!);
  expect(row.open).toBe(false);
  expect(screen.getAllByRole("meter")).toHaveLength(3);
});

it("keeps restored results static and does not replay after language changes", async () => {
  show();
  for (const lng of LANGUAGES) {
    await act(async () => { await changeLanguage(lng); });
    expect(screen.getByRole("heading", { name: concentrationCopy[lng].finalTitle })).toBeTruthy();
    expect(sheet().getAttribute("data-reveal")).toBe("false");
    expect(screen.getByRole("meter", { name: concentrationCopy[lng].accuracy }).getAttribute("aria-valuenow")).toBe(String(result.accuracy));
    expect(screen.getByRole("img", { name: `${concentrationCopy[lng].rating} ${result.rank}` })).toBeTruthy();
    expect(sheet().textContent).not.toMatch(/。$/);
  }
});

it.each(LANGUAGES)("downloads localized answers and calculated points in %s and releases the file URL", async language => {
  await changeLanguage(language);
  vi.useFakeTimers();
  const create = vi.fn(() => "blob:concentration-result"), revoke = vi.fn();
  vi.stubGlobal("URL", class extends URL { static override createObjectURL = create; static override revokeObjectURL = revoke; });
  let exported = "";
  vi.stubGlobal("Blob", class extends Blob {
    constructor(parts: BlobPart[], options?: BlobPropertyBag) { super(parts, options); exported = parts.join(""); }
  });
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
    expect(this.download).toBe("umaguess-concentration-126000.txt");
    expect(this.href).toBe("blob:concentration-result");
  });
  show();
  const copy = concentrationCopy[language];
  fireEvent.click(screen.getByRole("button", { name: copy.download }));
  expect(exported).toContain(copy.certificate);
  expect(exported).toContain("周末玩家");
  expect(exported).toContain(`${copy.score}: ${result.score}/400`);
  expect(exported).toContain(`${copy.concentration}: ${result.concentration}% (${result.rank})`);
  expect(exported).toContain(`20. ${result.items[19]!.profile.names[language]}`);
  expect(exported).toContain(copy.fields.birthday);
  expect(exported).toContain(game.answers[0]!.name);
  expect(click).toHaveBeenCalledTimes(1);
  expect(create).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("status").textContent).toBe(copy.downloaded);
  act(() => vi.advanceTimersByTime(1000));
  expect(revoke).toHaveBeenCalledWith("blob:concentration-result");
});

it.each([[20, "SSS"], [18, "SS"], [17, "S"], [14, "A"], [10, "B"], [7, "C"], [0, "D"]] as const)("shows the matching illustrated rank after %i complete answers", (answered, rank) => {
  const completed = { ...createGame(42, 1000), finishedAt: 126000 };
  questions(42).slice(0, answered).forEach((profile, index) => {
    completed.answers[index] = { name: translations(profile)[0]!, translation: translations(profile)[1] ?? "", japanese: profile.names.ja, english: profile.english ?? "", cv: profile.cv[0]!, birthday: `${profile.birthday.month}/${profile.birthday.day}`, height: String(profile.height) };
  });
  render(<ConcentrationResults game={completed} playerName="周末玩家" onAgain={vi.fn()} disabled={false} />);
  const badge = screen.getByRole("img", { name: `评级 ${rank}` });
  expect(badge.querySelector("img")?.getAttribute("src")).toBe(`/assets/ui/ranks/${rank.toLowerCase()}.webp`);
  fireEvent.error(badge.querySelector("img")!);
  expect(badge.querySelector("img")).toBeNull();
  expect(badge.textContent).toBe(rank);
  expect(screen.getAllByRole("meter")).toHaveLength(3);
});

it("ends the presentation when hidden, never replays on return and cleans up on unmount", () => {
  vi.useFakeTimers();
  const schedule = vi.spyOn(window, "setTimeout"), cancel = vi.spyOn(window, "clearTimeout");
  const view = show(true);
  const timer = schedule.mock.results[schedule.mock.calls.findIndex(call => call[1] === 800)]!.value;
  visibility(true);
  expect(sheet().getAttribute("data-reveal")).toBe("false");
  expect(cancel).toHaveBeenCalledWith(timer);
  visibility(false);
  expect(sheet().getAttribute("data-reveal")).toBe("false");
  view.unmount();
  schedule.mockClear(); cancel.mockClear();
  const fresh = show(true);
  const freshTimer = schedule.mock.results[schedule.mock.calls.findIndex(call => call[1] === 800)]!.value;
  fresh.unmount(); expect(cancel).toHaveBeenCalledWith(freshTimer);
});

it("does not restart a running reveal on language changes", async () => {
  vi.useFakeTimers(); show(true);
  act(() => vi.advanceTimersByTime(300));
  await act(async () => { await changeLanguage("ja"); });
  expect(sheet().getAttribute("data-reveal")).toBe("true");
  act(() => vi.advanceTimersByTime(500));
  expect(sheet().getAttribute("data-reveal")).toBe("false");
});

it("shows the final state for system and explicit reduced motion, including changes mid-reveal", () => {
  vi.useFakeTimers();
  osReduced = true; let view = show(true);
  expect(sheet().getAttribute("data-reveal")).toBe("false"); view.unmount();
  osReduced = false; view = show(true); motionSettings();
  expect(sheet().getAttribute("data-reveal")).toBe("true");
  fireEvent.click(screen.getByRole("button", { name: "reduce" }));
  expect(sheet().getAttribute("data-reveal")).toBe("false");
  fireEvent.click(screen.getByRole("button", { name: "full" }));
  expect(sheet().getAttribute("data-reveal")).toBe("false");
  view.unmount();
  localStorage.setItem("umaguess:motion-preference", "reduced");
  act(() => window.dispatchEvent(new StorageEvent("storage", { key: "umaguess:motion-preference" })));
  show(true);
  expect(sheet().getAttribute("data-reveal")).toBe("false");
});
