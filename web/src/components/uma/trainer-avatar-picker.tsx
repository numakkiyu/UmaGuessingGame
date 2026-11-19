import "@/i18n/trainer";
import { useId, useMemo, useRef, useState } from "react";
import { Check, ChevronDown, ChevronLeft, ChevronRight, Search, X } from "lucide-react";
import { useTranslation, matchLanguage, type Language } from "@/i18n";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import catalog from "@/lib/trainer-avatars.json";
import { TrainerAvatar } from "./trainer-avatar";

type Avatar = { id: string; names: Partial<Record<Language, string>>; search: string[] };
const avatars: readonly Avatar[] = catalog;
const normal = (value: string) => value.normalize("NFKC").toLowerCase().replace(/[\s・·]/g, "");
export function TrainerAvatarPicker({ value, onChange, disabled }: { value: string; onChange: (id: string) => void; disabled: boolean }) {
  const { t, i18n } = useTranslation("trainer");
  const locale = matchLanguage(i18n.resolvedLanguage ?? "") ?? "zh-Hans";
  const nameOf = (item: Avatar) => item.names[locale] || item.names.ja || item.names["zh-Hans"] || "";
  const [expanded, setExpanded] = useState(false);
  const [query, setQuery] = useState(""), [page, setPage] = useState(0);
  const panelId = useId(), searchId = useId(), labelId = useId();
  const disclosure = useRef<HTMLButtonElement>(null);
  const selected = avatars.find(item => item.id === value) ?? avatars[0]!;
  const results = useMemo(() => {
    const needle = normal(query.trim());
    if (!needle) return avatars;
    const exact = avatars.filter(item => [...Object.values(item.names), item.search[0]].some(name => name && normal(name) === needle));
    return exact.length ? exact : avatars.filter(item => [...Object.values(item.names), ...item.search].some(name => name && normal(name).includes(needle)));
  }, [query]);
  const pages = Math.ceil(avatars.length / 5);
  const candidates = avatars.slice(page * 5, page * 5 + 5);
  const recommended = candidates.length < 5 ? [...candidates, ...avatars.slice(0, 5 - candidates.length)] : candidates;
  function select(id: string) {
    onChange(id);
    setPage(Math.floor(avatars.findIndex(item => item.id === id) / 5));
  }
  return <fieldset disabled={disabled} className="uma-avatar-picker">
    <legend id={labelId} className="mb-3 font-semibold">{t("profile.avatar")}</legend>
    <div className="uma-avatar-choices">{recommended.map(item => <button key={item.id} type="button" aria-pressed={value === item.id} aria-label={t("profile.choose", { name: nameOf(item) })} onClick={() => select(item.id)}><TrainerAvatar id={item.id} size={80} />{value === item.id ? <Check aria-hidden="true" /> : null}</button>)}</div>
    <div className="uma-avatar-pagination"><Button type="button" variant="ghost" size="icon" aria-label={t("profile.previous")} onClick={() => setPage(current => (current + pages - 1) % pages)}><ChevronLeft aria-hidden="true" /></Button><span aria-hidden="true">{page + 1} / {pages}</span><Button type="button" variant="ghost" size="icon" aria-label={t("profile.next")} onClick={() => setPage(current => (current + 1) % pages)}><ChevronRight aria-hidden="true" /></Button></div>
    <button ref={disclosure} type="button" className="uma-avatar-dropdown" aria-expanded={expanded} aria-controls={panelId} onClick={() => setExpanded(open => !open)}><TrainerAvatar id={value} size={44} /><span>{nameOf(selected)}</span><span>{t("profile.browseAvatars")}</span><ChevronDown aria-hidden="true" /></button>
    {expanded ? <div id={panelId} className="uma-avatar-browser" role="group" aria-labelledby={labelId}>
      <Label htmlFor={searchId} className="sr-only">{t("profile.searchAvatars")}</Label>
      <div className="uma-avatar-search"><Search aria-hidden="true" /><Input id={searchId} type="search" value={query} placeholder={t("profile.searchPlaceholder")} onChange={event => setQuery(event.target.value)} onKeyDown={event => {
        if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); setExpanded(false); disclosure.current?.focus(); }
        if (event.key === "Enter" && !event.nativeEvent.isComposing) { event.preventDefault(); if (results.length === 1) select(results[0]!.id); }
      }} />{query ? <Button type="button" size="icon" variant="ghost" aria-label={t("profile.clearSearch")} onClick={() => setQuery("")}><X aria-hidden="true" /></Button> : null}</div>
      <p role="status" className="uma-avatar-result-count">{t("profile.avatarCount", { count: results.length })}</p>
      <div className="uma-avatar-catalog">{results.map(item => <button key={item.id} type="button" aria-pressed={item.id === value} aria-label={t("profile.choose", { name: nameOf(item) })} onClick={() => select(item.id)}><TrainerAvatar id={item.id} size={88} /><span>{nameOf(item)}</span>{item.id === value ? <Check aria-hidden="true" /> : null}</button>)}</div>
      {!results.length ? <p className="uma-avatar-empty">{t("profile.noAvatars")}</p> : null}
    </div> : null}
  </fieldset>;
}
