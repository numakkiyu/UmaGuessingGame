import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import { changeLanguage, LANGUAGES } from "@/i18n";
import { concentrationCopy } from "@/i18n/concentration";
import { ConfirmationDialog } from "@/components/uma/confirmation-dialog";
import { saveTrainerProfile } from "@/lib/trainer-profile";
import { ConcentrationPage } from "./concentration-page";
import { ConcentrationModeCard } from "./concentration-mode-card";
import { STORAGE_KEY, createGame, decodeGame, questions } from "./engine";

vi.mock("@/lib/api", () => ({ useApi: () => ({ session: null }) }));
function show() { return render(<MemoryRouter><ConcentrationPage /><ConfirmationDialog /></MemoryRouter>); }
function read() { return decodeGame(localStorage.getItem(STORAGE_KEY)! )!; }
beforeEach(async () => {
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  await changeLanguage("zh-Hans");
});
afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });

it("starts a random game with the shared nickname and exposes its single-player entry", async () => {
  saveTrainerProfile({ displayName: "周末玩家", avatarId: "1062" });
  const view = show();
  expect(screen.getByText("周末玩家")).toBeTruthy();
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(document.body.textContent).not.toContain("题组编号");
  fireEvent.click(screen.getByRole("button", { name: "开始测试" }));
  await screen.findByLabelText("角色名");
  expect(read().seed).toBeGreaterThanOrEqual(0);
  expect(read().seed).toBeLessThanOrEqual(4294967295);
  expect(screen.getByRole("img").getAttribute("src")).toMatch(/^\/assets\/ui\/standing\/[a-f0-9]+\.webp$/);
  view.unmount();
  render(<MemoryRouter><ConcentrationModeCard /></MemoryRouter>);
  await act(async () => { await import("@/components/uma/menu-character-art"); });
  expect(screen.getByRole("link").getAttribute("href")).toBe("/play/concentration");
});

it("keeps all 20 answers through navigation, language changes and a remount, then immediately scores on submission", async () => {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(createGame(42, Date.now())));
  let view = show();
  const drawn = questions(42);
  for (let index = 0; index < 20; index++) {
    fireEvent.change(screen.getByLabelText(/^角色名/), { target: { value: drawn[index]!.chineseNames[0] } });
    if (index < 19) fireEvent.click(screen.getByRole("button", { name: "下一题" }));
  }
  expect(read().answers.every(a => a.name)).toBe(true);
  for (const language of LANGUAGES) {
    await act(async () => { await changeLanguage(language); });
    expect(screen.getByRole("heading", { name: concentrationCopy[language].title })).toBeTruthy();
    expect((screen.getByLabelText(new RegExp(`^${concentrationCopy[language].fields.name}`)) as HTMLInputElement).value).toBe(drawn[19]!.chineseNames[0]);
    expect(read().seed).toBe(42);
    expect(screen.queryByText(drawn[19]!.english!)).toBeNull();
  }
  view.unmount(); await changeLanguage("zh-Hans"); view = show();
  expect(read().activeIndex).toBe(19);
  expect(screen.getByRole("img").getAttribute("alt")).not.toContain(drawn[19]!.names["zh-Hans"]);
  fireEvent.click(screen.getByRole("button", { name: "交卷看结果" }));
  const dialog = await screen.findByRole("alertdialog");
  expect(dialog.textContent).not.toContain("编队");
  fireEvent.click(within(dialog).getByRole("button", { name: "交卷看结果" }));
  await screen.findByRole("heading", { name: "你的浓度测试结果" });
  expect(read().finishedAt).not.toBeNull();
  expect(document.querySelector(".uma-concentration-result")?.getAttribute("data-reveal")).toBe("true");
  expect(document.querySelector(".uma-concentration-metrics")?.textContent).toContain("100%");
  view.unmount(); show();
  expect(screen.getByRole("heading", { name: "你的浓度测试结果" })).toBeTruthy();
  expect(document.querySelector(".uma-concentration-result")?.getAttribute("data-reveal")).toBe("false");
});

