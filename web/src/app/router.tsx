// Game routes are lazy-loaded so entering the lobby does not load every mode
import { lazy, Suspense, useEffect, type ReactNode } from "react";
import { createBrowserRouter, Navigate, useLocation, type RouteObject } from "react-router";
import { useApi } from "@/lib/api";
import { useTranslation } from "@/i18n";
import { AppShell } from "./app-shell";
import { RouteErrorBoundary } from "./error-boundary";
import { lazyWithRecovery } from "./lazy-recovery";
const PlayFormatGate = lazy(() => import("@/components/uma/play-format-gate").then(m => ({default:m.PlayFormatGate})));
import { LoadingBlock } from "@/components/uma/page-state";
// 大厅同步加载；加入页、长篇帮助与来源按需加载。
import { LobbyPage } from "@/features/rooms/lobby-page";
const JoinPage = lazyWithRecovery(() => import("@/features/rooms/join-page").then(m => ({default:m.JoinPage})));
const HelpPage = lazyWithRecovery(() => import("@/features/rooms/help-page").then(m => ({default:m.HelpPage})));
const SourcesPage = lazyWithRecovery(() => import("@/features/rooms/sources-page").then(m => ({default:m.SourcesPage})));
import { NotFoundPage } from "./not-found";

// 玩法与活动页懒加载：首屏不加载全部模式
const ClassicPlayPage = lazyWithRecovery(() =>
  import("@/features/practice/practice-start-page").then((m) => ({ default: m.PracticeStartPage })),
);
const ChallengeGamePage = lazyWithRecovery(() =>
  import("@/features/challenges/challenge-game-page").then(m => ({default:m.ChallengeGamePage})),
);
const PracticeGamePage = lazyWithRecovery(() =>
  import("@/features/practice/practice-game-page").then((m) => ({ default: m.PracticeGamePage })),
);
const RoomEntryPage = lazyWithRecovery(() =>
  import("@/features/rooms/room-entry-page").then((m) => ({ default: m.RoomEntryPage })),
);
const ModeInfoPage = lazyWithRecovery(() =>
  import("@/features/modes/mode-info-page").then((m) => ({ default: m.ModeInfoPage })),
);

// –G07 五个玩法的开局页（各自独立 chunk）
const BluffPlayEntryPage = lazyWithRecovery(() =>
  import("@/features/modes/mode-play-pages").then((m) => ({ default: m.BluffPlayEntryPage })),
);
const CluePuzzlePlayPage = lazyWithRecovery(() =>
  import("@/features/modes/mode-play-pages").then((m) => ({ default: m.CluePuzzlePlayPage })),
);
const RapportPlayPage = lazyWithRecovery(() =>
  import("@/features/modes/mode-play-pages").then((m) => ({ default: m.RapportPlayPage })),
);
const CreativePlayPage = lazyWithRecovery(() =>
  import("@/features/modes/mode-play-pages").then((m) => ({ default: m.CreativePlayPage })),
);
const TradePlayPage = lazyWithRecovery(() =>
  import("@/features/modes/mode-play-pages").then((m) => ({ default: m.TradePlayPage })),
);

const MatchmakingPage = lazyWithRecovery(() =>
  import("@/features/rooms/matchmaking-page").then((m) => ({ default: m.MatchmakingPage })),
);
const CreateRoomPage = lazyWithRecovery(() => import("@/features/rooms/create-room-page").then(m => ({default:m.CreateRoomPage})));
const CreateEventPage = lazyWithRecovery(() =>
  import("@/features/events/create-event-page").then((m) => ({ default: m.CreateEventPage })),
);
const EventHostPage = lazyWithRecovery(() =>
  import("@/features/events/event-host-page").then((m) => ({ default: m.EventHostPage })),
);
const EventDisplayPage = lazyWithRecovery(() =>
  import("@/features/display/event-display-page").then((m) => ({ default: m.EventDisplayPage })),
);
const EventResultsPage = lazyWithRecovery(() =>
  import("@/features/events/event-results-page").then((m) => ({ default: m.EventResultsPage })),
);

/** 懒加载路由的骨架：结构与目标页一致，避免布局跳动 */
function LazyFallback() {
  const { t } = useTranslation("common");
  return (
    <div className="uma-stack" role="status" aria-live="polite" data-route-loading>
      <span className="sr-only">{t("state.loading")}</span>
      <LoadingBlock label={t("state.loading")} rows={3} />
    </div>
  );
}

function withSuspense(node: ReactNode): ReactNode {
  return <Suspense fallback={<LazyFallback />}>{node}</Suspense>;
}

