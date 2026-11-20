/** Public guide; rules for an actual round continue to come from that round. */
import { Link } from "react-router";
import { StatusCell } from "@/components/uma/status-cell";
import { Button } from "@/components/ui/button";
import { useReducedMotion } from "@/lib/hooks/use-reduced-motion";
import { useTranslation } from "@/i18n";
import { modes } from "@/i18n/modes";
import "@/i18n/public-pages";
import "@/i18n/concentration-entry";

const fields=["star","surface","distance","style","g1","g23","grade","dormitory","sex"] as const;
const modeIds=Object.keys(modes["zh-Hans"]);

export function HelpPage(){
 const {t}=useTranslation("publicPages",{keyPrefix:"help"}),{t:ct}=useTranslation("classic"),{t:mt}=useTranslation("modes"),{t:concentration}=useTranslation("concentrationEntry");
 function list(group:string,keys:readonly string[]){return <ul className="space-y-2 text-base leading-7">{keys.map(key=><li key={key}>{t(`${group}.${key}`)}</li>)}</ul>;}
 return <article className="uma-prose mx-auto flex min-w-0 flex-col gap-8">
  <header><h1 className="u-fluid-title">{t("title")}</h1><p className="mt-3 text-base leading-7">{t("intro")}</p></header>
  <section aria-labelledby="formats-heading" className="uma-stack uma-stack--tight"><h2 id="formats-heading" className="text-xl font-bold">{t("formatsTitle")}</h2><p className="text-sm leading-6 text-[var(--color-muted)]">{t("formatsHint")}</p><dl className="grid gap-3 sm:grid-cols-3">{(["solo","friends","event"] as const).map(format=><div key={format} className="min-w-0 rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel)] p-4"><dt className="font-bold">{t(`formats.${format}.name`)}</dt><dd className="mt-2 text-sm leading-6">{t(`formats.${format}.text`)}</dd></div>)}</dl><div className="flex flex-wrap gap-2"><Button asChild variant="secondary"><Link to="/join">{t("join")}</Link></Button><Button asChild variant="secondary"><Link to="/events/new">{t("event")}</Link></Button></div></section>
  <section aria-labelledby="modes-heading" className="uma-stack uma-stack--tight"><h2 id="modes-heading" className="text-xl font-bold">{t("modesTitle")}</h2><p className="text-sm leading-6 text-[var(--color-muted)]">{t("modesHint")}</p><ul className="grid gap-x-5 gap-y-3 sm:grid-cols-2">{modeIds.map(id=><li key={id} className="min-w-0 border-b border-[var(--color-line)] pb-3"><Link className="inline-flex min-h-11 items-center font-bold text-[var(--color-blue-deep)] underline-offset-4 hover:underline" to={`/play/${id}`}>{mt(`${id}.name`)}</Link><p className="text-sm leading-6">{mt(`${id}.description`)}</p></li>)}<li className="min-w-0 border-b border-[var(--color-line)] pb-3"><Link className="inline-flex min-h-11 items-center font-bold text-[var(--color-blue-deep)] underline-offset-4 hover:underline" to="/play/concentration">{concentration("title")}</Link><p className="text-sm leading-6">{concentration("description")}</p></li></ul></section>
  <section aria-labelledby="classic-heading" className="uma-stack uma-stack--tight"><h2 id="classic-heading" className="text-xl font-bold">{t("classicTitle")}</h2><p className="text-base leading-7">{t("classicIntro")}</p></section>
  <section aria-labelledby="feedback-heading" className="uma-stack uma-stack--tight"><h2 id="feedback-heading" className="text-xl font-bold">{t("feedbackTitle")}</h2><p className="text-base leading-7">{t("feedbackHint")}</p><div className="grid gap-3 sm:grid-cols-3"><StatusCell state="correct" value={t("example.medium")} label={t("example.distance")} compact/><StatusCell state="near" value={t("example.mile")} label={t("example.adjacent")} compact/><StatusCell state="wrong" value={t("example.sprint")} label={t("example.distance")} compact/></div>{list("feedback",["correct","near","wrong"])}</section>
  <section aria-labelledby="fields-heading" className="uma-stack uma-stack--tight"><h2 id="fields-heading" className="text-xl font-bold">{t("fieldsTitle")}</h2><dl className="grid gap-3 sm:grid-cols-2">{fields.map(field=><div key={field} className="min-w-0 rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel)] p-3"><dt className="font-semibold">{ct(`fields.${field}`)}</dt><dd className="mt-1 text-sm leading-6 text-[var(--color-muted)]">{t(`fields.${field}`)}</dd></div>)}</dl><p className="text-sm leading-6 text-[var(--color-muted)]">{t("jMark")}</p></section>
  <section aria-labelledby="quota-heading" className="uma-stack uma-stack--tight"><h2 id="quota-heading" className="text-xl font-bold">{t("quotaTitle")}</h2>{list("quota",["budget","duplicate","ending","scoring","event"])}</section>
  <section aria-labelledby="search-heading" className="uma-stack uma-stack--tight"><h2 id="search-heading" className="text-xl font-bold">{t("searchTitle")}</h2>{list("search",["names","ime","keyboard"])}</section>
  <section aria-labelledby="recovery-heading" className="uma-stack uma-stack--tight"><h2 id="recovery-heading" className="text-xl font-bold">{t("recoveryTitle")}</h2>{list("recovery",["practice","online","identity"])}</section>
  <section aria-labelledby="a11y-heading" className="uma-stack uma-stack--tight"><h2 id="a11y-heading" className="text-xl font-bold">{t("accessTitle")}</h2>{list("access",["keyboard","motion","zoom"])}<MotionPreferenceControl/></section>
  <div className="flex flex-col gap-2 border-t border-[var(--color-line)] pt-6 sm:flex-row"><Button asChild variant="primary" size="primary"><Link to="/play/classic">{t("play")}</Link></Button><Button asChild variant="secondary" size="primary"><Link to="/">{t("home")}</Link></Button><Button asChild variant="secondary" size="primary"><Link to="/sources">{t("sources")}</Link></Button></div>
 </article>;
}

function MotionPreferenceControl(){
 const {preference,setPreference,reduced,systemReduced}=useReducedMotion();
 const {t}=useTranslation("publicPages",{keyPrefix:"help.motion"});
 return <fieldset className="mt-4 min-w-0 rounded-[var(--radius-card)] border border-[var(--color-line)] p-4"><legend className="px-1 text-base font-bold">{t("title")}</legend><p className="mb-3 text-sm leading-6 text-[var(--color-muted)]">{t(reduced?"currentReduced":"currentFull")}</p><div className="flex flex-col gap-2 sm:flex-row">{(["system","reduced","full"] as const).map(value=><label key={value} className="flex min-h-11 min-w-0 flex-1 cursor-pointer items-start gap-2 rounded-[var(--radius-card)] border border-[var(--color-line)] px-3 py-2 text-sm has-checked:border-[var(--color-brand-green)] has-checked:bg-[var(--color-brand-green-soft)]"><input type="radio" name="motion-preference" value={value} checked={preference===value} onChange={()=>setPreference(value)} className="mt-0.5 size-4 shrink-0 accent-[var(--color-brand-green)]"/><span className="flex min-w-0 flex-col"><span className="font-semibold">{t(value)}</span><span className="text-[var(--text-aux)] leading-6 text-[var(--color-muted)]">{t(value==="system"?systemReduced?"systemReduced":"systemFull":`${value}Hint`)}</span></span></label>)}</div></fieldset>;
}
