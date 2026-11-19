import { SelectField } from "@/components/ui/select";
import { useTranslation } from "react-i18next";
import { changeLanguage, LANGUAGES, type Language } from ".";

const LABELS: Record<Language, string> = { "zh-Hans": "简体中文", "zh-Hant": "繁體中文", ja: "日本語" };

export function LanguageSelect({ fullWidth = false, compact = false }: { fullWidth?: boolean; compact?: boolean } = {}) {
  const { t, i18n } = useTranslation("common");
  return (
    <label className="flex min-h-11 min-w-0 items-center gap-2 text-sm text-[var(--color-ink)]">
      <span className="sr-only">{t("language.label")}</span>
      <SelectField value={i18n.resolvedLanguage ?? "zh-Hans"} onValueChange={value => void changeLanguage(value as Language)} className={fullWidth ? "w-full min-h-12 text-sm font-semibold" : compact ? "w-auto min-w-28 min-h-[50px] rounded-[14px] border-[#dce6d5] bg-[#f8fbf4] text-sm font-semibold" : "w-auto min-w-36 text-sm font-semibold"} options={[...LANGUAGES.map(language => ({value:language,lang:language,label:LABELS[language]}))]} />
    </label>
  );
}
