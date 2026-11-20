import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { ArrowRight, Check, ChevronDown, Clock3, Download, Minus, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { MenuCharacterArt } from "@/components/uma/menu-character-art";
import { useReducedMotion } from "@/lib/hooks/use-reduced-motion";
import { matchLanguage, useTranslation } from "@/i18n";
import { FIELDS, gameResult, type ConcentrationGame, type Field, type Profile } from "./engine";

/** Presentation ends independently of scoring and never replays after an interruption. */
function useResultReveal(reveal: boolean) {
  const { reduced } = useReducedMotion();
  const [motion, setMotion] = useState(() => ({ ended: !reveal || reduced || document.hidden, reduced }));
  // A preference change cancels this reveal permanently, including a quick on/off toggle.
  if (motion.reduced !== reduced) setMotion({ ended: motion.ended || reduced, reduced });
  useEffect(() => {
    if (motion.ended) return;
    const stop = () => setMotion(current => ({ ...current, ended: true }));
    const visibility = () => { if (document.hidden) stop(); };
    document.addEventListener("visibilitychange", visibility);
    const timer = window.setTimeout(stop, 800);
    return () => { window.clearTimeout(timer); document.removeEventListener("visibilitychange", visibility); };
  }, [motion.ended]);
  return { playing: !motion.ended && !reduced, skip: () => setMotion(current => ({ ...current, ended: true })) };
}

function referenceAnswer(profile: Profile, field: Field): string {
  switch (field) {
    case "name": case "translation": return profile.chineseNames.join(" / ") + (field === "name" ? ` / ${profile.names.ja}` : "");
    case "japanese": return profile.names.ja;
    case "english": return profile.english ?? "—";
    case "cv": return profile.cv.join(" / ");
    case "birthday": return `${profile.birthday.month}/${profile.birthday.day}`;
    case "height": return `${profile.height} cm`;
  }
}

function RankBadge({ rank, label }: { rank: ReturnType<typeof gameResult>["rank"]; label: string }) {
  const [failed, setFailed] = useState(false);
  return <div className="uma-concentration-rank" role="img" aria-label={`${label} ${rank}`} data-rank={rank}>
    {failed ? <strong aria-hidden="true">{rank}</strong> : <img src={`/assets/ui/ranks/${rank.toLowerCase()}.webp`} width={256} height={256} alt="" aria-hidden="true" decoding="async" onError={() => setFailed(true)} />}
  </div>;
}

export function ConcentrationResults({ game, playerName, onAgain, disabled, reveal = false }: {
  game: ConcentrationGame; playerName: string; onAgain: () => void; disabled: boolean; reveal?: boolean;
}) {
  const { t, i18n } = useTranslation("concentration");
  const result = useMemo(() => gameResult(game), [game]);
  const language = matchLanguage(i18n.resolvedLanguage ?? "") ?? "zh-Hans";
  const { playing, skip } = useResultReveal(reveal);
  const [downloaded, setDownloaded] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, []);
  const seconds = Math.max(0, Math.floor(((game.finishedAt ?? game.startedAt) - game.startedAt) / 1000));
  const elapsed = `${Math.floor(seconds / 60).toString().padStart(2, "0")}:${(seconds % 60).toString().padStart(2, "0")}`;

  function download() {
    const text = [t("certificate"), playerName, `${t("concentration")}: ${result.concentration}% (${result.rank})`, `${t("score")}: ${result.score}/${result.max}`, "",
      ...result.items.flatMap((item, index) => [
        `${index + 1}. ${item.profile.names[language]} (${item.score.total}/20)`,
        ...FIELDS.filter(field => item.score.weight[field] > 0).map(field => `${t(`fields.${field}`)}: ${item.answer[field] || t("noAnswer")} → ${referenceAnswer(item.profile, field)} (${item.score.points[field]}/${item.score.weight[field]})`), "",
      ])].join("\n");
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain;charset=utf-8" }));
    const a = document.createElement("a"); a.href = url; a.download = `umaguess-concentration-${game.finishedAt}.txt`; a.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000); setDownloaded(true);
  }

  return <>
    <section className="uma-concentration-result" aria-labelledby="concentration-result" data-reveal={playing}>
      <header className="uma-concentration-result-heading">
        <h1 id="concentration-result" ref={heading} tabIndex={-1}>{t("finalTitle")}</h1>
        <div className="uma-concentration-result-player"><strong>{playerName}</strong><span className="uma-concentration-time"><Clock3 size={16} aria-hidden="true" /><span>{t("elapsed")}</span><time dateTime={`PT${seconds}S`}>{elapsed}</time></span></div>
      </header>
      <div className="uma-concentration-award-stage">
        <MenuCharacterArt mode="concentration" className="uma-concentration-award-art is-left" />
        <div className="uma-concentration-award">
          <RankBadge key={result.rank} rank={result.rank} label={t("rating")} />
          <p className="uma-concentration-value"><span className="sr-only">{t("concentration")}</span><strong><span>{result.concentration}</span><small>%</small></strong><span aria-hidden="true">{t("concentration")}</span></p>
        </div>
        <MenuCharacterArt mode="rapport" className="uma-concentration-award-art is-right" />
      </div>
      <dl className="uma-concentration-scoreline"><div><dt>{t("score")}</dt><dd><strong>{result.score}</strong><span> / {result.max}</span></dd></div></dl>
      <dl className="uma-concentration-metrics">{(["accuracy", "completeness", "relevance"] as const).map((key, index) => <div key={key} style={{ "--metric-index": index } as CSSProperties}>
        <dt id={`concentration-metric-${key}`}>{t(key)}</dt><dd>{result[key]}<small>%</small></dd>
        <div role="meter" aria-labelledby={`concentration-metric-${key}`} aria-valuenow={result[key]} aria-valuemin={0} aria-valuemax={100}><span style={{ transform: `scaleX(${result[key] / 100})` }}><span /></span></div>
      </div>)}</dl>
      <footer className="uma-concentration-result-footer">
        <div className="uma-concentration-result-actions"><Button variant="primary" size="primary" onClick={onAgain} disabled={disabled}><RotateCcw size={17} aria-hidden="true" />{t("again")}</Button><Button variant="secondary" size="primary" onClick={download}><Download size={17} aria-hidden="true" />{t("download")}</Button></div>
        <div className="uma-concentration-result-notices"><span role="status">{downloaded ? t("downloaded") : ""}</span>{playing ? <Button variant="link" onClick={() => { skip(); heading.current?.focus({ preventScroll: true }); }}>{t("skipReveal")}</Button> : null}</div>
      </footer>
    </section>
    <section className="uma-concentration-review" aria-labelledby="concentration-review"><header><h2 id="concentration-review">{t("review")}</h2><p>{t("reviewHint")}</p></header>
      {result.items.map((item, index) => <details className="uma-concentration-review-item" key={item.profile.id}>
        <summary><span className="uma-concentration-review-number">{index + 1}</span><img src={item.profile.imagePath} width={44} height={58} alt="" loading="lazy" /><strong>{item.profile.names[language]}</strong><span className="uma-concentration-review-score"><span className="sr-only">{t("score")}</span>{item.score.total}<small> / 20</small></span><ChevronDown size={17} aria-hidden="true" /></summary>
        <div className="uma-concentration-review-body">
          <div className="uma-concentration-review-fields">{FIELDS.filter(field => item.score.weight[field] > 0).map(field => {
            const correct = item.score.points[field] === item.score.weight[field];
            const status = correct ? "correct" : item.answer[field].trim() ? "noPoints" : "noAnswer";
            const StatusIcon = correct ? Check : Minus;
            return <div key={field} data-correct={correct}>
              <h3>{t(`fields.${field}`)}<span className="uma-concentration-field-score"><StatusIcon size={14} aria-hidden="true" /><span className="sr-only">{t(status)}</span>{item.score.points[field]} / {item.score.weight[field]}</span></h3>
              <dl><div><dt>{t("yourAnswer")}</dt><dd>{item.answer[field] || t("noAnswer")}</dd></div><div><dt>{t("correctAnswer")}</dt><dd>{referenceAnswer(item.profile, field)}</dd></div></dl>
            </div>;
          })}</div>
          <nav aria-label={t("source")} className="uma-concentration-sources">{item.profile.sources.map(url => <a key={url} href={url} target="_blank" rel="noopener noreferrer">{t(url.startsWith("https://umamusume.jp/") ? "official" : url.startsWith("https://wiki.biligame.com/") ? "wiki" : "moegirl")}<ArrowRight size={13} aria-hidden="true" /></a>)}</nav>
        </div>
      </details>)}
    </section>
  </>;
}
