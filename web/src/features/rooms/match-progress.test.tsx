import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,it} from "vitest";
import {MatchProgress} from "./match-progress";
import {changeLanguage,LANGUAGES} from "@/i18n";
import {matchmakingCopy} from "@/i18n/matchmaking";
import type {RoomSnapshot} from "@/lib/api/types";
const room={kind:"matchmaking"} as RoomSnapshot;
afterEach(async()=>{cleanup();await changeLanguage("zh-Hans");});
it("updates public opponent progress in all languages and never renders guesses or hidden orders",async()=>{
 for(const lang of LANGUAGES){await changeLanguage(lang);const v=render(<MatchProgress room={room} raw={{publicState:{modeId:"classic",self:{subjectId:"me"},subjects:[{subjectId:"me",attempts:1},{subjectId:"other",attempts:3,characterId:"secret-answer"}]},subjectNames:{other:"特别周 · 018"}}}/>);
 expect(screen.getByText("特别周 · 018")).toBeTruthy();expect(screen.getByText(matchmakingCopy[lang].progress.guesses.replace("{{count}}","3"))).toBeTruthy();expect(v.container.textContent).not.toContain("secret-answer");
 v.rerender(<MatchProgress room={room} raw={{publicState:{modeId:"sort",self:{subjectId:"me"},subjects:[{subjectId:"other",submitted:true,order:["secret-order"],correctPairs:9}]},subjectNames:{other:"特别周 · 018"}}}/>);
 expect(screen.getByText(matchmakingCopy[lang].progress.submitted)).toBeTruthy();expect(v.container.textContent).not.toContain("secret-order");
 v.rerender(<MatchProgress room={room} raw={{publicState:{modeId:"memory",self:{subjectId:"me"},subjects:[{subjectId:"other",matchedPairs:2,flipped:["secret-flip"]}]},subjectNames:{other:"特别周 · 018"}}}/>);
 expect(screen.getByText(matchmakingCopy[lang].progress.pairs.replace("{{count}}","2"))).toBeTruthy();expect(v.container.textContent).not.toContain("secret-flip");v.unmount();}
});
it("does not add matching UI to friend rooms",()=>{const{container}=render(<MatchProgress room={{kind:"friend"} as RoomSnapshot} raw={{publicState:{subjects:[{subjectId:"p"}]}}}/>);expect(container.textContent).toBe("");});
