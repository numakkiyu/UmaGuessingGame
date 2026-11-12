import "@/i18n/classic-online";
import "@/i18n/classic";
import { useTranslation } from "@/i18n";
import { characterName } from "@/i18n/entities";
import { useGameCommand } from "@/lib/hooks/use-game-command";
import { validClassicGuess, type ClassicGuessCommand } from "@/lib/api/game-commands";
import { phaseLabel } from "@/lib/api/round-snapshot";
import { TeamRoundControl } from "@/features/rooms/team-round-control";
/**
 * 经典对局页 /rooms/:roomId。
 *
 * 手机结构：
 * 本局状态 / 网络 → 角色搜索 → 当前角色 + 猜一次 → 最新猜测（头像+名字+九字段）
 * → 历史记录 → 规则/结束
 *
 * 桌面：主区 8–9 栏（搜索 + 猜测记录），侧区 3–4 栏（状态、规则、对手影子）。
 *
 * 关键约束：
 * - 状态覆盖 loading / ready / empty / error / forbidden / ended。
 * - 提交必须用同一幂等键重试；超时显示「正在确认提交结果」，不显示失败。
 * - 不伪造成功：只有拿到 accepted 回执才把行标为已提交。
 * - 首屏必须看见搜索、提交、剩余错误额度、至少一条最新反馈。
 */
