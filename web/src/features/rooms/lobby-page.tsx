
import { ArrowRight, BookOpen, Clock3, Users, Flag, Sparkles, Handshake, Swords } from "lucide-react";
import { Link, useSearchParams } from "react-router";
import { useMemo } from "react";
import { useTranslation } from "@/i18n";
import { EmptyView, ErrorView, ForbiddenView, LoadingBlock, toErrorState } from "@/components/uma/page-state";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { useModes, usePublicConfig } from "@/lib/api/queries";

import { toKnownModeId, type Mode, type ModeId } from "@/lib/api/types";
import { describeFailure } from "@/lib/api/client";
import { anyCanPlay, formEntry, type PlayerForm } from "@/lib/deployment/capabilities";
import { cn } from "@/lib/utils";

/** 模式分类：「可玩项目（知识 / 合作 / 策略）」 */
type ModeCategory = "knowledge" | "coop" | "strategy";

const CATEGORY_OF: Partial<Record<ModeId, ModeCategory>> = {
  classic: "knowledge",
  "aptitude-grid": "knowledge",
  "race-rebuild": "knowledge",
  sort: "knowledge",
  memory: "knowledge",
  audio: "knowledge",
  "co-race-path": "coop",
  "clue-puzzle": "coop",
  rapport: "coop",
  creative: "coop",
  "strategy-race": "strategy",
  draft: "strategy",
  bluff: "strategy",
  trade: "strategy",
  pedigree: "strategy",
};

