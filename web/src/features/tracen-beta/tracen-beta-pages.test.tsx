import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage } from "@/i18n";
import { chooseSelect } from "@/test/select";
import { AccountPanel, ChallengePanel, TrainerLeaderboard } from "./tracen-beta-pages";
import { accountKey } from "@/features/account/account-session";
import { saveTrainerProfile } from "@/lib/trainer-profile";

const handlers = { onConnect: vi.fn(), onStart: vi.fn(), onBoard: vi.fn() };
const periods = [{ value: "current", label: "本期" }, { value: "previous", label: "上期" }];
const entries = [
  { publicPlayerId: "328491", displayName: "春日", avatarId: "1001", rank: 1, score: 100 },
  { publicPlayerId: "940152", displayName: "星见", avatarId: "1003", rank: 2, score: 73 },
  { publicPlayerId: "251630", displayName: "青叶", avatarId: "1013", rank: 2, score: 73 },
];
afterEach(async () => { cleanup(); vi.clearAllMocks(); await changeLanguage("zh-Hans"); });

it("uses the supplied round rules, progress and score", () => {
  const { rerender } = render(<ChallengePanel state="intro" maxWrong={6} minutes={12} authenticated {...handlers} />);
  expect(screen.getByText("最多猜错 6 次")).toBeTruthy();
  expect(screen.getByText("每场 12 分钟")).toBeTruthy();
  rerender(<ChallengePanel state="active" remainingWrong={3} remainingTime="02:17" authenticated {...handlers}><p>作答区域</p></ChallengePanel>);
  expect(screen.getByText("还可猜错 3 次")).toBeTruthy();
  expect(screen.getByText("02:17")).toBeTruthy();
  expect(screen.getByText("作答区域")).toBeTruthy();
  rerender(<ChallengePanel state="result" score={0} authenticated {...handlers} />);
  expect(screen.getByText("0 分")).toBeTruthy();
  expect(screen.queryByText("作答区域")).toBeNull();
  expect(document.body.textContent).not.toMatch(/84|19:42|示例|预览/);
});
it("requests login without granting access locally", () => {
  render(<ChallengePanel state="intro" maxWrong={8} minutes={20} authenticated={false} {...handlers} />);
  screen.getByRole("button", { name: "登录账户" }).click();
  expect(handlers.onConnect).toHaveBeenCalledOnce();
  expect(handlers.onStart).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "开始挑战" })).toBeNull();
});
it("keeps supplied rankings visible until the selected period returns new data", async () => {
  const changePeriod = vi.fn();
  const props = { state: "entries" as const, entries, period: "current", periods, onPeriodChange: changePeriod, onRetry: vi.fn() };
  const { rerender } = render(<TrainerLeaderboard {...props} />);
  const table = screen.getByRole("table", { name: "训练员挑战榜" });
  expect([...table.querySelectorAll('tbody tr td:first-child')].map(e => e.textContent)).toEqual(["1", "2", "2"]);
  expect(screen.queryByText("我的挑战记录")).toBeNull();
  await chooseSelect(screen.getByRole("combobox", { name: "榜单范围" }), "previous");
  expect(changePeriod).toHaveBeenCalledWith("previous");
  expect(table.querySelectorAll('tbody tr')).toHaveLength(3);
  rerender(<TrainerLeaderboard {...props} period="previous" entries={entries.slice(1)} personalRecord={{ rank: 2, score: 73 }} />);
  expect(screen.getByRole('table', { name: '训练员挑战榜' }).querySelectorAll('tbody tr')).toHaveLength(2);
  expect(screen.getByText("第 2 名")).toBeTruthy();
  rerender(<TrainerLeaderboard {...props} state="unavailable" personalRecord={undefined} />);
  expect(screen.getByRole("status").textContent).toContain("挑战榜加载失败");
  expect(screen.queryByRole("table")).toBeNull();
  rerender(<TrainerLeaderboard {...props} entries={[]} personalRecord={null} />);
  expect(screen.getAllByText("暂无上榜成绩")).toHaveLength(2);
});
it("reads the shared account session and global trainer profile", async () => {
  saveTrainerProfile({ displayName: "喜欢麦昆的训练员", avatarId: "1013" });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(accountKey, { authenticated: false });
  render(<QueryClientProvider client={client}><MemoryRouter><AccountPanel config={{ enabled: true, requireLogin: {}, providers: [{ id: "google", available: true }] }} /></MemoryRouter></QueryClientProvider>);
  expect(screen.getByText("喜欢麦昆的训练员")).toBeTruthy();
  expect(screen.getByText("未登录")).toBeTruthy();
  expect(screen.getByRole("button", { name: "使用 Google 登录" })).toBeTruthy();
  expect(screen.queryByRole("textbox")).toBeNull();
  await act(async () => { client.setQueryData(accountKey, { authenticated: true, csrfToken: "in-memory" }); });
  await waitFor(() => expect(screen.getByText("已登录")).toBeTruthy());
  expect(screen.queryByRole("button", { name: "使用 Google 登录" })).toBeNull();
});
it.each(["zh-Hans", "zh-Hant", "ja"] as const)("keeps the supplied result when switching to %s", async language => {
  await changeLanguage(language);
  render(<ChallengePanel state="result" score={73} authenticated {...handlers} />);
  expect(screen.getByText(language === "ja" ? "73 点" : "73 分")).toBeTruthy();
  expect(document.body.textContent).not.toMatch(/示例|預覽|プレビュー|サンプル|OAuth|OIDC|UUID/);
});
