import { SelectField } from "@/components/ui/select";
/** Lobby roster editing. Names are presentation; every mutation uses a member id. */
import "@/i18n/teams";
import { useTranslation } from "@/i18n";
import { usePlayerText } from "@/i18n/player-text";
import { useMemo, useRef, useState } from "react";
import { Crown, Plus, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useApi } from "@/lib/api";
import { ApiError, describeFailure, localizeFailure, type Failure } from "@/lib/api/client";
import { TEAM_COLOR_STYLES, TEAM_COLOR_TOKENS, countSubjects, readRoomVersion, readTeamMembers, readTeams, validateTeams, type TeamColorToken, type TeamMember } from "@/lib/api/teams";

export type TeamPanelProps = { roomId: string; room: unknown; canManage: boolean; locked: boolean; onChanged: () => void | Promise<unknown> };

export function TeamPanel({ roomId, room, canManage, locked, onChanged }: TeamPanelProps) {
  const { t, i18n } = useTranslation("teams");
  const language = i18n.language;
  const { api } = useApi();
  const [busy,setBusy] = useState(false), inFlight = useRef(false);
  const [failure,setFailure] = useState<Failure|null>(null);
  const [notice,setNotice] = usePlayerText("teams");
  const [conflict,setConflict] = useState(false);
  const info = failure ? localizeFailure(failure) : null;
  const [label,setLabel] = useState(""),[color,setColor] = useState<TeamColorToken>("blue");
  const teams = useMemo(()=>{ void language; return readTeams(room); },[room,language]),members = useMemo(()=>{ void language; return readTeamMembers(room); },[room,language]);
  const version = readRoomVersion(room), count = countSubjects(members), problems = validateTeams(teams,members);
  const competitors = members.filter(m=>m.role==="player"||m.role==="team-captain");
  const unassigned = competitors.filter(m=>!m.teamId);
  const editable = canManage&&!locked;
  async function run(action:()=>Promise<void>) {
    if(!editable||inFlight.current)return;
    inFlight.current=true;setBusy(true);setFailure(null);setConflict(false);setNotice("");
    try { await action();await onChanged(); }
    catch(error){
      const info=describeFailure(error);
      if(error instanceof ApiError&&["version-conflict","room-version-conflict"].includes(error.code)){
        await onChanged();setConflict(true);setFailure(info);
      }else setFailure(info);
    }finally{inFlight.current=false;setBusy(false);}
  }
  function place(member:TeamMember,teamId:string|null,captain:boolean){
    if(version===null){setNotice("loading");return;}
    void run(async()=>{
      await api.updateMembership(roomId,member.membershipId,{expectedRoomVersion:version,teamId,requestedRole:captain?"team-captain":"player"});
      setNotice(teamId ? captain ? "appointed" : "joined" : "removed", { name: member.displayName });
    });
  }
  return <section className="min-w-0 space-y-4" aria-labelledby="teams-heading">
    <header><div className="flex flex-wrap items-center justify-between gap-2"><h2 id="teams-heading" className="text-lg font-bold">{t("heading")}</h2><p className="text-sm text-[var(--color-muted)]">{t("count", { count: count.teams })}{count.players ? t("individuals", { count: count.players }) : ""}</p></div><p className="mt-2 text-sm leading-6 text-[var(--color-muted)]">{t("hint")}</p></header>
    {info?<Alert tone="warning" title={conflict ? t("changedTitle") : info.title}>{conflict ? t("changedDetail") : info.detail}</Alert>:null}
    {notice?<p role="status" className="text-sm leading-6 text-[var(--color-action-green)]">{notice}</p>:null}
    {problems.length?<Alert tone="warning" title={t("check")}><ul className="list-disc space-y-1 pl-4">{problems.map((p,i)=><li key={i}>{p}</li>)}</ul></Alert>:null}
    {teams.length?<div className="grid min-w-0 gap-3 lg:grid-cols-2">{teams.map(team=>{
      const group=competitors.filter(m=>m.teamId===team.teamId);
      const tone=TEAM_COLOR_STYLES[team.colorToken as TeamColorToken]??TEAM_COLOR_STYLES.slate;
      return <section key={team.teamId} aria-label={t("team", { team: team.label })} className="min-w-0 rounded-[var(--radius-card)] border-2 p-4" style={{backgroundColor:tone.background,borderColor:tone.border}}>
        <div className="flex items-center gap-2 text-[var(--color-ink-strong)]"><Users className="size-4 shrink-0" aria-hidden="true"/><h3 className="min-w-0 flex-1 break-words font-bold">{team.label}</h3><span className="shrink-0 text-sm">{t("members", { count: group.length })}</span></div>
        {group.length?<ul className="mt-3 divide-y divide-[var(--color-line-strong)]">{group.map((member,index)=><li key={member.membershipId} className="min-w-0 space-y-2 py-3" aria-label={t("memberOrder", { name: member.displayName, index: index + 1 })}>
          <div className="flex flex-wrap items-center gap-2"><span className="min-w-0 break-words font-semibold text-[var(--color-ink-strong)]">{member.displayName}</span>{member.isCaptain?<span className="inline-flex items-center gap-1 text-sm text-[var(--color-ink-strong)]"><Crown className="size-4" aria-hidden="true"/>{t("captain")}</span>:<span className="text-sm text-[var(--color-ink-strong)]">{t("member")}</span>}</div>
          {member.publicCode ? <p className="text-xs text-[var(--color-muted)]">{t("publicCode")} {member.publicCode}</p> : null}
          {editable?<div className="flex flex-wrap gap-2">{!member.isCaptain?<Button className="h-auto min-h-11 max-w-full whitespace-normal [overflow-wrap:anywhere]" variant="secondary" onClick={()=>place(member,team.teamId,true)} disabled={busy}>{t("appoint")}</Button>:null}<Button className="h-auto min-h-11 max-w-full whitespace-normal [overflow-wrap:anywhere]" variant="secondary" onClick={()=>place(member,null,false)} disabled={busy}>{t("remove")}</Button></div>:null}
        </li>)}</ul>:<p className="mt-3 text-sm leading-6 text-[var(--color-ink-strong)]">{t("empty")}</p>}
      </section>;
    })}</div>:<p className="text-sm leading-6 text-[var(--color-muted)]">{t("noTeams")}</p>}
    {editable&&teams.length?<section aria-labelledby="unassigned-heading" className="space-y-3"><h3 id="unassigned-heading" className="font-semibold">{t("unassigned")}</h3>{unassigned.length?<ul className="space-y-3">{unassigned.map((member,index)=><li key={member.membershipId} aria-label={t("unassignedOrder", { name: member.displayName, index: index + 1 })} className="min-w-0 space-y-2 rounded-[var(--radius-control)] border border-[var(--color-line)] bg-[var(--color-panel)] p-3"><p className="break-words font-semibold">{member.displayName}</p>{member.publicCode ? <p className="text-xs text-[var(--color-muted)]">{t("publicCode")} {member.publicCode}</p> : null}<div className="flex flex-wrap gap-2">{teams.map(team=><Button key={team.teamId} className="h-auto min-h-11 max-w-full whitespace-normal [overflow-wrap:anywhere]" variant="secondary" disabled={busy} onClick={()=>place(member,team.teamId,!competitors.some(m=>m.teamId===team.teamId&&m.isCaptain))}>{t("join", { team: team.label })}</Button>)}</div></li>)}</ul>:<p className="text-sm text-[var(--color-muted)]">{t("allAssigned")}</p>}</section>:null}
    {editable?<details className="uma-card uma-disclosure p-4" open={teams.length===0?true:undefined}><summary className="min-h-11 cursor-pointer content-center font-semibold">{t("create")}</summary><div className="mt-3 space-y-3"><div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem]"><div className="space-y-2"><Label htmlFor="team-label">{t("name")}</Label><Input id="team-label" value={label} onChange={e=>setLabel(e.target.value)} maxLength={24} placeholder={t("placeholder")} disabled={busy}/></div><div className="space-y-2"><Label htmlFor="team-color">{t("color")}</Label><SelectField id="team-color" value={color} onValueChange={value =>setColor(value as TeamColorToken)} disabled={busy} options={[...TEAM_COLOR_TOKENS.map(c=>({value:c,label:t(`colors.${c}`)}))]} /></div></div><Button className="h-auto min-h-11 max-w-full whitespace-normal [overflow-wrap:anywhere]" variant="secondary" disabled={busy||!label.trim()||[...label.trim()].length>24||teams.length>=8} onClick={()=>void run(async()=>{await api.createTeam(roomId,{label:label.trim(),colorToken:color});setLabel("");setNotice("created");})}><Plus className="size-4" aria-hidden="true"/>{t("create")}</Button></div></details>:null}
    {locked?<p className="text-sm leading-6 text-[var(--color-muted)]">{t("locked")}</p>:null}
  </section>;
}
