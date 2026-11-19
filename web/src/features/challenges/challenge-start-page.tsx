import { usePlayerName } from "@/lib/player-name";
import "@/i18n/challenge";
import "@/i18n/classic";
import { useTranslation } from "@/i18n";
import { useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Trophy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert } from "@/components/ui/alert";
import { useApi } from "@/lib/api";
import { ApiError, createIdempotencyKey, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import type { components } from "@/lib/api/schema";
import { newPracticeId } from "@/lib/local/practice-store";

type Request={key:string;body:components["schemas"]["ChallengeCreate"]};
export function ChallengeStartPage() {
 const displayName = usePlayerName();
 const {t}=useTranslation("challenge"),{t:tc}=useTranslation("classic");
 const {api,ensureSession}=useApi();const navigate=useNavigate();const queryClient=useQueryClient();
 const [budget,setBudget]=useState(8),[busy,setBusy]=useState(false),[failure,setFailure]=useState<Failure|null>(null),[uncertain,setUncertain]=useState(false);
 const request=useRef<Request|null>(null);
 const [previous]=useState(()=>{try{return localStorage.getItem("umaguess:last-challenge")??"";}catch{return "";}});
 async function start(){
  if(busy)return;setBusy(true);setFailure(null);
  try{
   const identity=await ensureSession(displayName);
   request.current??={key:createIdempotencyKey(),body:{modeId:"classic",clientTimestamp:Date.now(),nonce:newPracticeId(),wrongBudget:budget}};
   const response=await api.createChallenge(request.current.body,request.current.key);
   const challenge=response.data;if(!challenge)throw new Error(t("missingQuestion"));
   try{localStorage.setItem("umaguess:last-challenge",challenge.challengeId);}catch{/* Continuation link is optional, never a credential. */}
   // Seed only the in-memory cache. History state survives reload and would
   // otherwise resurrect the original, empty challenge instead of its progress.
   queryClient.setQueryData(["challenge",challenge.challengeId,identity.sessionId],{view:challenge,receivedAt:Date.now()});
   navigate(`/challenges/${challenge.challengeId}`);
  }catch(error){
   setFailure(describeFailure(error));
   const rejected=error instanceof ApiError && [400,401,403,422,429].includes(error.status);
   if(rejected){request.current=null;setUncertain(false);}else setUncertain(request.current!==null);
  }finally{setBusy(false);}
 }
 const failureText=failure?localizeFailure(failure):null;
 return <div className="uma-prose mx-auto space-y-5">
  <header><p className="text-sm font-bold text-[var(--color-action-green)]">{t("tag")}</p><h1 className="u-fluid-title mt-1">{t("title")}</h1><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("intro")}</p></header>
  <section className="uma-panel space-y-4 p-5"><div className="space-y-2"><Label htmlFor="challenge-budget">{tc("budget")}</Label><Input id="challenge-budget" className="w-28" type="number" min={4} max={12} value={budget} onChange={e=>setBudget(Number(e.target.value))} disabled={busy||uncertain}/></div>
   <Button variant="primary" size="primary" onClick={()=>void start()} disabled={busy||budget<4||budget>12||!Number.isInteger(budget)} block><Trophy className="size-4" aria-hidden="true"/>{busy?t("preparing"):uncertain?t("confirmStart"):t("start")}</Button>
   {uncertain?<Alert tone="info" title={t("uncertainStart")}>{t("uncertainStartDetail")}</Alert>:null}
   {failureText?<Alert fault tone="warning" title={failureText.title}><p>{failureText.detail}</p>{failureText.diagnosticId?<p className="mt-1">{t("diagnostic",{id:failureText.diagnosticId})}</p>:null}</Alert>:null}
   <div className="flex flex-wrap gap-2"><Button asChild variant="ghost"><Link to="/play/classic">{t("practice")}</Link></Button>{/^chl_[a-f0-9]{32}$/.test(previous)?<Button asChild variant="secondary"><Link to={`/challenges/${previous}`}>{t("continue")}</Link></Button>:null}</div>
  </section>
  <details className="uma-card uma-disclosure p-4"><summary className="cursor-pointer font-bold">{t("rules")}</summary><div className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]"><p>{t("rule1")}</p><p>{t("rule2")}</p><p>{t("rule3")}</p></div></details>
 </div>;
}
