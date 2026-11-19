import { satisfiesWork } from "./matchmaking-hash";
self.onmessage=(event:MessageEvent<{challengeId:string;bits:number}>)=>{
  const {challengeId,bits}=event.data;
  if (!/^mwp_[a-zA-Z0-9]+$/.test(challengeId) || challengeId.length>36 || !Number.isInteger(bits) || bits<12 || bits>20) { self.postMessage({error:true});return; }
  const deadline=performance.now()+24000;
  for(let n=0;n<2**26;n++){
    if (satisfiesWork(challengeId,String(n),bits)) { self.postMessage({nonce:String(n)});return; }
    if(n%4096===0 && performance.now()>deadline)break;
  }
  self.postMessage({error:true});
};
