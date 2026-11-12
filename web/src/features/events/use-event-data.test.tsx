import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useEventData } from "./use-event-data";
const f=vi.hoisted(()=>({api:{getEvent:vi.fn(),getStandings:vi.fn()}}));
vi.mock("@/lib/api",()=>({useApi:()=>({api:f.api})}));
beforeEach(()=>{f.api.getEvent.mockReset().mockResolvedValue({data:{eventId:"e",eventVersion:1}});f.api.getStandings.mockReset().mockResolvedValue({data:{eventId:"e",eventVersion:1,items:[],correctionIds:[]}});});
afterEach(cleanup);
it("refresh returns false on read failure or mismatched response and retains last known event",async()=>{const h=renderHook(()=>useEventData("e"));await waitFor(()=>expect(h.result.current.event?.eventId).toBe("e"));f.api.getStandings.mockRejectedValueOnce(Error("private upstream trace"));let ok;await act(async()=>{ok=await h.result.current.refetch();});expect(ok).toBe(false);expect(h.result.current.event?.eventId).toBe("e");f.api.getEvent.mockResolvedValueOnce({data:{eventId:"another"}});await act(async()=>{ok=await h.result.current.refetch();});expect(ok).toBe(false);expect(h.result.current.isError).toBe(true);await act(async()=>{ok=await h.result.current.refetch();});expect(ok).toBe(true);expect(h.result.current.isError).toBe(false);});
