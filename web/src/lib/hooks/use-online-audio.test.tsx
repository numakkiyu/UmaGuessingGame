import {act,cleanup,renderHook} from "@testing-library/react";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {useOnlineAudio} from "./use-online-audio";
import {normalizeAudioRound} from "@/lib/api/audio";
class Media {
 static items:Media[]=[];oncanplaythrough:(()=>void)|null=null;onerror:(()=>void)|null=null;onended:(()=>void)|null=null;onplaying:(()=>void)|null=null;preload="";src="";play=vi.fn(async()=>{this.onplaying?.();});pause=vi.fn();load=vi.fn();removeAttribute=vi.fn();constructor(src?:string){this.src=src??"";Media.items.push(this);}
}
const round=normalizeAudioRound({publicState:{clips:[0,1].map(i=>({clipId:`c${i}`,audioPath:`/assets/audio/clip${i}.webm`,maxPlays:2})),candidates:[]}});
beforeEach(()=>{Media.items=[];vi.stubGlobal("Audio",Media);});
afterEach(()=>{cleanup();vi.unstubAllGlobals();sessionStorage.clear();});
async function preload(){await act(async()=>{for(const m of Media.items)if(m.preload==="auto")m.oncanplaythrough?.();});}
it("暂停同步关闭播放器，恢复不自动播放，旧playing事件不耗次数",async()=>{
 const failed=vi.fn();const h=renderHook(({enabled})=>useOnlineAudio("identity:round:subject",round,enabled,failed),{initialProps:{enabled:true}});await preload();await act(async()=>h.result.current.play(round.clips[0]!));const m=Media.items.at(-1)!,late=m.onplaying;expect(h.result.current.plays.get("c0")).toBe(1);await act(async()=>h.rerender({enabled:false}));expect(m.pause).toHaveBeenCalled();expect(h.result.current.playing).toBeNull();await act(async()=>{h.rerender({enabled:true});late?.();});expect(m.play).toHaveBeenCalledTimes(1);expect(h.result.current.plays.get("c0")).toBe(1);await act(async()=>h.result.current.play(round.clips[0]!));expect(h.result.current.plays.get("c0")).toBe(2);expect(failed).not.toHaveBeenCalled();
});
it("换段停止旧元素，过期失败回调不影响新音频，换身份不继承预算",async()=>{
 const failed=vi.fn();const h=renderHook(({scope})=>useOnlineAudio(scope,round,true,failed),{initialProps:{scope:"old:round:subject"}});await preload();await act(async()=>h.result.current.play(round.clips[0]!));const old=Media.items.at(-1)!,late=old.onerror;await act(async()=>h.result.current.play(round.clips[1]!));expect(old.pause).toHaveBeenCalled();await act(async()=>late?.());expect(h.result.current.playing).toBe("c1");expect(failed).not.toHaveBeenCalled();await act(async()=>h.rerender({scope:"new:round:subject"}));expect(h.result.current.playing).toBeNull();expect(h.result.current.plays.size).toBe(0);
});
it("准备超时能重试且不耗次数，卸载取消等待并释放媒体",async()=>{
 vi.useFakeTimers();try{const h=renderHook(()=>useOnlineAudio("scope",round,true,vi.fn()));await act(async()=>{await vi.advanceTimersByTimeAsync(15001);});expect(h.result.current.preloads.get("c0")).toBe("failed");expect(h.result.current.plays.size).toBe(0);await act(async()=>h.result.current.reprepare());await preload();expect(h.result.current.preloads.get("c0")).toBe("ready");const pending=Media.items;h.unmount();expect(pending.every(m=>m.pause.mock.calls.length>0)).toBe(true);expect(vi.getTimerCount()).toBe(0);}finally{vi.useRealTimers();}
});
