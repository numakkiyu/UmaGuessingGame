import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router";
import { changeLanguage, LANGUAGES } from ".";
import { practiceFlow } from "./practice-flow";
import { ActivityPracticeLoader, ActivityPracticeStart, PracticeGameHeading, PracticeNotices, PracticeUtilityActions, useActivityPractice, type PracticeAdapter } from "@/features/practice/activity-practice-kit";
import type { PracticeBase } from "@/lib/domain/aptitude-practice";
import { PracticeConflict } from "@/lib/local/practice-store";

const language = (lang: "zh-Hans" | "zh-Hant" | "ja") => act(async () => { await changeLanguage(lang); });
afterEach(async () => { cleanup(); vi.restoreAllMocks(); await changeLanguage("zh-Hans"); });
const pack = { version: "original" };
const game: PracticeBase = { schemaVersion: 1, modeId: "test", gameId: "original-game", packKey: "original-pack", rulesVersion: "original-rules", createdAt: 1000, updatedAt: 1000, deadlineAt: Date.now() + 180000, ended: false, revision: 0 };
function fixture(): PracticeAdapter<typeof pack, PracticeBase> {
  return {
    modeId: "test", title: "遊戲名稱", summary: "說明", hint: "提示", prepareLabel: "prepare", startLabel: "start", filePrefix: "test", rules: ["原有規則"],
    fetch: vi.fn().mockResolvedValue({ data: pack }), read: () => ({ pack, media: [] }), validate: value => value as { schemaVersion: 1; pack: typeof pack; game: PracticeBase }, create: () => game,
    prepare: vi.fn().mockResolvedValue(true), volatile: new Map(),
    store: { latestPack: vi.fn().mockResolvedValue(null), list: vi.fn().mockResolvedValue([]), load: vi.fn().mockResolvedValue(null), install: vi.fn().mockResolvedValue(undefined), save: vi.fn().mockResolvedValue(undefined), remove: vi.fn().mockResolvedValue(undefined) },
  };
}
const mount = (content: React.ReactNode) => render(<MemoryRouter>{content}</MemoryRouter>);

describe("shared practice flow localization", () => {
  it("keeps all keys and interpolation values aligned", () => {
    const source = practiceFlow["zh-Hans"];
    for (const lang of LANGUAGES) {
      expect(Object.keys(practiceFlow[lang]).sort()).toEqual(Object.keys(source).sort());
      for (const key of Object.keys(source) as (keyof typeof source)[]) {
        expect(practiceFlow[lang][key].trim()).not.toBe("");
        const params = (value: string) => [...value.matchAll(/{{(\w+)}}/g)].map(m => m[1]).sort();
        expect(params(practiceFlow[lang][key])).toEqual(params(source[key]));
      }
    }
  });

  it("changes active preparation progress without restarting or aborting it", async () => {
    const adapter = fixture();
    let release!: () => void;
    let signal!: AbortSignal;
    adapter.prepare = vi.fn((_pack, currentSignal, progress) => { signal = currentSignal; progress(2, 7); return new Promise<boolean>(resolve => { release = () => resolve(true); }); });
    mount(<ActivityPracticeStart adapter={adapter} />);
    fireEvent.click(await screen.findByRole("button", { name: "prepare" }));
    await screen.findByText("正在准备音频 2 / 7");
    await language("ja");
    expect(screen.getByText("音声を準備中 2 / 7")).toBeTruthy();
    expect(signal.aborted).toBe(false);
    expect(adapter.fetch).toHaveBeenCalledTimes(1);
    expect(adapter.store.latestPack).toHaveBeenCalledTimes(1);
    expect(adapter.prepare).toHaveBeenCalledTimes(1);
    await act(async () => release());
    await screen.findByText(practiceFlow.ja.ready);
    expect(adapter.store.install).toHaveBeenCalledWith(pack);
    await language("zh-Hant");
    expect(screen.getByText(practiceFlow["zh-Hant"].ready)).toBeTruthy();
  });

  it("re-localizes a load error in place without loading another record", async () => {
    const adapter = fixture();
    render(<MemoryRouter initialEntries={["/practice/test/missing"]}><Routes><Route path="/practice/test/:gameId" element={<ActivityPracticeLoader adapter={adapter}>{() => <p>unexpected</p>}</ActivityPracticeLoader>} /></Routes></MemoryRouter>);
    await screen.findByText("还没找到这局挑战");
    await language("ja");
    expect(screen.getByRole("alert").textContent).toContain("チャレンジ記録が見つかりません");
    expect(screen.getByRole("link", { name: practiceFlow.ja.back }).getAttribute("href")).toBe("/play/test");
    expect(adapter.store.load).toHaveBeenCalledTimes(1);
  });

  it("retains confirmed progress, deadline, conflict and the timer on a language change", async () => {
    const adapter = fixture(), interval = vi.spyOn(window, "setInterval");
    function Playing() {
      const p = useActivityPractice(adapter, { schemaVersion: 1, game, pack });
      return <><PracticeGameHeading title="玩家原名" modeId="test" now={game.deadlineAt - 90000} deadlineAt={p.game.deadlineAt} finished={p.game.ended} /><pre>{JSON.stringify(p.game)}</pre><PracticeNotices saved={p.saved} stale={p.stale} message={p.message} /><button onClick={() => void p.act((g, at) => ({ ...g, revision: g.revision + 1, updatedAt: at }))}>act</button><PracticeUtilityActions {...p} finished={p.game.ended} /></>;
    }
    mount(<Playing />);
    fireEvent.click(screen.getByRole("button", { name: "act" }));
    await waitFor(() => expect(adapter.store.save).toHaveBeenCalledTimes(1));
    const before = screen.getByText(/"gameId":"original-game"/).textContent;
    await language("ja");
    expect(screen.getByText("残り 1:30")).toBeTruthy();
    expect(screen.getByText(/"gameId":"original-game"/).textContent).toBe(before);
    expect(screen.getByRole("heading", { name: "玩家原名" })).toBeTruthy();
    expect(interval.mock.calls.filter(call => call[1] === 250)).toHaveLength(1);
    vi.mocked(adapter.store.save).mockRejectedValueOnce(new PracticeConflict());
    fireEvent.click(screen.getByRole("button", { name: "act" }));
    await screen.findByText(practiceFlow.ja.stale);
    await language("zh-Hant");
    expect(screen.getByText(practiceFlow["zh-Hant"].stale)).toBeTruthy();
    expect(screen.getByText(/"gameId":"original-game"/).textContent).toBe(before);
    expect(screen.getByRole("button", { name: practiceFlow["zh-Hant"].finish }).hasAttribute("disabled")).toBe(true);
    expect(interval.mock.calls.filter(call => call[1] === 250)).toHaveLength(1);
  });
});
