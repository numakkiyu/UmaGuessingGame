import {
  BookImage,
  ExternalLink,
  Github,
  HeartHandshake,
  LibraryBig,
} from "lucide-react";

type Props = {
  className?: string;
};

const sourceLinks = [
  {
    label: "赛马娘官网",
    href: "https://umamusume.jp/",
  },
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
        "site-footer relative overflow-hidden rounded-[32px] border border-[rgba(120,90,58,0.16)] bg-[linear-gradient(180deg,rgba(255,251,245,0.94),rgba(249,241,229,0.9))] px-5 py-6 text-sm text-[var(--color-muted)] shadow-[var(--shadow-panel)] sm:px-6 sm:py-7",
        className ?? "",
      ].join(" ")}
    >
      <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-[linear-gradient(90deg,rgba(156,69,24,0),rgba(156,69,24,0.22),rgba(156,69,24,0))]" />
      <div className="pointer-events-none absolute -right-12 bottom-0 h-32 w-32 rounded-full bg-[rgba(200,108,53,0.1)] blur-3xl" />

      <div className="grid gap-4 lg:grid-cols-[1.2fr_1fr]">
        <section className="rounded-[24px] border border-[rgba(104,79,48,0.1)] bg-white/72 p-4">
          <div className="flex items-center gap-2 text-[var(--color-ink)]">
            <HeartHandshake className="size-4 text-[var(--color-brand-strong)]" />
            <p className="text-sm font-semibold">鸣谢与维护</p>
          </div>
          <p className="mt-3 leading-7">
            这份题库和角色资料整理，离不开哔哩哔哩游戏百科、萌娘百科以及长期维护词条的编辑者。
            也感谢每一位帮忙核对别名、补充信息和指出问题的玩家。
          </p>
          <p className="mt-4 text-xs leading-6 text-[var(--color-muted)]">
            赛马娘猜猜乐（UmaGuessingGame）是由 BHCN STUDIO 开发维护的开源项目，采用 MIT
            开源方式发布；任何人都可以在保留许可说明的前提下，自由进行修改、再发布与继续扩展。
          </p>
          <div className="mt-4 flex flex-wrap gap-2.5">
            {projectLinks.map((linkItem) => (
              <a
                key={linkItem.label}
                href={linkItem.href}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 rounded-full border border-[rgba(104,79,48,0.12)] bg-[rgba(255,248,240,0.76)] px-3 py-1.5 text-xs font-medium text-[var(--color-ink)] transition hover:bg-white"
              >
                <Github className="size-3.5 text-[var(--color-brand-strong)]" />
                {linkItem.label}
              </a>
            ))}
          </div>
        </section>

        <section className="rounded-[24px] border border-[rgba(104,79,48,0.1)] bg-white/72 p-4">
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
                className="group flex items-center justify-between rounded-[16px] border border-[rgba(104,79,48,0.08)] bg-[rgba(255,248,240,0.72)] px-3 py-3 transition hover:bg-white"
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