const SortPracticeStartPage = lazyWithRecovery(() => import("@/features/practice/sort-practice-start-page").then(m=>({default:m.SortPracticeStartPage})));
const SortPracticeGamePage = lazyWithRecovery(() => import("@/features/practice/sort-practice-game-page").then(m=>({default:m.SortPracticeGamePage})));

const MemoryPracticeStartPage = lazyWithRecovery(() => import("@/features/practice/memory-practice-start-page").then(m => ({default:m.MemoryPracticeStartPage})));
const MemoryPracticeGamePage = lazyWithRecovery(() => import("@/features/practice/memory-practice-game-page").then(m => ({default:m.MemoryPracticeGamePage})));

const GridPracticeStartPage = lazyWithRecovery(() => import("@/features/practice/grid-practice-pages").then(m => ({default:m.GridPracticeStartPage})));
const GridPracticeGamePage = lazyWithRecovery(() => import("@/features/practice/grid-practice-pages").then(m => ({default:m.GridPracticeGamePage})));
const DraftPracticeStartPage = lazyWithRecovery(() => import("@/features/practice/draft-practice-pages").then(m => ({default:m.DraftPracticeStartPage})));
const DraftPracticeGamePage = lazyWithRecovery(() => import("@/features/practice/draft-practice-pages").then(m => ({default:m.DraftPracticeGamePage})));

const RacePracticeStartPage = lazyWithRecovery(() => import("@/features/practice/race-practice-pages").then(m => ({default:m.RacePracticeStartPage})));
const RacePracticeGamePage = lazyWithRecovery(() => import("@/features/practice/race-practice-pages").then(m => ({default:m.RacePracticeGamePage})));

const PathPracticeStartPage = lazyWithRecovery(() => import("@/features/practice/path-practice-pages").then(m => ({default:m.PathPracticeStartPage})));
const PathPracticeGamePage = lazyWithRecovery(() => import("@/features/practice/path-practice-pages").then(m => ({default:m.PathPracticeGamePage})));

const AudioPracticeStartPage = lazyWithRecovery(() => import("@/features/practice/audio-practice-pages").then(m => ({default:m.AudioPracticeStartPage})));
const AudioPracticeGamePage = lazyWithRecovery(() => import("@/features/practice/audio-practice-pages").then(m => ({default:m.AudioPracticeGamePage})));
const PedigreePracticeStartPage = lazyWithRecovery(() => import("@/features/practice/pedigree-practice-pages").then(m => ({default:m.PedigreePracticeStartPage})));
const PedigreePracticeGamePage = lazyWithRecovery(() => import("@/features/practice/pedigree-practice-pages").then(m => ({default:m.PedigreePracticeGamePage})));
const StrategyPracticeStartPage = lazyWithRecovery(() => import("@/features/practice/strategy-practice-pages").then(m => ({default:m.StrategyPracticeStartPage})));
const StrategyPracticeGamePage = lazyWithRecovery(() => import("@/features/practice/strategy-practice-pages").then(m => ({default:m.StrategyPracticeGamePage})));

