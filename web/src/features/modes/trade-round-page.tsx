import { roomDisplayName } from "@/lib/room-display-name";
import { GameStage } from "@/components/uma/game-stage";
import { SelectField } from "@/components/ui/select";
import "@/i18n/trade-online";
import {useMemo, useState} from "react";
import {Link, useParams} from "react-router";
import type {TFunction} from "i18next";
import {useTranslation} from "@/i18n";
import {usePlayerText} from "@/i18n/player-text";
import {tradeCardLabel, tradeObjectiveText} from "@/i18n/trade-content";
import {ConnectionBar} from "@/components/uma/connection-bar";
import {EmptyView, ErrorView, ForbiddenView, LoadingBlock} from "@/components/uma/page-state";
import {Alert} from "@/components/ui/alert";
import {Button} from "@/components/ui/button";
import {useApi} from "@/lib/api";
import {ApiError, describeFailure, localizeFailure} from "@/lib/api/client";
import {validTradeCommand, type TradeCommand} from "@/lib/api/game-commands";
import {incomingPending, normalizeTradeRound, outgoingPending, type TradeRecord, type TradeRound} from "@/lib/api/trade";
import {useRoom} from "@/lib/api/room-queries";
import {phaseLabel, useRoundSnapshot} from "@/lib/api/round-snapshot";
import {useCountdown} from "@/lib/hooks/use-countdown";
import {useGameCommand} from "@/lib/hooks/use-game-command";
import {emptyTradeDraft, loadTradeDraft, saveTradeDraft, type TradeDraft} from "@/lib/local/trade-online-draft";
import {useRealtime} from "@/lib/realtime/use-realtime";
import {cn} from "@/lib/utils";
import {RejoinPrompt, isCredentialRefreshFailure} from "./rejoin-prompt";

type TradeTranslate = TFunction<"tradeOnline">;
/** 把服务端的交易错误翻译成玩家能懂的话，但不掩盖失败 */
export function tradeFailureMessage(error: unknown, t?: TradeTranslate): { title: string; detail: string } {
  const copy = (key: string, fallback: string) => t ? t(key) : fallback;
  if (error instanceof ApiError) {
    switch (error.code) {
      case "card-not-held":
        return {
          title: copy("errors.cardNotHeldTitle", "这张牌不在你手里"),
          detail: copy("errors.cardNotHeldDetail", "你出价的那张牌已经不在你的手牌里了。刷新后重新选择"),
        };
      case "trade-closed":
      case "trade-not-pending":
        return {
          title: copy("errors.closedTitle", "这条提案已经结束"),
          detail: copy("errors.closedDetail", "对方可能已经回应或提案已过期。刷新后查看最新状态"),
        };
      case "trade-expired":
        return {
          title: copy("errors.expiredTitle", "这条提案已经过期"),
          detail: copy("errors.expiredDetail", "上一轮的提案已收起。请在当前轮次重新商量"),
        };
      case "insufficient-cards":
        return {
          title: copy("errors.insufficientTitle", "手牌不够"),
          detail: copy("errors.insufficientDetail", "你没有可以出价的牌"),
        };
      default:
        // Unknown problem details are internal diagnostics, not reviewed player copy.
        break;
    }
  }
  const info = localizeFailure(describeFailure(error));
  return { title: info.title, detail: info.detail };
}


