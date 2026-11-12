import {useCallback,useEffect,useMemo,useRef,useState} from "react";
import {preloadClip,type AudioClip,type AudioRound} from "@/lib/api/audio";
import {loadAudioPlays,saveAudioPlays} from "@/lib/local/audio-online-draft";
export type AudioPreload="loading"|"ready"|"failed";
/** Media identity deliberately excludes locale and mutable progress. A stale
 * element cannot consume a play or update the next clip/round. No autoplay. */
export function useOnlineAudio(scope:string|null,round:AudioRound|null,enabled:boolean,onFailure:()=>void){
 const mediaKey=JSON.stringify(round?.clips.map(c=>[c.clipId,c.audioPath,c.maxPlays])??[]);
 const [state,setState]=useState<{scope:string|null;values:Map<string,number>}>({scope:null,values:new Map()}),[preloads,setPreloads]=useState<Map<string,AudioPreload>>(new Map()),[retry,setRetry]=useState(0),[playing,setPlaying]=useState<string|null>(null),[limited,setLimited]=useState(false);
 const element=useRef<HTMLAudioElement|null>(null),failRef=useRef(onFailure);useEffect(()=>{failRef.current=onFailure;},[onFailure]);
 const plays=useMemo(()=>state.scope===scope?state.values:scope&&round?loadAudioPlays(scope,round):new Map<string,number>(),[state,scope,round]);
 const release=useCallback(()=>{const audio=element.current;element.current=null;if(audio){audio.onended=null;audio.onerror=null;audio.onplaying=null;audio.pause();audio.removeAttribute("src");audio.load();}},[]);
 const stop=useCallback(()=>{release();setPlaying(null);},[release]);
 useEffect(()=>{const clips=JSON.parse(mediaKey) as [string,string,number][],controller=new AbortController();release();queueMicrotask(()=>{if(!controller.signal.aborted){setPlaying(null);setPreloads(new Map(clips.map(([id])=>[id,"loading"])));}});void Promise.all(clips.map(async([id,path])=>{const ok=await preloadClip(path,controller.signal);if(!controller.signal.aborted)setPreloads(prev=>new Map(prev).set(id,ok?"ready":"failed"));}));return()=>{controller.abort();release();};},[mediaKey,retry,scope,release]);
 useEffect(()=>{let active=true;if(!enabled){release();queueMicrotask(()=>{if(active)setPlaying(null);});}return()=>{active=false;};},[enabled,release]);
 const currentPlays=useRef(plays);useEffect(()=>{currentPlays.current=plays;},[plays]);
 function play(clip:AudioClip){
  if(element.current&&playing===clip.clipId){stop();return;}
  const used=currentPlays.current.get(clip.clipId)??0;if(!enabled||!scope||preloads.get(clip.clipId)!=="ready"||used>=clip.maxPlays)return;
  stop();const audio=new Audio(clip.audioPath);element.current=audio;setPlaying(clip.clipId);let counted=false;
  const failed=()=>{if(element.current!==audio)return;stop();setPreloads(prev=>new Map(prev).set(clip.clipId,"failed"));failRef.current();};
  audio.onerror=failed;audio.onended=()=>{if(element.current===audio)stop();};audio.onplaying=()=>{if(element.current!==audio||counted)return;counted=true;const next=new Map(currentPlays.current).set(clip.clipId,used+1);currentPlays.current=next;setState({scope,values:next});if(!saveAudioPlays(scope,next))setLimited(true);};
  void audio.play().catch(failed);
 }
 return {plays,preloads,playing,limited,play,stop,reprepare:()=>{stop();setRetry(n=>n+1);}};
}
