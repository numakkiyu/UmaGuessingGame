import { roomText } from "@/i18n/room";
import { useTranslation } from "@/i18n";
import { Crown } from "lucide-react";
import type { Membership, RoomSnapshot } from "@/lib/api/types";

function MemberName({ member, selfId, event }: { member: Membership; selfId: string; event: boolean }) {
  const { t } = useTranslation("room");
  const controls = member.capabilities.some(cap => cap === "host" || cap === "manage-room");
  return <div className="min-w-0">
    <p className="flex flex-wrap items-center gap-1.5 font-semibold"><span className="break-words [overflow-wrap:anywhere]">{member.nickname ?? member.displayName}</span>{member.membershipId === selfId ? <span className="text-xs text-[var(--color-muted)]">{t("self")}</span> : null}{controls ? <span className="inline-flex items-center gap-1 text-xs text-[var(--color-action-green)]"><Crown className="size-3.5" aria-hidden="true" />{t(event ? "host" : "owner")}</span> : null}</p>
    <p className="mt-0.5 text-xs text-[var(--color-muted)]">{t("publicCode")} <span className="tabular-nums tracking-wider">{member.publicCode}</span></p>
  </div>;
}
function role(member: Membership) { return roomText(member.role === "team-captain" ? "captain" : member.capabilities.includes("play") ? "player" : member.role === "display" ? "display" : "spectator"); }
function ready(member: Membership) { return member.capabilities.includes("play") ? roomText(member.ready ? "ready" : "notReady") : "—"; }

export function RoomRoster({ room }: { room: RoomSnapshot }) {
  const { t } = useTranslation("room");
  return <section className="uma-card min-w-0 p-4" aria-label={t("roster")}>
    <div className="uma-section-head"><h2 className="text-base font-bold">{t("members")}</h2><p className="text-sm text-[var(--color-muted)]">{t("memberCount", { count: room.members.length })}</p></div>
    <div className="mt-3 hidden md:block"><table className="w-full table-fixed text-left text-sm"><caption className="sr-only">{t("rosterCaption")}</caption><thead className="border-b border-[var(--color-line)] text-xs text-[var(--color-muted)]"><tr><th scope="col" className="w-1/2 pb-2 font-medium">{t("trainer")}</th><th scope="col" className="pb-2 font-medium">{t("role")}</th><th scope="col" className="pb-2 font-medium">{t("readiness")}</th></tr></thead><tbody>{room.members.map(member => <tr key={member.membershipId} className="border-b border-[var(--color-line)] last:border-0"><td className="py-3 pr-3"><MemberName member={member} selfId={room.viewer.membershipId} event={room.kind === "event"} /></td><td className="py-3">{role(member)}</td><td className="py-3">{ready(member)}</td></tr>)}</tbody></table></div>
    <ul className="mt-3 space-y-2 md:hidden">{room.members.map(member => <li key={member.membershipId} className="flex items-center justify-between gap-3 rounded-lg border border-[var(--color-line)] p-3"><MemberName member={member} selfId={room.viewer.membershipId} event={room.kind === "event"} /><div className="shrink-0 text-right text-xs leading-6 text-[var(--color-muted)]"><p>{role(member)}</p><p>{ready(member)}</p></div></li>)}</ul>
  </section>;
}
