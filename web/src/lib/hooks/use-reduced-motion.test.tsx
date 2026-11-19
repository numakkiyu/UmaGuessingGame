import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useReducedMotion } from "./use-reduced-motion";

let systemReduced=false;
const listeners=new Set<() => void>();
function RootObserver(){const motion=useReducedMotion();return <output aria-label="root">{motion.preference}:{String(motion.reduced)}</output>;}
function Settings(){const motion=useReducedMotion();return <><button onClick={()=>motion.setPreference("reduced")}>reduce</button><button onClick={()=>motion.setPreference("system")}>system</button><output aria-label="settings">{motion.preference}:{String(motion.reduced)}</output></>;}
function os(value:boolean){act(()=>{systemReduced=value;listeners.forEach(listener=>listener());});}
beforeEach(()=>{systemReduced=false;listeners.clear();vi.stubGlobal("matchMedia",()=>({get matches(){return systemReduced;},addEventListener:(_name:string,listener:()=>void)=>listeners.add(listener),removeEventListener:(_name:string,listener:()=>void)=>listeners.delete(listener)}));});
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();});

it("explicit reduction survives OS changes across provider and settings consumers",()=>{
  render(<><RootObserver/><Settings/></>);fireEvent.click(screen.getByText("reduce"));expect(screen.getByLabelText("root").textContent).toBe("reduced:true");os(true);os(false);expect(document.documentElement.getAttribute("data-motion")).toBe("reduced");expect(screen.getByLabelText("settings").textContent).toBe("reduced:true");
  fireEvent.click(screen.getByText("system"));expect(screen.getByLabelText("root").textContent).toBe("system:false");os(true);expect(document.documentElement.getAttribute("data-motion")).toBe("reduced");os(false);expect(document.documentElement.hasAttribute("data-motion")).toBe(false);
});

it("denied persistence keeps the same active-page preference for every consumer",()=>{
  render(<><RootObserver/><Settings/></>);vi.spyOn(localStorage,"setItem").mockImplementation(()=>{throw new DOMException("private details","SecurityError");});fireEvent.click(screen.getByText("reduce"));os(true);os(false);expect(screen.getByLabelText("root").textContent).toBe("reduced:true");expect(document.documentElement.getAttribute("data-motion")).toBe("reduced");expect(document.body.textContent).not.toContain("private details");
});

it("another tab preference change synchronizes the root and controls",()=>{
  render(<><RootObserver/><Settings/></>);act(()=>{localStorage.setItem("umaguess:motion-preference","reduced");window.dispatchEvent(new StorageEvent("storage",{key:"umaguess:motion-preference",newValue:"reduced"}));});expect(screen.getByLabelText("root").textContent).toBe("reduced:true");expect(screen.getByLabelText("settings").textContent).toBe("reduced:true");
});
