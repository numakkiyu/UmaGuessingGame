import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import ErrorPresentation from "./error-presentation";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { changeLanguage } from "@/i18n";
import { dismissFault } from "@/test/dismiss-fault";

afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });
const fault = (key: string, action?: React.ReactNode) => <ErrorPresentation noticeKey={key} fallback={<div>页面提示</div>} title="连接中断" tone="danger" action={action}><p>请稍后再试</p></ErrorPresentation>;

it("keeps only one dialog open and returns focus to the invoking control", async () => {
  const host = document.createElement("button");document.body.append(host);host.focus();
  render(fault("focus-one"));
  expect(await screen.findAllByRole("alertdialog")).toHaveLength(1);
  await dismissFault();
  await waitFor(() => expect(document.activeElement).toBe(host));
  expect(screen.queryByRole("alertdialog")).toBeNull();host.remove();
});
it("leaves no duplicate banner after dismissal and still reports a later failed attempt", async () => {
  const first=render(fault("dismissed-remount"));await dismissFault();
  expect(screen.queryByText("页面提示")).toBeNull();
  expect(screen.queryByRole("button",{name:"查看提示"})).toBeNull();
  expect(screen.queryByText("连接中断")).toBeNull();
  first.unmount();render(fault("dismissed-remount"));
  expect(await screen.findByRole("alertdialog")).toBeTruthy();await dismissFault();
  expect(screen.queryByText("页面提示")).toBeNull();
});
it("coalesces simultaneous copies of the same failure",async()=>{
  render(<>{fault("same-occurrence")}{fault("same-occurrence")}</>);
  expect(await screen.findAllByRole("alertdialog")).toHaveLength(1);
  await dismissFault();expect(screen.queryByRole("alertdialog")).toBeNull();
  expect(screen.queryByText("页面提示")).toBeNull();
});
it("keeps the recovery callback usable and prevents repeated clicks during its lock", async () => {
  const retry=vi.fn();render(fault("retry-lock",<Button onClick={retry}>再试一次</Button>));
  const dialog=await screen.findByRole("alertdialog");const button=within(dialog).getByRole("button",{name:"再试一次"});
  fireEvent.click(button);fireEvent.click(button);expect(retry).toHaveBeenCalledTimes(1);
  await waitFor(()=>expect(button.closest("fieldset")!.disabled).toBe(false),{timeout:1500});fireEvent.click(button);expect(retry).toHaveBeenCalledTimes(2);
});
it("keeps recovery text when a decorative character image fails", async () => {
  render(fault("broken-decoration"));const dialog=await screen.findByRole("alertdialog");
  fireEvent.error(dialog.querySelector("img")!);expect(dialog.querySelector("img")).toBeNull();
  expect(within(dialog).getByText("请稍后再试")).toBeTruthy();expect(within(dialog).getByRole("button",{name:"返回当前页面"})).toBeTruthy();
});
it("localizes an open notice and preserves dismissal across a language change", async () => {
  render(fault("locale-open"));await screen.findByRole("alertdialog");
  await act(()=>changeLanguage("ja"));expect(screen.getByText("うまくいかなかったようです")).toBeTruthy();
  await dismissFault();await act(()=>changeLanguage("zh-Hant"));expect(screen.queryByRole("alertdialog")).toBeNull();expect(screen.queryByRole("button",{name:"查看提醒"})).toBeNull();
});
it("dismisses with Escape or a click on the surrounding space", async () => {
  const page=render(fault("escape-and-space"));const dialog=await screen.findByRole("alertdialog");
  fireEvent.keyDown(dialog,{key:"Escape"});await waitFor(()=>expect(screen.queryByRole("alertdialog")).toBeNull());
  page.unmount();render(fault("space-only"));fireEvent.pointerDown(await screen.findByRole("alertdialog"));
  await waitFor(()=>expect(screen.queryByRole("alertdialog")).toBeNull());
});
it("queues distinct failures without showing inline copies or concurrent dialogs",async()=>{
  render(<>{fault("queue-first")}<ErrorPresentation noticeKey="queue-first" fallback={<div>第二条页面提示</div>} title="连接中断" tone="danger">请重新加载图片</ErrorPresentation></>);
  const first=await screen.findByRole("alertdialog");
  expect(within(first).getByText("连接中断")).toBeTruthy();
  expect(screen.queryByText("页面提示")).toBeNull();expect(screen.queryByText("第二条页面提示")).toBeNull();
  fireEvent.click(within(first).getByRole("button",{name:"关闭"}));
  await waitFor(()=>expect(screen.getAllByRole("alertdialog")).toHaveLength(1));
  expect(within(screen.getByRole("alertdialog")).getByText("请重新加载图片")).toBeTruthy();
  await dismissFault();expect(screen.queryByRole("alertdialog")).toBeNull();
});
it("keeps ordinary warnings inline and avoids consecutive character pairs", async () => {
  const warning=render(<Alert tone="warning" title="先记住这些卡牌">八秒后盖牌</Alert>);
  expect(screen.queryByRole("alertdialog")).toBeNull();expect(screen.getByRole("alert")).toBeTruthy();warning.unmount();
  const first=render(fault("pair-first"));const pair=(await screen.findByRole("alertdialog")).querySelector("[data-pair]")!.getAttribute("data-pair");first.unmount();
  render(fault("pair-second"));expect((await screen.findByRole("alertdialog")).querySelector("[data-pair]")!.getAttribute("data-pair")).not.toBe(pair);
});
