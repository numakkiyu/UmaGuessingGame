
import { CircleAlert, CircleCheck, Loader2, WifiOff } from "lucide-react";
import type { NetworkQuality } from "@/lib/realtime/network-quality";
import { cn } from "@/lib/utils";
import { useTranslation } from "@/i18n";

export type ConnectionStatus = "connecting" | "online" | "reconnecting" | "offline" | "stale";

const STATUS_META: Record<
  ConnectionStatus,
  { label: string; tone: "ok" | "warn" | "bad"; Icon: typeof CircleCheck; hint: string }
> = {
  /*
   * 连接状态只说玩家能感知的事实。
   * 不出现 WebSocket / SSE / 轮询 / 快照 / roomVersion 等内部词。
   */
  connecting: {
    label: "connection.connecting",
    tone: "ok",
    Icon: Loader2,
    hint: "connection.checking",
  },
  online: {
    label: "connection.online",
    tone: "ok",
    Icon: CircleCheck,
    hint: "",
  },
  reconnecting: {
    label: "connection.reconnecting",
    tone: "warn",
    Icon: Loader2,
    hint: "connection.retained",
  },
  offline: {
    label: "connection.offline",
    tone: "bad",
    Icon: WifiOff,
    hint: "connection.retained",
  },
  stale: {
    label: "connection.stale",
    tone: "warn",
    Icon: CircleAlert,
    hint: "connection.updating",
  },
};

export type ConnectionBarProps = {
  status: ConnectionStatus;
  quality?: NetworkQuality | null;
  /** 补充说明，例如重连尝试次数 */
  detail?: string;
  className?: string;
};

export function ConnectionBar({ status, quality, detail, className }: ConnectionBarProps) {
  const { t } = useTranslation("common");
  const meta = STATUS_META[status];
  const { Icon } = meta;

  return (
    <div
      role="status"
      aria-live="polite"
      data-tone={meta.tone === "ok" ? undefined : meta.tone}
      className={cn("uma-connbar flex-wrap", className)}
    >
      <Icon
        aria-hidden="true"
        className={cn("size-4 shrink-0", (status === "reconnecting" || status === "connecting") && "motion-safe:animate-spin")}
      />
      {/*
        状态同时有图标 + 文字，不只靠颜色。
        shrink-0 + whitespace-nowrap：标签必须保持单行。否则长房间名（如 24 字显示名）
        会把「已连接」挤成竖排三行（实测 320 宽下就是这样）。
      */}
      <span className="shrink-0 whitespace-nowrap font-semibold">{t(meta.label)}</span>
      {/*
        颜色继承父级 .uma-connbar，不在这里硬设 muted。
        连接条在 warn 态是黄底（--color-near #f1c550），muted (#526b87) 落在
        黄底上只有 3.37:1，低于 13px 正文要求的 4.5:1。父级已按 tone 分别设为
        --color-ink-strong / --color-ink，继承就是对比度最高的那个。
      */}
      <span className="min-w-0 truncate">{status === "online" ? detail : t(meta.hint)}</span>
      {status === "online" && quality ? <span className="ml-auto shrink-0 whitespace-nowrap text-sm" data-quality={quality}>{t("connection.qualityLabel")}：{t(`connection.quality.${quality}`)}</span> : null}
    </div>
  );
}
