import { chooseSelect } from "@/test/select";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { changeLanguage } from "@/i18n";
import { MemoryRouter } from "react-router";
import { RoomManagement } from "./room-management";
import { ApiError } from "@/lib/api/client";
import type { RoomSnapshot } from "@/lib/api/types";
const api=vi.hoisted(()=>({ transferRoomHost:vi.fn(),leaveOrRemoveMembership:vi.fn() }));
vi.mock("@/lib/api",()=>({useApi:()=>({api})}));
afterEach(async()=>{cleanup();vi.clearAllMocks();await changeLanguage("zh-Hans");});
const room={roomId:"r",kind:"event",roomVersion:7,viewer:{membershipId:"me",capabilities:["host","manage-room"]},members:[{membershipId:"me",displayName:"特别周",publicCode:"000123",role:"host"},{membershipId:"next",displayName:"特别周",publicCode:"987321",role:"player"},{membershipId:"screen",displayName:"屏幕",publicCode:"123456",role:"display"}]} as RoomSnapshot;
function open(){render(<MemoryRouter><RoomManagement room={room} onChanged={vi.fn()} /></MemoryRouter>);fireEvent.click(screen.getByText("房间管理与退出"));}
it("同名接任者用房内 ID 区分，展示端不能成为主持",async()=>{open();fireEvent.keyDown(screen.getByLabelText("转交主持人"),{key:"ArrowDown"});expect(await screen.findByRole("option",{name:"特别周（房内 ID 987321）"})).toBeTruthy();expect(screen.queryByRole("option",{name:/屏幕/})).toBeNull();});
it("未知结果确认复用同一个对象、版本和请求，避免重复转交",async()=>{
 api.transferRoomHost.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({data:{}});open();
 await chooseSelect(screen.getByLabelText("转交主持人"),"next");fireEvent.click(screen.getByRole("button",{name:"转交主持人"}));
 fireEvent.click(screen.getByRole("button",{name:"确认转交"}));await screen.findByText("正在确认刚才的操作");
 fireEvent.click(screen.getByRole("button",{name:"确认刚才的结果"}));await waitFor(()=>expect(api.transferRoomHost).toHaveBeenCalledTimes(2));
 expect(api.transferRoomHost.mock.calls[0]).toEqual(api.transferRoomHost.mock.calls[1]);expect(api.transferRoomHost.mock.calls[0]?.[1]).toEqual({targetMembershipId:"next",expectedRoomVersion:7});
});
it("明确拒绝后可关闭确认框并重选，不困在重试中",async()=>{
 api.transferRoomHost.mockRejectedValueOnce(new ApiError({status:409,code:"room-version-conflict",message:"changed",requestId:null}));open();
 await chooseSelect(screen.getByLabelText("转交主持人"),"next");fireEvent.click(screen.getByRole("button",{name:"转交主持人"}));fireEvent.click(screen.getByRole("button",{name:"确认转交"}));
 await screen.findByText("房间安排刚刚有变");expect(screen.getByRole("button",{name:"先留在这里"}).hasAttribute("disabled")).toBe(false);
});

it("未知转交切语不重发或更换目标，成功通知也随当前语言显示", async () => {
 let reject!: (error: unknown) => void;
 api.transferRoomHost.mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail; })).mockResolvedValueOnce({data:{}});
 open(); await chooseSelect(screen.getByLabelText("转交主持人"),"next");
 fireEvent.click(screen.getByRole("button", {name:"转交主持人"})); fireEvent.click(screen.getByRole("button", {name:"确认转交"}));
 await act(async () => { await changeLanguage("ja"); });
 expect(api.transferRoomHost).toHaveBeenCalledTimes(1);
 expect(screen.getByRole("dialog").textContent).toContain("特别周（ルーム内 ID 987321）");
 expect(screen.getByRole("button", {name:"確認しています…"}).hasAttribute("disabled")).toBe(true);
 await act(async () => { reject(new Error("private transport error")); });
 await screen.findByText("直前の操作を確認中");
 await act(async () => { await changeLanguage("zh-Hant"); });
 expect(screen.getByLabelText("轉交主持人").hasAttribute("disabled")).toBe(true);
 expect(screen.getByRole("button", {name:"先留在這裡"}).hasAttribute("disabled")).toBe(true);
 fireEvent.click(screen.getByRole("button", {name:"確認剛才的結果"}));
 await waitFor(() => expect(api.transferRoomHost).toHaveBeenCalledTimes(2));
 expect(api.transferRoomHost.mock.calls[1]).toEqual(api.transferRoomHost.mock.calls[0]);
 await screen.findByText("主持人已轉交，接下來交給夥伴安排囉");
 await act(async () => { await changeLanguage("ja"); });
 expect(screen.getByRole("status").textContent).toContain("進行役を交代しました");
 expect(document.body.textContent).not.toContain("private transport error");
});
it("已收到明确拒绝的错误切语重译，退出未知结果仍复用原凭证", async () => {
 api.transferRoomHost.mockRejectedValueOnce(new ApiError({status:409,code:"room-version-conflict",message:"private server error",requestId:null}));
 open(); await chooseSelect(screen.getByLabelText("转交主持人"),"next");
 fireEvent.click(screen.getByRole("button",{name:"转交主持人"})); fireEvent.click(screen.getByRole("button",{name:"确认转交"}));
 await screen.findByText("房间安排刚刚有变");
 await act(async () => { await changeLanguage("ja"); });
 expect(screen.getByRole("alert").textContent).not.toContain("房间安排刚刚有变");
 expect(screen.getByRole("button",{name:"このまま残る"}).hasAttribute("disabled")).toBe(false);
 fireEvent.click(screen.getByRole("button",{name:"このまま残る"}));
 api.leaveOrRemoveMembership.mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce({data:{}});
 fireEvent.click(screen.getByRole("button",{name:"ルームを退出"})); fireEvent.click(screen.getByRole("button",{name:"退出する"}));
 await screen.findByText("直前の操作を確認中");
 await act(async () => { await changeLanguage("zh-Hant"); });
 fireEvent.click(screen.getByRole("button",{name:"確認剛才的結果"}));
 await waitFor(() => expect(api.leaveOrRemoveMembership).toHaveBeenCalledTimes(2));
 expect(api.leaveOrRemoveMembership.mock.calls[1]).toEqual(api.leaveOrRemoveMembership.mock.calls[0]);
 expect(api.leaveOrRemoveMembership.mock.calls[0]?.slice(0,3)).toEqual(["r","me",undefined]);
});
