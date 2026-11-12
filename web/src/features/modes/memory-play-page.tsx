
import { useCallback, useState } from "react";
import { Link } from "react-router";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { StartProgress } from "@/features/modes/start-progress";
import { useStartRound } from "@/features/modes/use-start-round";
import { useApi } from "@/lib/api";
import { useModes } from "@/lib/api/queries";
import { describeUnavailable } from "@/lib/copy/unavailable";

const MODE_ID = "memory" as const;

export function MemoryPlayPage() {
  const { session } = useApi();
  const modesQuery = useModes();
  const [displayName, setDisplayName] = useState(session?.displayName ?? "");

  const controller = useStartRound({
    modeId: MODE_ID,
    modeName: "记忆配对",
    displayName,
  });

  const mode = (modesQuery.data ?? []).find((item) => item.modeId === MODE_ID);
  // 准入诊断翻译成玩家说法（内部拒绝码不直出）。
  const unavailableCopy = mode && mode.availability !== "available"
    ? describeUnavailable(mode.modeId, mode.unavailableReason)
    : null;

  const onStart = useCallback(() => {
    void controller.start();
  }, [controller]);

  return (
    <div className="uma-prose mx-auto flex flex-col gap-5">
      <header>
        <h1 className="u-fluid-title text-[var(--color-ink)]">学园记忆翻牌</h1>
        <p className="mt-2 text-base leading-7 text-[var(--color-ink)]">
          4×4 共 16 张牌，藏着 8 对马娘头像。先在 <strong>8 秒</strong>内看完所有牌面，
          然后盖牌，每次翻开两张：同一对就永久亮起，不同就盖回去
        </p>
      </header>

      {unavailableCopy ? (
        <Alert tone="warning" title={unavailableCopy.title}>
          <p>{unavailableCopy.detail}</p>
        </Alert>
      ) : null}

      <section className="uma-panel flex flex-col gap-4 p-5" aria-labelledby="memory-start">
        <h2 id="memory-start" className="text-base font-bold text-[var(--color-ink)]">
          开始对局
        </h2>

        <div className="flex flex-col gap-2">
          <Label htmlFor="memory-name">显示名</Label>
          <Input
            id="memory-name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            placeholder="其他玩家看到的名字"
            maxLength={24}
            disabled={controller.running}
            autoComplete="nickname"
          />
        </div>

        <Button
          variant="primary"
          size="primary"
          onClick={onStart}
          disabled={controller.running || Boolean(unavailableCopy)}
          block
        >
          {controller.running ? "正在开局……" : "开始对局"}
        </Button>

        <StartProgress
          steps={controller.steps}
          running={controller.running}
          failure={controller.failure}
          onRetry={onStart}
        />
      </section>

      {/* 规则与计分默认折叠，理由同 sort / mode-start：不挡主操作。 */}
      <details className="uma-card uma-disclosure p-4">
        <summary className="cursor-pointer text-base font-bold text-[var(--color-ink)]">
          规则与计分
        </summary>
        <ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]">
          <li>最多 20 次尝试；每次尝试是「翻开两张」，翻一张不算一次尝试</li>
          <li>答对一对得 12.5 分，答错一次扣 2 分，最低 0 分</li>
          <li>8 对全部配对成功，或用完 20 次尝试，本局结束</li>
          <li>翻开两张牌后会显示是否配对成功；未配对的牌会重新盖回</li>
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
