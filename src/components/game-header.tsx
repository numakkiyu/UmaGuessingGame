"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { TurnstileWidget } from "@/components/turnstile-widget";

type Props = {
  questionBankSize: number;
  maxGuesses: number;
  turnstileEnabled: boolean;
  turnstileSiteKey: string;
};

type StartGameResponse = {
  roomCode: string;
};

const menuEntries = [
  {
    id: "single",
    title: "单人游戏",
    description: "随机抽取一位目标马娘，直接开始这一局。",
    enabled: true,
  },
  {
    id: "multi",
    title: "多人对战",
    description: "和其他玩家同题竞速，比谁更快猜中。",
    enabled: false,
  },
  {
    id: "friend",
    title: "好友对战",
    description: "开房邀请朋友一起猜同一题。",
    enabled: false,
  },
] as const;

const REQUEST_TIMEOUT_MS = 7000;

async function requestJson<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
  const controller = new AbortController();
  const timeoutId = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);

  try {
    const response = await fetch(input, {
      ...init,
      signal: controller.signal,
    });
    const text = await response.text();
    const data = text ? (JSON.parse(text) as { error?: string }) : null;
    if (!response.ok) {
      throw new Error(data?.error ?? "这会儿还没法开始，稍后再试一次吧。");
    }

    return data as T;
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") {
      throw new Error("这会儿还没法开始，稍后再试一次吧。");
    }
    if (error instanceof SyntaxError) {
      throw new Error("这会儿还没法开始，稍后再试一次吧。");
    }
    throw error;
  } finally {
    window.clearTimeout(timeoutId);
  }
}

function normalizePlayerError(message: string, fallback: string) {
  if (!message) return fallback;
  if (message.includes("缺少 Turnstile token")) {
    return "请先完成人机验证。";
  }
  if (message.includes("Turnstile 校验失败")) {
    return "验证已过期，请重新完成验证。";
  }
  if (message.includes("question_bank_ready.json 为空")) {
    return "题库还在整理中，稍后再来试试。";
  }
  return message;
}

