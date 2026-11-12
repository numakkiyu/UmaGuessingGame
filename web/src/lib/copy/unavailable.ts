// Translate admission diagnostics into player guidance without exposing raw rejection codes

import i18n from "@/i18n";

type Copy = { title: string; detail: string };
type Reason = "audio" | "bluff" | "short" | "generic";

const BY_CODE: Array<{ code: RegExp; key: Reason }> = [
  { code: /no_local_audio_file/, key: "audio" },
  { code: /unreviewed_fact_not_publishable|no_reviewed_secret_fact/, key: "bluff" },
  { code: /no_usable_items|minimum \d+/, key: "short" },
];

/** Empty reasons remain absent; internal diagnostics are never rendered. */
export function describeUnavailable(modeId: string, reason: string | null | undefined): Copy | null {
  if (!reason || !reason.trim()) return null;
  const key = BY_CODE.find(entry => entry.code.test(reason))?.key
    ?? (modeId === "audio" || modeId === "bluff" ? modeId : "generic");
  return {
    title: i18n.t(`unavailable.${key}.title`, { ns: "entry" }),
    detail: i18n.t(`unavailable.${key}.detail`, { ns: "entry" }),
  };
}
