import {expect,it} from "vitest";
import {networkQuality} from "./network-quality";
it("reports five bands, includes jitter and repeated failures without fabricating unmeasured quality",()=>{
 expect(networkQuality([])).toBeNull();
 expect([50,200,500,1000,2000].map(ms=>networkQuality([ms,ms,ms]))).toEqual(["excellent","good","fair","poor","bad"]);
 expect(networkQuality([40,50,60,70,2400])).toBe("poor");
 expect(networkQuality([50],1)).toBe("poor");expect(networkQuality([50],2)).toBe("bad");
 expect(networkQuality([50,50,50],0)).toBe("excellent");
});
