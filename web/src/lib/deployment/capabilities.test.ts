import { describe, expect, it } from "vitest";
import { anyCanPlay, canPlay, modeEntry, formEntry } from "./capabilities";
import type { PublicConfig } from "@/lib/api/types";

const config = (forms: Record<string, boolean>): PublicConfig => ({ siteName:"test", apiVersion:"1", serverTime:"2026-11-10T00:00:00Z", deploymentMode:"lan", maxRoomMembers:16, maxRoomSpectators:16, enabledModeIds:["classic"], defaultTransport:"auto", assetBaseUrl:"/assets", buildVersion:"test", modeCapabilities: { classic: {local:false,verified:false,solo:false,friend:false,event:false,matchmaking:false,...forms} } });
describe("deployment player entries", () => {
  it("missing or incomplete capabilities do not advertise games", () => {
    expect(canPlay(undefined, "classic", "friend")).toBe(false);
    expect(anyCanPlay(undefined, "event")).toBe(false);
    expect(modeEntry(undefined, "classic")).toBeNull();
    expect(modeEntry(config({}), "classic")).toBeNull();
  });
  it("a disabled practice entry leads to the permitted purpose", () => {
    expect(modeEntry(config({ local: true, friend: true }), "classic")).toBe("/play/classic");
    expect(modeEntry(config({ local: false, verified: true }), "classic")).toBe("/play/classic/verified");
    expect(modeEntry(config({ local: false, solo: true }), "classic")).toBe("/play/classic/online");
    expect(modeEntry(config({ local: false, friend: true }), "classic")).toBe("/rooms/new?mode=classic");
    expect(modeEntry(config({ event: true }), "classic")).toBe("/events/new");
    expect(modeEntry(config({ matchmaking: true }), "classic")).toBe("/matchmaking?mode=classic");
  });
});

it("three player forms do not fall through to a different kind of play", () => {
 const all=config({local:true,friend:true,event:true});
 expect(formEntry(all,"classic","single")).toBe("/play/classic");
 expect(formEntry(all,"classic","multiplayer")).toBe("/rooms/new?mode=classic");
 expect(formEntry(all,"classic","team")).toBe("/events/new?mode=classic");
 expect(formEntry(config({friend:true}),"classic","single")).toBeNull();
 expect(formEntry(config({local:true}),"classic","multiplayer")).toBeNull();
 expect(formEntry(config({friend:true}),"classic","team")).toBeNull();
});
