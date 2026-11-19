import { describe, expect, it, vi } from "vitest";
import { continueEventCreation, EVENT_CREATION_MAX_AGE, EVENT_CREATION_STORAGE, newEventCreation, readEventCreation, type EventCreation } from "./event-creation";
import type { Api } from "@/lib/api/contract";

const roomId = `rom_${"a".repeat(32)}`;
function attempt() {
  return newEventCreation("original-session", {name:"学园游玩会",kind:"event",modeConfig:{modeId:"classic",variant:"public",winPolicy:"window-attempts",maxWrongGuesses:8,durationSeconds:180,shadowEnabled:false},maxPlayers:20,maxSpectators:10,visibility:"invite",allowSpectators:true},{name:"学园游玩会",tiePolicy:"shared-rank"},1000);
}
function fakeApi() {
  return {
    createRoom:vi.fn().mockResolvedValue({data:{room:{roomId}}}),
    createEvent:vi.fn().mockResolvedValue({data:{eventId:"event"}}),
  } as unknown as Pick<Api,"createRoom"|"createEvent">;
}

describe("one recoverable event arrangement",()=>{
  it("reuses both exact commands and persists the room before the second write",async()=>{
    const api=fakeApi();let pending=attempt();const original=pending;
    vi.mocked(api.createEvent).mockRejectedValueOnce(new Error("response lost"));
    const save=(next:EventCreation)=>{pending=next;};
    await expect(continueEventCreation(api,pending,"original-session",save,1001)).rejects.toThrow("response lost");
    expect(pending.roomId).toBe(roomId);
    await expect(continueEventCreation(api,pending,"original-session",save,1002)).resolves.toBe("event");
    expect(api.createRoom).toHaveBeenCalledTimes(1);
    expect(vi.mocked(api.createEvent).mock.calls.map(call=>call.slice(0,2))).toEqual([
      [{...original.event,roomId},original.eventKey], [{...original.event,roomId},original.eventKey],
    ]);
  });
  it("confirms an uncertain room with the same body and key",async()=>{
    const api=fakeApi(),pending=attempt();
    vi.mocked(api.createRoom).mockRejectedValueOnce(new Error("unknown"));
    await expect(continueEventCreation(api,pending,"original-session",()=>{},1001)).rejects.toThrow();
    await continueEventCreation(api,pending,"original-session",()=>{},1002);
    expect(vi.mocked(api.createRoom).mock.calls.map(call=>call.slice(0,2))).toEqual([[pending.room,pending.roomKey],[pending.room,pending.roomKey]]);
  });
  it("never sends an expired arrangement or replays it as a different identity",async()=>{
    const api=fakeApi(),pending=attempt();
    await expect(continueEventCreation(api,pending,"different-session",()=>{},1001)).rejects.toThrow();
    await expect(continueEventCreation(api,pending,"original-session",()=>{},1000+EVENT_CREATION_MAX_AGE)).rejects.toThrow();
    expect(api.createRoom).not.toHaveBeenCalled();expect(api.createEvent).not.toHaveBeenCalled();
  });
  it("reloads only whitelisted fields, rejects corruption, and distinguishes unavailable storage",()=>{
    const value={...attempt(),csrfToken:"never restore",room:{...attempt().room,csrfToken:"never upload"}};
    const read=(raw:string)=>readEventCreation({getItem:vi.fn().mockReturnValue(raw)},1001);
    const restored=read(JSON.stringify(value));
    expect(restored.kind).toBe("ready");
    expect(JSON.stringify(restored)).not.toContain("csrfToken");
    expect(read("broken").kind).toBe("invalid");
    expect(read(JSON.stringify({...attempt(),room:{...attempt().room,maxPlayers:201}})).kind).toBe("invalid");
    expect(readEventCreation({getItem:()=>{throw new Error("private storage");}}).kind).toBe("unavailable");
    const getter=vi.fn().mockReturnValue(JSON.stringify(attempt()));
    expect(readEventCreation({getItem:getter},1000+EVENT_CREATION_MAX_AGE).kind).toBe("expired");
    expect(getter).toHaveBeenCalledWith(EVENT_CREATION_STORAGE);
  });
});