function TradeCard({trade,labelOf,personOf,disabled,onRespond}:{trade:TradeRecord;labelOf:(id:string)=>string;personOf:(id:string)=>string;disabled:boolean;onRespond?:(accept:boolean)=>void}) {
 const {t}=useTranslation("tradeOnline");
 const status=["pending","accepted","rejected","declined","expired","cancelled"].includes(trade.status)?trade.status:"ended";
 return <li className={cn("min-w-0 rounded-[var(--radius-control)] border p-3",status==="pending"?"border-[var(--color-near-edge)] bg-[var(--color-panel)]":"border-[var(--color-line)]")}>
  <div className="flex flex-wrap justify-between gap-2 text-sm"><h3 className="font-bold">{t(trade.isIncoming?"incoming":trade.isOutgoing?"outgoing":"partnerProposal")}</h3><span>{t(`status.${status}`)}</span></div>
  <p className="mt-2 break-words text-sm text-[var(--color-muted)]">{personOf(trade.senderSubjectId)} → {personOf(trade.recipientSubjectId)}</p>
  <dl className="mt-2 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-sm"><dt>{t("give")}</dt><dd className="break-words font-semibold">{labelOf(trade.offeredInstanceId)}</dd><dt>{t("receive")}</dt><dd className="break-words font-semibold">{labelOf(trade.requestedInstanceId)}</dd></dl>
  <p className="mt-2 text-xs text-[var(--color-muted)]">{trade.round?t("recordRound",{round:trade.round}):t("roundUnknown")}</p>
  {onRespond&&trade.isIncoming&&status==="pending"?<div className="mt-3 flex flex-wrap gap-2"><Button className="h-auto min-h-11 flex-1 whitespace-normal" variant="primary" disabled={disabled} onClick={()=>onRespond(true)}>{t("accept")}</Button><Button className="h-auto min-h-11 flex-1 whitespace-normal" variant="secondary" disabled={disabled} onClick={()=>onRespond(false)}>{t("decline")}</Button></div>:null}
 </li>;
}
function ResultPanel({round,name}:{round:TradeRound;name:(id:string)=>string}) {
 const {t}=useTranslation("tradeOnline");
 if(!round.result.length)return <p role="status">{t("unknownResult")}</p>;
 function goals(p:TradeRound["result"][number]) {return <ul className="mt-1 space-y-1">{[...p.satisfiedIds,...p.unsatisfiedIds].map(id=>{const o=round.objectives.find(o=>o.objectiveId===id);return o?<li key={id} className="break-words"><span className="font-medium">{t(p.satisfiedIds.includes(id)?"achieved":"notAchieved")}</span>：{tradeObjectiveText(o).text}</li>:null;})}</ul>;}
 function formal(id:string) {const s=round.scores.find(s=>s.subjectId===id);return s?t("points",{points:s.points}):"—";}
 return <section className="uma-panel min-w-0 p-4" aria-labelledby="trade-result"><h2 id="trade-result" className="text-xl font-bold">{t("results")}</h2>{!round.competitive?<p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("entertainment")}</p>:null}{!round.scores.length?<p className="mt-2 text-sm" role="status">— {t("waitingScore")}</p>:null}
  <table className="mt-4 hidden w-full table-fixed text-left text-sm md:table"><caption className="sr-only">{t("results")}</caption><thead><tr><th className="w-1/4 p-2">{t("participant")}</th><th className="p-2">{t("completed")}</th><th className="w-[15%] p-2">{t("taskScore")}</th><th className="w-[15%] p-2">{t("formal")}</th></tr></thead><tbody>{round.result.map(p=><tr key={p.subjectId} className="border-t border-[var(--color-line)]"><th scope="row" className="break-words p-2 align-top">{name(p.subjectId)}</th><td className="p-2 align-top">{t("completedCount",p)}{goals(p)}</td><td className="p-2 align-top">{t("points",{points:p.points})}</td><td className="p-2 align-top">{formal(p.subjectId)}</td></tr>)}</tbody></table>
  <ul className="mt-4 space-y-4 md:hidden">{round.result.map(p=><li key={p.subjectId} className="border-t border-[var(--color-line)] pt-3 text-sm"><h3 className="break-words font-bold">{name(p.subjectId)}</h3><p className="mt-2">{t("completed")}：{t("completedCount",p)}</p>{goals(p)}<dl className="mt-2 grid grid-cols-2 gap-2"><dt>{t("taskScore")}</dt><dd>{t("points",{points:p.points})}</dd><dt>{t("formal")}</dt><dd>{formal(p.subjectId)}</dd></dl></li>)}</ul>
 </section>;
}
export function TradeRoundPage() {
 const {t,i18n}=useTranslation("tradeOnline"),{roomId}=useParams<{roomId:string}>(),{api,session}=useApi();
 const roomQuery=useRoom(roomId??null),room=roomQuery.data?.room,roundId=room?.currentRound?.roundId??null,snapshot=useRoundSnapshot(roundId);
 const round=useMemo(()=>snapshot.raw&&snapshot.meta?.roundId===roundId?normalizeTradeRound(snapshot.raw):null,[snapshot.raw,snapshot.meta?.roundId,roundId]);
 const commandScope=session&&roundId?`${session.sessionId}:${roundId}:trade`:null;
 const command=useGameCommand(commandScope,validTradeCommand,(body,key)=>body.kind==="propose"?api.createTrade(roundId!,body.body,key):api.respondToTrade(roundId!,body.tradeId,body.body,key));
 const draftScope=commandScope&&round?.selfSubjectId?`${commandScope}:${round.selfSubjectId}:${round.currentRound}`:null;
 const [draft,setDraft]=useState<TradeDraft>(emptyTradeDraft),[initialized,setInitialized]=useState<string|null>(null);
 if(initialized!==draftScope){setInitialized(draftScope);setDraft(draftScope?loadTradeDraft(draftScope)??emptyTradeDraft:emptyTradeDraft);}
 const [refreshing,setRefreshing]=useState(false),[failure,setFailure]=useState<unknown>(null),[notice,setNotice]=usePlayerText("tradeOnline");
 const phase=snapshot.meta?.phase??null,cancelled=phase==="cancelled",published=phase==="revealed"||phase==="settled";
 const collecting=phase==="collecting"&&!snapshot.meta?.paused,countdown=useCountdown(snapshot.meta?.deadlineAt,collecting);
 const busy=command.busy||refreshing||!!command.pending||command.expired||snapshot.isError||roomQuery.isError;
 const canPlay=!!round?.selfSubjectId&&snapshot.canSubmit!==false&&collecting&&!countdown.expired&&!busy;
 const offeredId=draft.offeredId&&round?.selfHand.includes(draft.offeredId)?draft.offeredId:null;
 const recipientId=draft.recipientId&&round?.hands.has(draft.recipientId)&&draft.recipientId!==round.selfSubjectId?draft.recipientId:null;
 const requestedId=recipientId&&draft.requestedId&&round?.hands.get(recipientId)?.includes(draft.requestedId)?draft.requestedId:null;
 const incoming=round?incomingPending(round):[],outgoing=round?outgoingPending(round):[];
 const realtime=useRealtime({roomId:roomId??null,enabled:!!roomId,handlers:{onSnapshotNeeded:()=>{void snapshot.refetch();void roomQuery.refetch();}}});
 const message=failure?tradeFailureMessage(failure,t):null;
 function personOf(id:string) {const m=room?.members.find(m=>m.membershipId===id);return m?`${m.nickname??m.displayName}${/^\d{6}$/.test(m.publicCode??"")?` (${t("roomId",{id:m.publicCode})})`:""}`:t("partner");}
 function labelOf(id:string) {const c=round?.instances.find(c=>c.instanceId===id);return c?(tradeCardLabel(c.cardId,c.label,i18n.language).text||t("missingCard")):t("missingCard");}
 function choose(part:Partial<TradeDraft>) {const next={offeredId,recipientId,requestedId,...part};setDraft(next);setNotice("");if(draftScope&&!saveTradeDraft(draftScope,next))setNotice("saveFailed");}
 async function send(body?:TradeCommand) {
  if(!roundId||command.busy||refreshing||!body&&!command.pending)return;
  if(body&&!canPlay)return;
  setRefreshing(true);setFailure(null);setNotice("");
  try {const result=await command.run(body);if(!result)return;if(!result.ok){setFailure(result.error);if(!result.uncertain)await snapshot.refetch();return;}setNotice(body?.kind==="propose"?"proposalSent":body?.kind==="respond"?body.body.accept?"acceptedNotice":"declinedNotice":"restored");await snapshot.refetch();}finally{setRefreshing(false);}
 }
 function propose() {if(!canPlay||outgoing.length)return;if(!offeredId||!recipientId||!requestedId){setNotice("chooseAll");return;}void send({kind:"propose",body:{phaseVersion:snapshot.meta!.phaseVersion,recipientMembershipId:recipientId,offeredInstanceId:offeredId,requestedInstanceId:requestedId}});}
 function respond(trade:TradeRecord,accept:boolean){if(!canPlay||trade.round!==round?.currentRound)return;void send({kind:"respond",tradeId:trade.tradeId,body:{accept}});}
 const choice=(id:string,selected:boolean,onClick:()=>void)=><button type="button" onClick={onClick} disabled={!canPlay||!!outgoing.length} aria-label={t("chooseCard",{label:labelOf(id)})} aria-pressed={selected} className={cn("min-h-11 w-full min-w-0 whitespace-normal break-words rounded-[var(--radius-control)] border px-3 py-2 text-left text-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",selected?"border-[var(--color-blue-deep)] bg-[var(--color-page-tint)] font-bold":"border-[var(--color-line-strong)] bg-[var(--color-panel)]")}>{labelOf(id)}{selected?<span className="ml-2 text-xs">✓</span>:null}</button>;
 if(!roomId)return <ErrorView failure={{kind:"error",title:t("missingRoom"),detail:t("missingRoomDetail"),diagnosticId:null,retryable:false}}/>;
 const cardList=(ids:string[])=>ids.map(labelOf).join(" / ");
 const rawError=(error:unknown)=>describeFailure(error).kind==="forbidden"?<ForbiddenView failure={describeFailure(error)} onRetry={()=>{void snapshot.refetch();void roomQuery.refetch();}}/>:<ErrorView failure={describeFailure(error)} onRetry={()=>{void snapshot.refetch();void roomQuery.refetch();}}/>;
 return <GameStage mode="trade" className="uma-stack">
  <ConnectionBar status={realtime.status} detail={roomDisplayName(room, t("room"))}/>
  {isCredentialRefreshFailure(snapshot.error)||isCredentialRefreshFailure(failure)?<RejoinPrompt roomId={roomId}/>:null}
  {roomQuery.isLoading||snapshot.isLoading?<LoadingBlock label={t("loading")} rows={3}/>:null}
  {roomQuery.isError?rawError(roomQuery.error):null}{snapshot.isError?rawError(snapshot.error):null}
  {command.pending?<Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button variant="secondary" className="mt-3 h-auto min-h-11 whitespace-normal" disabled={refreshing||command.busy} onClick={()=>void send()}>{t("confirmAction")}</Button></Alert>:null}
  {command.busy?<p role="status" className="text-sm">{t("confirmBusy")}</p>:null}
  {command.expired?<Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" className="mt-3 h-auto min-h-11 whitespace-normal" disabled={refreshing} onClick={()=>void(async()=>{setRefreshing(true);try{if(await snapshot.refetch())command.acknowledgeExpired();}finally{setRefreshing(false);}})()}>{t("refresh")}</Button></Alert>:null}
  {command.storageLimited?<Alert tone="warning" title={t("notice")}>{t("storage")}</Alert>:null}
  {message&&!command.pending?<Alert tone="danger" title={message.title}>{message.detail}</Alert>:null}{notice?<p role="status" className="text-sm leading-6">{notice}</p>:null}
  {round?<>
   <header className="border-b border-[var(--color-line)] pb-4"><h1 className="u-fluid-task">{t("title")}</h1><p className="mt-2 max-w-prose text-sm leading-6 text-[var(--color-muted)]">{t("intro")}</p><p className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm"><span>{phaseLabel(phase)}</span><span>{t("round",{round:round.currentRound})}</span>{collecting&&countdown.text?<span className="font-bold">{countdown.text}</span>:null}</p></header>
   {cancelled?<Alert tone="info" title={t("cancelled")}>{t("cancelledDetail")}</Alert>:null}
   {!cancelled&&!published?<p role="status" className="text-sm leading-6">{snapshot.meta?.paused?t("paused"):countdown.expired&&collecting?t("deadline"):phase==="locked"?t("waitingReveal"):phase!=="collecting"?t("beforeStart"):!round.selfSubjectId?t("observer"):outgoing.length?t("openProposal"):null}</p>:null}
   {!cancelled&&!published&&incoming.length?<section className="uma-panel min-w-0 p-4" aria-labelledby="trade-incoming"><h2 id="trade-incoming" className="text-xl font-bold">{t("incomingHeading",{count:incoming.length})}</h2><ul className="mt-3 grid gap-3 md:grid-cols-2">{incoming.map(trade=><TradeCard key={trade.tradeId} trade={trade} labelOf={labelOf} personOf={personOf} disabled={!canPlay||trade.round!==round.currentRound} onRespond={accept=>respond(trade,accept)}/>)}</ul></section>:null}
   {published?<ResultPanel round={round} name={personOf}/>:null}
   {!cancelled&&!published&&round.selfSubjectId?<div className="grid min-w-0 items-start gap-4 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
    <section className="uma-panel min-w-0 p-4" aria-labelledby="trade-propose"><h2 id="trade-propose" className="text-xl font-bold">{t("propose")}</h2><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("proposeHint")}</p><h3 className="mt-4 font-semibold">{t("hand")}</h3><ul className="mt-2 grid gap-2 sm:grid-cols-2">{round.selfHand.map(id=><li key={id}>{choice(id,id===offeredId,()=>choose({offeredId:id===offeredId?null:id}))}</li>)}</ul>
     <label htmlFor="trade-recipient" className="mt-4 block font-semibold">{t("recipient")}</label><SelectField id="trade-recipient" disabled={!canPlay||!!outgoing.length} value={recipientId??""} onValueChange={value =>choose({recipientId:value||null,requestedId:null})} className="mt-2" options={[({value:"",label:t("recipient")}),...[...round.hands.keys()].filter(id=>id!==round.selfSubjectId).map(id=>({value:id,label:personOf(id)}))]} />
     {recipientId?<><h3 className="mt-4 font-semibold">{t("wanted")}</h3><ul className="mt-2 grid gap-2 sm:grid-cols-2">{(round.hands.get(recipientId)??[]).map(id=><li key={id}>{choice(id,id===requestedId,()=>choose({requestedId:id===requestedId?null:id}))}</li>)}</ul></>:null}
     <div className="mt-4 flex flex-wrap gap-2"><Button variant="primary" size="primary" className="h-auto min-h-11 flex-1 whitespace-normal" disabled={!canPlay||!!outgoing.length||!offeredId||!recipientId||!requestedId} onClick={propose}>{t("send")}</Button><Button variant="secondary" className="h-auto min-h-11 whitespace-normal" disabled={!canPlay} onClick={()=>choose(emptyTradeDraft)}>{t("clear")}</Button></div>
     {outgoing.length?<p className="mt-3 text-sm leading-6">{t("outgoingHint")}</p>:null}
    </section>
    <aside className="uma-panel min-w-0 p-4" aria-labelledby="trade-objectives"><h2 id="trade-objectives" className="font-bold">{t("objectives")}</h2><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("objectivesHint")}</p><ul className="mt-3 space-y-3">{round.selfObjectives.map(o=><li key={o.objectiveId} className="border-t border-[var(--color-line)] pt-3 text-sm"><p className="break-words leading-6">{tradeObjectiveText(o).text}</p><p className="mt-1 font-semibold">{t("points",{points:o.points})} · {t(o.satisfied?"achieved":"notAchieved")}</p></li>)}</ul><p className="mt-3 text-xs leading-5 text-[var(--color-muted)]">{t("preview")}</p></aside>
   </div>:null}
   <section className="uma-panel min-w-0 p-4" aria-labelledby="trade-public-hands"><h2 id="trade-public-hands" className="font-bold">{t("publicHands")}</h2><table className="mt-3 hidden w-full table-fixed text-left text-sm md:table"><caption className="sr-only">{t("publicHands")}</caption><thead><tr><th className="w-1/3 p-2">{t("participant")}</th><th className="p-2">{t("cardsHeading")}</th></tr></thead><tbody>{[...round.hands].map(([id,hand])=><tr key={id} className="border-t border-[var(--color-line)]"><th scope="row" className="break-words p-2 font-medium">{personOf(id)}</th><td className="break-words p-2 leading-6">{cardList(hand)}</td></tr>)}</tbody></table><ul className="mt-3 space-y-3 md:hidden">{[...round.hands].map(([id,hand])=><li key={id} className="border-t border-[var(--color-line)] pt-3 text-sm"><h3 className="break-words font-semibold">{personOf(id)}</h3><p className="mt-1 break-words leading-6">{cardList(hand)}</p></li>)}</ul></section>
   <details className="uma-panel min-w-0 p-4" open={!!outgoing.length&&!cancelled&&!published}><summary className="min-h-11 cursor-pointer content-center font-semibold">{t("history")} ({round.trades.length})</summary>{round.trades.length?<ul className="mt-3 grid gap-3 md:grid-cols-2">{[...round.trades].reverse().map(trade=><TradeCard key={trade.tradeId} trade={trade} labelOf={labelOf} personOf={personOf} disabled/>)}</ul>:<p className="mt-2 text-sm">{t("noHistory")}</p>}</details>
   <details className="uma-panel min-w-0 p-4"><summary className="min-h-11 cursor-pointer content-center font-semibold">{t("rules")}</summary><div className="mt-2 max-w-prose space-y-2 text-sm leading-6 text-[var(--color-muted)]"><p>{t("timing",{rounds:round.rounds,seconds:round.roundSeconds})}</p><p>{t("privacy")}</p><p>{t("reuse")}</p>{!round.competitive?<p>{t("entertainment")}</p>:null}</div><details className="mt-3"><summary className="min-h-11 cursor-pointer content-center text-sm font-semibold">{t("allObjectives")}</summary><p className="mt-2 text-sm text-[var(--color-muted)]">{t("allObjectivesHint")}</p><ul className="mt-3 grid gap-3 sm:grid-cols-2">{round.objectives.map(o=><li key={o.objectiveId} className="border-t border-[var(--color-line)] pt-2 text-sm"><p className="break-words leading-6">{tradeObjectiveText(o).text}</p><p>{t("points",{points:o.points})}</p></li>)}</ul></details></details>
   {round.instances.some(c=>tradeCardLabel(c.cardId,c.label,i18n.language).original)||round.objectives.some(o=>tradeObjectiveText(o,i18n.language).original)?<p className="text-xs leading-5 text-[var(--color-muted)]">{t("original")}</p>:null}
  </>:!snapshot.isLoading&&!roomQuery.isLoading&&!snapshot.isError&&!roomQuery.isError?<EmptyView title={t("emptyTitle")} detail={t("emptyDetail")}/>:null}
  <Button asChild variant="secondary" className="h-auto min-h-11 whitespace-normal"><Link to="/">{t("back")}</Link></Button>
 </GameStage>;
}