import { ArrowRight, ChevronDown, Flag, RefreshCw, Users } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useParams } from "react-router";
import { ConnectionBar } from "@/components/uma/connection-bar";
import { EmptyView, ErrorView, ForbiddenView, LoadingBlock } from "@/components/uma/page-state";
import { SearchInput } from "@/components/uma/search-input";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ShareResultButton } from "@/components/uma/share-result-button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { GuessCard, GuessTable } from "@/features/classic/guess-card";
import { useApi } from "@/lib/api";
import {
  guessedCharacterIds,
  normalizeClassicRound,
  normalizeRowFromFeedback,
  type NormalizedRound,
} from "@/lib/api/classic";
import { useCharacters } from "@/lib/api/queries";
import { useRoom } from "@/lib/api/room-queries";
import { ApiError, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import type { Character } from "@/lib/api/types";
import { useRealtime } from "@/lib/realtime/use-realtime";
import { cn } from "@/lib/utils";

export function ClassicRoomPage() {
  const { t } = useTranslation("classicOnline");
  const { t: tc } = useTranslation("classic");
  const { roomId } = useParams<{ roomId: string }>();
  const { api, session } = useApi();

  const roomQuery = useRoom(roomId ?? null);
  const charactersQuery = useCharacters();

  const roundId = roomQuery.data?.room.currentRound?.roundId ?? null;
  const roundQuery = useRoundSnapshot(roundId);

  const round = useMemo(() => {
    const normalized = normalizeClassicRound(roundQuery.data);
    if (!normalized) return null;
    const characters = charactersQuery.data?.items ?? [];
    return { ...normalized, rows: normalized.rows.map(row => {
      const known = characters.find(character => character.characterId === row.characterId);
      return known ? { ...row, displayName: known.displayName, localizedNames: known.localizedNames } : row;
    }) };
  }, [roundQuery.data, charactersQuery.data]);

  const [selected, setSelected] = useState<Character | null>(null);
  const [submitFailure, setSubmitFailure] = useState<Failure | null>(null);
  const [surrendering, setSurrendering] = useState(false);
  const [rejected, setRejected] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const command = useGameCommand(
    session && roundId ? `${session.sessionId}:${roundId}:classic` : null,
    validClassicGuess,
    (body, key) => api.createSubmission(roundId!, body, key),
  );
  const phase = { kind: command.busy || refreshing ? "submitting" : command.pending ? "confirming" : "idle" };
  const submitInfo = submitFailure ? rejected ? { ...submitFailure, title: t("rejectedTitle"), detail: t("rejectedDetail") } : localizeFailure(submitFailure) : null;

  /* 根据记录容器宽度选择表格或卡片；手动选择优先，切回自动后才继续跟随宽度 */
  const recordsRef = useRef<HTMLDivElement | null>(null);
  const [autoView, setAutoView] = useState<"cards" | "table">("table");
  const [manualView, setManualView] = useState<"cards" | "table" | null>(null);
  const view = manualView ?? autoView;

  /* 测量记录容器宽度以适配侧栏与分屏；回合渲染后重新绑定观察器 */
  useEffect(() => {
    const el = recordsRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const apply = (width: number) => setAutoView(width >= 720 ? "table" : "cards");
    apply(el.getBoundingClientRect().width);
    const ro = new ResizeObserver((entries) => {
      for (const entry of entries) apply(entry.contentRect.width);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [round]);

  // 实时层：收到通知就重新拉快照
  const realtime = useRealtime({
    roomId: roomId ?? null,
    enabled: Boolean(roomId),
    handlers: {
      onSnapshotNeeded: () => {
        void roundQuery.refetch();
        void roomQuery.refetch();
      },
    },
  });

  const guessedIds = useMemo(() => guessedCharacterIds(round), [round]);
  /**
   * 认输：本人退出本局。
   *
   * 认输只影响自己，不授予踢人权。它必须有幂等键：重复点击或超时重试
   * 要用同一个键，否则会记录两次。成功后重新取回合视图，让服务端投影决定结束状态，
   * 而不是前端自己宣布结束。
   */
  const surrender = useCallback(async () => {
    if (!roundId || !round || !round.canSubmit) return;
    if (surrendering) return;
    setSurrendering(true);
    setSubmitFailure(null); setRejected(false);
    try {
      // createSurrender already carries the idempotency key internally (the
      // contract layer sets idempotency: true), so no key is passed here.
      await api.createSurrender(roundId, { phaseVersion: round.phaseVersion });
      // The server projection decides the ended state; the client must not
      // announce an outcome on its own.
      await roundQuery.refetch();
    } catch (error) {
      setSubmitFailure(describeFailure(error));
    } finally {
      setSurrendering(false);
    }
  }, [api, roundId, round, surrendering, roundQuery]);

  const characters = charactersQuery.data?.items ?? [];

  /** Replay an uncertain guess with its original body and key, even after
   * a newer snapshot, language, controller or phase has arrived. */
  async function submitGuess(character?: Character) {
    if (refreshing || surrendering || !roundId || !round) return;
    if (character && (!round.canSubmit || phase.kind !== "idle" || command.expired || roundQuery.isError)) return;
    const body: ClassicGuessCommand | undefined = character ? {
      phaseVersion: round.phaseVersion,
      submissionRevision: round.submissionRevision,
      controlGeneration: round.controlGeneration,
      modeAction: { kind: "classic.guess", characterId: character.characterId },
    } : undefined;
    setRefreshing(true); setSubmitFailure(null); setRejected(false);
    try {
      const result = await command.run(body);
      if (!result) return;
      if (!result.ok) {
        setSubmitFailure(describeFailure(result.error));
        if (!result.uncertain && result.error instanceof ApiError && result.error.isConflict) await roundQuery.refetch();
        return;
      }
      const response = result.value;
      if (response.data?.outcome !== "accepted") {
        setRejected(true);
        setSubmitFailure({ kind: "error", title: "", detail: "", diagnosticId: response.requestId, retryable: true });
        return;
      }
      setSelected(null);
      if (response.data.feedback) roundQuery.applyReceipt({ feedback: response.data.feedback, submissionRevision: response.data.submissionRevision ?? round.submissionRevision + 1 });
      await roundQuery.refetch();
    } finally { setRefreshing(false); }
  }

  const busy = phase.kind !== "idle" || command.expired || roundQuery.isError || surrendering;
  const canPlay = round !== null && !round.surrendered && (round.phase === "collecting" || round.phase === "briefing") && !round.paused && round.canSubmit;
  const ended = round !== null && (round.surrendered || round.solved || round.failed || round.phase === "settled" || round.phase === "revealed" || round.phase === "cancelled");

  // ---- 页面状态：逐项区分，不把失败显示成空 ----
  if (!roomId) {
    return <ErrorView failure={{ kind: "error", title: t("missingTitle"), detail: t("missingDetail"), diagnosticId: null, retryable: false }} />;
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
    return failure.kind === "forbidden" ? (
      <ForbiddenView failure={failure} onRetry={() => void roomQuery.refetch()} />
    ) : (
      <ErrorView failure={failure} onRetry={() => void roomQuery.refetch()} />
    );
  }

  const room = roomQuery.data?.room;
  if (!room) {
    return <EmptyView title={t("missingRoom")} detail={t("missingRoomDetail")} />;
  }

  const connectionStatus = realtime.status;

  return (
    <div className="uma-stack">
      {/* 网络状态条：断线保留内容，只禁用危险提交 */}
      <TeamRoundControl room={roomQuery.data?.room} raw={roundQuery.data} onChanged={async () => { await roundQuery.refetch(); }}/>
      <ConnectionBar
        status={connectionStatus}
        detail={room.name}
        className="short:hidden"
      />

      {/* 紧凑状态条保留玩法、阶段与剩余额度，为作答反馈留出首屏空间 */}
      <section className="uma-panel flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2">
        {/* 短屏下隐藏标题：品牌头已经表达了身份，短屏要优先给反馈腾空间 */}
        <h1 className="hidden text-base font-bold text-[var(--color-ink)] short:hidden sm:block sm:text-lg">
          {tc("title")}
        </h1>
        <span className="text-[length:var(--text-aux)] text-[var(--color-muted)]">
          {round ? round.paused ? t("paused") : phaseLabel(round.phase) : t("notStarted")}
        </span>
        <div className="ml-auto flex items-center gap-2">
          <QuotaBadge round={round} />
          <Button variant="ghost" size="icon-sm" onClick={() => void roundQuery.refetch()} aria-label={t("refresh")}>
            <RefreshCw aria-hidden="true" className="size-4" />
          </Button>
        </div>
      </section>

      {command.pending && !command.busy ? <Alert tone="warning" title={t("confirmTitle")}><p>{t("confirmDetail")}</p><Button variant="secondary" disabled={refreshing} onClick={() => void submitGuess()}>{t("confirmAction")}</Button></Alert> : null}
      {command.expired ? <Alert tone="warning" title={t("expiredTitle")}><p>{t("expiredDetail")}</p><Button variant="secondary" onClick={() => { void roundQuery.refetch().then(ok => { if (ok) command.acknowledgeExpired(); }); }}>{t("latest")}</Button></Alert> : null}
      {command.storageLimited ? <p className="text-sm text-[var(--color-muted)]">{t("limited")}</p> : null}

      {/* 房间没有进行中的回合 */}
      {!roundId ? (
        <EmptyView
          title={t("waitingTitle")}
          detail={t("waitingDetail")}
          action={
            <Button asChild variant="secondary">
              <Link to={`/rooms/${roomId}`}>{t("waitingBack")}</Link>
            </Button>
          }
        />
      ) : null}

      {roundId && roundQuery.isLoading ? <LoadingBlock label={t("loadingRound")} rows={2} /> : null}

      {roundId && roundQuery.isError ? (
        (() => {
          const failure = describeFailure(roundQuery.error);
          return failure.kind === "forbidden" ? (
            <ForbiddenView failure={failure} onRetry={() => void roundQuery.refetch()} />
          ) : (
            <ErrorView failure={failure} onRetry={() => void roundQuery.refetch()} />
          );
        })()
      ) : null}

      {round ? (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(0,15rem)] xl:items-start">
          {/* 视觉层级：
          - L1 核心 = 猜测记录表格，占中央主区，给足宽度
          - L2 支撑 = 搜索/当前选择/猜一次，通栏在上
          - L3 参考 = 本局信息/规则/认输，右侧窄栏，xl 以下收起

          此前把猜测记录放进右侧 17rem 窄栏、把本局信息放在左侧大块，
          正是用户指出的"次要排中间、核心被压缩"。表格在窄栏里横向滚动
          无法避免，读表也失去意义，因此改为上下结构。 */}
          {/* L1+L2 主列：猜测记录在上（核心），搜索/提交紧随其下 */}
          <div className="uma-stack uma-stack--tight min-w-0">
          {round.paused ? <p role="status" className="text-sm leading-6 text-[var(--color-muted)]">{t("pausedDetail")}</p> : null}
          {round.phase === "locked" ? (
            <Alert tone="info" title={t("lockedTitle")}>
              {t("lockedDetail")}
            </Alert>
          ) : null}
          {/* ---- 搜索/提交区 ---- */}
          <div className="uma-stack uma-stack--tight min-w-0">
            {/* 结束状态：明确显示结果，禁止用本地旧状态继续提交 */}
            {ended ? (
              <ClassicEndPanel round={round} />
            ) : (
              <>
                {/* 搜索与提交同行；隐藏重复标签时仍保留可访问名称和完整触摸区 */}
                <section className="uma-panel p-2.5 sm:p-3" aria-labelledby="guess-heading">
                  <div className="flex items-center justify-between gap-2">
                    <h2 id="guess-heading" className="text-base font-bold text-[var(--color-ink)]">
                      {tc("who")}
                    </h2>
                    {selected ? (
                      <span className="min-w-0 truncate rounded-full bg-[var(--color-brand-green-soft)] px-2.5 py-1 text-[length:var(--text-aux)] font-bold text-[var(--color-ink)]">
                        {t("selected", { name: characterName(selected) })}
                      </span>
                    ) : (
                      <p className="hidden text-[length:var(--text-aux)] text-[var(--color-muted)] tall:block">
                        {t("searchHint")}
                      </p>
                    )}
                  </div>

                  <div className="mt-2 flex items-start gap-2">
                    <div className="min-w-0 flex-1">
                      <SearchInput
                        items={characters}
                        loading={charactersQuery.isLoading}
                        loadError={charactersQuery.isError ? describeFailure(charactersQuery.error).detail : null}
                        disabled={!canPlay || busy}
                        disabledHint={canPlay ? t("submitting") : t("notAccepting")}
                        excludeIds={guessedIds}
                        hideLabel
                        onSelect={(character) => {
                          setSelected(character);
                        }}
                        label={tc("search.label")}
                        placeholder={t("guessPlaceholder")}
                      />
                    </div>
                    {/* 一个页面只有一个最强主操作；它就在输入框右侧，
                        不再需要滚过一整块选中面板才够得到。 */}
                    <Button
                      variant="primary"
                      size="primary"
                      disabled={!canPlay || busy || !selected}
                      onClick={() => {
                        if (selected) void submitGuess(selected);
                      }}
                      className="shrink-0"
                    >
                      {phase.kind === "submitting"
                        ? t("submitting")
                        : phase.kind === "confirming"
                          ? t("confirming")
                          : tc("guess")}
                      {busy ? null : <ArrowRight aria-hidden="true" className="size-4" />}
                    </Button>
                  </div>

                  {submitInfo && !command.pending ? (
                    <div className="mt-3">
                      <Alert
                        tone={
                          submitInfo.kind === "timeout"
                            ? "warning"
                            : submitInfo.kind === "forbidden"
                              ? "danger"
                              : "danger"
                        }
                        title={submitInfo.title}
                        action={
                          <Button variant="secondary" onClick={() => void roundQuery.refetch()}>
                            {t("refreshState")}
                          </Button>
                        }
                      >
                        <p>{submitInfo.detail}</p>
                        {submitInfo.diagnosticId ? (
                          <p className="mt-1 text-[length:var(--text-aux)]">
                            {t("problemId")}：<span className="uma-code">{submitInfo.diagnosticId}</span>
                          </p>
                        ) : null}
                      </Alert>
                    </div>
                  ) : null}


                </section>
              </>
            )}



          {/* ---- 猜测记录（L1 核心，占中央主区） ---- */}
          {!ended ? (
            <section aria-labelledby="history-heading" className="uma-stack uma-stack--tight min-w-0">
                  {/*
                    量「主区可用宽度」而不是 window.innerWidth：侧栏是否展开、页面是否
                    分屏都会改变真正留给记录的宽度，而窗口宽度不会。探针 div 是块级元素，
                    宽度等于父容器内容宽。
                  */}
                  <div ref={recordsRef} aria-hidden="true" className="h-0" />
                  {round.rows.length === 0 ? (
                    <>
                      <div className="uma-section-head">
                        <h2 id="history-heading" className="text-base font-bold text-[var(--color-ink)]">
                          {t("history")}
                        </h2>
                        <p className="text-[length:var(--text-aux)] text-[var(--color-muted)]">{t("total", { count: 0 })}</p>
                      </div>
                      <EmptyView
                        title={t("noGuess")}
                        detail={t("noGuessDetail")}
                      />
                    </>
                  ) : (
                    /*
                      标题、条数与视图切换合并成一行。

                      原来是「记录标题」一整行 + 下面再一整行 Tabs，加上两者之间 24px 的
                      region 间距，合计约 48px。390 竖屏下最新卡片因此落在 y≈462，
                      九字段的第一行正好被 844 的首屏截断（用户实测：卡片约从 y=530 才开始）。
                      合并后省下这一行，最新卡片回到首屏内。

                      压缩的是重复的容器和空白，不是内容：九字段、次数、切换能力都在。
                      Tabs 用短标签是为了在 390 宽度里与标题同行放下，语义未变。
                    */
                    <Tabs
                      value={view}
                      onValueChange={(value) => {
                        if (value === "auto") setManualView(null);
                        else setManualView(value === "table" ? "table" : "cards");
                      }}
                    >
                      <div className="uma-section-head items-center justify-between gap-2">
                        <div className="flex items-baseline gap-2">
                          <h2 id="history-heading" className="text-base font-bold text-[var(--color-ink)]">
                            {t("history")}
                          </h2>
                          <p className="text-[length:var(--text-aux)] text-[var(--color-muted)]">
                            {t("total", { count: round.rows.length })}
                          </p>
                        </div>
                        {/*
                          默认按宽度自动选，切过一次就锁定用户选择。
                          「自动」放最后：它不是常用选项，但必须存在，
                          否则玩家在手机上切到表格后再横过来就回不到默认了。
                        */}
                        <TabsList aria-label={t("view")} className="shrink-0">
                          <TabsTrigger value="table" className="whitespace-nowrap">{tc("table")}</TabsTrigger>
                          <TabsTrigger value="cards" className="whitespace-nowrap">{tc("cards")}</TabsTrigger>
                          <TabsTrigger value="auto" className="whitespace-nowrap">{tc("auto")}</TabsTrigger>
                        </TabsList>
                      </div>
                      <TabsContent value="cards">
                        <ul className="flex flex-col gap-3">
                          {round.rows
                            .slice()
                            .reverse()
                            .map((row) => (
                              <li key={`${row.characterId}-${row.attempt}`}>
                                <GuessCard row={row} latest={row.attempt === round.rows.length} />
                              </li>
                            ))}
                        </ul>
                      </TabsContent>
                      <TabsContent value="table">
                        <GuessTable rows={round.rows} />
                      </TabsContent>
                    </Tabs>
                  )}
                </section>
          ) : null}

          </div>
          </div>

          {/* ---- 侧区：状态 / 规则 / 对手（矮宽屏隐藏，把空间让给反馈） ---- */}
          <aside className="uma-stack uma-stack--tight min-w-0 wide-short:hidden">
            <section className="uma-card p-4">
              <h2 className="flex items-center gap-2 text-base font-bold text-[var(--color-ink)]">
                <Users aria-hidden="true" className="size-4 text-[var(--color-action-green)]" />
                {t("info")}
              </h2>
              {/*
                这里不再重复「剩余错误额度」。
                顶部状态条的 QuotaBadge 已经常驻显示同一个数字（剩余额度 N），
                侧栏再写一遍「剩余错误额度 N 次」只是把同一信息说两次；
                「错误额度上限」同理，它只在开局时有用，不是每回合都要看的东西。

                保留的是顶部状态条没有、而玩家确实会问的：用了多少次、额度总量、在哪一局。
                已提交次数与额度合在一行，读起来是「3 / 8 次」这种直觉形式。
              */}
              <dl className="mt-3 space-y-2 text-sm">
                <InfoRow
                  label={t("used")}
                  value={
                    round.wrongBudget === null
                      ? t("attempts", { count: round.attempts })
                      : t("budget", { count: round.attempts, budget: round.wrongBudget })
                  }
                />
                <InfoRow label={t("room")} value={room.name} />
              </dl>
            </section>

            {/* 对手影子：只显示「第几次 + 每格状态」，不含角色身份与字段值。
                 要求独立成区、明确「对手第几次」，不能混入自己的历史。 */}
            {round.shadows.length > 0 ? (
              <section className="uma-panel p-4" aria-labelledby="shadows-heading">
                <div className="uma-section-head">
                  <h2 id="shadows-heading" className="text-base font-bold text-[var(--color-ink)]">
                    {t("shadows")}
                  </h2>
                  <span className="text-[length:var(--text-aux)] text-[var(--color-muted)]">
                    {t("total", { count: round.shadows.length })}
                  </span>
                </div>
                <ul className="mt-2 flex flex-col gap-2">
                  {round.shadows.map((shadow) => (
                    <li
                      key={shadow.attempt}
                      className="rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel-soft)] px-3 py-2"
                    >
                      <p className="text-sm font-semibold text-[var(--color-ink)]">
                        {t("shadowAttempt", { count: shadow.attempt })}
                      </p>
                      <div className="mt-1 flex flex-wrap gap-1">
                        {shadow.statuses.map((cell) => (
                          <span
                            key={cell.field}
                            className={cn(
                              "rounded-full px-2 py-0.5 text-[0.625rem] font-bold",
                              cell.status === "correct"
                                ? "bg-[var(--color-brand-green-soft)] text-[var(--color-ink)]"
                                : cell.status === "near"
                                  ? "bg-[var(--color-gold)] text-[var(--color-ink-strong)]"
                                  : "bg-[var(--color-panel)] text-[var(--color-muted)]",
                            )}
                          >
                            {tc(`fields.${cell.field}`, { defaultValue: t("unknownField") })}
                          </span>
                        ))}
                      </div>
                    </li>
                  ))}
                </ul>
              </section>
            ) : null}

            {/*
              规则与认输默认折叠。
              手机实测：规则速查 232px 常驻，加上认输区，把「猜一次」挤到第二屏之后。
              它们是 L3（参考/危险操作），不是每回合都要看的东西——折叠起来，
              点开仍然完整可读，不是删掉。
            */}
            <details className="uma-card uma-disclosure p-4">
              <summary className="flex cursor-pointer items-center justify-between gap-2 text-base font-bold text-[var(--color-ink)]">
                {t("rules")}
                <ChevronDown aria-hidden="true" className="uma-disclosure__chevron size-4" />
              </summary>
              <ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]">
                <li>{t("correctRule")}</li>
                <li>{t("nearRule")}</li>
                <li>{t("wrongRule")}</li>
                <li>{t("guessRule")}</li>
              </ul>
              <Button asChild variant="link" className="mt-2 px-0">
                <Link to="/help">{t("fullRules")}</Link>
              </Button>
            </details>

            {/* 不显示 shapeWarnings：仅供测试与诊断，不作为玩家界面文案 */}
            <details className="uma-card uma-disclosure uma-danger-zone p-4">
              <summary className="flex cursor-pointer items-center justify-between gap-2 text-sm font-semibold text-[var(--color-ink)]">
                {t("more")}
                <ChevronDown aria-hidden="true" className="uma-disclosure__chevron size-4" />
              </summary>
              <p className="mt-2 text-[length:var(--text-aux)] leading-6 text-[var(--color-muted)]">
                {t("surrenderDetail")}
              </p>
              <Button
                variant="secondary"
                className="mt-2 w-full"
                disabled={busy || !round?.canSubmit || round.surrendered || ended}
                onClick={() => void surrender()}
              >
                <Flag aria-hidden="true" className="size-4" />
                {surrendering ? t("surrendering") : t("surrender")}
              </Button>
            </details>
          </aside>
        </div>
      ) : null}
    </div>
  );
}

/** 回合快照查询：抽出来便于 realtime 触发 refetch */
function useRoundSnapshot(roundId: string | null) {
  const { api } = useApi();
  const requestSeq = useRef(0);
  const [state, setState] = useState<{
    data: Awaited<ReturnType<typeof api.getRound>>["data"] | null;
    isLoading: boolean;
    isError: boolean;
    error: unknown;
  }>({ data: null, isLoading: Boolean(roundId), isError: false, error: null });

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  /**
   * 把一次已接受提交的回执合并进当前快照。
   *
   * 只做「追加一行 + 更新 revision/额度」，不推断胜负：solved/failed 仍以服务端快照为准。
   * 这样用户点完立刻看到反馈，同时后台 refetch 会用服务端权威值覆盖。
   */
  const applyReceipt = useCallback(
    (receipt: { feedback: unknown; submissionRevision: number }) => {
      setState((prev) => {
        const current = prev.data;
        if (!current) return prev;
        const normalized = normalizeClassicRound(current);
        if (!normalized) return prev;
        if (normalized.submissionRevision > receipt.submissionRevision) return prev;

        // 用同一个归一化函数处理回执里的 cells，保证与快照路径一致
        const syntheticRow = normalizeRowFromFeedback(receipt.feedback);
        if (!syntheticRow) return prev;

        // 幂等：同一 attempt 已存在就不重复追加（超时重试/快照先到都可能造成重复）
        const exists = normalized.rows.some((row) => row.attempt === syntheticRow.attempt);
        const rows = exists ? normalized.rows : [...normalized.rows, syntheticRow];

        const nextView = structuredClone(current) as typeof current;
        const pub = nextView.publicState as unknown as Record<string, unknown>;
        const self = (pub["self"] ?? {}) as Record<string, unknown>;
        self["attempts"] = rows.length;
        const feedback = receipt.feedback && typeof receipt.feedback === "object" ? receipt.feedback as Record<string, unknown> : null;
        if (typeof feedback?.remainingWrong === "number" && Number.isSafeInteger(feedback.remainingWrong) && feedback.remainingWrong >= 0) self["remainingWrong"] = feedback.remainingWrong;
        self["rows"] = rows.map((row) => ({
          characterId: row.characterId,
          displayName: row.displayName,
          avatarPath: row.avatarPath,
          attempt: row.attempt,
          correct: row.correct,
          cells: row.cells.map((cell) => ({ field: cell.field, value: cell.value, status: cell.status })),
        }));
        pub["self"] = self;
        (nextView as { submissionRevision?: number }).submissionRevision = receipt.submissionRevision;

        return { ...prev, data: nextView, isLoading: false, isError: false, error: null };
      });
    },
    [],
  );

  const refetch = useCallback(async () => {
    if (!roundId) return false;
    const seq = ++requestSeq.current;
    setState((prev) => ({ ...prev, isLoading: true }));
    try {
      const response = await api.getRound(roundId);
      if (mounted.current && seq === requestSeq.current) {
        setState({ data: response.data, isLoading: false, isError: false, error: null });
        return true;
      }
    } catch (error) {
      if (mounted.current && seq === requestSeq.current) {
        const denied = error instanceof ApiError && (error.isUnauthenticated || error.isForbidden || error.isNotFound);
        setState((prev) => ({ ...prev, data: denied ? null : prev.data, isLoading: false, isError: true, error }));
      }
    }
    return false;
  }, [api, roundId]);

  useEffect(() => {
    if (!roundId) {
      setState({ data: null, isLoading: false, isError: false, error: null });
      return;
    }
    void refetch();
  }, [roundId, refetch]);

  return { ...state, refetch, applyReceipt };
}

function QuotaBadge({ round }: { round: NormalizedRound | null }) {
  const { t } = useTranslation("classicOnline");
  if (!round) {
    return <span className="rounded-full bg-[var(--color-panel-soft)] px-3 py-1 text-sm text-[var(--color-muted)]">{t("notStarted")}</span>;
  }
  const remaining = round.remainingWrong;
  // 额度紧张时用 near 底 + 深字（不靠颜色单独表达，文字里有数字）
  const tight = remaining !== null && remaining <= 2;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-sm font-bold",
        tight
          ? "bg-[var(--color-near)] text-[var(--color-ink-strong)]"
          : "bg-[var(--color-brand-green-soft)] text-[var(--color-ink)]",
      )}
    >
      {t("remaining")}
      <span className="uma-code">{remaining === null ? "—" : remaining}</span>
    </span>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-2">
      <dt className="text-[var(--color-muted)]">{label}</dt>
      <dd className="min-w-0 truncate font-semibold text-[var(--color-ink)]">{value}</dd>
    </div>
  );
}

