import {act,cleanup,renderHook,waitFor} from "@testing-library/react";
import {afterEach,expect,it,vi} from "vitest";
import {ApiError} from "./client";
import {useRoundSnapshot} from "./round-snapshot";
const f=vi.hoisted(()=>({getRound:vi.fn()}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:f})}));
afterEach(()=>{cleanup();vi.resetAllMocks();});
const allowed={data:{round:{roundId:"round",modeId:"clue-puzzle",phase:"collecting",phaseVersion:1},privateState:{secret:"private clue"},canSubmit:true}};
it.each([401,403,404])("denied read%d clears previous private snapshot",async(status)=>{
 f.getRound.mockResolvedValueOnce(allowed);
 const{result}=renderHook(()=>useRoundSnapshot("round"));
 await waitFor(()=>expect(result.current.raw).not.toBeNull());
 f.getRound.mockRejectedValueOnce(new ApiError({status,code:"denied",message:"denied",requestId:null}));
 await act(async()=>{await result.current.refetch();});
 expect(result.current.raw).toBeNull();expect(result.current.meta).toBeNull();expect(result.current.canSubmit).toBe(false);expect(result.current.isError).toBe(true);
});
it("transient read failure retains confirmed snapshot for recovery",async()=>{
 f.getRound.mockResolvedValueOnce(allowed);const{result}=renderHook(()=>useRoundSnapshot("round"));await waitFor(()=>expect(result.current.raw).not.toBeNull());
 const previous=result.current.raw;f.getRound.mockRejectedValueOnce(new Error("network interrupted"));await act(async()=>{await result.current.refetch();});
 expect(result.current.raw).toBe(previous);expect(result.current.isError).toBe(true);
});
it("an older successful read cannot restore private data after denial",async()=>{
 f.getRound.mockResolvedValueOnce(allowed);const{result}=renderHook(()=>useRoundSnapshot("round"));await waitFor(()=>expect(result.current.raw).not.toBeNull());
 let resolveOld!:(value:typeof allowed)=>void;f.getRound.mockImplementationOnce(()=>new Promise(r=>{resolveOld=r;}));let old!:Promise<boolean>;act(()=>{old=result.current.refetch();});
 f.getRound.mockRejectedValueOnce(new ApiError({status:404,code:"denied",message:"denied",requestId:null}));await act(async()=>{await result.current.refetch();});
 await act(async()=>{resolveOld(allowed);await old;});expect(result.current.raw).toBeNull();expect(result.current.isError).toBe(true);
});