it("cancels submission without losing input, updates pending dialog language, and requires confirmation before replay", async () => {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(createGame(1, Date.now())));
  show();
  fireEvent.change(screen.getByLabelText(/^角色名/), { target: { value: "我的作答" } });
  for (let i = 0; i < 19; i++) fireEvent.click(screen.getByRole("button", { name: "下一题" }));
  fireEvent.click(screen.getByRole("button", { name: "交卷看结果" }));
  await screen.findByRole("alertdialog");
  await act(async () => { await changeLanguage("ja"); });
  expect(screen.getByRole("alertdialog").textContent).toContain("19 問が空欄");
  fireEvent.click(screen.getByRole("button", { name: "キャンセル" }));
  await act(async () => { await changeLanguage("zh-Hans"); });
  expect(read().answers[0]!.name).toBe("我的作答");
  expect(read().finishedAt).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "交卷看结果" }));
  fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "交卷看结果" }));
  await screen.findByRole("heading", { name: "你的浓度测试结果" });
  fireEvent.click(screen.getByRole("button", { name: "再测一次" }));
  fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "开始测试" }));
  await screen.findByLabelText("角色名");
  expect(read().finishedAt).toBeNull();
  expect(read().activeIndex).toBe(0);
  expect(read().answers.every(a => !a.name)).toBe(true);
});

it("preserves unreadable stored data until a new game is explicitly started", () => {
  localStorage.setItem(STORAGE_KEY, "broken"); show();
  expect(screen.getByRole("alert").textContent).toContain("上次的记录无法读取");
  expect(localStorage.getItem(STORAGE_KEY)).toBe("broken");
});

it("keeps month and day edits and previous answers when moving between characters", () => {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(createGame(42, Date.now())));
  show();
  fireEvent.change(screen.getByLabelText("角色名"), { target: { value: "我的作答" } });
  fireEvent.change(screen.getByLabelText("生日日期"), { target: { value: "02" } });
  expect(read().answers[0]!.birthday).toBe("/02");
  expect(screen.getByRole("progressbar").getAttribute("value")).toBe("1");
  fireEvent.change(screen.getByLabelText("生日月份"), { target: { value: "5" } });
  fireEvent.click(screen.getByRole("button", { name: "下一题" }));
  expect(screen.getByRole("progressbar").getAttribute("value")).toBe("1");
  expect((screen.getByLabelText("生日月份") as HTMLInputElement).value).toBe("");
  fireEvent.click(screen.getByRole("button", { name: "上一题" }));
  expect((screen.getByLabelText("角色名") as HTMLInputElement).value).toBe("我的作答");
  expect((screen.getByLabelText("生日月份") as HTMLInputElement).value).toBe("5");
  expect((screen.getByLabelText("生日日期") as HTMLInputElement).value).toBe("02");
  fireEvent.change(screen.getByLabelText("生日月份"), { target: { value: "" } });
  expect(read().answers[0]!.birthday).toBe("/02");
});

it("accepts fullwidth month and day digits and rejects non-numeric birthday edits", () => {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(createGame(42, Date.now())));
  show();
  fireEvent.change(screen.getByLabelText("生日月份"), { target: { value: "０５" } });
  fireEvent.change(screen.getByLabelText("生日日期"), { target: { value: "０２" } });
  expect(read().answers[0]!.birthday).toBe("05/02");
  fireEvent.change(screen.getByLabelText("生日月份"), { target: { value: "五月" } });
  expect(read().answers[0]!.birthday).toBe("05/02");
});

it("keeps IME confirmation Enter from submitting while allowing a subsequent normal submit", () => {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(createGame(42, Date.now())));
  show();
  const input = screen.getByLabelText("角色名");
  fireEvent.change(input, { target: { value: "特别周" } });
  expect(fireEvent.keyDown(input, { key: "Enter", isComposing: true })).toBe(false);
  expect(fireEvent.keyDown(input, { key: "Enter", keyCode: 229 })).toBe(false);
  expect(read().activeIndex).toBe(0);
  expect(read().answers[0]!.name).toBe("特别周");
  expect(fireEvent.keyDown(input, { key: "Enter", isComposing: false, keyCode: 13 })).toBe(true);
  fireEvent.submit(input.closest("form")!);
  expect(read().activeIndex).toBe(1);
  expect(read().answers[0]!.name).toBe("特别周");
});

it("keeps playing when storage fails and does not claim the answers were saved", () => {
  const write = vi.spyOn(localStorage, "setItem").mockImplementation(() => { throw new Error("quota"); });
  try {
    show();
    fireEvent.click(screen.getByRole("button", { name: "开始测试" }));
    expect(screen.getByRole("alert").textContent).toContain("作答暂时无法保存");
    expect(screen.queryByText("作答自动保存")).toBeNull();
    fireEvent.change(screen.getByLabelText("角色名"), { target: { value: "我的作答" } });
    fireEvent.click(screen.getByRole("button", { name: "下一题" }));
    fireEvent.click(screen.getByRole("button", { name: "上一题" }));
    expect((screen.getByLabelText("角色名") as HTMLInputElement).value).toBe("我的作答");
  } finally { write.mockRestore(); }
});
