import { localizeAptitudePresentation } from "@/i18n/aptitude";
import { api } from "@/lib/api";
import type { DraftGame, GridGame } from "@/lib/domain/aptitude-practice";
import { createDraftGame, createGridGame, draftStore, gridStore, readDraftPack, readGridPack, validateDraftArchive, validateGridArchive, volatileDraftGames, volatileGridGames, type DraftPack, type GridPack } from "@/lib/local/aptitude-practice-store";
import type { PracticeAdapter } from "./activity-practice-kit";

export const gridAdapter: PracticeAdapter<GridPack, GridGame> = localizeAptitudePresentation({
  modeId: "aptitude-grid", title: "适性交叉占格", summary: "这格派谁？找到同时符合距离与跑法两项条件的赛马娘，填满九格", hint: "每局 180 秒，同一位只能占一格。确认合法的占格不可撤销，不计排名", prepareLabel: "准备占格资料", startLabel: "开始占格", filePrefix: "umaguess-grid",
  rules: ["行列分别是距离和跑法条件，两项适性都为 A 才能占格。先选空格，再选角色，最后确认", "确认前不提示某位角色是否符合。选错不扣机会，可以换人再试；合法占格后不能撤销，同一位赛马娘不能重复使用", "180 秒内可逐格确认，得分为正确占格数占九格的比例，按百分制向下取整。结束后会展示每格条件和一种完整排法"],
  fetch: signal => api.getAptitudePracticePack(signal), read: raw => { const data = readGridPack(raw); return { pack: data.pack, media: data.cards }; }, validate: validateGridArchive, create: createGridGame, store: gridStore, volatile: volatileGridGames,
});
export const draftAdapter: PracticeAdapter<DraftPack, DraftGame> = localizeAptitudePresentation({
  modeId: "draft", title: "公开编队夺标", summary: "预算内排好阵容，让四位赛马娘各自挑战最合适的任务吧", hint: "12 张公开角色卡，4 个任务，预算 24。每局 240 秒，最终确认前都能调整，不计排名", prepareLabel: "准备编队资料", startLabel: "开始编队", filePrefix: "umaguess-draft",
  rules: ["四个任务各需要一位不同角色，牌价总和不能超过 24。角色的场地、距离与跑法等级全部公开", "每个任务按三项适性计分，A 得 3 点、B 得 2 点、C 得 1 点，其余及未知为 0 点。四项任务满分 36 点，再按百分制向下取整。适配点数不是原作胜率", "选择与换人会保留为草稿；确认编队才计分，并立即结束这一局。缺人、重复或超预算不能确认，时间到不会自动替你提交", "结束后可以对照一种最佳编队。最佳编队可能不止一套，同分不按提交速度区别"],
  fetch: signal => api.getDraftPracticePack(signal), read: raw => { const data = readDraftPack(raw); return { pack: data.pack, media: data.cards }; }, validate: validateDraftArchive, create: createDraftGame, store: draftStore, volatile: volatileDraftGames,
});
