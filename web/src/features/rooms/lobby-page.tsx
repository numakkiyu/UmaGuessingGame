

import { pageScene } from "@/components/uma/theme-art";
import { ArrowRight, BookOpen, Clock3, Users, Flag, Sparkles, Handshake, Swords } from "lucide-react";
import { Link, Navigate, useLocation, useSearchParams } from "react-router";
import { lazy, Suspense, useMemo } from "react";
const MenuCharacterArt = lazy(() => import("@/components/uma/menu-character-art").then(module => ({ default: module.MenuCharacterArt })));
const CharacterStage = lazy(() => import("@/components/uma/character-stage").then(module => ({ default: module.CharacterStage })));
import { useTranslation } from "@/i18n";
import { EmptyView, ErrorView, ForbiddenView, LoadingBlock, toErrorState } from "@/components/uma/page-state";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { useModes, usePublicConfig } from "@/lib/api/queries";

import { toKnownModeId, type Mode, type ModeId } from "@/lib/api/types";
import { describeFailure } from "@/lib/api/client";
import { anyCanPlay, formEntry, type PlayerForm } from "@/lib/deployment/capabilities";


import { entryTimeKey, friendSeats } from "./mode-entry-details";
import { ConcentrationModeCard } from "@/features/concentration/concentration-mode-card";

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
  const { t: theme } = useTranslation("theme");
  const configQuery = usePublicConfig();
  const modesQuery = useModes();

  const [params] = useSearchParams();
  const location = useLocation();
  const enabledModeIds = configQuery.data?.enabledModeIds;
  const form: PlayerForm = location.pathname === "/friends" ? "multiplayer" : location.pathname === "/party" ? "team" : "single";
  const legacyForm = params.get("form");

  // 服务端启用清单与本页模式清单取交集；未知模式不显示为可开局
  const playableModes = useMemo(() => {
    const modes = modesQuery.data ?? [];
    const enabled = enabledModeIds ? new Set<string>(enabledModeIds) : null;
    return modes.filter((mode) => {
      if (mode.availability !== "available") return false;
      if (enabled && !enabled.has(mode.modeId)) return false;
      if (!formEntry(configQuery.data, mode.modeId, form)) return false;
      if (form === "multiplayer") {
        const seats = friendSeats(mode, configQuery.data?.maxRoomMembers ?? 0);
        if (seats.max < seats.min) return false;
      }
      return toKnownModeId(mode.modeId) !== null;
    });
  }, [modesQuery.data, enabledModeIds, configQuery.data, form]);

  const classicEntry = formEntry(configQuery.data, "classic", "single");

  const modesFailure = modesQuery.isError ? describeFailure(modesQuery.error) : null;
  const configFailure = configQuery.isError ? describeFailure(configQuery.error) : null;

  return (
    <div className="uma-stack">
      {location.pathname === "/" && (legacyForm === "multiplayer" || legacyForm === "team") ? <Navigate replace to={legacyForm === "team" ? "/party" : "/friends"} /> : null}
      <section className="uma-lobby-scene" aria-labelledby="lobby-title">
        <div className="uma-lobby-scenery" style={{backgroundImage:`url("${pageScene(location.pathname)}")`}} aria-hidden="true" />
        <div className="uma-lobby-inner">
        <div className="uma-lobby-copy">
          <p className="uma-lobby-ribbon"><Flag aria-hidden="true" />{form === "single" ? theme("heroTag") : t("tag")}</p>
          <h1 id="lobby-title">{form === "single" ? <><span>{theme("heroFirst")}</span><span>{theme("heroSecond")}</span></> : t(form)}</h1>
          <p className="uma-lobby-intro">{form === "single" ? theme("heroIntro") : t(form === "multiplayer" ? "friendIntro" : "eventIntro")}</p>
          <div className="uma-lobby-actions">
            {form === "single" && classicEntry ? <Button asChild variant="primary" size="primary"><Link to={classicEntry}>{t("start")}<ArrowRight aria-hidden="true" className="size-4" /></Link></Button> : null}
            {form !== "single" ? <Button asChild variant="secondary" size="primary"><Link to="/join">{t("join")}</Link></Button> : null}
            {form === "team" && anyCanPlay(configQuery.data,"event") ? <Button asChild variant="primary" size="primary"><Link to="/events/new">{t("createEvent")}</Link></Button> : null}
            <Button asChild variant="link"><Link to="/help">{t("rules")}</Link></Button>
          </div>
        </div>
        <Suspense fallback={<div className="uma-character-stage" aria-hidden="true" />}><CharacterStage key={location.pathname} /></Suspense>
        </div>
      </section>


      <section aria-labelledby="modes-heading" className="uma-stack uma-stack--tight">
        <div className="uma-section-head">
          <h2 id="modes-heading" className="u-fluid-task text-[var(--color-ink)]">
            {t(`${form}Heading`)}
          </h2>
        </div>


        {modesQuery.isLoading ? <LoadingBlock label={t("loading")} rows={2} /> : null}

        {modesFailure ? (
          modesFailure.kind === "forbidden" ? (
            <ForbiddenView failure={modesFailure} onRetry={() => void modesQuery.refetch()} />
          ) : (
            <ErrorView failure={modesFailure} onRetry={() => void modesQuery.refetch()} />
          )
        ) : null}

        {!modesQuery.isLoading && !configQuery.isLoading && !modesFailure && !configFailure && playableModes.length === 0 && form !== "single" ? (
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

        {playableModes.length > 0 || form === "single" ? (
          <ul className="uma-mode-grid">
            {playableModes.map(mode => <li key={mode.modeId} className="min-w-0"><ModeCard mode={mode} category={CATEGORY_OF[mode.modeId as ModeId] ?? "knowledge"} href={formEntry(configQuery.data, mode.modeId, form)!} form={form} instanceLimit={configQuery.data?.maxRoomMembers ?? 0} /></li>)}
            {form === "single" ? <li className="min-w-0"><ConcentrationModeCard /></li> : null}
          </ul>
        ) : null}
      </section>


      {configFailure && configFailure.kind !== "forbidden" ? (
        <Alert tone="warning" title={t("partial")}>
          <p>{configFailure.detail}</p>
          <p className="mt-1 text-[var(--text-aux)]">
            {t("partialDetail")}
          </p>
        </Alert>
      ) : null}


      {configFailure?.kind === "forbidden" && modesFailure?.kind !== "forbidden" ? (
        <ForbiddenView failure={configFailure} />
      ) : null}


      <section aria-labelledby="guide-heading" className="uma-lobby-help">
        <details className="uma-disclosure">
          <summary id="guide-heading" className="flex min-h-11 cursor-pointer items-center gap-2 font-semibold text-[var(--color-ink)]">
            <BookOpen aria-hidden="true" className="size-5 text-[var(--color-action-green)]" />
            {t("firstTime")}
          </summary>
          <ol className="mt-3 space-y-2 text-sm leading-6 text-[var(--color-muted)]">
            {[1, 2, 3, 4].map(step => <li key={step}>{step}. {t(`guide${step}`)}</li>)}
          </ol>
          <Button asChild variant="secondary" className="mt-4">
            <Link to="/help">{t("fullRules")}</Link>
          </Button>
        </details>

        <details className="uma-disclosure">
          <summary className="min-h-11 cursor-pointer font-semibold text-[var(--color-ink)]">{t("sources")}</summary>
          <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
            {t("sourceDetail")}
          </p>
          <Button asChild variant="secondary" className="mt-4">
            <Link to="/sources">{t("viewSources")}</Link>
          </Button>
        </details>
      </section>
    </div>
  );
}



function ModeCard({ mode, category, href, form, instanceLimit }: { mode: Mode; category: ModeCategory; href: string; form: PlayerForm; instanceLimit: number }) {
  const { t } = useTranslation(["lobby", "modes"]);
  const seats = friendSeats(mode, instanceLimit);
  const known = toKnownModeId(mode.modeId);
  const displayName = known ? t(`${known}.name`, { ns: "modes" }) : t("unknown");
  const difficulty = known ? t(`${known}.difficulty`, { ns: "modes" }) : undefined;

  return (
    <Link
      to={href}
      title={t("available", { name: displayName })}
      className="uma-mode-tile group"
    >

      <Suspense fallback={null}><MenuCharacterArt mode={mode.modeId} className="uma-mode-art" /></Suspense>
      <h3 className="font-bold text-[var(--color-ink)] group-hover:underline">
        {displayName}
        <span className="sr-only">{t("availableLabel")}</span>
      </h3>
      <p className="uma-mode-description">{known ? t(`${known}.description`, { ns: "modes" }) : ""}</p>

      <dl className="uma-mode-meta">
        <div className="inline-flex items-center gap-1">
          <Users aria-hidden="true" className="size-3.5" />
          <dt className="sr-only">{t("players")}</dt>
          <dd>
            {form === "single" ? t("playersExact", { count: 1 }) : form === "team" ? t("partyPlayers") : seats.min === seats.max
              ? t("playersExact", { count: seats.min })
              : t("playersRange", { min: seats.min, max: seats.max })}
          </dd>
        </div>
        <div className="inline-flex items-center gap-1">
          <Clock3 aria-hidden="true" className="size-3.5" />
          <dt className="sr-only">{t("time")}</dt>
          <dd>{t(entryTimeKey(mode.modeId, form))}</dd>
        </div>
      </dl>


      {difficulty ? (
        <p className="uma-mode-advice">
          <span className="font-semibold">{t("difficulty")}</span>
          {difficulty}
        </p>
      ) : null}
      <span className="uma-mode-category">{category === "knowledge" ? <Sparkles aria-hidden="true" /> : category === "coop" ? <Handshake aria-hidden="true" /> : <Swords aria-hidden="true" />}{t(`categories.${category}`)}</span>
    </Link>
  );
}


export function LobbySkeleton() {
  return (
    <div className="uma-stack" aria-hidden="true">
      <Skeleton className="h-64 w-full rounded-[var(--radius-panel-lg)]" />
      <Skeleton className="h-8 w-48" />
      <div className="uma-mode-grid">
        <Skeleton className="h-40" />
        <Skeleton className="h-40" />
        <Skeleton className="h-40" />
      </div>
    </div>
  );
}


export function LobbyErrorState({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  useTranslation();
  const state = toErrorState(error);
  if (state.kind === "forbidden") return <ForbiddenView failure={state.failure} onRetry={onRetry} />;
  if (state.kind === "error") return <ErrorView failure={state.failure} onRetry={onRetry} />;
  return null;
}
