export function prepareMatchWork(challengeId:string,bits:number,signal:AbortSignal):Promise<string>{
  return new Promise((resolve,reject)=>{
    if(signal.aborted){reject(new DOMException("Aborted","AbortError"));return;}
    const worker=new Worker(new URL("./matchmaking-work.worker.ts",import.meta.url),{type:"module"});
    const finish=()=>{worker.terminate();clearTimeout(timer);signal.removeEventListener("abort",abort);};
    const abort=()=>{finish();reject(new DOMException("Aborted","AbortError"));};
    const timer=setTimeout(()=>{finish();reject(new Error("Admission preparation timed out"));},25000);
    signal.addEventListener("abort",abort,{once:true});
    worker.onmessage=(event:MessageEvent<{nonce?:string}>)=>{finish();if(event.data.nonce!==undefined)resolve(event.data.nonce);else reject(new Error("Admission preparation failed"));};
    worker.onerror=()=>{finish();reject(new Error("Admission preparation failed"));};
    worker.postMessage({challengeId,bits});
  });
}
