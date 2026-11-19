
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SearchInput } from "@/components/uma/search-input";
import { changeLanguage } from "@/i18n";
import type { SearchableCharacter } from "@/lib/search";

const CHARACTERS: SearchableCharacter[] = [
  { characterId: "1001", displayName: "特别周", aliases: ["スペシャルウィーク", "Special Week", "特周"] },
  { characterId: "1002", displayName: "无声铃鹿", localizedNames: {ja: "サイレンススズカ"}, aliases: ["サイレンススズカ", "Silence Suzuka"] },
  { characterId: "1003", displayName: "东海帝皇", aliases: ["トウカイテイオー", "Tokai Teio"] },
];

beforeEach(async () => { await changeLanguage("zh-Hans"); });

describe("SearchInput", () => {
  it("切换三语时按要求展示具体名称，保留搜索与原角色身份", async () => {
    const user = userEvent.setup(), onSelect = vi.fn();
    const character: SearchableCharacter = {
      characterId: "1001", displayName: "特别周", aliases: ["Special Week", "小特", "スペシャルドリーマー"],
      localizedNames: {"zh-Hans":"特别周", "zh-Hant":"特別週", ja:"スペシャルウィーク"},
      searchNames: {translations:["特别周", "特別週", "特殊周"], japanese:"スペシャルウィーク", english:"Special Week", nicknames:["小特", "萝卜特"]},
    };
    render(<SearchInput items={[character]} onSelect={onSelect}/>);
    const input=screen.getByRole("combobox");
    await user.type(input,"Special");
    let option=await screen.findByRole("option");
    const kinds=()=>Array.from(option.querySelectorAll("[data-name-kind]")).map(e=>e.getAttribute("data-name-kind"));
    expect(kinds()).toEqual(["otherNames","japanese","english","nicknames"]);
    expect(within(option).getByText("特別週、特殊周")).toBeTruthy();
    expect(option.querySelector("[data-search-names]")?.textContent).toBe("特別週、特殊周、スペシャルウィーク、Special Week、小特、萝卜特");
    expect(option.textContent).not.toMatch(/其他别名：|日文原名：|英文名：|外号：/);
    expect(option.textContent).not.toContain("スペシャルドリーマー");
    await act(async()=>{await changeLanguage("zh-Hant");});
    option=screen.getByRole("option");
    expect(kinds()).toEqual(["otherNames","japanese","english"]);
    expect(within(option).getByText("特別週")).toBeTruthy();
    expect(option.querySelector("[data-search-names]")?.textContent).toBe("特别周、特殊周、スペシャルウィーク、Special Week");
    expect(option.textContent).not.toMatch(/小特|萝卜特/);
    await act(async()=>{await changeLanguage("ja");});
    option=screen.getByRole("option");
    expect(kinds()).toEqual(["english"]);
    expect(option.textContent).toBe("スペシャルウィークSpecial Week");
    expect((input as HTMLInputElement).value).toBe("Special");
    expect(onSelect).not.toHaveBeenCalled();
    await user.click(option);
    expect(onSelect).toHaveBeenCalledWith(character);
  });

  it("旧资料不把未分类称号和外号误作译名，日语缺名时不展示中文", async () => {
    const user=userEvent.setup();
    await changeLanguage("ja");
    render(<SearchInput items={[{characterId:"legacy",displayName:"待确认中文名",aliases:["旧称号","legacy"]}]} onSelect={vi.fn()}/>);
    await user.type(screen.getByRole("combobox"),"legacy");
    const option=await screen.findByRole("option");
    expect(option.textContent).toBe("名前を確認中");
  });

  it("用中文名可以搜到角色并点击选择", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<SearchInput items={CHARACTERS} onSelect={onSelect} />);

    const input = screen.getByRole("combobox");
    await user.type(input, "特别");

    const option = await screen.findByRole("option", { name: /特别周/ });
    await user.click(option);

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect.mock.calls[0]?.[0]).toMatchObject({ characterId: "1001" });
  });

  it("按住候选超过 120 毫秒再松开仍能选择，鼠标焦点留在输入框", async () => {
    const user = userEvent.setup(), onSelect = vi.fn();
    render(<SearchInput items={CHARACTERS} onSelect={onSelect} />);
    const input = screen.getByRole("combobox");
    await user.type(input, "特别");
    const option = await screen.findByRole("option", { name: /特别周/ });
    await user.pointer({ target: option, keys: "[MouseLeft>]" });
    expect(document.activeElement).toBe(input);
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 160)); });
    expect(screen.getByRole("option", { name: /特别周/ })).toBe(option);
    await user.pointer({ target: option, keys: "[/MouseLeft]" });
    expect(onSelect).toHaveBeenCalledExactlyOnceWith(CHARACTERS[0]);
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("键盘移入候选不会提前收起，移出搜索区域才关闭", async () => {
    const user = userEvent.setup(), onSelect = vi.fn();
    render(<><SearchInput items={CHARACTERS} onSelect={onSelect} /><button>其他操作</button></>);
    await user.type(screen.getByRole("combobox"), "特别");
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("option", { name: /特别周/ }));
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 160)); });
    expect(screen.getByRole("listbox")).toBeTruthy();
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "其他操作" }));
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it.each(["loading", "failed", "dismissed"])("%s 状态下 Enter 不提交不可见的候选", async state => {
    const user = userEvent.setup(), onSelect = vi.fn(), onSubmit = vi.fn();
    render(<SearchInput items={CHARACTERS} onSelect={onSelect} onSubmit={onSubmit} submitLabel="猜一次"
      loading={state === "loading"} loadError={state === "failed" ? "角色资料暂时没能打开" : null} />);
    await user.type(screen.getByRole("combobox"), "特别");
    if (state === "dismissed") await user.keyboard("{Escape}");
    await user.keyboard("{Enter}");
    expect(onSelect).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("用日文名也能搜到，并展示日文原名", async () => {
    const user = userEvent.setup();
    render(<SearchInput items={CHARACTERS} onSelect={vi.fn()} />);

    await user.type(screen.getByRole("combobox"), "スズカ");

    const option = await screen.findByRole("option", { name: /无声铃鹿/ });
    expect(option).toBeTruthy();
    expect(screen.queryByText("日文原名：")).toBeNull();
    expect(screen.getByText("サイレンススズカ")).toBeTruthy();
    expect(screen.queryByText("别名命中")).toBeNull();
  });

  it("用英文别名也能搜到", async () => {
    const user = userEvent.setup();
    render(<SearchInput items={CHARACTERS} onSelect={vi.fn()} />);

    await user.type(screen.getByRole("combobox"), "teio");

    expect(await screen.findByRole("option", { name: /东海帝皇/ })).toBeTruthy();
  });

  it("输入法 composition 期间按 Enter 不触发选择", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<SearchInput items={CHARACTERS} onSelect={onSelect} />);

    const input = screen.getByRole("combobox");
    await user.type(input, "特别");
    await screen.findByRole("option", { name: /特别周/ });

    // 模拟输入法组合中
    input.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }));
    await user.keyboard("{Enter}");
    expect(onSelect).not.toHaveBeenCalled();

    input.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true }));
    await user.keyboard("{Enter}");
    expect(onSelect).toHaveBeenCalledTimes(1);
  });

  it("方向键可以移动高亮项，Enter 选中当前项", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<SearchInput items={CHARACTERS} onSelect={onSelect} />);

    await user.type(screen.getByRole("combobox"), "ス");
    const options = await screen.findAllByRole("option");
    expect(options.length).toBeGreaterThan(1);

    await user.keyboard("{ArrowDown}");
    await user.keyboard("{Enter}");

    expect(onSelect).toHaveBeenCalledTimes(1);
  });

  it("Escape 关闭候选面板但不选择", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<SearchInput items={CHARACTERS} onSelect={onSelect} />);

    const input = screen.getByRole("combobox");
    await user.type(input, "特别");
    await screen.findByRole("option", { name: /特别周/ });

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(screen.queryByRole("option", { name: /特别周/ })).toBeNull();
    });
    expect(onSelect).not.toHaveBeenCalled();
    // 输入内容保留，不清空用户已打的字
    expect((input as HTMLInputElement).value).toBe("特别");
  });

  it("空输入不显示候选，也不误报无结果", () => {
    render(<SearchInput items={CHARACTERS} onSelect={vi.fn()} />);
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(screen.queryByText(/没有找到/)).toBeNull();
  });

  it("无结果时显示明确的空状态，而不是加载中", async () => {
    const user = userEvent.setup();
    render(<SearchInput items={CHARACTERS} onSelect={vi.fn()} />);

    await user.type(screen.getByRole("combobox"), "不存在的角色");

    expect(await screen.findByText(/没有找到/)).toBeTruthy();
    expect(screen.queryByText(/正在查找角色/)).toBeNull();
  });

  it("加载中显示载入提示，不显示无结果", async () => {
    const user = userEvent.setup();
    render(<SearchInput items={[]} onSelect={vi.fn()} loading />);

    await user.type(screen.getByRole("combobox"), "特别");

    expect(await screen.findByText(/正在查找角色/)).toBeTruthy();
    expect(screen.queryByText(/没有找到/)).toBeNull();
  });

  it("加载失败显示错误，不显示成无结果", async () => {
    const user = userEvent.setup();
    render(<SearchInput items={[]} onSelect={vi.fn()} loadError="角色列表加载失败" />);

    await user.type(screen.getByRole("combobox"), "特别");

    expect(await screen.findByText("角色列表加载失败")).toBeTruthy();
    expect(screen.queryByText(/没有找到/)).toBeNull();
  });

  it("已猜过的角色被排除", async () => {
    const user = userEvent.setup();
    render(
      <SearchInput
        items={CHARACTERS}
        onSelect={vi.fn()}
        excludeIds={new Set(["1001"])}
      />,
    );

    await user.type(screen.getByRole("combobox"), "特别");

    expect(await screen.findByText(/没有找到/)).toBeTruthy();
  });
});
