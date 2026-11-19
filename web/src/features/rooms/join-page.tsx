import { usePlayerName } from "@/lib/player-name";
import "@/i18n/entry";

// Resolve the invitation before joining; room identity and member authority come from the response
import { ArrowRight, KeyRound } from "lucide-react";
import { useId, useState } from "react";
import { useTranslation } from "@/i18n";
import { useNavigate, useSearchParams } from "react-router";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useApi } from "@/lib/api";
import { createFailure, describeFailure, localizeFailure } from "@/lib/api/client";
import { cn } from "@/lib/utils";

/** 房间匹配码长度 6–16（契约 InviteResolutionCreate minLength 6 / maxLength 16） */
const INVITE_MIN = 6;
const INVITE_MAX = 16;
const NAME_MAX = 24;

type SubmitPhase = "idle" | "submitting" | "confirming";

export function JoinPage() {
  const { t } = useTranslation(["entry", "common"]);
  const { api, ensureSession } = useApi();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const inviteId = useId();
  const errorId = useId();

  const [inviteCode, setInviteCode] = useState(searchParams.get("code")?.slice(0, 16) ?? "");
  const displayName = usePlayerName();
  const [phase, setPhase] = useState<SubmitPhase>("idle");
  const [storedFailure, setFailure] = useState<ReturnType<typeof describeFailure> | null>(null);
  const [fieldError, setFieldError] = useState<"invalidCode" | "invalidName" | null>(null);

  const failure = storedFailure ? localizeFailure(storedFailure) : null;
  const normalizedCode = inviteCode.trim().toUpperCase();
  const codeValid = normalizedCode.length >= INVITE_MIN && normalizedCode.length <= INVITE_MAX;
  const nameValid = [...displayName.trim()].length > 0 && [...displayName.trim()].length <= NAME_MAX;
  const busy = phase !== "idle";

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (busy) return;

    setFailure(null);
    setFieldError(null);

    if (!codeValid) {
      setFieldError("invalidCode");
      return;
    }
    if (!nameValid) {
      setFieldError("invalidName");
      return;
    }

    setPhase("submitting");
    try {
      // 1) 解析房间匹配码：只得到最小 roomId 与可加入状态，不返回成员秘密
      const resolution = await api.resolveRoomInvitation({ inviteCode: normalizedCode });
      const body = resolution.data;

      if (!body || !body.joinable) {
        // 统一提示，不区分「房间不存在」与「房间已满」，避免探测
        setFailure(createFailure("join-unavailable", { kind: "error", diagnosticId: resolution.requestId, retryable: false }));
        setPhase("idle");
        return;
      }

      // 2) 确保会话存在（加入需要匿名会话）
      await ensureSession(displayName.trim());

      // Join before navigating: a room read cannot grant membership from a URL.
      setPhase("confirming");
      await api.joinRoom(body.roomId, { inviteCode: normalizedCode, requestedRole: "player" });
      navigate(`/rooms/${encodeURIComponent(body.roomId)}`, {
        replace: false,
      });
    } catch (error) {
      setFailure(describeFailure(error));
      setPhase("idle");
    }
  }

  return (
    <div className="mx-auto w-full max-w-[480px]">
      <header className="mb-5">
        <h1 className="u-fluid-title text-[var(--color-ink)]">{t("join.title")}</h1>
        <p className="mt-2 text-base leading-7 text-[var(--color-muted)]">
          {t("join.intro")}
        </p>
      </header>

      <form
        noValidate
        onSubmit={(event) => void handleSubmit(event)}
        className="uma-panel flex flex-col gap-5 p-5"
        aria-describedby={failure ? errorId : undefined}
      >
        <div className="flex flex-col gap-2">
          <Label htmlFor={inviteId}>
            <KeyRound aria-hidden="true" className="size-4 text-[var(--color-action-green)]" />
            {t("join.code")}
          </Label>
          <Input
            id={inviteId}
            name="inviteCode"
            value={inviteCode}
            onChange={(event) => setInviteCode(event.target.value)}
            // 房间匹配码是短码，用等宽数字更易核对
            className="uma-code text-lg tracking-[0.12em]"
            placeholder={t("join.codePlaceholder")}
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            inputMode="text"
            maxLength={INVITE_MAX}
            required
            aria-invalid={fieldError !== null && !codeValid}
            aria-describedby={`${inviteId}-hint`}
            disabled={busy}
          />
          <p id={`${inviteId}-hint`} className="text-[var(--text-aux)] text-[var(--color-muted)]">
            {t("join.codeHint", { min: INVITE_MIN, max: INVITE_MAX })}
          </p>
        </div>

        {fieldError ? (
          <Alert tone="danger" title={t("join.check")}>
            <p>{t(`join.${fieldError}`, { min: INVITE_MIN, max: fieldError === "invalidCode" ? INVITE_MAX : NAME_MAX })}</p>
          </Alert>
        ) : null}

        {failure ? (
          <div id={errorId}>
            <Alert tone={failure.kind === "forbidden" ? "danger" : "warning"} title={failure.title}>
              <p>{failure.detail}</p>
              {failure.diagnosticId ? (
                <p className="mt-1 text-[var(--text-aux)]">
                  {t("state.problemId", { ns: "common" })}：<span className="uma-code">{failure.diagnosticId}</span>
                </p>
              ) : null}
            </Alert>
          </div>
        ) : null}

        {/* submitting：只禁用本动作，不全屏阻塞 */}
        <Button type="submit" variant="primary" size="primary" block disabled={busy}>
          {phase === "submitting"
            ? t("join.submitting")
            : phase === "confirming"
              ? t("join.confirming")
              : t("join.title")}
          {busy ? null : <ArrowRight aria-hidden="true" className="size-4" />}
        </Button>

        <p aria-live="polite" className="sr-only">
          {phase === "submitting" ? t("join.submitting") : phase === "confirming" ? t("join.confirming") : ""}
        </p>

        <div className={cn("border-t border-[var(--color-line)] pt-4 text-sm leading-6 text-[var(--color-muted)]")}>
          <p className="font-semibold text-[var(--color-ink)]">{t("join.noCode")}</p>
          <p className="mt-1">
            {t("join.noCodeDetail")}
          </p>
          <Button asChild variant="link" className="mt-1 px-0">
            <a href="/">{t("actions.home", { ns: "common" })}</a>
          </Button>
        </div>
      </form>
    </div>
  );
}
