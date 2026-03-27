import Link from "next/link";
import { ExternalLink, Github, LibraryBig, Scale } from "lucide-react";
import { SiteFooter } from "@/components/site-footer";
import { openSourceSections } from "@/lib/open-source";

export default function OpenSourcePage() {
  return (
    <main className="relative mx-auto flex min-h-screen w-full max-w-[1320px] flex-col px-4 py-4 sm:px-6 lg:px-8">
      <div className="pointer-events-none absolute inset-x-6 top-0 -z-10 h-72 rounded-b-[56px] bg-[linear-gradient(180deg,rgba(151,216,28,0.16),rgba(237,247,255,0))]" />
      <div className="pointer-events-none absolute inset-x-10 top-6 -z-10 h-[300px] rounded-[40px] bg-[url('/assets/ui/backgrounds/bwiki-main-bg.png')] bg-cover bg-center opacity-[0.08] blur-[2px]" />

      <section className="uma-hero-shell relative overflow-hidden px-5 py-6 sm:px-7 sm:py-7 lg:px-9 lg:py-8">
        <div className="pointer-events-none absolute inset-x-0 top-0 h-1 bg-[linear-gradient(90deg,var(--color-brand),var(--color-brand-blue))]" />
        <div className="relative flex flex-col gap-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
                开源与协议
              </p>
              <h1 className="mt-2 font-[var(--font-display)] text-[2.3rem] font-bold leading-tight text-[var(--color-ink)] sm:text-[3rem]">
                本站使用到的开源项目
              </h1>
              <p className="mt-3 max-w-3xl text-sm leading-7 text-[var(--color-muted)] sm:text-base">
                这里列出了当前站点直接使用到的主要开源项目、协议、仓库地址和维护者信息，方便查看与继续维护。
              </p>
            </div>

            <Link href="/" className="uma-ghost-button text-sm">
              返回主页
            </Link>
          </div>

          <div className="grid gap-3 lg:grid-cols-[1.1fr_0.9fr]">
            <div className="rounded-[24px] border border-[var(--color-line)] bg-white/84 p-4 shadow-[var(--shadow-soft)]">
              <div className="flex items-center gap-2 text-[var(--color-ink)]">
                <Scale className="size-4 text-[var(--color-brand-strong)]" />
                <p className="text-sm font-semibold">项目许可</p>
              </div>
              <p className="mt-3 text-sm leading-7 text-[var(--color-muted)]">
                赛马娘猜猜乐（UmaGuessingGame）以 MIT 协议开源发布。第三方依赖则分别沿用各自的原始开源协议，具体信息见下方列表。
              </p>
            </div>

            <div className="rounded-[24px] border border-[var(--color-line)] bg-white/84 p-4 shadow-[var(--shadow-soft)]">
              <div className="flex items-center gap-2 text-[var(--color-ink)]">
                <LibraryBig className="size-4 text-[var(--color-brand-strong)]" />
                <p className="text-sm font-semibold">补充说明</p>
              </div>
              <p className="mt-3 text-sm leading-7 text-[var(--color-muted)]">
                本页按当前仓库直接依赖与开发工具整理。后续如果增减依赖，也会同步更新这里的名单与链接。
              </p>
            </div>
          </div>
        </div>
      </section>

      <section className="mt-5 grid gap-5">
        {openSourceSections.map((section) => (
          <section key={section.id} className="uma-panel p-4 sm:p-5">
            <div className="uma-panel-head -mx-4 -mt-4 px-4 pb-4 pt-4 sm:-mx-5 sm:px-5">
              <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
                {section.title}
              </p>
              <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
                {section.description}
              </p>
            </div>

            <div className="mt-4 grid gap-3 lg:grid-cols-2">
              {section.items.map((item) => (
                <article
                  key={`${section.id}-${item.name}`}
                  className="rounded-[22px] border border-[var(--color-line)] bg-white/82 p-4 shadow-[var(--shadow-soft)]"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h2 className="text-lg font-semibold text-[var(--color-ink)]">
                      {item.name}
                    </h2>
                    <span className="uma-chip text-xs">{item.license}</span>
                  </div>

                  <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
                    {item.purpose}
                  </p>

                  <dl className="mt-4 grid gap-2 text-sm">
                    <div className="grid grid-cols-[80px_1fr] gap-2">
                      <dt className="text-[var(--color-muted)]">版本</dt>
                      <dd className="font-medium text-[var(--color-ink)]">{item.version}</dd>
                    </div>
                    <div className="grid grid-cols-[80px_1fr] gap-2">
                      <dt className="text-[var(--color-muted)]">维护者</dt>
                      <dd className="font-medium text-[var(--color-ink)]">{item.maintainer}</dd>
                    </div>
                  </dl>

                  <div className="mt-4 flex flex-wrap gap-2.5">
                    <a
                      href={item.repository}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-2 rounded-full border border-[var(--color-line)] bg-[rgba(239,247,255,0.76)] px-3 py-1.5 text-xs font-medium text-[var(--color-ink)] transition hover:bg-white"
                    >
                      <Github className="size-3.5 text-[var(--color-brand-strong)]" />
                      GitHub
                    </a>
                    {item.homepage ? (
                      <a
                        href={item.homepage}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-2 rounded-full border border-[var(--color-line)] bg-[rgba(239,247,255,0.76)] px-3 py-1.5 text-xs font-medium text-[var(--color-ink)] transition hover:bg-white"
                      >
                        <ExternalLink className="size-3.5 text-[var(--color-brand-strong)]" />
                        官网
                      </a>
                    ) : null}
                  </div>
                </article>
              ))}
            </div>
          </section>
        ))}
      </section>

      <SiteFooter className="mt-5" />
    </main>
  );
}
