import i18n from "@/i18n";

export function roomDisplayName(room: { kind?: string; name?: string } | undefined, fallback: string): string {
  return room?.kind === "matchmaking" ? i18n.t("nav.matchmaking", { ns: "common" }) : room?.name || fallback;
}
