import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach,beforeEach,expect,it,vi } from "vitest";
import { MemoryRouter,Routes,Route } from "react-router";
import { changeLanguage } from "@/i18n";
import { ApiError } from "@/lib/api/client";
import { EventDisplayPage } from "./event-display-page";
const f=vi.hoisted(()=>({api:{getEvent:vi.fn(),redeemDisplayGrant:vi.fn()},data:{} as Record<string,unknown>}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:f.api})}));
vi.mock("@/features/events/use-event-data",async original=>({...await original<object>(),useEventData:()=>f.data}));
vi.mock("./public-round-stage",()=>({PublicRoundStage:({competitive}:{competitive:boolean})=><div data-testid="stage">{String(competitive)}</div>}));
const denied=()=>new ApiError({status:401,code:"session-not-found",message:"private trace",requestId:null});
function show(){return render(<MemoryRouter initialEntries={["/events/e/display"]}><Routes><Route path="/events/:eventId/display" element={<EventDisplayPage/>}/></Routes></MemoryRouter>);}
beforeEach(async()=>{
 f.api.getEvent.mockReset().mockRejectedValue(denied());f.api.redeemDisplayGrant.mockReset();sessionStorage.clear();
 f.data={event:{eventId:"e",roomId:"r",name:"伙伴杯",status:"running",tiePolicy:"announced-tiebreak"},standings:{items:[]},segments:[],isLoading:false,isError:false,refetch:vi.fn(),loadedAt:Date.now()};
 await changeLanguage("zh-Hans");
});
afterEach(cleanup);
it("restores only through restricted read, ignores saved Boolean, language preserves draft without writes",async()=>{
 sessionStorage.setItem("display-authorized","true");show();await screen.findByLabelText("配对码");
 expect(f.api.getEvent).toHaveBeenCalledWith("e",expect.any(AbortSignal),true);
 fireEvent.change(screen.getByLabelText("配对码"),{target:{value:"example-pair-code-1234"}});
 await act(async()=>{await changeLanguage("ja");});
 expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("example-pair-code-1234");
 expect(f.api.redeemDisplayGrant).not.toHaveBeenCalled();expect(f.api.getEvent).toHaveBeenCalledTimes(1);
});
it("unknown redemption is not retried; explicit read restores a lost response",async()=>{
 show();await screen.findByLabelText("配对码");
 f.api.redeemDisplayGrant.mockRejectedValueOnce(new TypeError("secret network trace"));
 fireEvent.change(screen.getByLabelText("配对码"),{target:{value:"example-pair-code-1234"}});fireEvent.click(screen.getByRole("button",{name:"打开大屏"}));
 await screen.findByText(/还没确认是否配对成功/);
 expect(f.api.redeemDisplayGrant).toHaveBeenCalledExactlyOnceWith({grantCode:"example-pair-code-1234",eventId:"e"});
 await act(async()=>{await changeLanguage("zh-Hant");});expect(f.api.redeemDisplayGrant).toHaveBeenCalledTimes(1);
 f.api.getEvent.mockResolvedValueOnce({data:{eventId:"e"}});
 fireEvent.click(screen.getByRole("button",{name:"查看配對狀態"}));await screen.findByRole("heading",{name:"伙伴杯"});
 expect(document.body.textContent).not.toContain("secret network trace");expect(JSON.stringify(sessionStorage)).not.toContain("example-pair-code-1234");
});
it("deduplicates pending redemption and relocalizes known mismatch without changing original code",async()=>{
 show();await screen.findByLabelText("配对码");let reject!:(e:unknown)=>void;
 f.api.redeemDisplayGrant.mockImplementationOnce(()=>new Promise((_,no)=>{reject=no;}));
 fireEvent.change(screen.getByLabelText("配对码"),{target:{value:"example-pair-code-1234"}});
 const form=screen.getByRole("textbox").closest("form")!;fireEvent.submit(form);fireEvent.submit(form);
 expect(f.api.redeemDisplayGrant).toHaveBeenCalledTimes(1);
 await act(async()=>{reject(new ApiError({status:409,code:"grant-event-mismatch",message:"private",requestId:null}));});
 await screen.findByText(/属于另一个活动/);await act(async()=>{await changeLanguage("ja");});expect(screen.getByText(/別のイベント/)).toBeTruthy();expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("example-pair-code-1234");
});
it("cookie-backed refresh shows tied scores and room IDs, cancelled event never mounts old round",async()=>{
 f.api.getEvent.mockResolvedValue({data:{eventId:"e"}});
 f.data.event={eventId:"e",roomId:"r",name:"伙伴杯",status:"cancelled",tiePolicy:"announced-tiebreak"};
 f.data.standings={items:[{subjectId:"private-subject",displayName:"同名",publicCode:"001234",rank:1,scoreUnits:3333}]};
 f.data.segments=[{segmentId:"s",roundId:"old",modeId:"classic",status:"running",hasDetail:true,competitive:true}];
 show();await screen.findByRole("heading",{name:"伙伴杯"});expect(screen.queryByTestId("stage")).toBeNull();
 expect(screen.getAllByText("房内 ID 001234").length).toBeGreaterThan(0);expect(screen.getAllByText("33.33").length).toBeGreaterThan(0);expect(screen.getByText(/正式同分仍并列/)).toBeTruthy();expect(document.body.textContent).not.toContain("private-subject");
});
it("real restore rejects mismatched event and does not redeem",async()=>{
 f.api.getEvent.mockResolvedValueOnce({data:{eventId:"other"}});show();await screen.findByLabelText("配对码");expect(f.api.redeemDisplayGrant).not.toHaveBeenCalled();
});
it("unmount during restore aborts read",async()=>{f.api.getEvent.mockImplementationOnce(()=>new Promise(()=>{}));const view=show();await waitFor(()=>expect(f.api.getEvent).toHaveBeenCalled());const signal=f.api.getEvent.mock.calls[0]![1] as AbortSignal;view.unmount();expect(signal.aborted).toBe(true);});
it("offline pairing check reports unknown connection rather than falsely disconnected",async()=>{show();await screen.findByLabelText("配对码");f.api.getEvent.mockRejectedValueOnce(new TypeError("network unavailable"));fireEvent.click(screen.getByRole("button",{name:"查看配对状态"}));await screen.findByText(/暂时无法确认连接/);expect(screen.queryByText(/还没有连接到这个活动/)).toBeNull();});
it("screen size limits visible rows truthfully without altering the official tied ranks or writing",async()=>{
 const width=window.innerWidth,height=window.innerHeight;
 function resize(w:number,h:number){Object.defineProperty(window,"innerWidth",{configurable:true,value:w});Object.defineProperty(window,"innerHeight",{configurable:true,value:h});window.dispatchEvent(new Event("resize"));}
 try{
  resize(1280,720);f.api.getEvent.mockResolvedValue({data:{eventId:"e"}});f.data.standings={items:Array.from({length:6},(_,i)=>({subjectId:`subject-${i}`,displayName:"伙伴",publicCode:String(i).padStart(6,"0"),rank:1,scoreUnits:3333}))};
  const view=show();await screen.findByRole("heading",{name:"伙伴杯"});expect(view.container.querySelectorAll("tbody tr")).toHaveLength(2);expect(screen.getByText("展示前 2 组，共 6 组")).toBeTruthy();
  await act(async()=>resize(1920,1080));expect(view.container.querySelectorAll("tbody tr")).toHaveLength(4);
  await act(async()=>resize(320,720));expect(view.container.querySelectorAll("tbody tr")).toHaveLength(6);expect(f.api.redeemDisplayGrant).not.toHaveBeenCalled();
 }finally{await act(async()=>resize(width,height));}
});
