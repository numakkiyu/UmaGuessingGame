import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { SubmissionProgressPanel } from "./submission-progress-panel";

afterEach(cleanup);
describe("host submission status",()=>{
  it("does not call a closed round not started or nudge missing entrants",()=>{
    const round={subjectNames:{entrant:"特别来宾"},publicState:{subjects:[{subjectId:"entrant",submitted:false,attempts:0}]}};
    const view=render(<SubmissionProgressPanel round={round} phase="prepared"/>);
    expect(screen.getByText("未开始")).toBeTruthy();
    view.rerender(<SubmissionProgressPanel round={round} phase="collecting"/>);
    expect(screen.getByText("尚未作答")).toBeTruthy();
    view.rerender(<SubmissionProgressPanel round={round} phase="settled"/>);
    expect(screen.getByText("未作答")).toBeTruthy();
    expect(screen.queryByText("未开始")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("progress only uses supplied public fields",()=>{
 it("briefing and pause never nudge players and mixed reporting never claims everyone is done",()=>{
  const round={publicState:{subjects:[{subjectId:"a",submitted:false},{subjectId:"b"}]}};
  const v=render(<SubmissionProgressPanel round={round} phase="briefing"/>);
  expect(screen.queryByRole("alert")).toBeNull();
  v.rerender(<SubmissionProgressPanel round={round} phase="collecting" paused/>);
  expect(screen.queryByRole("alert")).toBeNull();
  v.rerender(<SubmissionProgressPanel round={{publicState:{subjects:[{subjectId:"a",submitted:true},{subjectId:"b"}]}}} phase="collecting"/>);
  expect(screen.queryByText("大家都已作答")).toBeNull();
  expect(screen.getByText("部分参赛者的进度暂时看不到，还不能确认大家是否都已作答")).toBeTruthy();
 });
 it("solved and exhausted attempts take precedence and same names have distinct room codes",()=>{
  render(<SubmissionProgressPanel phase="collecting" round={{publicState:{subjects:[{subjectId:"a",submitted:false,solved:true},{subjectId:"b",submitted:false,failed:true}]}}} room={{members:[{membershipId:"a",displayName:"同名伙伴",publicCode:"001234"},{membershipId:"b",displayName:"同名伙伴",publicCode:"567890"}]}}/>);
  expect(screen.getByText("已答对")).toBeTruthy();expect(screen.getByText("机会已用完")).toBeTruthy();
  expect(screen.getByText("房内 ID 001234")).toBeTruthy();expect(screen.getByText("房内 ID 567890")).toBeTruthy();
  expect(screen.queryByText(/还有.*尚未作答/)).toBeNull();
 });
 it("missing progress stays unknown while an explicit empty participant list stays empty",()=>{
  const v=render(<SubmissionProgressPanel round={{publicState:{subjects:[{subjectId:"a"}]}}} phase="collecting"/>);
  expect(screen.queryByRole("progressbar")).toBeNull();expect(screen.queryByRole("alert")).toBeNull();
  v.rerender(<SubmissionProgressPanel round={{publicState:{subjects:[]}}} phase="collecting"/>);
  expect(screen.getByText("这一局还没有参赛选手或队伍")).toBeTruthy();
 });
});
