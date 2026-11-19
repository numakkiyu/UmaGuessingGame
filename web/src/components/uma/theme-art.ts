export const THEME_CHARACTERS = [
  { key: "specialweek", id: "1001", runner: "01_specialweek", source: "https://zh.moegirl.org.cn/特别周" },
  { key: "silencesuzuka", id: "1002", runner: "02_silencesuzuka", source: "https://wiki.biligame.com/umamusume/角色语音_无声铃鹿" },
  { key: "tokaiteio", id: "1003", runner: "03_tokaiteio", source: "https://wiki.biligame.com/umamusume/东海帝皇" },
  { key: "oguricap", id: "1006", runner: null, source: "https://umamusume.jp/character/oguricap" },
  { key: "goldship", id: "1007", runner: null, source: "https://umamusume.jp/character/goldship" },
  { key: "mejiromcqueen", id: "1013", runner: null, source: "https://umamusume.jp/character/mejiromcqueen" },
] as const;
export function pickDifferent<T>(items: readonly T[], previous?: T): T {
  const remaining = items.filter(item => item !== previous);
  const choices = remaining.length ? remaining : items;
  return choices[Math.floor(Math.random() * choices.length)]!;
}
export function pageScene(path: string): string {
  if (path === "/") return "/assets/ui/backgrounds/academy.webp";
  if (/^\/(party|events)(\/|$)/.test(path)) return "/assets/ui/backgrounds/concert-stage.webp";
  if (/^\/(friends|join|rooms)(\/|$)/.test(path)) return "/assets/ui/backgrounds/clubroom.webp";
  if (/^\/matchmaking(?:\/|$)/.test(path)) return "/assets/ui/backgrounds/stage.webp";
  if (/^\/help(?:\/|$)/.test(path)) return "/assets/ui/scenes/results-riverside.jpg";
  if (/^\/sources(?:\/|$)/.test(path)) return "/assets/ui/backgrounds/academy.webp";
  if (/^\/(play|practice)\/(classic|sort|pedigree|race-rebuild|audio)/.test(path)) return "/assets/ui/scenes/lobby-classroom.jpg";
  if (/^\/(play|practice|challenges)(\/|$)/.test(path)) return "/assets/ui/backgrounds/academy.webp";
  return "/assets/ui/backgrounds/training-course.webp";
}
