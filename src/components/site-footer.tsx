import {
  BookImage,
  ExternalLink,
  Github,
  LibraryBig,
} from "lucide-react";
import Link from "next/link";
import { SiteBrand } from "@/components/site-brand";

type Props = {
  className?: string;
};

const sourceLinks = [
  {
    label: "哔哩哔哩游戏百科",
    href: "https://wiki.biligame.com/umamusume/%E8%B5%9B%E9%A9%AC%E5%A8%98%E4%B8%80%E8%A7%88",
  },
  {
    label: "萌娘百科",
    href: "https://zh.moegirl.org.cn/zh-cn/%E8%B5%9B%E9%A9%AC%E5%A8%98_Pretty_Derby/%E8%B5%9B%E9%A9%AC%E5%A8%98%E4%B8%80%E8%A7%88",
  },
] as const;

const projectLinks = [
  {
    label: "项目仓库",
    href: "https://github.com/numakkiyu/UmaGuessingGame",
  },
  {
    label: "维护者 GitHub",
    href: "https://github.com/numakkiyu",
  },
] as const;

export function SiteFooter({ className }: Props) {
  return (
    <footer
      className={[
        "site-footer uma-panel bg-[linear-gradient(180deg,rgba(255,255,255,0.9),rgba(239,247,255,0.88))] px-5 py-6 text-sm text-[var(--color-muted)] sm:px-6 sm:py-7",
        className ?? "",
      ].join(" ")}
    >
      <div className="pointer-events-none absolute inset-x-0 top-0 h-1 bg-[linear-gradient(90deg,var(--color-brand),var(--color-brand-blue))]" />

      <div className="grid gap-4 lg:grid-cols-[1.2fr_1fr]">
        <section className="uma-footer-card rounded-[24px] border border-[var(--color-line)] bg-white/82 p-4 shadow-[var(--shadow-soft)]">
          <SiteBrand
            compact
            href="/"
            showMark={false}
            subtitle="BHCN STUDIO 维护的开源项目，欢迎继续补资料、提建议、一起把题库做得更完整。"
          />
          <p className="mt-3 leading-7">
            这份题库和角色资料整理，离不开哔哩哔哩游戏百科、萌娘百科以及长期维护词条的编辑者。
            也感谢每一位帮忙核对别名、补充信息和指出问题的玩家。
          </p>
          <p className="mt-4 text-xs leading-6 text-[var(--color-muted)]">
            赛马娘猜猜乐（UmaGuessingGame）是由 BHCN STUDIO 开发维护的开源项目，采用 MIT
            开源方式发布；任何人都可以在保留许可说明的前提下，自由进行修改、再发布与继续扩展。
          </p>
          <p className="mt-3 text-xs leading-6 text-[var(--color-muted)]">
            本网站所有角色图片版权归属于 Cygames。
          </p>
          <div className="mt-4 flex flex-wrap gap-2.5">
            {projectLinks.map((linkItem) => (
              <a
                key={linkItem.label}
                href={linkItem.href}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 rounded-full border border-[var(--color-line)] bg-[rgba(239,247,255,0.76)] px-3 py-1.5 text-xs font-medium text-[var(--color-ink)] transition hover:bg-white"
              >
                <Github className="size-3.5 text-[var(--color-brand-strong)]" />
                {linkItem.label}
              </a>
            ))}
            <Link
              href="/open-source"
              className="inline-flex items-center gap-2 rounded-full border border-[var(--color-line)] bg-[rgba(239,247,255,0.76)] px-3 py-1.5 text-xs font-medium text-[var(--color-ink)] transition hover:bg-white"
            >
              <LibraryBig className="size-3.5 text-[var(--color-brand-strong)]" />
              开源协议
            </Link>
          </div>
        </section>

        <section className="rounded-[24px] border border-[var(--color-line)] bg-white/82 p-4 shadow-[var(--shadow-soft)]">
          <div className="flex items-center gap-2 text-[var(--color-ink)]">
            <LibraryBig className="size-4 text-[var(--color-brand-strong)]" />
            <p className="text-sm font-semibold">资料与来源</p>
          </div>
          <div className="mt-3 space-y-2.5">
            {sourceLinks.map((linkItem) => (
              <a
                key={linkItem.label}
                href={linkItem.href}
                target="_blank"
                rel="noreferrer"
                className="group flex items-center justify-between rounded-[16px] border border-[var(--color-line)] bg-[rgba(239,247,255,0.72)] px-3 py-3 transition hover:bg-white"
              >
                <span className="inline-flex items-center gap-2 text-[var(--color-ink)]">
                  <BookImage className="size-4 text-[var(--color-brand-strong)]" />
                  {linkItem.label}
                </span>
                <ExternalLink className="size-4 text-[var(--color-muted)] transition group-hover:text-[var(--color-brand-strong)]" />
              </a>
            ))}
          </div>
          <p className="mt-4 text-xs leading-6 text-[var(--color-muted)]">
            资料若有缺漏或图片显示异常，欢迎继续反馈，我们会按现有来源逐步补齐。
          </p>
        </section>
      </div>
    </footer>
  );
}
