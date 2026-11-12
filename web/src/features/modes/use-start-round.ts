// Each startup step retains its own body and request key for retries after an uncertain response
import { useCallback, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { useApi } from "@/lib/api";
import { createIdempotencyKey, describeFailure, type Failure } from "@/lib/api/client";
import i18n from "@/i18n";
import type { components, Mode, ModeId } from "@/lib/api/types";
import { packModeConfig } from "@/lib/api/types";
import { useModes } from "@/lib/api/queries";

/**
 * 使用 GenericPackModeConfig 的模式（契约里 classic 之外的全部模式）。
 * 类型上排除 classic：它的配置结构完全不同（maxWrongGuesses/winPolicy/...），
 * 混用会在编译期就发现，而不是运行时被服务端 422 拒绝。
 */
export type PackModeId = Exclude<ModeId, "classic">;

export type StartStep = "session" | "room" | "round" | "start";
export type StepState = "pending" | "active" | "done" | "failed";

export const STEP_ORDER: StartStep[] = ["session", "room", "round", "start"];

export type UseStartRoundOptions = {
  modeId: PackModeId;
  /** Localized mode name, captured once as part of a generated room name. */
  modeName: string;
  /** 显示名（默认「玩家」） */
  displayName: string;
  /**
   * 该玩法开局必需的额外配置。
   *
   * strategy-race 的单队练习局要求 practice=true；不传就准备不出回合，
   * 单人玩家点「开始对局」必然失败。
   */
  modeConfig?: Record<string, unknown>;
};

export type StartRoundController = {
  steps: Record<StartStep, StepState>;
  running: boolean;
  failure: Failure | null;
  start: () => Promise<void>;
  reset: () => void;
};

const INITIAL_STEPS: Record<StartStep, StepState> = {
  session: "pending",
  room: "pending",
  round: "pending",
  start: "pending",
};



/** Use the advertised information style, falling back to closed until modes are loaded. */
function resolveInformationStyle(modes: readonly Mode[] | undefined, modeId: string): "open" | "closed" {
  const entry = modes?.find((mode) => mode.modeId === modeId);
  return entry?.informationStyle === "open" ? "open" : "closed";
}

type StartAttempt = {
  signature: string;
  displayName: string;
  body: components["schemas"]["RoomCreate"];
  keys: Record<"room" | "round" | "briefing" | "collecting", string>;
  roomId?: string;
  roundId?: string;
  phaseVersion: number;
  nextTransition: number;
};

export function useStartRound({ modeId, modeName, displayName, modeConfig: extraConfig }: UseStartRoundOptions): StartRoundController {
  const { api, ensureSession } = useApi();
  const navigate = useNavigate();
  const modesQuery = useModes();
  const [steps, setSteps] = useState<Record<StartStep, StepState>>(INITIAL_STEPS);
  const [running, setRunning] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const attempt = useRef<StartAttempt | null>(null);
  const inFlight = useRef(false);

  const setStep = (step: StartStep, state: StepState) =>
    setSteps(prev => ({ ...prev, [step]: state }));

  const reset = useCallback(() => {
    if (inFlight.current) return;
    attempt.current = null;
    setSteps(INITIAL_STEPS);
    setFailure(null);
  }, []);

  const start = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    setRunning(true);
    setFailure(null);
    setSteps(INITIAL_STEPS);

    try {
      // Only explicit game/form edits start a different operation. Locale and
      // refreshed metadata must not mutate an uncertain request's body or key.
      const signature = JSON.stringify([modeId, displayName.trim(), extraConfig]);
      if (!attempt.current || attempt.current.signature !== signature) {
        const name = displayName.trim() || i18n.t("start.defaultPlayer", { ns: "entry" });
        attempt.current = {
          signature,
          displayName: name,
          body: {
            name: i18n.t("start.defaultRoom", { ns: "entry", name, mode: modeName }),
            kind: "solo",
            modeConfig: {
              ...packModeConfig(modeId, "2.0.0-draft.1", resolveInformationStyle(modesQuery.data, modeId)),
              contentPackId: "",
              informationStyle: resolveInformationStyle(modesQuery.data, modeId),
              ...(extraConfig ?? {}),
            },
            maxPlayers: 1,
            maxSpectators: 0,
            visibility: "invite",
            allowSpectators: false,
          },
          keys: { room: createIdempotencyKey(), round: createIdempotencyKey(), briefing: createIdempotencyKey(), collecting: createIdempotencyKey() },
          phaseVersion: 1,
          nextTransition: 0,
        };
      }
      const current = attempt.current;
      setStep("session", "active");
      await ensureSession(current.displayName);
      setStep("session", "done");

      setStep("room", "active");
      if (!current.roomId) {
        const response = await api.createRoom(current.body, current.keys.room);
        const created = response.data as unknown as { room?: { roomId?: string }; roomId?: string } | null;
        const roomId = created?.room?.roomId ?? created?.roomId;
        if (!roomId) throw new Error("Missing created room identifier");
        current.roomId = roomId;
      }
      setStep("room", "done");

      setStep("round", "active");
      if (!current.roundId) {
        const response = await api.createRound(current.roomId, { modeConfig: current.body.modeConfig }, undefined, current.keys.round);
        const round = response.data as unknown as { roundId?: string; phaseVersion?: number } | null;
        if (!round?.roundId) throw new Error("Missing created round identifier");
        current.roundId = round.roundId;
        current.phaseVersion = round.phaseVersion ?? 1;
      }
      setStep("round", "done");

      setStep("start", "active");
      const transitions = ["briefing", "collecting"] as const;
      while (current.nextTransition < transitions.length) {
        const target = transitions[current.nextTransition]!;
        const response = await api.createTransition(current.roundId, { target, expectedPhaseVersion: current.phaseVersion }, undefined, current.keys[target]);
        current.phaseVersion = (response.data as unknown as { phaseVersion?: number } | null)?.phaseVersion ?? current.phaseVersion + 1;
        current.nextTransition++;
      }
      setStep("start", "done");
      navigate(`/rooms/${encodeURIComponent(current.roomId)}`);
    } catch (error) {
      setFailure(describeFailure(error));
      setSteps(prev => {
        const next = { ...prev };
        for (const step of STEP_ORDER) if (next[step] === "active") next[step] = "failed";
        return next;
      });
    } finally {
      inFlight.current = false;
      setRunning(false);
    }
  }, [api, displayName, ensureSession, extraConfig, modeId, modeName, modesQuery.data, navigate]);

  return { steps, running, failure, start, reset };
}
