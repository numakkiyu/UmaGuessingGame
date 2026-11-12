import { useTranslation } from "@/i18n";
import { eventText } from "@/i18n/event-results";
import { publicCodeOf, displayNameOf, type EventMeta, type SegmentRef } from "./use-event-data";
import i18n from "@/i18n";
import { toKnownModeId } from "@/lib/api/types";
import { cn } from "@/lib/utils";
export function eventModeLabel(modeId:string):string {const id=toKnownModeId(modeId);return id?i18n.t(`${id}.name`,{ns:"modes"}):eventText("game");}
export function eventSegmentLabel(segment:SegmentRef,index:number):string {return segment.name||eventText(segment.modeId?"itemMode":"item",{number:segment.ordinal??index+1,mode:segment.modeId?eventModeLabel(segment.modeId):""});}
export function EventTieArrangement({event,className}:{event:EventMeta;className?:string}){
 const {t}=useTranslation("eventResults");
 return <div className={cn("mt-3 border-t border-[var(--color-line)] pt-3 text-sm leading-6",className)}><h2 className="font-semibold">{t("tieHeading")}</h2><p className="text-[var(--color-muted)]">{t(event.tiePolicy==="shared-rank"?"sharedTie":event.tiePolicy==="announced-tiebreak"?"announcedTie":"unknownTie")}</p></div>;
}

/** Public room number is a label, never an internal subject ID or credential. */
export function EventSubjectName({subject}:{subject:{displayName:string;subjectId:string;publicCode?:string}}){
 const {t}=useTranslation("eventResults");
 const code=publicCodeOf(subject.publicCode);
 return <span className="block min-w-0 break-words"><span className="block">{displayNameOf(subject)}</span>{code?<span className="block text-xs font-normal text-[var(--color-muted)]">{t("publicCode",{code})}</span>:null}</span>;
}
