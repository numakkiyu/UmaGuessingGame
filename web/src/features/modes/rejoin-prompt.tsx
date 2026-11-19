// Rejoining preserves the existing member identity; creating a new session would lose room membership
import "@/i18n/round-ui";
import { useTranslation } from "@/i18n";
import { Link } from "react-router";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ApiError, type Failure } from "@/lib/api/client";

/** 这个失败是不是「刷新后凭据失效」 */
export function isCredentialRefreshFailure(error: unknown): boolean {
  return error instanceof ApiError && (error.code === "CSRF_UNAVAILABLE" || error.code === "csrf-token-invalid");
}

/** 从任意异常判断是否是凭据失效 */
export function failureIsCredentialRefresh(failure: Failure | null): boolean {
  return failure?.messageKey === "session-rejoin-required";
}

/**
 * 刷新后凭据失效的提示 + 重新进入入口。
 *
 * @param roomId 房间号，用于「重新进入这一局」
 */
export function RejoinPrompt({ roomId }: { roomId: string }) {
  const { t } = useTranslation("roundUI");
  return (
    <Alert
      tone="warning"
      title={t("rejoinTitle")}
      action={
        <Button asChild variant="secondary">
          <Link to={`/rooms/${encodeURIComponent(roomId)}`}>{t("rejoinAction")}</Link>
        </Button>
      }
    >
      <p>{t("rejoinDetail")}</p>
      <p className="mt-1">{t("kept")}</p>
    </Alert>
  );
}
