import { roomDisplayName } from "@/lib/room-display-name";
import "@/i18n/start-requirements";
import { startIssue } from "./start-requirements";

import "@/i18n/room";
import { useTranslation } from "@/i18n";
import { Loader2, Play, UserRound } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router";
import { EmptyView, ErrorView, ForbiddenView, LoadingBlock } from "@/components/uma/page-state";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useApi } from "@/lib/api";
import { describeFailure, localizeFailure } from "@/lib/api/client";
import { toKnownModeId } from "@/lib/api/types";
import { useRoom } from "@/lib/api/room-queries";
import { TeamPanel } from "./team-panel";
import { RoomRoster } from "./room-roster";
import { RoomManagement } from "./room-management";
import { ConnectionBar } from "@/components/uma/connection-bar";
import { connectionDetail } from "@/lib/copy";
import { useRealtime } from "@/lib/realtime/use-realtime";
const ROOM_STATUSES = ["lobby", "ready", "playing", "finished", "archived", "cancelled"];

export function RoomLobbyView({ roomId }: { roomId: string }) {
  const { t } = useTranslation(["room", "modes"]);
  const { api } = useApi();
  const { t: requirement } = useTranslation("startRequirements");
  const roomQuery = useRoom(roomId);
  const [starting, setStarting] = useState(false);
  const [updatingReady, setUpdatingReady] = useState(false);
  const [copied, setCopied] = useState("");
  const [failure, setFailure] = useState<ReturnType<typeof describeFailure> | null>(null);

  // Track joins, departures and round starts while players wait
  const realtime = useRealtime({
    roomId,
    enabled: Boolean(roomId),
    handlers: {
      onSnapshotNeeded: () => {
        void roomQuery.refetch();
      },
    },
  });

  const connectionStatus = realtime.status;
  const info = failure ? localizeFailure(failure) : null;

  if (roomQuery.isLoading) return <LoadingBlock label={t("loadingRoom")} rows={3} />;

  if (roomQuery.isError) {
    const info = describeFailure(roomQuery.error);
    return info.kind === "forbidden" ? (
      <ForbiddenView failure={info} onRetry={() => void roomQuery.refetch()} />
    ) : (
      <ErrorView failure={info} onRetry={() => void roomQuery.refetch()} />
    );
  }

  const room = roomQuery.data?.room;
  if (!room) return <EmptyView title={t("missingTitle")} detail={t("missingDetail")} />;

  const self = room.viewer;
  const canStart = self.capabilities.includes("host") || self.capabilities.includes("manage-room");

  const issue = startIssue(room.modeId, room.members);

  async function startRound() {
    if (starting || issue) return;
    setStarting(true);
    setFailure(null);
    try {
      const roundResponse = await api.createRound(roomId, {});
      const roundId = (roundResponse.data as unknown as { roundId?: string } | null)?.roundId;
      if (!roundId) throw new Error("创建回合后没有拿到 roundId");
      let phaseVersion = (roundResponse.data as unknown as { phaseVersion?: number } | null)?.phaseVersion ?? 1;
      const phases = ["memory","audio"].includes(roundResponse.data?.modeId??"") ? ["briefing"] as const : ["briefing", "collecting"] as const;
      for (const target of phases) {
        const transition = await api.createTransition(roundId, {
          target,
          expectedPhaseVersion: phaseVersion,
        });
        phaseVersion = (transition.data as unknown as { phaseVersion?: number } | null)?.phaseVersion ?? phaseVersion + 1;
      }
      await roomQuery.refetch();
    } catch (error) {
      setFailure(describeFailure(error));
    } finally {
      setStarting(false);
    }
  }

  async function toggleReady() {
    if (updatingReady || !room) return;
    setUpdatingReady(true); setFailure(null);
    try {
      await api.updateMembership(roomId, self.membershipId, { ready: !self.ready, expectedRoomVersion: room.roomVersion });
      await roomQuery.refetch();
    } catch (error) { setFailure(describeFailure(error)); await roomQuery.refetch(); }
    finally { setUpdatingReady(false); }
  }
  async function copyInvitation() {
    if (!room?.inviteCode) return;
    try {
      await navigator.clipboard.writeText(new URL(`/join?code=${encodeURIComponent(room.inviteCode)}`, window.location.origin).href);
      setCopied("copied");
    } catch { setCopied("copyFailed"); }
  }

  return (
    <div className="uma-stack">
      <ConnectionBar
        status={connectionStatus}
        detail={connectionDetail(roomDisplayName(room, t("forms.matchmaking")), realtime.transport)}
      />

      <section className="uma-panel p-5">
        <h1 className="u-fluid-task text-[var(--color-ink)]">{room.kind === "matchmaking" ? t("forms.matchmaking") : room.name}</h1>
        {/*
          ：不显示 room.status 原始值与 roomVersion。
          「lobby / roomVersion」是内部状态，玩家只需要知道「在等人」还是「已开始」。
        */}
        <p className="mt-1 text-sm text-[var(--color-muted)]">
          {t(`forms.${room.kind === "solo" || room.kind === "friend" || room.kind === "matchmaking" ? room.kind : "event"}`)} ·{" "}
          {t(ROOM_STATUSES.includes(room.status) ? `statuses.${room.status}` : "inProgress")}
        </p>
        <p className="mt-2 font-semibold text-[var(--color-action-green)]">{t("project", { mode: toKnownModeId(room.modeId) ? t(`${room.modeId}.name`, { ns: "modes" }) : t("unknownMode") })}</p>
        {/* 分享使用邀请链接和匹配码，不展示内部 roomId */}
      </section>

      {canStart && room.kind !== "solo" && room.kind !== "matchmaking" && room.inviteCode ? <section className="uma-card border-[var(--color-brand-green)] p-4" aria-labelledby="room-invitation"><h2 id="room-invitation" className="text-base font-bold">{t("invite")}</h2><div className="mt-3 flex flex-wrap items-center gap-3"><p className="text-sm">{t("codeLabel")}<strong className="uma-code select-all text-xl tracking-widest" data-testid="room-invite-code">{room.inviteCode}</strong></p><Button variant="secondary" onClick={() => void copyInvitation()}>{t("copyInvite")}</Button></div><p role="status" className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t(copied || "inviteHint")}</p></section> : null}

      <RoomRoster room={room} />
      {self.capabilities.includes("play") && room.kind !== "matchmaking" ? <Button className="w-full sm:w-fit" variant="secondary" disabled={updatingReady || starting} onClick={() => void toggleReady()}>{t(updatingReady ? "confirming" : self.ready ? "cancelReady" : "getReady")}</Button> : null}

      {info ? (
        <Alert tone="danger" title={info.title}>
          <p>{info.detail}</p>
          {info.diagnosticId ? (
            <p className="mt-1 text-[length:var(--text-aux)]">
              {t("problemId")}：<span className="uma-code">{info.diagnosticId}</span>
            </p>
          ) : null}
        </Alert>
      ) : null}

      {/* Optional setup stays folded so the main action follows the roster on
          narrow screens. Existing teams remain accessible in older rooms. */}
      {room.kind === "event" || ["clue-puzzle","strategy-race","rapport"].includes(room.modeId) || room.teams.length > 0 ? (
        <details className="uma-card uma-disclosure p-4" open={room.teams.length > 0 ? true : undefined}>
          <summary className="min-h-11 cursor-pointer content-center font-semibold">{t(room.teams.length > 0 ? "adjustTeams" : "needTeams")}</summary>
          <div className="mt-3"><TeamPanel
            roomId={roomId}
            room={room}
            canManage={canStart}
            locked={room.status === "playing" || room.status === "finished"}
            onChanged={() => roomQuery.refetch()}
          /></div>
        </details>
      ) : null}

      {/* 开局控制在房主区；非房主只看到说明 */}
      <section className="uma-card p-4">
        {canStart ? (
          <>
            <h2 className="text-base font-bold text-[var(--color-ink)]">{t("start")}</h2>
            <p className="mt-1 text-sm text-[var(--color-muted)]">
              {t("startHint", { mode: toKnownModeId(room.modeId) ? t(`${room.modeId}.name`, { ns: "modes" }) : t("unknownMode") })}
            </p>
            {issue ? <p role="status" className="mt-3 text-sm leading-6 text-[var(--color-near-edge)]">{requirement(issue)}</p> : null}
            <Button
              variant="primary"
              size="primary"
              className="mt-3 h-auto min-h-11 w-full whitespace-normal sm:w-auto"
              onClick={() => void startRound()}
              disabled={starting || !!issue}
            >
              {starting ? (
                <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin" />
              ) : (
                <Play aria-hidden="true" className="size-4" />
              )}
              {t(starting ? "starting" : "start")}
            </Button>
          </>
        ) : (
          <>
            <h2 className="flex items-center gap-2 text-base font-bold text-[var(--color-ink)]">
              <UserRound aria-hidden="true" className="size-4" />
              {t("waitHost", { role: t(room.kind === "event" ? "host" : "owner") })}
            </h2>
            <p className="mt-1 text-sm text-[var(--color-muted)]">
              {t("waitHint", { role: t(room.kind === "event" ? "host" : "owner") })}
            </p>
          </>
        )}
      </section>

      <RoomManagement room={room} onChanged={() => roomQuery.refetch()} />

      <div className="flex flex-col gap-2">
        <Button asChild variant="secondary" size="primary">
          <Link to="/">{t("home")}</Link>
        </Button>
      </div>
    </div>
  );
}

/** 路由入口：等待区（无进行中回合时） */
export function RoomPage() {
  const { t } = useTranslation("room");
  const { roomId } = useParams<{ roomId: string }>();
  if (!roomId) return <ErrorView failure={{ kind: "error", title: t("missingRoomTitle"), detail: t("missingRoomDetail"), diagnosticId: null, retryable: false }} />;
  return <RoomLobbyView roomId={roomId} />;
}
