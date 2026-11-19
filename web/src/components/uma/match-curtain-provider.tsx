import { createContext, useCallback, useContext, useEffect, useRef, useState, type ComponentType, type ReactNode } from "react";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";

export type MatchCurtainModel = {
  id: string; content: ReactNode; title: string; description: string;
  ready: boolean; failed?: boolean; onCovered: () => void; onCancel: () => void;
};
type Control = { show: (model: MatchCurtainModel) => void; dismiss: () => void };
type CurtainProps = { model: MatchCurtainModel; onDone: () => void };
const Context = createContext<Control | null>(null);
export const useMatchCurtain = () => useContext(Context);

/** A missing decorative chunk must never hide confirmation or prevent entering a room. */
function Fallback({ model, onDone, failed }: CurtainProps & {failed:boolean}) {
  useEffect(() => {
    if (!failed || (!model.ready && !model.failed)) return;
    model.onCovered(); onDone();
  }, [model, onDone, failed]);
  return <Dialog open onOpenChange={open => { if (!open && !model.ready) model.onCancel(); }}><DialogContent>
    <DialogTitle className="sr-only">{model.title}</DialogTitle><DialogDescription className="sr-only">{model.description}</DialogDescription>{model.content}
  </DialogContent></Dialog>;
}

/** The shell owns the curtain so a room can load underneath its opening panels. */
export function MatchCurtainProvider({ children }: { children: ReactNode }) {
  const [model, setModel] = useState<MatchCurtainModel | null>(null);
  const [Curtain,setCurtain] = useState<ComponentType<CurtainProps>|null>(null);
  const [failed,setFailed] = useState(false);
  const retained = useRef(false);
  const show = useCallback((next: MatchCurtainModel) => { retained.current = next.ready || !!next.failed; setModel(next); }, []);
  const dismiss = useCallback(() => { if (!retained.current) setModel(null); }, []);
  const [control] = useState<Control>(() => ({ show, dismiss }));
  const done = useCallback(() => { retained.current = false; setModel(null); }, []);
  const needed = model !== null;
  useEffect(() => {
    if (!needed || Curtain) return;
    let active=true;
    const timeout=window.setTimeout(()=>{if(active)setFailed(true);},2500);
    void import("./match-curtain").then(module=>{if(active){setCurtain(()=>module.MatchCurtain);setFailed(false);}}).catch(()=>{if(active)setFailed(true);}).finally(()=>window.clearTimeout(timeout));
    return ()=>{active=false;window.clearTimeout(timeout);};
  },[needed, Curtain]);
  return <Context.Provider value={control}>{children}{model ? Curtain ? <Curtain key={model.id} model={model} onDone={done} /> : <Fallback model={model} onDone={done} failed={failed} /> : null}</Context.Provider>;
}
