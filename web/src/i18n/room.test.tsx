import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { changeLanguage, LANGUAGES } from ".";
import { teamsCopy } from "./teams";
import { roomCopy } from "./room";
import { RoomRoster } from "@/features/rooms/room-roster";
import type { RoomSnapshot } from "@/lib/api/types";
afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });
function leaves(value: unknown, prefix = ""): Record<string, string> {
  if (typeof value === "string") return { [prefix]: value };
  return Object.assign({}, ...Object.entries(value as object).map(([key, child]) => leaves(child, `${prefix}.${key}`)));
}
it("房间三语字段与动态插值齐全，不改变分享码或用户名字", () => {
 for (const catalog of [roomCopy, teamsCopy]) {
 const source=leaves(catalog["zh-Hans"]);
 for(const lang of LANGUAGES) {
  const target=leaves(catalog[lang]);expect(Object.keys(target).sort()).toEqual(Object.keys(source).sort());
  for(const [key,text] of Object.entries(source)) {
   expect(target[key]?.trim()).toBeTruthy();
   const params=(v:string)=>[...v.matchAll(/{{(\w+)}}/g)].map(m=>m[1]).sort();
   expect(params(target[key]!)).toEqual(params(text));
  }
 }
 }
});
it("切语保留同名成员和前导零编号，只翻译角色与准备状态",async()=>{
 const room={kind:"event",viewer:{membershipId:"a"},members:[
 {membershipId:"a",nickname:"小栗帽",displayName:"原始名",publicCode:"000123",capabilities:["host","manage-room","play"],role:"host",ready:true},
 {membershipId:"b",displayName:"小栗帽",publicCode:"001234",capabilities:["play"],role:"team-captain",ready:false},
 {membershipId:"c",displayName:"投影",publicCode:"999999",capabilities:[],role:"display"},
 ]} as unknown as RoomSnapshot;
 const before=JSON.stringify(room);render(<RoomRoster room={room}/>);
 for(const lang of LANGUAGES){
  await act(async()=>{await changeLanguage(lang);});
  expect(screen.getByRole("table").textContent).toContain(roomCopy[lang].host);
  expect(screen.getByRole("table").textContent).toContain(roomCopy[lang].captain);
  expect(screen.getByRole("table").textContent).toContain(roomCopy[lang].notReady);
  expect(screen.getByRole("table").textContent).toContain(roomCopy[lang].display);
  expect(screen.getAllByText("000123")).toHaveLength(2);
  expect(screen.getAllByText("001234")).toHaveLength(2);
  expect(screen.getAllByText("小栗帽")).toHaveLength(4);
  expect(JSON.stringify(room)).toBe(before);
 }
});
