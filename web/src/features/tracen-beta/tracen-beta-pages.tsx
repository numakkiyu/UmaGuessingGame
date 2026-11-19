import "@/i18n/tracen-beta";
import "@/styles/tracen-beta.css";
import type { ReactNode } from "react";
import { BookOpen, Check, Clock3, Flag, Trophy, UserRound } from "lucide-react";
import { useTranslation } from "@/i18n";
import { Button } from "@/components/ui/button";
import { SelectField } from "@/components/ui/select";
import { GameCompanion } from "@/components/uma/game-stage";
import { TrainerAvatar } from "@/components/uma/trainer-avatar";
import { useTrainerProfile } from "@/lib/trainer-profile";
import { AccountButtons, AccountStatus } from "@/features/account/account-ui";
import type { PublicConfig } from "@/lib/api/types";
import type { BoardView, TrainerStanding } from "./types";

export function BetaBadge() {
  const { t } = useTranslation("tracenBeta");
  return <span className="uma-beta-badge">{t("beta")}</span>;
}
export function AccountPanel({ config }: { config: NonNullable<PublicConfig["accounts"]> }) {
  const { t } = useTranslation("tracenBeta");
  const trainer = useTrainerProfile();
  return <section className="uma-beta-account uma-panel" aria-labelledby="beta-account-heading">
    <div className="uma-beta-heading"><UserRound aria-hidden="true" /><h2 id="beta-account-heading">{t("account")}</h2></div>
    <div className="uma-beta-trainer"><TrainerAvatar id={trainer.avatarId} /><span><strong>{trainer.displayName || t("guest")}</strong><AccountStatus /></span></div>
    <AccountButtons config={config} />
  </section>;
}

type ChallengeState =
  | { state: "intro"; maxWrong: number; minutes: number }
  | { state: "loading" }
  | { state: "active"; remainingWrong: number; remainingTime: string }
  | { state: "result"; score: number };

export function ChallengePanel(props: ChallengeState & {
  authenticated: boolean; onConnect: () => void; onStart: () => void; onBoard: () => void; children?: ReactNode;
}) {
  const { state, authenticated, onConnect, onStart, onBoard, children } = props;
  const { t } = useTranslation("tracenBeta");
  return <section key={state} className="uma-beta-challenge uma-panel uma-beta-reveal" aria-labelledby="beta-challenge-heading" aria-busy={state === "loading"}>
    <div className="uma-beta-heading"><Flag aria-hidden="true" /><h2 id="beta-challenge-heading">{t("challengeMode")}</h2><BetaBadge /></div>
    {props.state === "intro" ? <>
      <div className="uma-beta-invitation"><div><h3>{t("challengeIntro")}</h3></div><GameCompanion mode="classic" /></div>
      <ul className="uma-beta-rules"><li><Check aria-hidden="true" />{t("ruleWrong", { count: props.maxWrong })}</li><li><Clock3 aria-hidden="true" />{t("ruleTime", { minutes: props.minutes })}</li><li><Trophy aria-hidden="true" />{t("ruleTie")}</li></ul>
      <Button variant="primary" size="primary" block onClick={authenticated ? onStart : onConnect}>{t(authenticated ? "start" : "signIn")}</Button>
    </> : props.state === "loading" ? <div className="uma-beta-state" role="status"><div className="uma-beta-wait" aria-hidden="true"><span /><span /><span /></div><h3>{t("loading")}</h3></div>
    : props.state === "result" ? <div className="uma-beta-state"><Trophy className="uma-beta-trophy" aria-hidden="true" /><h3>{t("resultTitle")}</h3><p className="uma-beta-score">{t("points", { score: props.score })}</p><Button variant="primary" onClick={onBoard}>{t("resultBoard")}</Button></div>
    : <><div className="uma-beta-round-status"><span>{t("remaining", { count: props.remainingWrong })}</span><span><Clock3 aria-hidden="true" />{t("time")} <b className="tabular-nums">{props.remainingTime}</b></span></div>{children}</>}
  </section>;
}
export function TrainerLeaderboard({ state, entries, period, periods, onPeriodChange, personalRecord, onRetry }: {
  state: BoardView;
  entries: readonly TrainerStanding[];
  period: string;
  periods: readonly { value: string; label: string }[];
  onPeriodChange: (value: string) => void;
  personalRecord?: { rank: number; score: number } | null;
  onRetry: () => void;
}) {
  const { t } = useTranslation("tracenBeta");
  const empty = state === "empty" || (state === "entries" && entries.length === 0);
  return <section className="uma-beta-board uma-panel" aria-labelledby="beta-board-heading">
    <div className="uma-beta-board-header"><div><div className="uma-beta-heading"><Trophy aria-hidden="true" /><h2 id="beta-board-heading">{t("board")}</h2><BetaBadge /></div><p>{t("boardScope")}</p></div><label><span>{t("period")}</span><SelectField value={period} onValueChange={onPeriodChange} options={[...periods]} /></label></div>
    <p className="uma-beta-note">{t("boardRule")}</p>
    <div key={`${period}-${state}`} className="uma-beta-reveal" aria-busy={state === "loading"}>
      {state === "loading" ? <div className="uma-beta-state" role="status"><div className="uma-beta-wait" aria-hidden="true"><span /><span /><span /></div><h3>{t("boardLoading")}</h3></div>
      : state === "unavailable" ? <div className="uma-beta-state" role="status"><BookOpen aria-hidden="true" /><h3>{t("boardError")}</h3><p>{t("boardErrorDetail")}</p><Button onClick={onRetry}>{t("retry")}</Button></div>
      : empty ? <div className="uma-beta-state"><GameCompanion mode="sort" /><h3>{t("empty")}</h3><p>{t("emptyDetail")}</p></div>
      : <><table className="uma-beta-table"><caption className="sr-only">{t("board")}</caption><thead><tr><th scope="col">{t("rank")}</th><th scope="col">{t("trainer")}</th><th scope="col">{t("score")}</th></tr></thead><tbody>{entries.map(entry => <tr key={entry.publicPlayerId}><td>{entry.rank}</td><td><div className="uma-beta-trainer"><TrainerAvatar id={entry.avatarId} size={44} /><span><strong>{entry.displayName}</strong><small>{t("publicId", { id: entry.publicPlayerId })}</small></span></div></td><td>{t("points", { score: entry.score })}</td></tr>)}</tbody></table>
      <ol className="uma-beta-mobile-board" aria-label={t("board")}>{entries.map(entry => <li key={entry.publicPlayerId}><span className="uma-beta-rank"><span className="sr-only">{t("rank")}</span>{entry.rank}</span><div className="uma-beta-trainer"><TrainerAvatar id={entry.avatarId} size={44} /><span><strong>{entry.displayName}</strong><small>{t("publicId", { id: entry.publicPlayerId })}</small></span></div><strong>{t("points", { score: entry.score })}</strong></li>)}</ol></>}
    </div>
    {personalRecord !== undefined ? <aside className="uma-beta-personal"><h3>{t("record")}</h3>{personalRecord ? <><strong>{t("position", { rank: personalRecord.rank })}</strong><p>{t("points", { score: personalRecord.score })}</p></> : <strong>{t("noRecord")}</strong>}</aside> : null}
  </section>;
}
