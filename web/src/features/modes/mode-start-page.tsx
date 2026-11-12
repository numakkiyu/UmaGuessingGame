
import { useTranslation } from "@/i18n";
import "@/i18n/mode-rules";
import type { modeRules } from "@/i18n/mode-rules";
import { useCallback, useState } from "react";
import { Link } from "react-router";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { StartProgress } from "@/features/modes/start-progress";
import { useStartRound } from "@/features/modes/use-start-round";
import { useApi } from "@/lib/api";
import { canPlay } from "@/lib/deployment/capabilities";
import { useModes, usePublicConfig } from "@/lib/api/queries";
import { describeUnavailable } from "@/lib/copy/unavailable";

export type ModeStartConfig = {
  modeId: keyof typeof modeRules["zh-Hans"];
  /** Game configuration is independent of localized presentation. */
  modeConfig?: Record<string, unknown>;
};

export function ModeStartPage({ config }: { config: ModeStartConfig }) {
  const { t } = useTranslation(["entry", "modes", "modeRules", "common"]);
  const title = t(`${config.modeId}.name`, { ns: "modes" });
  const summary = t(`${config.modeId}.summary`, { ns: "modeRules" });
  const duration = t(`${config.modeId}.duration`, { ns: "modeRules" });
  const rules = t(`${config.modeId}.rules`, { ns: "modeRules", returnObjects: true }) as readonly string[];
  const { session } = useApi();
  const modesQuery = useModes();
 const publicConfig = usePublicConfig();
 const soloAllowed = canPlay(publicConfig.data,config.modeId,"solo");
 const friendAllowed = canPlay(publicConfig.data,config.modeId,"friend");
  const [displayName, setDisplayName] = useState(session?.displayName ?? "");

  const controller = useStartRound({
    modeId: config.modeId,
    modeName: title,
    displayName,
    modeConfig: config.modeConfig,
  });

  const mode = (modesQuery.data ?? []).find((item) => item.modeId === config.modeId);
  // Translate admission diagnostics into player-facing explanations
  const unavailable = mode && mode.availability !== "available"
    ? describeUnavailable(config.modeId, mode.unavailableReason)
    : null;

  /*
   * 需要多人才能开的玩法，不能在单人入口页让玩家直接点「开始对局」。
   *
   * 实测：clue-puzzle 要 3 人，单人点开局时服务端返回
   * `not-enough-participants: mode clue-puzzle needs at least 3 participants, found 1`，
   * 页面把它翻成「你填的内容有一处不合规则」——玩家会以为名字填错了。
   * 入口页本来就拿到 /modes 的 minPlayers，直接用，不必等失败再解释。
   */
  const needsParty = mode ? mode.minPlayers > 1 : false;

  const onStart = useCallback(() => {
    void controller.start();
  }, [controller]);

  return (
    <div className="uma-prose mx-auto flex flex-col gap-5">
      <header>
        <h1 className="u-fluid-title text-[var(--color-ink)]">{title}</h1>
        <p className="mt-2 text-base leading-7 text-[var(--color-ink)]">{summary}</p>
        {duration ? (
          <p className="mt-1 text-sm text-[var(--color-muted)]">{t("start.duration", { duration })}</p>
        ) : null}
      </header>

      {unavailable ? (
        <Alert tone="warning" title={unavailable.title}>
          <p>{unavailable.detail}</p>
        </Alert>
      ) : null}

      {!soloAllowed && !needsParty ? <Alert tone="info" title={t("start.soloClosed")}>{t("start.soloClosedDetail")}</Alert> : null}
      {friendAllowed ? <Button asChild variant={needsParty ? "primary" : "secondary"} size="primary"><Link to={`/rooms/new?mode=${encodeURIComponent(config.modeId)}`}>{t("start.friend")}</Link></Button> : null}
      <section className="uma-panel flex flex-col gap-4 p-5" aria-labelledby="mode-start">
        {/*
          标题不叫「开始对局」。
          它和下面那个真正的按钮同名，读屏器与自动化都会分不清
          「哪个是可点的」——我自己写 e2e 时就因为这个歧义点错了元素两次。
          区域标题改成「开局设置」，按钮保持「开始对局」这个动作名。
        */}
        <h2 id="mode-start" className="text-base font-bold text-[var(--color-ink)]">
          {t("start.settings")}
        </h2>

        <div className="flex flex-col gap-2">
          <Label htmlFor={`${config.modeId}-name`}>{t("start.name")}</Label>
          <Input
            id={`${config.modeId}-name`}
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            placeholder={t("start.namePlaceholder")}
            maxLength={24}
            disabled={controller.running}
            autoComplete="nickname"
          />
        </div>

        {needsParty ? (
          <Alert tone="info" title={t("start.party", { count: mode?.minPlayers })}>
            <p>
              {t("start.partyDetail", { count: mode?.minPlayers })}
            </p>
          </Alert>
        ) : null}

        <Button
          variant="primary"
          size="primary"
          onClick={onStart}
          disabled={controller.running || Boolean(unavailable) || needsParty || !soloAllowed || publicConfig.isLoading}
          block
        >
          {controller.running ? t("start.busy") : t("start.play")}
        </Button>

        <StartProgress
          steps={controller.steps}
          running={controller.running}
          failure={controller.failure}
          onRetry={onStart}
        />
      </section>

      {/*
        规则默认折叠。
        与 sort 入口页同一个问题：规则要点常驻在「开始对局」之前，
        手机竖屏下把主操作挤到首屏之外。规则内容一条没少，只是不再挡路。
      */}
      <details className="uma-card uma-disclosure p-4">
        <summary className="cursor-pointer text-base font-bold text-[var(--color-ink)]">
          {t("start.rules")}
        </summary>
        <ul className="mt-2 space-y-1.5 text-sm leading-6 text-[var(--color-muted)]">
          {rules.map((rule) => (
            <li key={rule}>{rule}</li>
          ))}
        </ul>
      </details>

      <div className="flex flex-col gap-2 border-t border-[var(--color-line)] pt-4 sm:flex-row">
        <Button asChild variant="secondary" size="primary">
          <Link to="/play/classic">{t("start.classic")}</Link>
        </Button>
        <Button asChild variant="secondary" size="primary">
          <Link to="/">{t("actions.home", { ns: "common" })}</Link>
        </Button>
      </div>
    </div>
  );
}