export function GameHeader({
  questionBankSize,
  maxGuesses,
  turnstileEnabled,
  turnstileSiteKey,
}: Props) {
  const router = useRouter();
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [turnstileToken, setTurnstileToken] = useState<string | null>(null);
  const [turnstileResetSignal, setTurnstileResetSignal] = useState(0);

  async function startSingleGame() {
    if (questionBankSize === 0 || starting) {
      return;
    }

    if (turnstileEnabled) {
      if (!turnstileSiteKey) {
        setError("验证功能还没准备好，稍后再试。");
        return;
      }
      if (!turnstileToken) {
        setError("请先完成人机验证。");
        return;
      }
    }

    setStarting(true);
    setError(null);

    try {
      const data = await requestJson<StartGameResponse>("/api/game/new", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          turnstileToken: turnstileEnabled ? turnstileToken : undefined,
        }),
      });
      router.push(`/single/${data.roomCode}`);
    } catch (nextError) {
      setError(
        nextError instanceof Error
          ? normalizePlayerError(nextError.message, "这会儿还没法开始，稍后再试一次吧。")
          : "这会儿还没法开始，稍后再试一次吧。",
      );
      if (turnstileEnabled) {
        setTurnstileToken(null);
        setTurnstileResetSignal((value) => value + 1);
      }
    } finally {
      setStarting(false);
    }
  }

  return (
    <section className="relative overflow-hidden rounded-[40px] border border-[var(--color-line)] bg-[var(--color-panel)] px-5 py-6 shadow-[var(--shadow-panel)] sm:px-7 sm:py-7 lg:px-9 lg:py-9">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top_left,rgba(255,228,188,0.78),transparent_26%),radial-gradient(circle_at_86%_18%,rgba(217,162,92,0.2),transparent_18%),linear-gradient(180deg,rgba(239,209,172,0.3),rgba(255,250,242,0))]" />
      <div className="pointer-events-none absolute inset-x-0 top-[112px] h-px bg-[linear-gradient(90deg,rgba(156,69,24,0),rgba(156,69,24,0.26),rgba(156,69,24,0))]" />
      <div className="pointer-events-none absolute -right-12 top-8 h-52 w-52 rounded-full bg-[rgba(200,108,53,0.12)] blur-3xl" />
      <div className="pointer-events-none absolute bottom-0 left-0 h-40 w-40 rounded-full bg-[rgba(214,160,58,0.12)] blur-3xl" />

      <div className="relative flex min-h-[78vh] flex-col justify-between">
        <div className="grid gap-8 lg:grid-cols-[minmax(0,1.2fr)_minmax(320px,0.8fr)] lg:items-stretch">
          <div className="flex flex-col justify-between">
            <div className="max-w-3xl">
              <div className="inline-flex rounded-full border border-[rgba(156,69,24,0.18)] bg-[rgba(255,245,232,0.92)] px-3 py-1 text-[11px] font-semibold uppercase tracking-[0.28em] text-[var(--color-brand-strong)]">
                开始游戏
              </div>
              <h1 className="mt-4 font-[var(--font-display)] text-[2.8rem] font-bold leading-[0.92] tracking-[0.04em] text-[var(--color-ink)] sm:text-[3.7rem] lg:text-[5rem]">
                赛马娘弗一把
              </h1>
              <p className="mt-5 max-w-2xl text-base leading-7 text-[var(--color-muted)] sm:text-lg">
                系统会随机抽取一位目标马娘。输入名字开始猜测，每次都会给出字段反馈，帮助你一步步缩小范围。
              </p>
            </div>

            <div className="mt-6 flex flex-wrap gap-2.5">
              <span className="rounded-full border border-[rgba(104,79,48,0.12)] bg-[var(--color-panel-soft)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
                当前题库 {questionBankSize} 位
              </span>
              <span className="rounded-full border border-[rgba(216,79,66,0.12)] bg-[rgba(216,79,66,0.1)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
                每局最多 {maxGuesses} 次猜测
              </span>
              <span className="rounded-full border border-[rgba(216,166,66,0.14)] bg-[rgba(216,166,66,0.16)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
                支持常见中文名、日文名和别名
              </span>
            </div>

            <div className="mt-8 flex flex-col gap-3">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                <button
                  type="button"
                  onClick={startSingleGame}
                  disabled={starting || questionBankSize === 0}
                  className="inline-flex min-h-13 items-center justify-center rounded-full bg-[var(--color-brand)] px-6 py-3 text-base font-semibold text-white transition hover:bg-[var(--color-brand-strong)] disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {questionBankSize === 0
                    ? "题库准备中"
                    : starting
                      ? "正在进入这一局"
                      : "开始单人局"}
                </button>
                <p className="text-sm leading-6 text-[var(--color-muted)]">
                  进入后会立即出题，也能把这一局分享给朋友一起猜。
                </p>
              </div>

              {turnstileEnabled ? (
                <div className="max-w-[420px] rounded-[24px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.74)] px-4 py-4">
                  <TurnstileWidget
                    siteKey={turnstileSiteKey}
                    resetSignal={turnstileResetSignal}
                    onTokenChange={setTurnstileToken}
                  />
                  <p className="mt-2 text-sm text-[var(--color-muted)]">
                    {turnstileToken ? "验证完成，可以开始游戏了。" : "先完成人机验证，再开始游戏。"}
                  </p>
                </div>
              ) : null}

              {error ? (
                <p className="max-w-[520px] rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                  {error}
                </p>
              ) : null}
            </div>
          </div>

          <div className="rounded-[32px] border border-[rgba(126,90,49,0.14)] bg-[linear-gradient(180deg,rgba(255,250,242,0.96),rgba(250,243,231,0.9))] p-4 shadow-[var(--shadow-soft)] sm:p-5">
            <div className="rounded-[24px] border border-[rgba(104,79,48,0.12)] bg-[rgba(255,255,255,0.7)] px-4 py-3">
              <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--color-brand-strong)]">
                模式选择
              </p>
              <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
                当前可以直接游玩单人模式。多人和好友对战会在后续版本开放。
              </p>
            </div>

            <div className="mt-4 overflow-hidden rounded-[26px] border border-[rgba(104,79,48,0.12)] bg-[rgba(255,255,255,0.82)]">
              {menuEntries.map((entry, index) => {
                const sharedClassName =
                  "flex w-full items-center justify-between gap-4 px-4 py-4 text-left transition sm:px-5";

                const content = (
                  <>
                    <div>
                      <p className="text-xl font-semibold text-[var(--color-ink)]">{entry.title}</p>
                      <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
                        {entry.description}
                      </p>
                    </div>
                    <span
                      className={[
                        "shrink-0 rounded-full px-3 py-1 text-xs font-semibold",
                        entry.enabled
                          ? "bg-[rgba(200,108,53,0.14)] text-[var(--color-brand-strong)]"
                          : "bg-[rgba(104,79,48,0.08)] text-[var(--color-muted)]",
                      ].join(" ")}
                    >
                      {entry.enabled ? "立即开始" : "敬请期待"}
                    </span>
                  </>
                );

                return entry.enabled ? (
                  <button
                    key={entry.id}
                    type="button"
                    onClick={startSingleGame}
                    disabled={starting || questionBankSize === 0}
                    className={[
                      sharedClassName,
                      "bg-[rgba(255,250,244,0.92)] hover:bg-white disabled:cursor-not-allowed disabled:opacity-60",
                      index > 0 ? "border-t border-[rgba(104,79,48,0.1)]" : "",
                    ].join(" ")}
                  >
                    {content}
                  </button>
                ) : (
                  <div
                    key={entry.id}
                    className={[
                      sharedClassName,
                      "cursor-not-allowed bg-[rgba(247,241,232,0.78)] opacity-80",
                      index > 0 ? "border-t border-[rgba(104,79,48,0.1)]" : "",
                    ].join(" ")}
                  >
                    {content}
                  </div>
                );
              })}
            </div>

            <div className="mt-4 rounded-[24px] border border-[rgba(200,108,53,0.14)] bg-[rgba(255,246,234,0.86)] px-4 py-4">
              <p className="text-sm font-semibold text-[var(--color-ink)]">分享这一局</p>
              <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
                每一局都会生成独立链接。把链接发出去，朋友打开后就能进入同一题。
              </p>
              <p className="mt-3 text-sm text-[var(--color-muted)]">
                如果你只是自己先玩，直接开始单人局就可以。
              </p>
              <div className="mt-3">
                <Link href="/" className="text-sm font-semibold text-[var(--color-brand-strong)]">
                  单人局入口就在上方
                </Link>
              </div>
            </div>
          </div>
        </div>

        <div className="mt-8 rounded-[28px] border border-[rgba(104,79,48,0.12)] bg-[rgba(255,255,255,0.64)] px-4 py-4 sm:px-5">
          <div className="grid gap-3 lg:grid-cols-[1.1fr_0.9fr] lg:items-center">
            <div>
              <p className="text-sm font-semibold text-[var(--color-ink)]">这一局如何结束</p>
              <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
                猜中后会立刻结算；如果用完 {maxGuesses} 次机会，系统会公开答案，方便你马上再来一局。
              </p>
            </div>
            <p className="text-sm leading-6 text-[var(--color-muted)]">
              当前先开放单人模式，后续会逐步加入多人和好友对战。
            </p>
          </div>
        </div>
      </div>
    </section>
  );
}
