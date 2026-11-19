import "@/i18n/matchmaking";
import { useTranslation } from "@/i18n";
import type { RoomSnapshot } from "@/lib/api/types";

type RecordValue = Record<string, unknown>;
const object = (value: unknown): RecordValue => value && typeof value === "object" && !Array.isArray(value) ? value as RecordValue : {};
const count = (value: unknown) => typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : 0;

/** Displays only the public projection already authorised for this participant. */
export function MatchProgress({room,raw}:{room?:RoomSnapshot;raw?:unknown}) {
  const {t} = useTranslation("matchmaking");
  if (room?.kind !== "matchmaking") return null;
  const view=object(raw), pub=object(view.publicState), self=object(pub.self), names=object(view.subjectNames);
  const subjects=Array.isArray(pub.subjects) ? pub.subjects.map(object).filter(row=>typeof row.subjectId === "string" && row.subjectId !== self.subjectId) : [];
  if (!subjects.length) return null;
  return <section className="border-l-4 border-[var(--color-brand-green)] bg-[var(--color-panel-soft)] px-3 py-3 sm:px-4" aria-labelledby="match-progress-title">
    <h2 id="match-progress-title" className="text-sm font-bold">{t("progress.title")}</h2>
    <ul className="mt-2 grid gap-x-5 gap-y-2 sm:grid-cols-2 lg:grid-cols-3">{subjects.map(row=>{
      const id=String(row.subjectId), label=typeof names[id] === "string" ? names[id] as string : t("player");
      const state=pub.modeId === "classic" ? row.solved ? t("progress.solved") : row.failed || row.surrendered ? t("progress.finished") : t("progress.guesses",{count:count(row.attempts)})
        : pub.modeId === "sort" ? t(row.submitted ? "progress.submitted" : "progress.thinking")
        : t("progress.pairs",{count:count(row.matchedPairs)});
      return <li key={id} className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-sm"><span className="min-w-0 break-words font-semibold">{label}</span><span className="text-[var(--color-muted)]">{state}</span></li>;
    })}</ul>
  </section>;
}
