import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import i18n, { changeLanguage, LANGUAGES } from ".";
import { errors } from "./errors";
import { entry } from "./entry";
import { lobby } from "./lobby";
import { modes } from "./modes";
import { modeRules } from "./mode-rules";
import { ErrorView } from "@/components/uma/page-state";
import { ApiError, describeFailure, NetworkError } from "@/lib/api/client";
import { CreateRoomPage } from "@/features/rooms/create-room-page";
import { JoinPage } from "@/features/rooms/join-page";
import { ModeStartPage } from "@/features/modes/mode-start-page";
import { LobbyPage } from "@/features/rooms/lobby-page";

const mock = vi.hoisted(() => ({ createRoom: vi.fn(), ensureSession: vi.fn(), resolve: vi.fn(), join: vi.fn() }));
vi.mock("@/lib/api", () => ({ useApi: () => ({ api: { createRoom: mock.createRoom, resolveRoomInvitation: mock.resolve, joinRoom: mock.join }, session: null, ensureSession: mock.ensureSession }) }));
vi.mock("@/lib/api/queries", () => ({
  usePublicConfig: () => ({ data: { maxRoomMembers: 8, enabledModeIds: ["classic"], modeCapabilities: { classic: { friend: true, local: true, event: true }, "clue-puzzle": { friend: true } } } }),
  useModes: () => ({ data: [{ modeId: "classic", availability: "available", name: "不得展示的原始名稱", description: "不得展示的原始說明", minPlayers: 1, maxPlayers: 2, estimatedMinutes: 3 }, { modeId: "clue-puzzle", availability: "available", minPlayers: 3 }] }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  mock.ensureSession.mockResolvedValue({});
});
afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });
const mount = (page: React.ReactNode) => render(<MemoryRouter>{page}</MemoryRouter>);
const language = (value: "zh-Hans" | "zh-Hant" | "ja") => act(async () => { await changeLanguage(value); });

function messages(value: object, prefix = ""): Record<string, string> {
  return Object.fromEntries(Object.entries(value).flatMap(([key, item]) => typeof item === "string"
    ? [[`${prefix}${key}`, item]] : Object.entries(messages(item, `${prefix}${key}.`))));
}

