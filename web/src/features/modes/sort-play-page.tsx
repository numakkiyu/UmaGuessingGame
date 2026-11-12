// An empty contentPackId selects the active pack on the server instead of pinning a build-specific identifier
import { Check, Loader2 } from "lucide-react";
import { useCallback, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useApi } from "@/lib/api";
import { createIdempotencyKey, describeFailure, type Failure } from "@/lib/api/client";
import { useModes, usePublicConfig } from "@/lib/api/queries";
import { canPlay } from "@/lib/deployment/capabilities";
import { cn } from "@/lib/utils";
import { describeUnavailable } from "@/lib/copy/unavailable";

type Step = "session" | "room" | "round" | "start";
type StepState = "pending" | "active" | "done" | "failed";

const STEP_LABELS: Record<Step, string> = {
  session: "正在准备你的座位",
  room: "创建单人房间",
  round: "生成本局题目",
  start: "开始接受提交",
};

const STEP_ORDER: Step[] = ["session", "room", "round", "start"];

export function SortPlayPage() {
  const { api, session, ensureSession } = useApi();
  const navigate = useNavigate();
  const modesQuery = useModes();
  const config = usePublicConfig();
  const soloAllowed = !config.isPending && !config.isError && canPlay(config.data, "sort", "solo");

  const [displayName, setDisplayName] = useState(session?.displayName ?? "");
  const [steps, setSteps] = useState<Record<Step, StepState>>({
    session: "pending",
    room: "pending",
    round: "pending",
    start: "pending",
  });
  const [running, setRunning] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [keys, setKeys] = useState<Partial<Record<Step, string>>>({});

  const mode = (modesQuery.data ?? []).find((item) => item.modeId === "sort");
  // 准入诊断翻译成玩家说法（内部拒绝码不直出）。
  const unavailableCopy = mode && mode.availability !== "available"
    ? describeUnavailable(mode.modeId, mode.unavailableReason)
    : null;

  const setStep = (step: Step, state: StepState) => setSteps((prev) => ({ ...prev, [step]: state }));

  const start = useCallback(async () => {
    if (running || !soloAllowed) return;
    setRunning(true);
    setFailure(null);
    setSteps({ session: "pending", room: "pending", round: "pending", start: "pending" });

    const keyFor = (step: Step) => {
      const existing = keys[step];
      if (existing) return existing;
      const created = createIdempotencyKey();
      setKeys((prev) => ({ ...prev, [step]: created }));
      return created;
    };

    try {
      setStep("session", "active");
      const name = displayName.trim() || "玩家";
      await ensureSession(name);
      setStep("session", "done");

      // sort 属于 GenericPackModeConfig，需要 contentPackId 与 rulesVersion
      const rulesVersion = mode?.rulesVersion ?? "2.0.0-draft.1";

      setStep("room", "active");
      const roomResponse = await api.createRoom(
        {
          name: `${name}的排序对局`,
          kind: "solo",
          modeConfig: {
            modeId: "sort",
            rulesVersion,
            // 空串：服务端按当前活动内容包解析（实测可接受，且前端拿不到真实包名）
            contentPackId: "",
            informationStyle: "open",
          },
          maxPlayers: 1,
          maxSpectators: 0,
          visibility: "invite",
          allowSpectators: false,
        },
        keyFor("room"),
      );
      const created = roomResponse.data as unknown as { room?: { roomId?: string }; roomId?: string } | null;
      const roomId = created?.room?.roomId ?? created?.roomId;
      if (!roomId) throw new Error("创建房间后没有拿到 roomId");
      setStep("room", "done");

      setStep("round", "active");
      const roundResponse = await api.createRound(roomId, {
        modeConfig: {
          modeId: "sort",
          rulesVersion,
          contentPackId: "",
          informationStyle: "open",
        },
      });
      const round = roundResponse.data as unknown as { roundId?: string; phaseVersion?: number } | null;
      const roundId = round?.roundId;
      if (!roundId) throw new Error("创建回合后没有拿到 roundId");
      setStep("round", "done");

      setStep("start", "active");
      let phaseVersion = round?.phaseVersion ?? 1;
      for (const target of ["briefing", "collecting"] as const) {
        const transition = await api.createTransition(roundId, {
          target,
          expectedPhaseVersion: phaseVersion,
        });
        phaseVersion =
          (transition.data as unknown as { phaseVersion?: number } | null)?.phaseVersion ?? phaseVersion + 1;
      }
      setStep("start", "done");

      navigate(`/rooms/${encodeURIComponent(roomId)}`);
    } catch (error) {
      setFailure(describeFailure(error));
      setSteps((prev) => {
        const next = { ...prev };
        for (const step of STEP_ORDER) if (next[step] === "active") next[step] = "failed";
        return next;
      });
    } finally {
      setRunning(false);
    }
  }, [api, displayName, ensureSession, keys, mode, navigate, running, soloAllowed]);

  return (
    <div className="uma-prose mx-auto flex flex-col gap-5">
      <header>
        <h1 className="u-fluid-title text-[var(--color-ink)]">按身高排序</h1>
        <p className="mt-2 text-base leading-7 text-[var(--color-ink)]">
          系统给出若干张马娘卡，你要按<strong>身高从小到大</strong>排好。
          排序值相同的卡要标成并列——只排顺序、不标并列会被判错
        </p>
      </header>

      {unavailableCopy ? (
        <Alert tone="warning" title={unavailableCopy.title}>
          <p>{unavailableCopy.detail}</p>
        </Alert>
      ) : null}

      {!config.isPending && !soloAllowed ? <Alert tone="info" title="这里暂未开放单人对局">可以返回大厅选择其他游玩方式</Alert> : null}

      <section className="uma-panel flex flex-col gap-4 p-5">
        <div className="flex flex-col gap-2">
          <Label htmlFor="sort-name">显示名</Label>
          <Input
            id="sort-name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            placeholder="其他玩家看到的名字"
            maxLength={24}
            disabled={running}
            autoComplete="nickname"
          />
        </div>

        <Button
          variant="primary"
          size="primary"
          onClick={() => void start()}
          disabled={running || Boolean(unavailableCopy) || !soloAllowed}
          block
        >
          {running ? "正在开局……" : "开始对局"}
        </Button>

        {running || failure ? (
          <ol className="flex flex-col gap-1.5 rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] p-3">
            {STEP_ORDER.map((step) => {
              const state = steps[step];
              return (
                <li key={step} className="flex items-center gap-2 text-sm">
                  {state === "done" ? (
                    <Check aria-hidden="true" className="size-4 text-[var(--color-action-green)]" />
                  ) : state === "active" ? (
                    <Loader2
                      aria-hidden="true"
                      className="size-4 text-[var(--color-blue-deep)] motion-safe:animate-spin"
                    />
                  ) : state === "failed" ? (
                    <span aria-hidden="true" className="inline-block size-4 rounded-full bg-[var(--color-danger)]" />
                  ) : (
                    <span
                      aria-hidden="true"
                      className="inline-block size-4 rounded-full border border-[var(--color-line-strong)]"
                    />
                  )}
                  <span className={cn(state === "pending" ? "text-[var(--color-muted)]" : "font-semibold text-[var(--color-ink)]")}>
                    {STEP_LABELS[step]}
                  </span>
                </li>
              );
            })}
          </ol>
        ) : null}

        {failure ? (
          <Alert
            tone="danger"
            title={failure.title}
            action={
              <Button variant="secondary" onClick={() => void start()}>
                再试一次
              </Button>
            }
          >
            <p>{failure.detail}</p>
            {failure.diagnosticId ? (
              <p className="mt-1 text-[length:var(--text-aux)]">
                问题编号：<span className="uma-code">{failure.diagnosticId}</span>
              </p>
            ) : null}
          </Alert>
        ) : null}
      </section>

      {/*
        计分方式默认折叠。
        原来它以常驻卡片放在「显示名 + 开始对局」之前，手机 390 下把主操作
        压到 500px 以下；桌面也让它排在首屏最显眼的位置。
        规则仍然完整可查，只是不再抢主操作的位置。
      */}
      <details className="uma-card uma-disclosure p-4">
        <summary className="cursor-pointer text-base font-bold text-[var(--color-ink)]">
          计分方式
        </summary>
        <ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]">
          <li>按「关系对」计分：任意两张卡的先后关系正确即得 1 对</li>
          <li>得分 = 100 × 正确关系对 ÷ 总关系对</li>
          <li>全部正确得满分</li>
        </ul>
      </details>

      <div className="flex flex-col gap-2 border-t border-[var(--color-line)] pt-4 sm:flex-row">
        <Button asChild variant="secondary" size="primary">
          <Link to="/help">先看规则</Link>
        </Button>
        <Button asChild variant="secondary" size="primary">
          <Link to="/">返回大厅</Link>
        </Button>
      </div>
    </div>
  );
}
