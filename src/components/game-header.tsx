"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { SiteBrand } from "@/components/site-brand";
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
    description: "系统会直接出题，进房后马上就能开始猜。",
    enabled: true,
  },
  {
    id: "multi",
    title: "多人对战",
    description: "以后会开放同题竞速，看看谁先冲线。",
    enabled: false,
  },
  {
    id: "friend",
    title: "好友对战",
    description: "以后会开放开房邀请，和朋友一起猜同一题。",
    enabled: false,
  },
] as const;

const startTips = [
  {
    title: "先猜熟悉的那位",
    description: "第一猜尽量把范围拉大，后面会更容易读线索。",
  },
  {
    title: "看颜色收范围",
    description: "绿色最稳，黄色很接近，灰色就换个方向再试。",
  },
  {
    title: "同一局可以分享",
    description: "把链接发给朋友后，对方能跟着一起看进度。",
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
    <section className="uma-hero-shell relative overflow-hidden px-5 py-6 sm:px-7 sm:py-7 lg:px-9 lg:py-8">
      <div className="pointer-events-none absolute inset-x-0 top-0 h-1 bg-[linear-gradient(90deg,var(--color-brand),var(--color-brand-blue))]" />
      <div className="pointer-events-none absolute inset-0 bg-[linear-gradient(180deg,rgba(255,255,255,0.28),rgba(255,255,255,0))]" />
      <div className="pointer-events-none absolute inset-x-0 top-0 h-[180px] bg-[url('/assets/ui/backgrounds/bwiki-main-bg.png')] bg-cover bg-top opacity-[0.08]" />
      <div className="pointer-events-none absolute inset-x-0 top-0 h-[180px] bg-[linear-gradient(180deg,rgba(148,216,28,0.12),rgba(255,255,255,0))]" />
      <div className="pointer-events-none absolute right-[-40px] top-[-24px] h-44 w-44 rounded-full bg-[rgba(255,235,167,0.2)] blur-3xl" />

      <div className="relative space-y-7">
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(340px,420px)] lg:items-start">
          <div>
            <div className="flex flex-wrap items-start justify-between gap-4">
              <SiteBrand
                className="max-w-[620px]"
                showTitle={false}
                subtitle="看线索，猜角色，一路把答案追到终点。"
              />

              <div className="uma-chip shrink-0">
                当前题库 {questionBankSize} 位
              </div>
            </div>

            <div className="mt-6 max-w-3xl">
              <h1 className="font-[var(--font-display)] text-[2.7rem] font-bold leading-[0.94] tracking-[0.02em] text-[var(--color-ink)] sm:text-[3.4rem] lg:text-[4rem]">
                赛马娘猜猜乐
              </h1>
              <p className="mt-3 max-w-xl text-base leading-7 text-[var(--color-muted)] sm:text-lg">
                系统会先藏好一位目标马娘。你只要从熟悉的名字开始，一边看颜色，一边把范围越收越紧。
              </p>
            </div>

            <div className="mt-6 flex flex-wrap gap-2.5">
              <span className="uma-chip">
                当前题库 {questionBankSize} 位
              </span>
              <span className="uma-chip uma-chip--green">
                每局 {maxGuesses} 次机会
              </span>
              <span className="uma-chip uma-chip--gold">
                支持常见别名
              </span>
            </div>

            <div className="mt-8 flex flex-col gap-4">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
                <button
                  type="button"
                  onClick={startSingleGame}
                  disabled={starting || questionBankSize === 0}
                  className="inline-flex min-h-13 items-center justify-center rounded-[18px] border border-[rgba(115,192,22,0.22)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] bg-[length:180px_64px] bg-left-top px-6 py-3 text-base font-semibold text-[#244117] shadow-[0_16px_28px_rgba(116,194,22,0.22)] transition hover:brightness-[1.02] disabled:cursor-not-allowed disabled:opacity-60"
                  style={{ backgroundImage: "url('/assets/ui/decor/nav-diamond.svg'), linear-gradient(180deg,#a8e533,#82cb1a)" }}
                >
                  {questionBankSize === 0
                    ? "题库准备中"
                    : starting
                      ? "正在进入这一局"
                      : "开始单人局"}
                </button>
                <p className="text-sm leading-6 text-[var(--color-muted)]">
                  进入后会直接开局，也能把这一局分享给朋友一起看。
                </p>
              </div>

              {turnstileEnabled ? (
                <div className="max-w-[440px] rounded-[24px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.78)] px-4 py-4">
                  <TurnstileWidget
                    siteKey={turnstileSiteKey}
                    resetSignal={turnstileResetSignal}
                    onTokenChange={setTurnstileToken}
                  />
                  <p className="mt-2 text-sm text-[var(--color-muted)]">
                    {turnstileToken ? "验证完成，可以开始了。" : "先完成人机验证，再开始游戏。"}
                  </p>
                </div>
              ) : null}

              {error ? (
                <p className="max-w-[560px] rounded-[20px] border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                  {error}
                </p>
              ) : null}
            </div>
          </div>

          <div className="uma-menu-card rounded-[32px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.8)] p-4 shadow-[var(--shadow-soft)] sm:p-5">
            <div className="overflow-hidden rounded-[26px] border border-[rgba(96,147,192,0.14)] bg-[var(--color-panel-strong)]">
              <div className="relative border-b border-[var(--color-line)] px-4 py-4">
                <div className="pointer-events-none absolute inset-x-0 top-0 h-1 bg-[linear-gradient(90deg,var(--color-brand),var(--color-brand-blue))]" />
                <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
                  开局建议
                </p>
                <p className="mt-2 text-lg font-semibold text-[var(--color-ink)]">
                  这一局从熟悉的角色开始最顺手
                </p>
              </div>

              <div className="p-4">
                <div className="overflow-hidden rounded-[22px] border border-[rgba(96,147,192,0.14)] bg-white/86 shadow-[var(--shadow-soft)]">
                  {menuEntries.map((entry, index) => (
                    <div
                      key={entry.id}
                      className={index > 0 ? "border-t border-[rgba(96,147,192,0.12)]" : ""}
                    >
                      {entry.enabled ? (
                        <button
                          type="button"
                          onClick={startSingleGame}
                          disabled={starting || questionBankSize === 0}
                          className="flex w-full items-center justify-between gap-4 px-4 py-4 text-left transition hover:bg-[rgba(229,244,255,0.74)] disabled:cursor-not-allowed disabled:opacity-60 sm:px-5"
                        >
                          <div>
                            <p className="text-xl font-semibold text-[var(--color-ink)]">
                              {entry.title}
                            </p>
                            <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
                              {entry.description}
                            </p>
                          </div>
                          <span
                            className="shrink-0 rounded-[14px] border border-[rgba(115,192,22,0.2)] bg-[linear-gradient(180deg,#a8e533,#82cb1a)] px-3 py-1 text-xs font-semibold text-[#244117]"
                            style={{ backgroundImage: "url('/assets/ui/decor/nav-diamond.svg'), linear-gradient(180deg,#a8e533,#82cb1a)" }}
                          >
                            现在开始
                          </span>
                        </button>
                      ) : (
                        <div className="flex items-center justify-between gap-4 px-4 py-4 opacity-75 sm:px-5">
                          <div>
                            <p className="text-xl font-semibold text-[var(--color-ink)]">
                              {entry.title}
                            </p>
                            <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
                              {entry.description}
                            </p>
                          </div>
                          <span className="shrink-0 rounded-[14px] bg-[rgba(227,233,240,0.92)] px-3 py-1 text-xs font-semibold text-[var(--color-muted)]">
                            敬请期待
                          </span>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </div>
        </div>

        <div className="grid gap-4 lg:grid-cols-[1.15fr_0.85fr]">
          <div className="uma-metric-card rounded-[28px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.72)] px-4 py-4 sm:px-5">
            <div className="grid gap-3 md:grid-cols-3">
              {startTips.map((item) => (
                <div
                  key={item.title}
                  className="rounded-[22px] border border-[rgba(96,147,192,0.12)] bg-white/78 px-4 py-4 shadow-[var(--shadow-soft)]"
                >
                  <p className="text-sm font-semibold text-[var(--color-ink)]">{item.title}</p>
                  <p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">
                    {item.description}
                  </p>
                </div>
              ))}
            </div>
          </div>

          <div className="uma-metric-card rounded-[28px] border border-[var(--color-line)] bg-[rgba(255,255,255,0.72)] px-4 py-4 sm:px-5">
            <p className="text-sm font-semibold text-[var(--color-ink)]">这一局会看到什么</p>
            <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
              星级、场地、距离、跑法、牡牝、GI 档、GII/GIII 档、学年和宿舍都会成为线索。
            </p>
            <div className="mt-3 flex flex-wrap gap-2">
              {["星级", "场地", "距离", "跑法", "学年", "宿舍"].map((label) => (
                <span
                  key={label}
                  className="rounded-full border border-[rgba(96,147,192,0.14)] bg-white px-3 py-1 text-sm font-medium text-[var(--color-ink)]"
                >
                  {label}
                </span>
              ))}
            </div>
          </div>
        </div>

        <div className="text-sm leading-6 text-[var(--color-muted)]">
          <Link href="/" className="font-semibold text-[var(--color-brand-strong)]">
            从主页进入
          </Link>
          {" "}
          后会直接开局，猜中就立刻结算，用完 {maxGuesses} 次机会后也会马上公开答案。
        </div>
      </div>
    </section>
  );
}