function SessionBoundary({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  const { restoreSession } = useApi();
  useEffect(() => {
    if (!/^\/(practice(?:\/|$)|play\/(?:classic|sort|memory|aptitude-grid|draft|race-rebuild|co-race-path|strategy-race|pedigree|audio)(?:\/(?:online|verified))?\/?$)/.test(pathname)) {
      // A failed recovery must never silently create a replacement identity.
      // Online actions retry explicitly through ensureSession.
      void restoreSession?.().catch(() => undefined);
    }
  }, [pathname, restoreSession]);
  return children;
}

export const routes: RouteObject[] = [
  /*
   * 大屏是独立舞台布局，刻意不放进 AppShell。
   *
   * 原因：投影场景不需要站点导航、页脚和跳过链接；把它们一起投到大屏上
   * 既占掉主舞台高度，也会让观众看到与当前活动无关的入口。
   * 这一层自己管理安全边距与全屏高度，不做「桌面缩放」。
   */
  {
    path: "/events/:eventId/display",
    element: <SessionBoundary>{withSuspense(<EventDisplayPage />)}</SessionBoundary>,
    errorElement: <RouteErrorBoundary />,
  },
  {
    path: "/",
    element: <SessionBoundary><AppShell /></SessionBoundary>,
    errorElement: <RouteErrorBoundary />,
    children: [
      { index: true, element: <LobbyPage /> },
      { path:"friends",element:withSuspense(<PlayFormatGate form="friend"><LobbyPage /></PlayFormatGate>) },
      { path:"party",element:withSuspense(<PlayFormatGate form="event"><LobbyPage /></PlayFormatGate>) },


      { path: "play/classic", element: withSuspense(<ClassicPlayPage />) },
      { path: "practice", element: withSuspense(<ClassicPlayPage />) },
      { path: "practice/:gameId", element: withSuspense(<PracticeGamePage />) },
      { path: "play/classic/verified", element: <Navigate replace to="/play/classic" /> },
      { path: "challenges/:challengeId", element: withSuspense(<ChallengeGamePage />) },
      { path: "play/classic/online", element: <Navigate replace to="/play/classic" /> },
      { path: "play/sort", element: withSuspense(<SortPracticeStartPage />) },
      { path: "practice/sort/:gameId", element: withSuspense(<SortPracticeGamePage />) },
      { path: "play/sort/online", element: <Navigate replace to="/play/sort" /> },
      { path: "play/aptitude-grid", element: withSuspense(<GridPracticeStartPage />) },
      { path: "practice/aptitude-grid/:gameId", element: withSuspense(<GridPracticeGamePage />) },
      { path: "play/aptitude-grid/online", element: <Navigate replace to="/play/aptitude-grid" /> },
      { path: "play/draft", element: withSuspense(<DraftPracticeStartPage />) },
      { path: "practice/draft/:gameId", element: withSuspense(<DraftPracticeGamePage />) },
      { path: "play/draft/online", element: <Navigate replace to="/play/draft" /> },
      { path: "play/race-rebuild", element: withSuspense(<RacePracticeStartPage />) },
      { path: "practice/race-rebuild/:gameId", element: withSuspense(<RacePracticeGamePage />) },
      { path: "play/race-rebuild/online", element: <Navigate replace to="/play/race-rebuild" /> },
      { path: "play/co-race-path", element: withSuspense(<PathPracticeStartPage />) },
      { path: "practice/co-race-path/:gameId", element: withSuspense(<PathPracticeGamePage />) },
      { path: "play/co-race-path/online", element: <Navigate replace to="/play/co-race-path" /> },
      { path: "play/memory", element: withSuspense(<MemoryPracticeStartPage />) },
      { path: "practice/memory/:gameId", element: withSuspense(<MemoryPracticeGamePage />) },
      { path: "play/memory/online", element: <Navigate replace to="/play/memory" /> },
      { path: "play/pedigree", element: withSuspense(<PedigreePracticeStartPage />) },
      { path: "practice/pedigree/:gameId", element: withSuspense(<PedigreePracticeGamePage />) },
      { path: "play/pedigree/online", element: <Navigate replace to="/play/pedigree" /> },
      { path: "play/audio", element: withSuspense(<AudioPracticeStartPage />) },
      { path: "practice/audio/:gameId", element: withSuspense(<AudioPracticeGamePage />) },
      { path: "play/audio/online", element: <Navigate replace to="/play/audio" /> },
      { path: "play/bluff", element: withSuspense(<BluffPlayEntryPage />) },
      // These four used to fall through to the generic info page, which told the player
      // "this mode has its own entry, go back to the lobby" — but the lobby led right
      // back here. The server could start these rounds the whole time; only the entry
      // was missing, so the mode was unreachable from any UI.
      { path: "play/clue-puzzle", element: withSuspense(<CluePuzzlePlayPage />) },
      { path: "play/strategy-race", element: withSuspense(<StrategyPracticeStartPage />) },
      { path: "practice/strategy-race/:gameId", element: withSuspense(<StrategyPracticeGamePage />) },
      { path: "play/strategy-race/online", element: <Navigate replace to="/play/strategy-race" /> },
      { path: "play/rapport", element: withSuspense(<RapportPlayPage />) },
      { path: "play/creative", element: withSuspense(<CreativePlayPage />) },
      { path: "play/trade", element: withSuspense(<TradePlayPage />) },
      // 其余已发布模式走通用说明页
      { path: "play/:modeId", element: withSuspense(<ModeInfoPage />) },

      { path: "join", element: withSuspense(<JoinPage />) },
      { path: "rooms/new", element: withSuspense(<PlayFormatGate form="friend"><CreateRoomPage /></PlayFormatGate>) },
      // /rooms/:roomId 按「有无进行中回合」分流到等待区或对应玩法页
      { path: "rooms/:roomId", element: withSuspense(<RoomEntryPage />) },
      { path: "matchmaking", element: withSuspense(<PlayFormatGate form="matchmaking"><MatchmakingPage /></PlayFormatGate>) },
      { path: "events/new", element: withSuspense(<PlayFormatGate form="event"><CreateEventPage /></PlayFormatGate>) },
      { path: "events/:eventId/host", element: withSuspense(<EventHostPage />) },
      { path: "events/:eventId/results", element: withSuspense(<EventResultsPage />) },
      { path: "help", element: withSuspense(<HelpPage />) },
      { path: "sources", element: withSuspense(<SourcesPage />) },
      { path: "*", element: <NotFoundPage /> },
    ],
  },
];

export const router = createBrowserRouter(routes);
