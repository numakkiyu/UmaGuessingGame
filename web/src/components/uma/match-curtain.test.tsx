import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MatchCurtain } from "./match-curtain";
import type { MatchCurtainModel } from "./match-curtain-provider";

beforeEach(() => {
  vi.useFakeTimers({toFake:["setTimeout","clearTimeout","requestAnimationFrame","cancelAnimationFrame","performance"]});
  vi.stubGlobal("ResizeObserver",class { observe(){} disconnect(){} });
  vi.spyOn(HTMLCanvasElement.prototype,"getContext").mockReturnValue(null);
});
afterEach(() => { cleanup();vi.useRealTimers();vi.unstubAllGlobals(); });
const advance = (ms:number) => act(async()=>{await vi.advanceTimersByTimeAsync(ms);});
function model(overrides: Partial<MatchCurtainModel> = {}): MatchCurtainModel {
  return { id:"ticket",content:<button>Confirm</button>,title:"Match",description:"Finding players",ready:false,onCovered:vi.fn(),onCancel:vi.fn(),...overrides };
}
it("starts after portal mount, preserves live confirmation, then uncovers a ready room",async()=>{
  const done=vi.fn(),m=model();const page=render(<MatchCurtain model={m} onDone={done}/>);
  await advance(1200);
  expect(document.querySelector('.uma-match-curtain')?.getAttribute('data-phase')).toBe('forming');
  expect(screen.getByRole('button',{name:'Confirm'})).toBeTruthy();
  page.rerender(<MatchCurtain model={{...m,ready:true}} onDone={done}/>);
  await advance(100);
  expect(m.onCovered).toHaveBeenCalledOnce();
  await advance(3600);
  expect(done).toHaveBeenCalledOnce();
});
it("cancelled tickets turn red before opening and do not trap a later navigation",async()=>{
  const done=vi.fn(),m=model();const page=render(<MatchCurtain model={m} onDone={done}/>);
  await advance(5100);page.rerender(<MatchCurtain model={{...m,failed:true}} onDone={done}/>);
  await advance(200);
  expect(document.querySelector('.uma-match-curtain')?.getAttribute('data-outcome')).toBe('failure');
  expect(m.onCovered).not.toHaveBeenCalled();
  await advance(4600);expect(done).toHaveBeenCalledOnce();
});
it("unmount cancels pending decoration and never enters a stale room",async()=>{
  const done=vi.fn(),m=model();const page=render(<MatchCurtain model={m} onDone={done}/>);
  await advance(1200);fireEvent.keyDown(screen.getByRole('dialog'),{key:'Escape'});
  expect(m.onCancel).toHaveBeenCalledOnce();page.unmount();await advance(10000);
  expect(m.onCovered).not.toHaveBeenCalled();expect(done).not.toHaveBeenCalled();
});
it("an immediate cancellation still closes, turns red and opens vertically once",async()=>{
  const done=vi.fn(),m=model({failed:true});render(<MatchCurtain model={m} onDone={done}/>);
  await advance(500);
  expect(document.querySelector('.uma-match-curtain')?.getAttribute('data-phase')).toBe('closing');
  expect(m.onCovered).not.toHaveBeenCalled();
  await advance(650);
  expect(document.querySelector('.uma-match-curtain')?.getAttribute('data-outcome')).toBe('failure');
  fireEvent.keyDown(screen.getByRole('dialog'),{key:'Escape'});
  expect(m.onCancel).not.toHaveBeenCalled();
  await advance(700);
  expect(document.querySelector('.uma-match-curtain')?.getAttribute('data-phase')).toBe('opening');
  expect(m.onCovered).toHaveBeenCalledOnce();
  await advance(4000);expect(done).toHaveBeenCalledOnce();
});
