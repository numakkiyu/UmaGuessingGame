import i18n, { registerMessages } from ".";
export const aptitudeValues = {
  "zh-Hans": { axes: { surface: "场地", distance: "距离", style: "跑法" }, values: { turf: "草地", dirt: "泥地", sprint: "短", mile: "英", medium: "中", long: "长", front: "逃", pace: "先", late: "差", end: "追" } },
  "zh-Hant": { axes: { surface: "場地", distance: "距離", style: "跑法" }, values: { turf: "草地", dirt: "沙地", sprint: "短", mile: "英里", medium: "中", long: "長", front: "逃", pace: "前列", late: "居中", end: "後追" } },
  ja: { axes: { surface: "バ場", distance: "距離", style: "脚質" }, values: { turf: "芝", dirt: "ダート", sprint: "短距離", mile: "マイル", medium: "中距離", long: "長距離", front: "逃げ", pace: "先行", late: "差し", end: "追込" } },
} as const;
registerMessages("aptitudeValues", aptitudeValues);
const enums: Record<string, Record<string, string>> = { surface: { "草地": "turf", "泥地": "dirt" }, distance: { "短": "sprint", "英": "mile", "中": "medium", "长": "long" }, style: { "逃": "front", "先": "pace", "差": "late", "追": "end" } };
export function axisName(axis: string): string { return Object.hasOwn(enums, axis) ? i18n.t(`axes.${axis}`, { ns: "aptitudeValues" }) : "—"; }
export function aptitudeValue(axis: string, value: string): string {
  const table = Object.hasOwn(enums, axis) ? enums[axis] : undefined;
  const key = table && Object.hasOwn(table, value) ? table[value] : undefined;
  return key ? i18n.t(`values.${key}`, { ns: "aptitudeValues" }) : "—";
}
export function taskConditions(task: { surface: string; distance: string; style: string }): string { return ["surface", "distance", "style"].map(axis => aptitudeValue(axis, task[axis as keyof typeof task])).join(" · "); }
