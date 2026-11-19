import "./classic";
import i18n from ".";
import type { NormalizedCell } from "@/lib/api/classic";

// The source enum is part of the immutable question data. Translate only the
// displayed value; never feed a translated value back into comparison or saves.
const values: Partial<Record<NormalizedCell["field"], Record<string, string>>> = {
  surface: { "草地": "turf", "泥地": "dirt" },
  distance: { "短": "sprint", "英": "mile", "中": "medium", "长": "long" },
  style: { "逃": "front", "先": "pace", "差": "late", "追": "end" },
  grade: { "中等部": "junior", "高等部": "senior" },
  dormitory: { "栗东宿舍": "ritto", "美浦宿舍": "miho", "独居": "alone" },
};
export function displayClassicValue(cell: Pick<NormalizedCell, "field" | "value">): string {
  if (!cell.value) return "—";
  if (cell.value === "未知") return i18n.t("values.unknown", { ns: "classic" });
  const star = cell.field === "star" ? /^([0-5])星$/.exec(cell.value) : null;
  if (star) return i18n.t("values.star", { ns: "classic", count: Number(star[1]) });
  const mapping = values[cell.field];
  if (!mapping) return cell.value;
  return cell.value.split("/").map(raw => {
    const key = mapping[raw.trim()];
    return key ? i18n.t(`values.${key}`, { ns: "classic" }) : raw;
  }).join(" / ");
}
