// Derive display time from the server deadline on each tick; decrementing a local counter would drift in background tabs
import { useEffect, useState } from "react";

export type Countdown = {
  /** 剩余毫秒；null 表示没有截止时间（不限时） */
  remainingMs: number | null;
  /** 剩余整秒，向上取整（显示用，避免 0 秒还显示 1 秒的歧义） */
  remainingSeconds: number | null;
  /** 是否已到期（按服务端截止时间判断） */
  expired: boolean;
  /** 格式化的 mm:ss 或 h:mm:ss */
  text: string;
};

function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`;
}

const NO_DEADLINE: Countdown = { remainingMs: null, remainingSeconds: null, expired: false, text: "--:--" };

/**
 * 跟踪一个 RFC 3339 截止时间。
 *
 * @param deadlineAt 服务端截止时间；null/undefined 表示不限时
 * @param enabled false 时暂停刷新（例如已结束、已暂停）
 */
export function useCountdown(deadlineAt: string | null | undefined, enabled = true): Countdown {
  const target = deadlineAt ? Date.parse(deadlineAt) : Number.NaN;
  const valid = Number.isFinite(target);

  const compute = (): Countdown => {
    if (!valid) return NO_DEADLINE;
    const remainingMs = target - Date.now();
    return {
      remainingMs,
      remainingSeconds: Math.max(0, Math.ceil(remainingMs / 1000)),
      expired: remainingMs <= 0,
      text: formatRemaining(remainingMs),
    };
  };

  const [state, setState] = useState<Countdown>(compute);

  useEffect(() => {
    // 首帧对齐必须在这里做：倒计时是「渲染时刻」的快照，若等到 interval 第一次
    // 触发（1 秒后）才更新，首帧会显示上一次渲染留下的旧值。
    if (!valid || !enabled) {
      // 仍要算出静态值，只是不再刷新
      setState(compute());
      return;
    }
    // 立即对齐一次，避免首帧显示旧值
    setState(compute());
    const timer = window.setInterval(() => {
      setState(compute());
    }, 1000);
    return () => window.clearInterval(timer);
    // target 是数字，valid/enabled 是布尔，依赖稳定
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target, valid, enabled]);

  return state;
}

/**
 * 观看阶段计时：服务端只给 phase（briefing）+ 配置里的 viewSeconds，
 * 没有单独的 previewEndsAt 字段（契约 StateMemory 有，实测投影没有）。
 * 因此用「进入 briefing 的本地时刻 + viewSeconds」作为展示倒计时，
 * 并明确标注这是展示用：真正的翻牌是否被接受由服务端判定。
 */
export function useViewingCountdown(viewSeconds: number | null, active: boolean): Countdown {
  const [startedAt, setStartedAt] = useState<number | null>(null);

  useEffect(() => {
    if (!active || viewSeconds === null) {
      setStartedAt(null);
      return;
    }
    setStartedAt(Date.now());
  }, [active, viewSeconds]);

  const compute = (): Countdown => {
    if (viewSeconds === null) return NO_DEADLINE;
    if (startedAt === null) {
      return {
        remainingMs: viewSeconds * 1000,
        remainingSeconds: viewSeconds,
        expired: false,
        text: formatRemaining(viewSeconds * 1000),
      };
    }
    const remainingMs = startedAt + viewSeconds * 1000 - Date.now();
    return {
      remainingMs,
      remainingSeconds: Math.max(0, Math.ceil(remainingMs / 1000)),
      expired: remainingMs <= 0,
      text: formatRemaining(remainingMs),
    };
  };

  const [state, setState] = useState<Countdown>(compute);

  useEffect(() => {
    if (!active || viewSeconds === null) {
      setState(compute());
      return;
    }
    setState(compute());
    const timer = window.setInterval(() => setState(compute()), 1000);
    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [startedAt, active, viewSeconds]);

  return state;
}
