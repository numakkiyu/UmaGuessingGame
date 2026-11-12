import i18n from "@/i18n";
const formats=new Map<string,Intl.NumberFormat>();

/** Keep the persisted hundredth-of-a-point precision in every event view. */
export function formatScoreUnits(units:number):string {
  const locale=i18n.resolvedLanguage??i18n.language;
  let formatter=formats.get(locale);if(!formatter){formatter=new Intl.NumberFormat(locale,{maximumFractionDigits:2,useGrouping:false});formats.set(locale,formatter);}
  return Number.isSafeInteger(units)?formatter.format(units/100):"—";
}
