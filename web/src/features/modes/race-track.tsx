import { useTranslation } from "@/i18n";
import "@/i18n/strategy";
// Animate only positions confirmed by the server; movement here does not simulate or predict the race
import { cn } from "@/lib/utils";

export type RaceTrackTeam = {
  subjectId: string;
  label: string;
  position: number;
  stamina: number;
  isSelf: boolean;
  avatarSrc: string | null;
};

export type RaceTrackProps = {
  teams: readonly RaceTrackTeam[];
  totalTurns: number;
  staminaCap: number;
  /** 每回合最多能前进多少格，用来估算终点位置 */
  maxGainPerTurn: number;
  targetPosition?: number;
  className?: string;
};

/**
 * 终点位置按「所有回合都用最大收益」估算。
 *
 * 这不是规则里的硬上限（跟跑在特定条件下也能 +4，冲刺 +5），只是给跑道一个共同的
 * 视觉尺度。写死成某个数字会让不同配置的局看起来不一致，所以由调用方传入每回合最大
 * 收益。
 */
function finishLine(totalTurns: number, maxGainPerTurn: number): number {
  return Math.max(1, totalTurns * Math.max(1, maxGainPerTurn));
}

export function RaceTrack({
  teams,
  totalTurns,
  staminaCap,
  maxGainPerTurn,
  className,
  targetPosition,
}: RaceTrackProps) {
  const { t } = useTranslation("strategy");
  const finish = finishLine(totalTurns, maxGainPerTurn);
  // Stable lanes let players follow the same team as positions change.
  // Ranking belongs in the standings, not in the physical lane order.
  const ordered = [...teams].sort((a, b) => a.subjectId.localeCompare(b.subjectId));

  return (
    <section
      className={cn("uma-race-track", className)}
      aria-label={t("track.label", { turns: totalTurns, finish })}
    >
      {/* 跑道本体：横向推进方向从左到右读，与「位置越大越靠前」一致 */}
      <div className="uma-race-track__lane" role="list">
        {ordered.map((team, index) => {
          const pct = Math.min(100, Math.max(0, (team.position / finish) * 100));
          const staminaPct = staminaCap > 0 ? Math.min(100, (team.stamina / staminaCap) * 100) : 0;
          return (
            <div
              key={team.subjectId}
              role="listitem"
              className={cn("uma-race-track__row", team.isSelf && "uma-race-track__row--self")}
              style={{ ["--i" as string]: index }}
            >
              <div className="uma-race-track__label">
                {team.avatarSrc ? (
                  <img
                    src={team.avatarSrc}
                    alt=""
                    aria-hidden="true"
                    width={28}
                    height={28}
                    loading="lazy"
                    decoding="async"
                    className="uma-race-track__avatar"
                  />
                ) : (
                  // 主体是成员而不是角色时没有头像可用。用首字代替，不留空圆——
                  // 空圆会让人以为头像加载失败。
                  <span className="uma-race-track__avatar uma-race-track__avatar--initial" aria-hidden="true">
                    {team.label.slice(0, 1)}
                  </span>
                )}
                <span className="uma-race-track__name">
                  {team.label}
                  {team.isSelf ? <span className="uma-race-track__you">{t("track.you")}</span> : null}
                </span>
              </div>

              <div className="uma-race-track__field" style={{ marginInline: "0.75rem", width: "auto" }}>
                {targetPosition !== undefined ? <span className="pointer-events-none absolute inset-y-0 border-l-2 border-dashed border-[var(--color-action-green)]" style={{ left: `${Math.min(100, Math.max(0, targetPosition / finish * 100))}%` }} aria-hidden="true" /> : null}
                {/*
                  标记 + 体力条一起跟着位置走。
                  体力条之前铺满整行，会被读成「赛程进度」——它其实是这支队伍的体力。
                  现在它固定在标记正下方，宽度只跟体力有关，两件事就不会混。
                */}
                <div
                  className="uma-race-track__marker-wrap"
                  style={{ ["--race-progress" as string]: pct }}
                  data-position={team.position}
                >
                  <div className="uma-race-track__marker">
                    {team.avatarSrc ? (
                      <img src={team.avatarSrc} alt="" aria-hidden="true" width={24} height={24} loading="lazy" />
                    ) : (
                      <span className="uma-race-track__marker-initial">{team.label.slice(0, 1)}</span>
                    )}
                  </div>
                  <div className="uma-race-track__stamina" aria-hidden="true">
                    <span style={{ transform: `scaleX(${staminaPct / 100})` }} />
                  </div>
                </div>
              </div>

              <div className="uma-race-track__stats">
                <span className="uma-code">{team.position}</span>
                <span className="uma-race-track__stamina-text">
                  {t("track.stamina", { stamina: team.stamina, cap: staminaCap })}
                </span>
              </div>
            </div>
          );
        })}
        <div className="uma-race-track__finish" aria-hidden="true">
          {t("track.finish", { finish })}
        </div>
      </div>

      <p className="mt-2 flex justify-between gap-2 text-xs text-[var(--color-muted)] min-[480px]:hidden"><span>{t("track.start")}</span><span>{t("track.scale", { finish })}</span></p>
      {/* 读屏用：标记位置本身没有文本，这里补一句完整描述 */}
      <p className="sr-only">
        {ordered
          .map((team) => t(team.isSelf ? "track.selfDescription" : "track.description", { name: team.label, position: team.position, stamina: team.stamina }))
          .join("；")}
      </p>
    </section>
  );
}
