import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { ArrowLeft, ArrowRight, BookOpen, Clock3 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Alert } from "@/components/ui/alert";
import { GameStage } from "@/components/uma/game-stage";
import { useConfirmation } from "@/lib/confirmation";
import { usePlayerName } from "@/lib/player-name";
import { useTranslation } from "@/i18n";
import "@/i18n/concentration";
import "@/styles/concentration.css";
import { FIELDS, QUESTION_COUNT, STORAGE_KEY, createGame, decodeGame, hasAnswer,
  profiles, questions, type ConcentrationGame, type Field, type Profile } from "./engine";

import { ConcentrationResults } from "./concentration-results";

type Phase = "ready" | "play" | "result";
type Notice = "storageError" | "restoreError" | "";
function load(): { game: ConcentrationGame | null; notice: Notice } {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    const game = raw ? decodeGame(raw) : null;
    return { game, notice: raw && !game ? "restoreError" : "" };
  } catch { return { game: null, notice: "storageError" }; }
}
function formatTime(milliseconds: number) {
  const seconds = Math.max(0, Math.floor(milliseconds / 1000));
  return `${Math.floor(seconds / 60).toString().padStart(2, "0")}:${(seconds % 60).toString().padStart(2, "0")}`;
}
function Elapsed({ game }: { game: ConcentrationGame }) {
  const { t } = useTranslation("concentration");
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (game.finishedAt !== null) return;
    const interval = window.setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(interval);
  }, [game.finishedAt]);
  return <span className="uma-concentration-time"><Clock3 size={16} aria-hidden="true" /><span>{t("elapsed")}</span><span className="tabular-nums">{formatTime((game.finishedAt ?? now) - game.startedAt)}</span></span>;
}
function Rules() {
  const { t } = useTranslation("concentration");
  return <details className="uma-card uma-concentration-rules"><summary><BookOpen size={18} aria-hidden="true" />{t("rulesTitle")}</summary><ol>{(t("rules", { returnObjects: true }) as string[]).map((rule, i) => <li key={i}>{rule}</li>)}</ol></details>;
}
function Portrait({ profile }: { profile: Profile }) {
  const { t } = useTranslation("concentration");
  const [failed, setFailed] = useState(false), [retry, setRetry] = useState(0);
  return <div className="uma-concentration-portrait">
    <div className="uma-concentration-portrait-backdrop" style={{ backgroundImage: `url("${profile.imagePath}")` }} aria-hidden="true" />
    {failed ? <Alert tone="warning" title={t("imageError")} action={<Button variant="secondary" onClick={() => { setFailed(false); setRetry(n => n + 1); }}>{t("retry")}</Button>} /> :
      <img key={retry} src={`${profile.imagePath}${retry ? `?retry=${retry}` : ""}`} width={profile.imageWidth} height={profile.imageHeight} alt={t("portrait")} onError={() => setFailed(true)} loading="eager" decoding="async" />}
  </div>;
}

