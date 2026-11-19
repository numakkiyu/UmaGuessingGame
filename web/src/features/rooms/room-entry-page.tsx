import "@/i18n/matchmaking";
// Route by the server's current round so a direct refresh returns to the same game
import "@/i18n/room";
import { useTranslation } from "@/i18n";
import { lazy, Suspense } from "react";
import { Link, useParams } from "react-router";
import { EmptyView, ErrorView, LoadingBlock } from "@/components/uma/page-state";
import { useRoom } from "@/lib/api/room-queries";
import { describeFailure } from "@/lib/api/client";
import { RoomManagement } from "./room-management";
import { RoomRoster } from "./room-roster";
import { Button } from "@/components/ui/button";

const ClassicRoomPage = lazy(() =>
  import("@/features/classic/classic-room-page").then((m) => ({ default: m.ClassicRoomPage })),
);
const SortRoundPage = lazy(() =>
  import("@/features/modes/sort-round-page").then((m) => ({ default: m.SortRoundPage })),
);
const MemoryRoundPage = lazy(() =>
  import("@/features/modes/memory-round-page").then((m) => ({ default: m.MemoryRoundPage })),
);
const PedigreeRoundPage = lazy(() =>
  import("@/features/modes/pedigree-round-page").then((m) => ({ default: m.PedigreeRoundPage })),
);
const AudioRoundPage = lazy(() =>
  import("@/features/modes/audio-round-page").then((m) => ({ default: m.AudioRoundPage })),
);
const BluffRoundPage = lazy(() =>
  import("@/features/modes/bluff-round-page").then((m) => ({ default: m.BluffRoundPage })),
);
const CluePuzzleRoundPage = lazy(() =>
  import("@/features/modes/clue-puzzle-round-page").then((m) => ({ default: m.CluePuzzleRoundPage })),
);
const StrategyRaceRoundPage = lazy(() =>
  import("@/features/modes/strategy-race-round-page").then((m) => ({ default: m.StrategyRaceRoundPage })),
);
const RapportRoundPage = lazy(() =>
  import("@/features/modes/rapport-round-page").then((m) => ({ default: m.RapportRoundPage })),
);
const CreativeRoundPage = lazy(() =>
  import("@/features/modes/creative-round-page").then((m) => ({ default: m.CreativeRoundPage })),
);
const TradeRoundPage = lazy(() =>
  import("@/features/modes/trade-round-page").then((m) => ({ default: m.TradeRoundPage })),
);
const AptitudeGridRoundPage = lazy(() =>
  import("@/features/modes/aptitude-grid-round-page").then((m) => ({ default: m.AptitudeGridRoundPage })),
);
const DraftRoundPage = lazy(() =>
  import("@/features/modes/draft-round-page").then((m) => ({ default: m.DraftRoundPage })),
);
const RaceRebuildRoundPage = lazy(() =>
  import("@/features/modes/race-rebuild-round-page").then((m) => ({ default: m.RaceRebuildRoundPage })),
);
const CoRacePathRoundPage = lazy(() =>
  import("@/features/modes/co-race-path-round-page").then((m) => ({ default: m.CoRacePathRoundPage })),
);
const RoomLobbyView = lazy(() =>
  import("@/features/rooms/room-page").then((m) => ({ default: m.RoomLobbyView })),
);

export function RoomEntryPage() {
  const { t } = useTranslation(["room","matchmaking"]);
  const { roomId } = useParams<{ roomId: string }>();

  // 复用与子视图相同的查询键，避免重复请求
  const roomQuery = useRoom(roomId ?? null);

  if (!roomId) {
    return (
      <ErrorView
        failure={{
          kind: "error",
          title: t("missingRoomTitle"),
          detail: t("missingRoomDetail"),
          diagnosticId: null,
          retryable: false,
        }}
      />
    );
  }

  if (roomQuery.isLoading) {
    return (
      <div className="uma-stack">
        <LoadingBlock label={t("loadingRoom")} rows={3} />
      </div>
    );
  }

  if (roomQuery.isError) {
    const failure = describeFailure(roomQuery.error);
    return <ErrorView failure={failure} onRetry={() => void roomQuery.refetch()} />;
  }

  const room = roomQuery.data?.room;
  const currentRound = room?.currentRound;

  /*
   * 按回合的 modeId 分流到对应玩法页。
   * 未实现的模式给出明确说明，而不是误用经典页（那会让用户以为在玩经典）。
   */
  const renderRound = () => {
    if (!currentRound) return <RoomLobbyView roomId={roomId} />;
    switch (currentRound.modeId) {
      case "classic":
        return <ClassicRoomPage />;
      case "sort":
        return <SortRoundPage />;
      case "memory":
        return <MemoryRoundPage />;
      case "aptitude-grid":
        return <AptitudeGridRoundPage />;
      case "draft":
        return <DraftRoundPage />;
      case "race-rebuild":
        return <RaceRebuildRoundPage />;
      case "co-race-path":
        return <CoRacePathRoundPage />;
      case "pedigree":
        return <PedigreeRoundPage />;
      case "audio":
        return <AudioRoundPage />;
      case "bluff":
        return <BluffRoundPage />;
      case "clue-puzzle":
        return <CluePuzzleRoundPage />;
      case "strategy-race":
        return <StrategyRaceRoundPage />;
      case "rapport":
        return <RapportRoundPage />;
      case "creative":
        return <CreativeRoundPage />;
      case "trade":
        return <TradeRoundPage />;
      default:
        return (
          <EmptyView
            title={t("unavailableTitle")}
            detail={t("unavailableDetail", { mode: t("unknownMode") })}
            action={
              <Button asChild variant="secondary">
                <Link to="/">{t("home")}</Link>
              </Button>
            }
          />
        );
    }
  };

  return (
    <Suspense
      key={currentRound?.roundId ?? roomId}
      fallback={
        <div className="uma-stack">
          <LoadingBlock label={t("loadingInterface")} rows={3} />
        </div>
      }
    >
      <div className={currentRound ? "uma-stack uma-play-surface" : "uma-stack"}>{renderRound()}{room?.kind === "matchmaking" && currentRound && ["settled","cancelled"].includes(currentRound.phase) ? <Button asChild variant="primary" size="primary"><Link to="/matchmaking">{t("again",{ns:"matchmaking"})}</Link></Button> : null}{currentRound && room ? <><details className="uma-disclosure"><summary className="min-h-11 cursor-pointer content-center font-semibold">{t("viewMembers")}</summary><RoomRoster room={room} /></details><RoomManagement room={room} onChanged={() => roomQuery.refetch()} /></> : null}</div>
    </Suspense>
  );
}