export function LobbyPage() {
  const { t } = useTranslation("lobby");
  const configQuery = usePublicConfig();
  const modesQuery = useModes();

  const [params, setParams] = useSearchParams();
  const enabledModeIds = configQuery.data?.enabledModeIds;
  const availableForms = (["single", "multiplayer", "team"] as const).filter(candidate =>
    (modesQuery.data ?? []).some(mode => mode.availability === "available" && (!enabledModeIds || enabledModeIds.includes(mode.modeId)) && formEntry(configQuery.data, mode.modeId, candidate)));
  const requested = params.get("form");
  const form: PlayerForm = availableForms.find(candidate => candidate === requested) ?? availableForms[0] ?? "single";

  // 服务端启用清单与本页模式清单取交集；未知模式不显示为可开局
  const playableModes = useMemo(() => {
    const modes = modesQuery.data ?? [];
    const enabled = enabledModeIds ? new Set<string>(enabledModeIds) : null;
    return modes.filter((mode) => {
      if (mode.availability !== "available") return false;
      if (enabled && !enabled.has(mode.modeId)) return false;
      if (!formEntry(configQuery.data, mode.modeId, form)) return false;
      return toKnownModeId(mode.modeId) !== null;
    });
  }, [modesQuery.data, enabledModeIds, configQuery.data, form]);

  const grouped = useMemo(() => {
    const buckets: Record<ModeCategory, Mode[]> = { knowledge: [], coop: [], strategy: [] };
    for (const mode of playableModes) {
      const known = toKnownModeId(mode.modeId);
      if (!known) continue;
      buckets[CATEGORY_OF[known] ?? "knowledge"].push(mode);
    }
    return buckets;
  }, [playableModes]);

  const classicMode = playableModes.find((mode) => mode.modeId === "classic");
  const classicEntry = formEntry(configQuery.data, "classic", "single");

  const modesFailure = modesQuery.isError ? describeFailure(modesQuery.error) : null;
  const configFailure = configQuery.isError ? describeFailure(configQuery.error) : null;

  return (
    <div className="uma-stack">
      <section className="uma-lobby-hero" aria-labelledby="lobby-title">
        <div className="uma-lobby-hero__scene" aria-hidden="true" />
        <div className="uma-lobby-hero__intro">
          <p className="uma-lobby-hero__tag"><Flag aria-hidden="true" className="size-4" />{t("tag")}</p>
          <h1 id="lobby-title" className="uma-lobby-hero__title">{t("title")}</h1>
          <p className="uma-lobby-hero__description">
            <span className="block">{t("intro")}</span>
            <span className="block">{t("formats")}</span>
          </p>
          <div className="uma-lobby-hero__actions">
            {classicEntry ? <Button asChild variant="primary" size="primary">
              <Link to={classicEntry}>{t("start")}<ArrowRight aria-hidden="true" className="size-4" /></Link>
            </Button> : <Button variant="primary" size="primary" disabled>{configQuery.isLoading ? t("checking") : t("closed")}</Button>}
            <Link to="/help" className="uma-lobby-hero__help">{t("rules")}</Link>
          </div>
          <p className="text-sm text-[var(--color-muted)]">
            {classicMode ? <>{t("duration", { minutes: classicMode.estimatedMinutes })} · </> : null}{t("noRegistration")}
          </p>
        </div>
        <aside className="uma-lobby-social" aria-label={t("withFriends")}>
          <div className="uma-lobby-social__group">
            <Users aria-hidden="true" className="size-5 text-[var(--color-action-green)]" />
            <h2>{t("multiplayer")}</h2>
            <p>{t("friendIntro")}</p>
            <div className="uma-lobby-social__actions"><Button asChild variant="secondary" size="sm"><Link to="/join">{t("join")}</Link></Button>
            {anyCanPlay(configQuery.data, "friend") ? <Button asChild variant="secondary" size="sm"><Link to="/rooms/new">{t("create")}</Link></Button> : null}
            </div>
            {anyCanPlay(configQuery.data, "matchmaking") ? <Button asChild className="uma-lobby-social__match" variant="link" size="sm"><Link to="/matchmaking">{t("match")}</Link></Button> : null}
          </div>
          {anyCanPlay(configQuery.data, "event") ? <div className="uma-lobby-social__group">
            <Flag aria-hidden="true" className="size-5 text-[var(--color-action-green)]" />
            <h2>{t("team")}</h2>
            <p>{t("eventIntro")}</p>
            <div className="uma-lobby-social__actions"><Button asChild variant="secondary" size="sm"><Link to="/events/new">{t("createEvent")}</Link></Button></div>
          </div> : null}
        </aside>
      </section>

      <section aria-label={t("chooseForm")} className="grid gap-3 md:grid-cols-3">
        {([
          { id:"single", icon:Sparkles },
          { id:"multiplayer", icon:Users },
          { id:"team", icon:Flag },
        ] as const).map(item => <button key={item.id} type="button" aria-pressed={form === item.id} disabled={!availableForms.includes(item.id)} onClick={() => setParams({ form: item.id }, { replace: true })} className={cn("min-h-11 min-w-0 rounded-xl border p-4 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)] disabled:cursor-not-allowed disabled:opacity-60", form === item.id ? "border-[var(--color-brand-green)] bg-[var(--color-brand-green-soft)]" : "border-[var(--color-line)] bg-[var(--color-panel)]")}><span className="flex items-center gap-2 text-base font-bold"><item.icon aria-hidden="true" className="size-5 text-[var(--color-action-green)]" />{t(item.id)}</span><span className="mt-2 block text-sm leading-6 text-[var(--color-muted)]">{availableForms.includes(item.id) ? t(`${item.id}Detail`) : t("formClosed")}</span></button>)}
      </section>
      {/* ---- 可玩项目 ---- */}
      <section aria-labelledby="modes-heading" className="uma-stack uma-stack--tight">
        <div className="uma-section-head">
          <h2 id="modes-heading" className="u-fluid-task text-[var(--color-ink)]">
            {t(`${form}Heading`)}
          </h2>
          <p className="text-sm text-[var(--color-muted)]">
            {modesQuery.isLoading || configQuery.isLoading
              ? t("loading")
                : modesFailure || configFailure
                ? t("failed")
                : t("count", { count: playableModes.length })}
          </p>
        </div>

        {/* 状态：loading / error / forbidden / empty / ready 分别处理，不混为一谈 */}
        {modesQuery.isLoading ? <LoadingBlock label={t("loading")} rows={2} /> : null}

        {modesFailure ? (
          modesFailure.kind === "forbidden" ? (
            <ForbiddenView failure={modesFailure} onRetry={() => void modesQuery.refetch()} />
          ) : (
            <ErrorView failure={modesFailure} onRetry={() => void modesQuery.refetch()} />
          )
        ) : null}

        {!modesQuery.isLoading && !configQuery.isLoading && !modesFailure && !configFailure && playableModes.length === 0 ? (
          <EmptyView
            title={t("empty")}
            detail={t("emptyDetail")}
            action={
              <Button variant="secondary" onClick={() => void modesQuery.refetch()}>
                {t("recheck")}
              </Button>
            }
          />
        ) : null}

        {playableModes.length > 0 ? (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {(["knowledge", "coop", "strategy"] as const).map((category) => {
              const items = grouped[category];
              if (items.length === 0) return null;
              return (
                <div key={category} className="min-w-0 md:col-span-2 xl:col-span-3">
                  <h3 className="uma-mode-group-heading">
                    {category === "knowledge" ? <Sparkles aria-hidden="true" /> : category === "coop" ? <Handshake aria-hidden="true" /> : <Swords aria-hidden="true" />}
                    {t(`categories.${category}`)}
                  </h3>
                  <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                    {items.map((mode) => (
                      <li key={mode.modeId} className="min-w-0">
                        <ModeCard mode={mode} category={category} href={formEntry(configQuery.data, mode.modeId, form)!} form={form} />
                      </li>
                    ))}
                  </ul>
                </div>
              );
            })}
          </div>
        ) : null}
      </section>

      {/* ---- 配置失败提示（不阻塞大厅浏览） ---- */}
      {configFailure && configFailure.kind !== "forbidden" ? (
        <Alert tone="warning" title={t("partial")}>
          <p>{configFailure.detail}</p>
          <p className="mt-1 text-[var(--text-aux)]">
            {t("partialDetail")}
          </p>
        </Alert>
      ) : null}

      {/*
        forbidden 去重：modes 与 config 可能同时返回 403。
        两者权限语义相同，重复渲染同一块提示会让用户以为有两处不同问题。
        这里只在 modes 没有走 forbidden 分支时才单独展示 config 的 forbidden。
      */}
      {configFailure?.kind === "forbidden" && modesFailure?.kind !== "forbidden" ? (
        <ForbiddenView failure={configFailure} />
      ) : null}

      {/* ---- 新手指引 / 来源 ---- */}
      <section aria-labelledby="guide-heading" className="grid gap-4 md:grid-cols-2">
        <div className="uma-card p-5">
          <h2 id="guide-heading" className="flex items-center gap-2 text-lg font-bold text-[var(--color-ink)]">
            <BookOpen aria-hidden="true" className="size-5 text-[var(--color-action-green)]" />
            {t("firstTime")}
          </h2>
          <ol className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]">
            {[1, 2, 3, 4].map(step => <li key={step}>{step}. {t(`guide${step}`)}</li>)}
          </ol>
          <Button asChild variant="secondary" className="mt-4">
            <Link to="/help">{t("fullRules")}</Link>
          </Button>
        </div>

        <div className="uma-card p-5">
          <h2 className="text-lg font-bold text-[var(--color-ink)]">{t("sources")}</h2>
          <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
            {t("sourceDetail")}
          </p>
          <Button asChild variant="secondary" className="mt-4">
            <Link to="/sources">{t("viewSources")}</Link>
          </Button>
        </div>
      </section>
    </div>
  );
}

