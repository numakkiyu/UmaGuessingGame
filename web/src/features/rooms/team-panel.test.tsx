import { chooseSelect } from "@/test/select";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { changeLanguage, LANGUAGES } from "@/i18n";
import { TeamPanel } from "./team-panel";
import { ApiError } from "@/lib/api/client";
import { countSubjects, readTeamMembers, readTeams, validateTeams } from "@/lib/api/teams";
import { teamsCopy } from "@/i18n/teams";
const api=vi.hoisted(()=>({createTeam:vi.fn(),updateMembership:vi.fn()}));
vi.mock("@/lib/api",()=>({useApi:()=>({api})}));
afterEach(async()=>{cleanup();vi.clearAllMocks();await changeLanguage("zh-Hans");});
const room={roomVersion:7,teams:[{teamId:"a",label:"自分の隊名🍀",colorToken:"green"}],members:[{membershipId:"a1",nickname:"同名",publicCode:"000111",role:"team-captain",teamId:"a"},{membershipId:"a2",nickname:"同名",publicCode:"000222",role:"player",teamId:null}]};
it("队名颜色与成员的待提交状态切语保留，完成通知按当下语境展示",async()=>{
 let resolve!:(value:unknown)=>void;api.createTeam.mockImplementationOnce(()=>new Promise(done=>{resolve=done;}));
 const onChanged=vi.fn(),before=JSON.stringify(room);
 render(<TeamPanel roomId="r" room={room} canManage locked={false} onChanged={onChanged}/>);
 fireEvent.click(screen.getByText("新建队伍",{selector:"summary"}));
 fireEvent.change(screen.getByLabelText("队名"),{target:{value:"原名／にんじん"}});
 await chooseSelect(screen.getByLabelText("队伍颜色"),"teal");
 await act(async()=>{await changeLanguage("ja");});
 expect((screen.getByLabelText("チーム名") as HTMLInputElement).value).toBe("原名／にんじん");
 expect((screen.getByLabelText("チームカラー") as HTMLSelectElement).value).toBe("teal");
 fireEvent.click(screen.getByRole("button",{name:"チームを作る"}));
 await act(async()=>{await changeLanguage("zh-Hant");});
 expect(api.createTeam).toHaveBeenCalledTimes(1);expect(api.createTeam.mock.calls[0]).toEqual(["r",{label:"原名／にんじん",colorToken:"teal"}]);
 expect(screen.getByRole("button",{name:"建立隊伍"}).hasAttribute("disabled")).toBe(true);
 await act(async()=>{resolve({data:{}});});
 await screen.findByText(teamsCopy["zh-Hant"].created);
 await act(async()=>{await changeLanguage("ja");});expect(screen.getByRole("status").textContent).toBe(teamsCopy.ja.created);
 expect(onChanged).toHaveBeenCalledOnce();expect(JSON.stringify(room)).toBe(before);
});
it("同名分队绑定成员ID和原版本，明确冲突提示可切语且不泄露异常",async()=>{
 api.updateMembership.mockRejectedValueOnce(new ApiError({status:409,code:"room-version-conflict",message:"private",requestId:null}));
 const onChanged=vi.fn();render(<TeamPanel roomId="r" room={room} canManage locked={false} onChanged={onChanged}/>);
 fireEvent.click(screen.getByRole("button",{name:"加入「自分の隊名🍀」"}));
 await screen.findByText(teamsCopy["zh-Hans"].changedTitle);
 expect(api.updateMembership.mock.calls[0]).toEqual(["r","a2",{expectedRoomVersion:7,teamId:"a",requestedRole:"player"}]);
 await act(async()=>{await changeLanguage("ja");});
 expect(screen.getByRole("alert").textContent).toContain(teamsCopy.ja.changedDetail);
 expect(document.body.textContent).not.toContain("private");
 expect(document.body.textContent).toContain("000111");expect(document.body.textContent).toContain("000222");
 await waitFor(()=>expect(onChanged).toHaveBeenCalledOnce());
});
it("预检三种错误跟随语境且计分主体完全不变，未知队不显示内部标识",async()=>{
 const invalid={teams:[{teamId:"a",label:"A"},{teamId:"b",label:"B"}],members:[{membershipId:"1",displayName:"甲",role:"team-captain"},{membershipId:"2",displayName:"乙",role:"team-captain",teamId:"a"},{membershipId:"3",displayName:"丙",role:"team-captain",teamId:"a"},{membershipId:"4",displayName:"丁",role:"player",teamId:"b"}]};
 const before=JSON.stringify(invalid);
 for(const lang of LANGUAGES){await changeLanguage(lang);const teams=readTeams(invalid),members=readTeamMembers(invalid),problems=validateTeams(teams,members);expect(problems).toHaveLength(3);expect(problems[0]).toContain(lang==="ja"?"リーダー":lang==="zh-Hant"?"隊長":"队长");expect(countSubjects(members)).toEqual({teams:2,players:0,total:2});expect(JSON.stringify(invalid)).toBe(before);const hidden=validateTeams([],members);expect(hidden.join(" ")).not.toContain("undefined");}
});
