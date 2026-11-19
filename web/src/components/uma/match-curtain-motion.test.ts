import { expect, it } from "vitest";
import { CenterWave, ClosingWave, openingAt } from "./match-curtain-motion";
it("quadrants close into one continuous viewport and clear it on opening", () => {
  for(const [w,h] of [[1440,900],[390,844],[320,568],[768,1024]]) {
    const panels=Array.from({length:4},()=>document.createElement("div"));
    const paths=Array.from({length:4},()=>document.createElementNS("http://www.w3.org/2000/svg","path"));
    const close=new ClosingWave(panels);close.resize(w!,h!);close.paint(1);
    for(const panel of panels)expect(panel.style.transform).toBe("translateX(0px)");
    const wave=new CenterWave(panels,paths);wave.resize(w!,h!);wave.paint(1);
    for(const panel of panels)expect(Math.abs(Number(panel.style.transform.match(/-?[\d.]+/)?.[0]))).toBeGreaterThan(h!/2);
    for(const p of [0,.1,.4,.7,1]) {
      const left=openingAt(0,p,w!,h!),right=openingAt(w!,p,w!,h!);
      expect(left.top).toBeCloseTo(right.top);expect(left.bottom).toBeCloseTo(right.bottom);
    }
  }
});