/** 模式卡 */
/*
 * 玩法卡片。
 *
 * 用户原话：「模式列表仍是大块同质白卡」。原因不是配色，而是冗余：
 * 列表层已经把 availability !== "available" 的模式过滤掉了，所以每张卡
 * 还挂一个「可开局」徽章、再挂一整行「查看说明」按钮，13 张卡把同一句
 * 话重复 13 次，卡片之间没有可扫读的差异。
 *
 * 改法（只动信息层级，不删信息）：
 * - 去掉「可开局」徽章：这个列表里 100% 可开局，徽章不携带任何信息。
 * - 去掉整行按钮，整张卡就是链接：点击目标变大，视觉噪音变小。
 * - 左侧加类别色条：知识/合作/策略三类一眼可分，这是卡片之间真实的差异。
 * - 人数与时长压成一行，门槛与说明保留。
 *
 * 可达性与触摸区不受影响：整卡仍是 <a>，高度远超 44px；
 * hover/focus 有明确反馈，键盘 Tab 顺序不变。
 */
const CATEGORY_ACCENT: Record<ModeCategory, string> = {
  knowledge: "bg-[var(--color-brand-green)]",
  coop: "bg-[var(--color-blue-deep)]",
  strategy: "bg-[var(--color-gold)]",
};

