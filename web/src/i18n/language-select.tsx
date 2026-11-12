import { SelectField } from "@/components/ui/select";
import { useTranslation } from "react-i18next";
import { changeLanguage, LANGUAGES, type Language } from ".";

const LABELS: Record<Language, string> = { "zh-Hans": "简体中文", "zh-Hant": "繁體中文", ja: "日本語" };

export function LanguageSelect() {
  const { t, i18n } = useTranslation("common");
  return (
    <label className="flex min-h-11 min-w-0 items-center gap-2 text-sm text-[var(--color-ink)]">
      <span className="sr-only">{t("language.label")}</span>
      <SelectField value={i18n.resolvedLanguage ?? "zh-Hans"} onValueChange={value => void changeLanguage(value as Language)} className="w-auto min-w-36 text-sm font-semibold" options={[...LANGUAGES.map(language => ({value:language,lang:language,label:LABELS[language]}))]} />
    </label>
  );
}
