import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ShareResultButton } from "@/components/uma/share-result-button";
import { changeLanguage, LANGUAGES } from ".";
import { sharingCopy } from "./sharing";

afterEach(async () => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  await changeLanguage("zh-Hans");
});

it("三语分享文案键齐全", () => {
  for (const lang of LANGUAGES) {
    expect(Object.keys(sharingCopy[lang]).sort()).toEqual(Object.keys(sharingCopy["zh-Hans"]).sort());
    for (const value of Object.values(sharingCopy[lang])) expect(value.trim()).toBeTruthy();
  }
});

it("复制过程中切语不再复制，原文及成功状态保持，反馈随语境切换", async () => {
  let finish!: () => void;
  const writeText = vi.fn(() => new Promise<void>(resolve => { finish = resolve; }));
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
  const original = "原昵称／トレーナー\n✓ ≈ ×\n3 / 8";
  render(<ShareResultButton text={original} />);
  fireEvent.click(screen.getByRole("button", { name: sharingCopy["zh-Hans"].copy }));
  await act(async () => { await changeLanguage("ja"); });
  expect(writeText).toHaveBeenCalledExactlyOnceWith(original);
  await act(async () => { finish(); });
  expect(screen.getByRole("button", { name: sharingCopy.ja.copied })).toBeTruthy();
  expect(screen.getByRole("status").textContent).toBe(sharingCopy.ja.success);
  await act(async () => { await changeLanguage("zh-Hant"); });
  expect(screen.getByRole("status").textContent).toBe(sharingCopy["zh-Hant"].success);
  expect(writeText).toHaveBeenCalledOnce();
});

it("复制失败实际提供可选原文，三秒后仍可手动复制，切语不暴露异常", async () => {
  const timer = vi.spyOn(globalThis, "setTimeout");
  const writeText = vi.fn().mockRejectedValue(new Error("private clipboard error"));
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
  const original = "原昵称\n已公开的分享内容";
  render(<ShareResultButton text={original} />);
  fireEvent.click(screen.getByRole("button", { name: sharingCopy["zh-Hans"].copy }));
  await screen.findByRole("textbox", { name: sharingCopy["zh-Hans"].text });
  const expire = timer.mock.calls.find(call => call[1] === 3000)?.[0] as () => void;
  expect(typeof expire).toBe("function");
  act(() => { expire(); });
  await act(async () => { await changeLanguage("ja"); });
  const field = screen.getByRole("textbox", { name: sharingCopy.ja.text }) as HTMLTextAreaElement;
  expect(field.value).toBe(original);
  expect(field.readOnly).toBe(true);
  fireEvent.focus(field);
  expect([field.selectionStart, field.selectionEnd]).toEqual([0, original.length]);
  expect(screen.getByRole("status").textContent).toBe(sharingCopy.ja.failed);
  expect(document.body.textContent).not.toContain("private clipboard error");
  expect(writeText).toHaveBeenCalledOnce();
});

it("同页两个分享按钮的读屏反馈互不关联，显式按钮名保持", async () => {
  vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText: vi.fn().mockRejectedValue(new Error("denied")) } });
  render(<><ShareResultButton text="第一份" label="分享一" /><ShareResultButton text="第二份" label="分享二" /></>);
  fireEvent.click(screen.getByRole("button", { name: "分享一" }));
  fireEvent.click(screen.getByRole("button", { name: "分享二" }));
  await waitFor(() => expect(screen.getAllByRole("textbox")).toHaveLength(2));
  const buttons = screen.getAllByRole("button");
  expect(buttons[0]?.getAttribute("aria-describedby")).not.toBe(buttons[1]?.getAttribute("aria-describedby"));
  for (const button of buttons) expect(document.getElementById(button.getAttribute("aria-describedby")!)?.textContent).toBe(sharingCopy["zh-Hans"].failed);
});