function ModeCard({ mode, category, href, form }: { mode: Mode; category: ModeCategory; href: string; form: PlayerForm }) {
  const { t } = useTranslation(["lobby", "modes"]);
  const known = toKnownModeId(mode.modeId);
  const displayName = known ? t(`${known}.name`, { ns: "modes" }) : t("unknown");
  const difficulty = known ? t(`${known}.difficulty`, { ns: "modes" }) : undefined;

  return (
    <Link
      to={href}
      title={t("available", { name: displayName })}
      className={cn(
        "group relative flex h-full min-w-0 flex-col gap-1.5 overflow-hidden rounded-[var(--radius-card)] border",
        "border-[var(--color-line)] bg-[var(--color-panel)] p-4 pl-5",
        "transition-colors duration-[var(--duration-quick)] ease-[var(--ease-primary)]",
        "hover:border-[var(--color-line-strong)] hover:bg-[var(--color-panel-soft)]",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-blue-deep)]",
      )}
    >
      {/* 类别色条：卡片之间唯一的真实差异，扫一眼就能分三类 */}
      <span
        aria-hidden="true"
        className={cn("absolute inset-y-3 left-0 w-1 rounded-full", CATEGORY_ACCENT[category])}
      />

      {/*
         明确要求模式卡显示「是否可开局」，所以这个状态必须留。
        但大厅列表已经把不可用的模式过滤掉了，13 张卡全写「可开局」
        就是同一句话重复 13 次。折中：状态写进卡片的可访问名称
        （屏幕阅读器与悬停提示都能读到），视觉上不再占一行徽章。
      */}
      <h4 className="text-base font-bold text-[var(--color-ink)] group-hover:underline">
        {displayName}
        <span className="sr-only">{t("availableLabel")}</span>
      </h4>
      <p className="text-sm leading-6 text-[var(--color-muted)]">{known ? t(`${known}.description`, { ns: "modes" }) : ""}</p>

      <dl className="mt-auto flex flex-wrap items-center gap-x-3 gap-y-1 pt-1 text-[var(--text-aux)] text-[var(--color-muted)]">
        <div className="inline-flex items-center gap-1">
          <Users aria-hidden="true" className="size-3.5" />
          <dt className="sr-only">{t("players")}</dt>
          <dd>
            {form === "single" ? t("playersExact", { count: 1 }) : mode.minPlayers === mode.maxPlayers
              ? t("playersExact", { count: mode.minPlayers })
              : t("playersRange", { min: mode.minPlayers, max: mode.maxPlayers })}
          </dd>
        </div>
        <div className="inline-flex items-center gap-1">
          <Clock3 aria-hidden="true" className="size-3.5" />
          <dt className="sr-only">{t("time")}</dt>
          <dd>{t("minutes", { minutes: mode.estimatedMinutes })}</dd>
        </div>
      </dl>

      {/*
        门槛单独一行并带「门槛：」前缀。
        之前它和人数/时长挤在同一个 flex-wrap 里，390 竖屏下会折到第二行，
        变成一句没有标签的话（"需要了解场地与距离适性"），读起来像另一个句子。
        单独成行 + 前缀，语义清楚；桌面端也只是多占一行，不影响密度。
      */}
      {difficulty ? (
        <p className="text-[var(--text-aux)] text-[var(--color-muted)]">
          <span className="font-semibold">{t("difficulty")}</span>
          {difficulty}
        </p>
      ) : null}
    </Link>
  );
}

/** 大厅骨架：结构与被替换区域一致，避免按钮跳动 */
export function LobbySkeleton() {
  return (
    <div className="uma-stack" aria-hidden="true">
      <Skeleton className="h-64 w-full rounded-[var(--radius-panel-lg)]" />
      <Skeleton className="h-8 w-48" />
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        <Skeleton className="h-40" />
        <Skeleton className="h-40" />
        <Skeleton className="h-40" />
      </div>
    </div>
  );
}

/** 供错误边界复用的状态包装 */
export function LobbyErrorState({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  useTranslation();
  const state = toErrorState(error);
  if (state.kind === "forbidden") return <ForbiddenView failure={state.failure} onRetry={onRetry} />;
  if (state.kind === "error") return <ErrorView failure={state.failure} onRetry={onRetry} />;
  return null;
}
