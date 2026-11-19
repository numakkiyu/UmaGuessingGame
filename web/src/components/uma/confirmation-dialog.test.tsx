import { StrictMode, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useNavigate } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import i18n from "@/i18n";
import { answerConfirmation, cancelConfirmation, confirmationStore, requestConfirmation, useConfirmation } from "@/lib/confirmation";
import { ConfirmationDialog } from "./confirmation-dialog";

const message = { ns: "common", key: "footer.copyright" };
afterEach(async () => { act(() => cancelConfirmation()); cleanup(); await i18n.changeLanguage("zh-Hans"); });

function Playground({ onCommit, scope = "1" }: {onCommit: () => void; scope?: string}) {
  const ask = useConfirmation(scope);
  const navigate = useNavigate();
  const [visible, setVisible] = useState(true);
  return <><main id="main-content" tabIndex={-1}>
    {visible ? <button onClick={() => void (async () => { if (await ask(message, "finish")) { onCommit(); setVisible(false); } })()}>结束练习</button> : null}
    <button onClick={() => navigate("/other")}>换页</button>
  </main><ConfirmationDialog/></>;
}
function mount(onCommit = vi.fn(), scope = "1") {
  return { onCommit, ...render(<StrictMode><MemoryRouter><Playground onCommit={onCommit} scope={scope}/></MemoryRouter></StrictMode>) };
}

describe("themed confirmation", () => {
  it("focuses cancel, traps Tab and restores focus after cancelling", async () => {
    const user = userEvent.setup(), {onCommit} = mount();
    const trigger = screen.getByRole("button", {name:"结束练习"});
    await user.click(trigger);
    expect(screen.getByRole("alertdialog")).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByRole("button", {name:"取消"}));
    for(let n=0;n<5;n++){ await user.tab(); expect(screen.getByRole("alertdialog").contains(document.activeElement)).toBe(true); }
    await user.click(screen.getByRole("button",{name:"取消"}));
    await waitFor(() => expect(document.activeElement).toBe(trigger));
    expect(onCommit).not.toHaveBeenCalled();
  });
  it.each(["escape", "close"])("%s cancels without committing", async method => {
    const user = userEvent.setup(), {onCommit} = mount();
    await user.click(screen.getByRole("button",{name:"结束练习"}));
    if(method === "escape") await user.keyboard("{Escape}"); else await user.click(screen.getByRole("button",{name:"关闭"}));
    expect(screen.queryByRole("alertdialog")).toBeNull(); expect(onCommit).not.toHaveBeenCalled();
  });
  it("confirms only once and falls back to main after the trigger disappears", async () => {
    const user = userEvent.setup(), {onCommit} = mount();
    await user.click(screen.getByRole("button",{name:"结束练习"}));
    const id = confirmationStore.getSnapshot()!.id;
    act(() => { answerConfirmation(id,true); answerConfirmation(id,true); });
    await waitFor(() => expect(onCommit).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(document.activeElement).toBe(document.getElementById("main-content")));
  });
  it("route navigation cancels the pending action", async () => {
    const user=userEvent.setup(),{onCommit}=mount();
    await user.click(screen.getByRole("button",{name:"结束练习"}));
    fireEvent.click(screen.getByText("换页"));
    await waitFor(()=>expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(onCommit).not.toHaveBeenCalled();
  });
  it("changed revision or deadline cancels a pending action", async () => {
    const onCommit=vi.fn(),user=userEvent.setup(),view=mount(onCommit);
    await user.click(screen.getByRole("button",{name:"结束练习"}));
    const id=confirmationStore.getSnapshot()!.id;
    view.rerender(<StrictMode><MemoryRouter><Playground onCommit={onCommit} scope="2"/></MemoryRouter></StrictMode>);
    act(()=>answerConfirmation(id,true));
    await waitFor(()=>expect(screen.queryByRole("alertdialog")).toBeNull());expect(onCommit).not.toHaveBeenCalled();
  });
  it("owner unmount cancels module and page work", async () => {
    const user=userEvent.setup(),{onCommit,unmount}=mount();
    await user.click(screen.getByRole("button",{name:"结束练习"}));unmount();
    await act(async()=>{});expect(confirmationStore.getSnapshot()).toBeNull();expect(onCommit).not.toHaveBeenCalled();
  });
  it("open copy follows locale changes without authorizing the operation", async () => {
    const user=userEvent.setup(),{onCommit}=mount();
    await user.click(screen.getByRole("button",{name:"结束练习"}));
    await act(()=>i18n.changeLanguage("zh-Hant")); expect(screen.getByRole("button",{name:"結束並查看結果"})).toBeTruthy();
    await act(()=>i18n.changeLanguage("ja")); expect(screen.getByRole("button",{name:"終了して結果を見る"})).toBeTruthy();
    expect(screen.getByRole("alertdialog").textContent).toContain("素材の著作権");expect(onCommit).not.toHaveBeenCalled();
  });
});

describe("confirmation ownership", () => {
  it("aborted audio preparation does not authorize a download", async () => {
    const controller=new AbortController(),download=vi.fn();
    const pending=requestConfirmation(message,"prepare",controller.signal).then(accepted=>{if(accepted)download();});
    controller.abort();await pending;expect(download).not.toHaveBeenCalled();expect(confirmationStore.getSnapshot()).toBeNull();
    expect(await requestConfirmation(message,"prepare",controller.signal)).toBe(false);
  });
  it("rejects overlapping decisions without closing the original", async () => {
    const controller=new AbortController(),first=requestConfirmation(message,"remove",controller.signal);
    const id=confirmationStore.getSnapshot()!.id;
    expect(await requestConfirmation(message,"finish",controller.signal)).toBe(false);
    expect(confirmationStore.getSnapshot()!.id).toBe(id);
    answerConfirmation(id,false);expect(await first).toBe(false);
  });
});
