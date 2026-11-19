/** Public provenance; source content and assets are not covered by the code licence. */
import { BookImage,ExternalLink,ShieldCheck } from "lucide-react";
import { Link } from "react-router";
import { Button } from "@/components/ui/button";
import { useTranslation } from "@/i18n";
import "@/i18n/public-pages";

const sources=[
 {id:"biliwiki",href:"https://wiki.biligame.com/umamusume/"},
 {id:"moegirl",href:"https://zh.moegirl.org.cn/"},
 {id:"official",href:"https://umamusume.jp/"},
 {id:"gametora",href:"https://gametora.com/zh-tw/umamusume/races"},
 {id:"jra",href:"https://www.jra.go.jp/datafile/seiseki/"},
] as const;
const repository="https://github.com/numakkiyu/UmaGuessingGame";
const external="inline-flex min-h-11 items-center gap-1 font-semibold text-[var(--color-blue-deep)] underline-offset-4 hover:underline";
export function SourcesPage(){
 const {t}=useTranslation("publicPages",{keyPrefix:"sources"});
 return <article className="uma-prose mx-auto flex min-w-0 flex-col gap-8">
  <header><h1 className="u-fluid-title">{t("title")}</h1><p className="mt-3 text-base leading-7">{t("intro")}</p></header>
  <section aria-labelledby="sources-heading" className="uma-stack uma-stack--tight"><h2 id="sources-heading" className="text-xl font-bold">{t("heading")}</h2><ul className="space-y-3">{sources.map(source=><li key={source.id} className="uma-card min-w-0 p-4"><div className="flex flex-wrap items-baseline justify-between gap-2"><h3 className="flex min-w-0 items-center gap-2 font-semibold"><BookImage aria-hidden="true" className="size-4 shrink-0 text-[var(--color-action-green)]"/>{t(`items.${source.id}.name`)}</h3><a href={source.href} target="_blank" rel="noreferrer noopener" className={external} aria-label={`${t("visit")} · ${t(`items.${source.id}.name`)}`}>{t("visit")}<ExternalLink aria-hidden="true" className="size-3.5"/></a></div></li>)}</ul></section>
  <section aria-labelledby="license-heading" className="uma-stack uma-stack--tight"><h2 id="license-heading" className="text-xl font-bold">{t("licenseTitle")}</h2><div className="rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel)] p-4"><p className="flex items-start gap-2 text-base leading-7"><ShieldCheck aria-hidden="true" className="mt-1 size-5 shrink-0 text-[var(--color-action-green)]"/><span>{t("license")}</span></p><p className="mt-3 text-sm leading-6">{t("codeLicense")}</p><a href={repository} target="_blank" rel="noreferrer noopener" className={`${external} mt-2 text-sm`}>{t("repo")}<ExternalLink aria-hidden="true" className="size-3.5"/></a></div></section>
  <section aria-labelledby="feedback-heading" className="uma-stack uma-stack--tight"><h2 id="feedback-heading" className="text-xl font-bold">{t("feedbackTitle")}</h2><p className="text-sm leading-6">{t("feedback")}</p><a href={`${repository}/issues`} target="_blank" rel="noreferrer noopener" className={`${external} text-sm`}>{t("report")}<ExternalLink aria-hidden="true" className="size-3.5"/></a></section>
  <div className="flex flex-col gap-2 border-t border-[var(--color-line)] pt-6 sm:flex-row"><Button asChild variant="secondary" size="primary"><Link to="/">{t("home")}</Link></Button><Button asChild variant="secondary" size="primary"><Link to="/help">{t("help")}</Link></Button></div>
 </article>;
}
