import { cleanup,renderHook,waitFor } from "@testing-library/react";
import { afterEach,expect,it,vi } from "vitest";
import { createContractApi } from "./contract";
import { useRoundSnapshot } from "./round-snapshot";
const f=vi.hoisted(()=>({getRound:vi.fn()}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:f})}));
afterEach(()=>{cleanup();f.getRound.mockReset();});
it("all four display reads restrict identity and their 401 never clears the player",async()=>{
 const unauth=vi.fn(),fetcher=vi.fn(async()=>new Response(JSON.stringify({title:"Expired",code:"session-not-found"}),{status:401,headers:{"Content-Type":"application/problem+json"}}));
 const api=createContractApi({fetchImpl:fetcher as unknown as typeof fetch,onUnauthenticated:unauth});
 for(const read of [api.getRoom,api.getRound,api.getEvent,api.getStandings]) await expect(read("e",undefined,true)).rejects.toThrow();
 expect(unauth).not.toHaveBeenCalled();for(const call of fetcher.mock.calls as unknown as [string,RequestInit][])expect(call[0]).toContain("?view=display");
 await expect(api.getRound("e")).rejects.toThrow();expect(unauth).toHaveBeenCalledTimes(1);
});
it.each(["player","display"])("display hook rejects wrong role or round without retaining private response (%s)",async role=>{
 f.getRound.mockResolvedValue({data:{publicState:{},viewerRole:role,round:{roundId:role==="display"?"other":"r",phase:"collecting"},privateState:{answer:"secret"}}});
 const h=renderHook(()=>useRoundSnapshot("r",true));await waitFor(()=>expect(h.result.current.isError).toBe(true));expect(h.result.current.raw).toBeNull();expect(f.getRound).toHaveBeenCalledWith("r",undefined,true);
});
it("ordinary participant hook keeps its existing projection",async()=>{
 f.getRound.mockResolvedValue({data:{publicState:{},viewerRole:"player",round:{roundId:"r",phase:"collecting"},privateState:{clue:"mine"}}});const h=renderHook(()=>useRoundSnapshot("r"));await waitFor(()=>expect(h.result.current.meta?.roundId).toBe("r"));expect(h.result.current.isError).toBe(false);expect(f.getRound).toHaveBeenCalledWith("r",undefined,false);
});