export function ConcentrationPage() {
  const { t } = useTranslation("concentration");
  const playerName = usePlayerName();
  const [initial] = useState(load);
  const [game, setGame] = useState(initial.game);
  const [notice, setNotice] = useState<Notice>(initial.notice);
  const [phase, setPhase] = useState<Phase>(initial.game ? initial.game.finishedAt === null ? "play" : "result" : "ready");
  const [revealResult, setRevealResult] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const confirm = useConfirmation(`${phase}:${game?.startedAt ?? ""}`);
  const heading = useRef<HTMLHeadingElement>(null);
  const gameSeed = game?.seed;
  const drawn = useMemo(() => gameSeed === undefined ? [] : questions(gameSeed), [gameSeed]);
  const current = game ? drawn[game.activeIndex]! : null;
  const count = game?.answers.filter(hasAnswer).length ?? 0;
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, [phase, game?.activeIndex]);

  function commit(next: ConcentrationGame) {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(next)); }
    catch { setNotice("storageError"); }
    setGame(next);
  }
  async function start() {
    if (confirming) return;
    setConfirming(true);
    try {
      if (game && !(await confirm({ ns: "concentration", key: "replayConfirm" }, "prepare", { title: { ns: "concentration", key: "again" }, action: { ns: "concentration", key: "start" } }))) return;
      const random = crypto.getRandomValues(new Uint32Array(1))[0]!;
      setRevealResult(false); setNotice(""); commit(createGame(random)); setPhase("play");
      window.scrollTo({ top: 0, behavior: "instant" });
    } finally { setConfirming(false); }
  }
  async function finish() {
    if (!game || game.finishedAt !== null || confirming) return;
    setConfirming(true);
    try {
      if (await confirm({ ns: "concentration", key: "confirm", values: { total: QUESTION_COUNT, blank: QUESTION_COUNT - count } }, "submit", { title: { ns: "concentration", key: "submit" }, action: { ns: "concentration", key: "submit" } })) {
        commit({ ...game, finishedAt: Math.max(Date.now(), game.startedAt) });
        setRevealResult(true); setPhase("result"); window.scrollTo({ top: 0, behavior: "instant" });
      }
    } finally { setConfirming(false); }
  }
  function update(field: Field, value: string) {
    if (game && game.finishedAt === null) commit({ ...game, answers: game.answers.map((answer, i) => i === game.activeIndex ? { ...answer, [field]: value.slice(0, 120) } : answer) });
  }
  function navigate(index: number) {
    if (confirming) return;
    if (game && game.finishedAt === null && index >= 0 && index < QUESTION_COUNT) commit({ ...game, activeIndex: index });
    window.scrollTo({ top: 0, behavior: "instant" });
  }
  function birthdayPart(part: number, value: string) {
    value = value.normalize("NFKC");
    if (!game || !/^\d{0,2}$/.test(value)) return;
    const pieces = game.answers[game.activeIndex]!.birthday.split("/");
    pieces[part] = value;
    update("birthday", pieces.some(Boolean) ? `${pieces[0] ?? ""}/${pieces[1] ?? ""}` : "");
  }

  return <GameStage mode="concentration" className="uma-concentration uma-stack" illustration={false}>
    {notice ? <Alert tone="warning" title={t(notice)} /> : null}
    {phase === "ready" ? <section className="uma-panel uma-concentration-welcome">
      <img src="/assets/ui/branding/party-logo.webp" width={88} height={88} alt="" />
      <h1 ref={heading} tabIndex={-1}>{t("title")}</h1><p>{t("intro")}</p>
      <div className="uma-concentration-tags"><span>{t("pool", { count: profiles.length })}</span><span>{t("format")}</span><span>{t("untimed")}</span></div>
      <div className="uma-concentration-player"><span>{t("nickname")}</span><strong>{playerName}</strong></div>
      <Button variant="primary" size="primary" block onClick={() => void start()} disabled={confirming}>{t("start")}<ArrowRight size={18} aria-hidden="true" /></Button>
      {game ? <Button variant="link" onClick={() => setPhase(game.finishedAt === null ? "play" : "result")}>{t(game.finishedAt === null ? "resume" : "results")}</Button> : null}
    </section> : null}
    {phase === "play" && game && current ? <>
      <div className="uma-concentration-game-heading"><h1 ref={heading} tabIndex={-1}>{t("title")}</h1><Elapsed game={game} /></div>
      <div className="uma-concentration-progress"><span aria-live="polite">{game.activeIndex + 1} / {QUESTION_COUNT}</span>{notice !== "storageError" ? <span>{t("draft")}</span> : null}<progress max={QUESTION_COUNT} value={count} aria-label={t("done", { count, total: QUESTION_COUNT })} /></div>
      <Portrait key={current.id} profile={current} />
      <form className="uma-panel uma-concentration-form" noValidate onKeyDown={e => {
        if (e.key === "Enter" && (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229)) e.preventDefault();
      }} onSubmit={e => { e.preventDefault(); if (game.activeIndex < QUESTION_COUNT - 1) navigate(game.activeIndex + 1); else void finish(); }}>
        <section className="uma-concentration-required"><h2>{t("baseTitle")}</h2><Label className="sr-only" htmlFor="concentration-name">{t("fields.name")}</Label><Input id="concentration-name" value={game.answers[game.activeIndex]!.name} onChange={e => update("name", e.target.value)} disabled={confirming} autoComplete="off" spellCheck={false} maxLength={120} placeholder={t("nameHint")} /></section>
        <section className="uma-concentration-bonus"><h2>{t("bonusTitle")}<span>{t("optional")}</span></h2>
          <div className="uma-concentration-fields">{FIELDS.filter(field => field !== "name").map(field => <div key={field}>
            {field === "birthday" ? <>
              <span className="uma-concentration-field-label">{t("fields.birthday")}</span>
              <div className="uma-concentration-birthday"><div><Label className="sr-only" htmlFor="concentration-month">{t("month")}</Label><Input id="concentration-month" value={game.answers[game.activeIndex]!.birthday.split("/")[0] ?? ""} onChange={e => birthdayPart(0, e.target.value)} inputMode="numeric" maxLength={2} autoComplete="off" disabled={confirming} /><span aria-hidden="true">{t("monthUnit")}</span></div><div><Label className="sr-only" htmlFor="concentration-day">{t("day")}</Label><Input id="concentration-day" value={game.answers[game.activeIndex]!.birthday.split("/")[1] ?? ""} onChange={e => birthdayPart(1, e.target.value)} inputMode="numeric" maxLength={2} autoComplete="off" disabled={confirming} /><span aria-hidden="true">{t("dayUnit")}</span></div></div>
            </> : <>
              <Label htmlFor={`concentration-${field}`}>{t(`fields.${field}`)}</Label>
              <div className={field === "height" ? "uma-concentration-height" : undefined}><Input id={`concentration-${field}`} value={game.answers[game.activeIndex]![field]} onChange={e => update(field, e.target.value)} disabled={confirming} autoComplete="off" autoCorrect="off" autoCapitalize="off" spellCheck={false} maxLength={120} inputMode={field === "height" ? "numeric" : "text"} lang={field === "japanese" ? "ja" : field === "english" ? "en" : undefined} placeholder={field === "translation" ? t("translationHint") : undefined} />{field === "height" ? <span aria-hidden="true">cm</span> : null}</div>
            </>}
          </div>)}</div>
        </section>
        <Button type="submit" variant="primary" size="primary" block disabled={confirming}>{t(game.activeIndex === QUESTION_COUNT - 1 ? "submit" : "next")}<ArrowRight size={16} aria-hidden="true" /></Button>
        {game.activeIndex > 0 ? <Button className="uma-concentration-previous" type="button" variant="ghost" disabled={confirming} onClick={() => navigate(game.activeIndex - 1)}><ArrowLeft size={16} aria-hidden="true" />{t("previous")}</Button> : null}
      </form>
    </> : null}
    {phase === "result" && game ? <ConcentrationResults game={game} reveal={revealResult} playerName={playerName} onAgain={() => void start()} disabled={confirming} /> : null}
    <Rules />
    <Button asChild variant="link" className="justify-self-start"><Link to="/">{t("home")}</Link></Button>
  </GameStage>;
}
