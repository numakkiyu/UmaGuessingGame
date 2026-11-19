import { useLayoutEffect, useRef, useSyncExternalStore } from "react";
import { useLocation } from "react-router";
import { useTranslation } from "@/i18n";
import { resolvePlayerText } from "@/i18n/player-text";
import { answerConfirmation, cancelConfirmation, confirmationStore } from "@/lib/confirmation";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

export function ConfirmationDialog() {
  const { t } = useTranslation("common");
  const { key } = useLocation();
  const request = useSyncExternalStore(confirmationStore.subscribe, confirmationStore.getSnapshot, confirmationStore.getServerSnapshot);
  const cancel = useRef<HTMLButtonElement>(null);
  useLayoutEffect(() => () => cancelConfirmation(), [key]);
  if (!request) return null;
  const answer = (accepted: boolean) => answerConfirmation(request.id, accepted);
  return <Dialog open onOpenChange={open => { if (!open) answer(false); }}>
    <DialogContent role="alertdialog" className="uma-confirmation-dialog border-t-4 border-t-[var(--color-action-green)]"
      onOpenAutoFocus={event => { event.preventDefault(); cancel.current?.focus(); }}
      onCloseAutoFocus={event => {
        event.preventDefault();
        const opener = request.opener;
        if (opener?.isConnected && !opener.matches(":disabled, [aria-disabled='true']")) opener.focus();
        else document.getElementById("main-content")?.focus();
      }}
      onPointerDownOutside={event => event.preventDefault()}>
      <DialogHeader>
        <DialogTitle>{t(`confirmation.${request.kind}.title`)}</DialogTitle>
        <DialogDescription className="mt-2 break-words">{resolvePlayerText(request.message)}</DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <Button ref={cancel} variant="secondary" size="primary" className="h-auto min-h-12 whitespace-normal" onClick={() => answer(false)}>{t("confirmation.cancel")}</Button>
        <Button variant={request.kind === "remove" ? "danger" : "primary"} size="primary" className="h-auto min-h-12 whitespace-normal" onClick={() => answer(true)}>{t(`confirmation.${request.kind}.action`)}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}