describe("entry resources", () => {
  it("keeps all message keys and interpolation parameters aligned", () => {
    for (const catalog of [errors, entry, lobby, modes, modeRules]) {
      const original = messages(catalog["zh-Hans"]);
      for (const lang of LANGUAGES) {
        const localized = messages(catalog[lang]);
        expect(Object.keys(localized).sort()).toEqual(Object.keys(original).sort());
        for (const [key, value] of Object.entries(localized)) {
          expect(value.trim()).not.toBe("");
          expect([...value.matchAll(/{{\s*(\w+)\s*}}/g)].map(m => m[1]).sort()).toEqual([...original[key]!.matchAll(/{{\s*(\w+)\s*}}/g)].map(m => m[1]).sort());
        }
      }
    }
  });

  it("re-localizes an already displayed failure without losing its diagnosis or retry semantics", async () => {
    const failure = describeFailure(new ApiError({ status: 422, code: "room-full", message: "SQL /private/raw", requestId: "safe-request" }));
    mount(<ErrorView failure={failure} />);
    expect(screen.getByText("这个房间已经满员了")).toBeTruthy();
    await language("ja");
    expect(screen.getByText("ルームが満員です")).toBeTruthy();
    expect(screen.getByText("safe-request")).toBeTruthy();
    expect(document.body.textContent).not.toMatch(/SQL|private|room-full/);
    expect(failure.retryable).toBe(false);
    expect(failure.kind).toBe("rejected");
  });

  it("maps every detailed code in every language and never renders raw details", async () => {
    for (const lang of LANGUAGES) {
      await language(lang);
      for (const [code, copy] of Object.entries(errors[lang])) {
        if (code === "join-unavailable") continue;
        const failure = describeFailure(new ApiError({ status: 422, code, message: "PRIVATE_RAW", requestId: "safe-id" }));
        expect(failure.detail).not.toContain("PRIVATE_RAW");
        // Category-only keys must still be classified by the HTTP status.
        if (!["forbidden", "unauthenticated", "not-found", "rejected", "limited", "busy", "unavailable", "timeout", "offline", "unknown"].includes(code)) {
          expect(failure.title).toBe(copy.title);
          expect(failure.detail).toBe(copy.detail);
        }
      }
      for (const code of ["constructor", "toString", "__proto__", "internal-new-code"]) {
        const failure = describeFailure(new ApiError({ status: 500, code, message: "PRIVATE_RAW", requestId: null }));
        expect(failure.kind).toBe("error");
        expect(failure.title).toBe(errors[lang].unavailable!.title);
      }
    }
  });

  it("localizes lobby metadata instead of rendering API prose", async () => {
    await language("ja");
    mount(<LobbyPage />);
    expect(screen.getByRole("heading", { name: "トレーナーさん、当ててみよう！" })).toBeTruthy();
    expect(screen.getByText(modes.ja.classic!.description)).toBeTruthy();
    expect(document.body.textContent).not.toContain("不得展示");
    expect(screen.getByRole("link", { name: lobby.ja.start }).getAttribute("href")).toBe("/play/classic");
    expect(screen.getByRole("link", { name: lobby.ja.rules }).getAttribute("href")).toBe("/help");
    expect(screen.getByRole("link", { name: lobby.ja.join }).getAttribute("href")).toBe("/join");
    expect(screen.getByRole("link", { name: lobby.ja.createEvent }).getAttribute("href")).toBe("/events/new");
    const card = screen.getByRole("link", { name: new RegExp(modes.ja.classic!.name + ".*プレイできます") });
    expect(card.getAttribute("title")).toBe(`${modes.ja.classic!.name} · プレイできます`);
    await language("zh-Hant");
    expect(screen.getByRole("heading", { name: "訓練員，來猜猜看！" })).toBeTruthy();
    expect(screen.getByText(modes["zh-Hant"].classic!.description)).toBeTruthy();
  });

  it.each(LANGUAGES)("keeps party requirements and a working friend entry in %s", async lang => {
    await language(lang);
    mount(<ModeStartPage config={{ modeId: "clue-puzzle" }} />);
    expect(screen.getByRole("heading", { name: modes[lang]["clue-puzzle"]!.name })).toBeTruthy();
    expect(screen.getByRole("button", { name: entry[lang].start.play }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByText(i18n.t("start.party", { ns: "entry", count: 3 }))).toBeTruthy();
    expect(screen.getByRole("link", { name: entry[lang].start.friend }).getAttribute("href")).toBe("/rooms/new?mode=clue-puzzle");
    expect(screen.getByRole("heading", { name: entry[lang].start.settings }).textContent).not.toBe(entry[lang].start.play);
    const disclosure = screen.getByText(entry[lang].start.rules).closest("details")!;
    expect(disclosure.querySelectorAll("li")).toHaveLength(7);
    expect(disclosure.textContent).toContain(modeRules[lang]["clue-puzzle"].rules[6]);
    expect(mock.createRoom).not.toHaveBeenCalled();
  });

  it("preserves a pending create body and key through a language switch; only form edits start a different request", async () => {
    mock.createRoom.mockRejectedValue(new NetworkError("PRIVATE_RAW", null));
    mount(<CreateRoomPage />);
    fireEvent.change(screen.getByLabelText("你的显示名"), { target: { value: "原名🍀" } });
    fireEvent.click(screen.getByRole("button", { name: "创建好友房间" }));
    await screen.findByText("连接中断");
    const first = mock.createRoom.mock.calls[0]!;
    expect(first[0].name).toBe("原名🍀的约局");
    await language("ja");
    expect(screen.getByText("接続が切れました")).toBeTruthy();
    expect((screen.getByLabelText("あなたのニックネーム") as HTMLInputElement).value).toBe("原名🍀");
    fireEvent.click(screen.getByRole("button", { name: "フレンドルームを作成" }));
    await waitFor(() => expect(mock.createRoom).toHaveBeenCalledTimes(2));
    expect(mock.createRoom.mock.calls[1]).toEqual(first);
    await screen.findByText("接続が切れました");
    fireEvent.change(screen.getByLabelText("ルーム名（省略可）"), { target: { value: "自分で付けた名前" } });
    fireEvent.click(screen.getByRole("button", { name: "フレンドルームを作成" }));
    await waitFor(() => expect(mock.createRoom).toHaveBeenCalledTimes(3));
    expect(mock.createRoom.mock.calls[2]![0].name).toBe("自分で付けた名前");
    expect(mock.createRoom.mock.calls[2]![1]).not.toBe(first[1]);
  });

  it("updates validation and unresolved invitation copy without modifying input", async () => {
    mock.resolve.mockResolvedValue({ data: { joinable: false }, requestId: "safe-join" });
    mount(<JoinPage />);
    fireEvent.click(screen.getByRole("button", { name: "加入房间" }));
    expect(screen.getByText("房间匹配码应为 6–16 位字符")).toBeTruthy();
    await language("ja");
    expect(screen.getByText("ルームコードは 6～16 桁で入力してください")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("ルームコード"), { target: { value: "abcd1234" } });
    fireEvent.change(screen.getByLabelText("ニックネーム"), { target: { value: "保持原名" } });
    fireEvent.click(screen.getByRole("button", { name: "ルームに参加" }));
    await screen.findByText("このルームには参加できません");
    expect(mock.resolve).toHaveBeenCalledWith({ inviteCode: "ABCD1234" });
    await language("zh-Hant");
    expect(screen.getByText("目前無法加入這個房間")).toBeTruthy();
    expect(screen.getByText("safe-join")).toBeTruthy();
    expect((screen.getByLabelText("顯示名稱") as HTMLInputElement).value).toBe("保持原名");
    expect(mock.ensureSession).not.toHaveBeenCalled();
    expect(mock.join).not.toHaveBeenCalled();
  });

  it("keeps HTTP categories authoritative when an API code happens to equal a category name", () => {
    expect(describeFailure(new ApiError({ status: 503, code: "busy", message: "PRIVATE_RAW", requestId: null })).retryable).toBe(true);
    expect(i18n.t("friend.create", { ns: "entry" })).toBe("创建好友房间");
  });
});
