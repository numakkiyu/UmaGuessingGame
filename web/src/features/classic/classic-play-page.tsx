
import { Check, Loader2 } from "lucide-react";
import { useCallback, useState } from "react";
import { Link, useNavigate } from "react-router";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useApi } from "@/lib/api";
import { createIdempotencyKey, describeFailure, type Failure } from "@/lib/api/client";
import { cn } from "@/lib/utils";

type Step = "session" | "room" | "round" | "start";
type StepState = "pending" | "active" | "done" | "failed";

const STEP_LABELS: Record<Step, string> = {
  session: "正在准备你的座位",
  room: "创建单人房间",
  round: "生成本局题目",
  start: "开始接受猜测",
};

const STEP_ORDER: Step[] = ["session", "room", "round", "start"];

export function ClassicPlayPage() {
  const { api, session, ensureSession } = useApi();
  const navigate = useNavigate();

  const [displayName, setDisplayName] = useState(session?.displayName ?? "");
  const [budget, setBudget] = useState(8);
  const [steps, setSteps] = useState<Record<Step, StepState>>({
    session: "pending",
    room: "pending",
    round: "pending",
    start: "pending",
  });
  const [running, setRunning] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  // 幂等键按步骤保存：同一步骤重试复用同一个键
  const [keys, setKeys] = useState<Partial<Record<Step, string>>>({});

  const setStep = (step: Step, state: StepState) => setSteps((prev) => ({ ...prev, [step]: state }));

  const start = useCallback(async () => {
    if (running) return;
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
      // 1) 会话
      setStep("session", "active");
      const name = displayName.trim() || "玩家";
      await ensureSession(name);
      setStep("session", "done");

      // 2) 房间
      setStep("room", "active");
      const roomResponse = await api.createRoom(
        {
          name: `${name}的经典对局`,
          kind: "solo",
          modeConfig: {
            modeId: "classic",
            maxWrongGuesses: budget,
            winPolicy: "solo",
            durationSeconds: 0,
            shadowEnabled: false,
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

      // 3) 回合
      setStep("round", "active");
      // 契约 RoundCreate.modeConfig 是 ClassicConfig 判别联合；实测后端也接受 {modeId, config:{}}
      // 的简写。这里按契约发完整字段，避免依赖未文档化的简写形态。
      const roundResponse = await api.createRound(roomId, {
        modeConfig: {
          modeId: "classic",
          maxWrongGuesses: budget,
          winPolicy: "solo",
          durationSeconds: 0,
          shadowEnabled: false,
        },
      });
      const round = roundResponse.data as unknown as { roundId?: string; phaseVersion?: number } | null;
      const roundId = round?.roundId;
      if (!roundId) throw new Error("创建回合后没有拿到 roundId");
      setStep("round", "done");

      // 4) 推阶段 briefing → collecting
      setStep("start", "active");
      let phaseVersion = round?.phaseVersion ?? 1;
      for (const target of ["briefing", "collecting"] as const) {
        const transition = await api.createTransition(roundId, {
          target,
          expectedPhaseVersion: phaseVersion,
        });
        phaseVersion = (transition.data as unknown as { phaseVersion?: number } | null)?.phaseVersion ?? phaseVersion + 1;
      }
      setStep("start", "done");

      navigate(`/rooms/${encodeURIComponent(roomId)}`);
    } catch (error) {
      const failureInfo = describeFailure(error);
      setFailure(failureInfo);
      // 标记第一个未完成的步骤为 failed
      setSteps((prev) => {
        const next = { ...prev };
        for (const step of STEP_ORDER) {
          if (next[step] === "active") next[step] = "failed";
        }
        return next;
      });
    } finally {
      setRunning(false);
    }
  }, [api, budget, displayName, ensureSession, keys, navigate, running]);

  return (
    <div className="uma-prose mx-auto flex flex-col gap-5">
      <header>
        <h1 className="u-fluid-title text-[var(--color-ink)]">经典弗一把</h1>
        <p className="mt-2 text-base leading-7 text-[var(--color-ink)]">
          系统会选定一位马娘，你每次猜一位，九字段分别反馈
          <strong>对上</strong>、<strong>接近</strong>或<strong>不符</strong>，
          直到猜中或错误额度用完。不用注册，进来就能开局
        </p>
      </header>

      <section className="uma-panel flex flex-col gap-4 p-5">
        <div className="flex flex-col gap-2">
          <Label htmlFor="display-name">显示名</Label>
          <Input
            id="display-name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            placeholder="其他玩家看到的名字"
            maxLength={24}
            disabled={running}
            autoComplete="nickname"
          />
          <p className="text-[length:var(--text-aux)] text-[var(--color-muted)]">
            最多 24 个字符。单人练习只有你自己能看到
          </p>
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="wrong-budget">错误额度</Label>
          <div className="flex items-center gap-3">
            <Input
              id="wrong-budget"
              type="number"
              min={4}
              max={12}
              value={budget}
              onChange={(event) => {
                const next = Number(event.target.value);
                if (Number.isFinite(next)) setBudget(Math.min(12, Math.max(4, Math.round(next))));
              }}
              disabled={running}
              className="w-24"
            />
            <span className="text-sm text-[var(--color-muted)]">次（可设 4–12，默认 8）</span>
          </div>
          <p className="text-[length:var(--text-aux)] text-[var(--color-muted)]">
            猜错扣一次；猜对不再额外扣额度
          </p>
        </div>

        <Button variant="primary" size="primary" onClick={() => void start()} disabled={running} block>
          {running ? "正在开局……" : "开始对局"}
        </Button>

        {/* 逐步骤进度：让用户知道卡在哪一步，而不是只有一个转圈 */}
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
                    <span aria-hidden="true" className="inline-block size-4 rounded-full border border-[var(--color-line-strong)]" />
                  )}
                  <span
                    className={cn(
                      state === "pending" ? "text-[var(--color-muted)]" : "font-semibold text-[var(--color-ink)]",
                    )}
                  >
                    {STEP_LABELS[step]}
                  </span>
                  <span className="sr-only">
                    {state === "done" ? "已完成" : state === "active" ? "进行中" : state === "failed" ? "失败" : "未开始"}
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