/** 结束面板：明确显示本局结果与权限，禁止继续提交 */
export function ClassicEndPanel({ round }: { round: NormalizedRound }) {
  const { t } = useTranslation("classicOnline");
  const characters = useCharacters().data?.items ?? [];
  const answerRow = round.rows.find(row => row.correct) ?? null;
  const known = characters.find(character => character.characterId === round.revealedAnswerCharacterId);
  const answerName = round.revealedAnswerCharacterId ? known ? characterName(known) : round.revealedAnswerName ?? (answerRow ? characterName(answerRow) : null) : null;
  const title = t(round.surrendered ? "surrendered" : round.solved ? "solved" : round.failed ? "failed" : "ended");
  const shareLines = [t(round.surrendered ? "shareSurrendered" : round.solved ? "shareSolved" : round.failed ? "shareFailed" : "shareEnded", { count: round.attempts })];
  // 答案已揭晓才加入分享；个人猜对不代表允许在分享中公开答案。
  if (round.revealedAnswerCharacterId) {
    shareLines.push(answerName ? t("shareAnswer", { name: answerName }) : t("answerRevealed"));
  }
  const shareText = shareLines.join("\n");
  return <section className="uma-panel p-5">
    <h2 className="u-fluid-task text-[var(--color-ink)]">{title}</h2>
    <p className="mt-2 text-base leading-7 text-[var(--color-muted)]">{t(round.surrendered ? "surrenderedDetail" : round.solved ? "solvedDetail" : round.failed ? "failedDetail" : "endedDetail", { count: round.attempts })}</p>
    {round.revealedAnswerCharacterId ? <p className="mt-2 text-base text-[var(--color-ink)]">{answerName ? <>{t("answer")}<strong>{answerName}</strong></> : t("answerRevealed")}</p> : null}
    <div className="mt-4 flex flex-col gap-2 sm:flex-row sm:flex-wrap">
      <Button asChild variant="primary" size="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/">{t("home")}</Link></Button>
      <Button asChild variant="secondary" size="primary" className="h-auto min-h-12 whitespace-normal"><Link to="/help">{t("seeRules")}</Link></Button>
      <ShareResultButton text={shareText}/>
    </div>
  </section>;
}
