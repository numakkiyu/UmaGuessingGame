import {afterEach,describe,expect,it} from "vitest";
import {cleanup,render,screen} from "@testing-library/react";
import {EntertainmentResults} from "./entertainment-results";
import {parseEvent} from "./use-event-data";
afterEach(cleanup);
describe("entertainment and formal results stay separate",()=>{
 it("uses the saved scoring flag and shows only independent entertainment scores",()=>{
  const {segments}=parseEvent({eventId:"e",segmentDetails:[
   {segmentId:"formal",name:"正式项目",competitive:true,modeConfig:{modeId:"sort"}},
   {segmentId:"fun",name:"学园交流",competitive:false,modeConfig:{modeId:"sort"},results:[{subjectId:"p",displayName:"特别来宾",scoreUnits:3333,maxUnits:6000}],roundCount:2,completedRounds:1},
   {segmentId:"creative",name:"创意时刻",competitive:true,modeConfig:{modeId:"creative"}},
  ]});
  expect(segments.find(item=>item.segmentId==="creative")?.competitive).toBe(false);
  render(<EntertainmentResults segments={segments}/>);
  expect(screen.getByRole("heading",{name:"学园交流"})).toBeTruthy();
  expect(screen.getByRole("heading",{name:"创意时刻"})).toBeTruthy();
  expect(screen.queryByText("正式项目")).toBeNull();
  expect(screen.getByText("33.33 / 60")).toBeTruthy();
  expect(screen.getByText(/1 \/ 2 局完成/)).toBeTruthy();
 });
});
