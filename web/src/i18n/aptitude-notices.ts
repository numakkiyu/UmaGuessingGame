import { registerMessages } from ".";
import { FeatureNotice } from "@/lib/player-notice";
export const aptitudeNotices = {
  "zh-Hans": {
    stopped: "本局已停止作答，可以查看结果", chooseCell: "请先选择一个空格", occupied: "这格已经有人啦，选另一格吧", used: "她已经上场了，每位赛马娘只能占一格", candidate: "请从这局的候选角色中选择", mismatch: "她还不同时符合这格的两项条件，换一位试试；这次不扣机会", fourTasks: "请为四个任务各选一位赛马娘", incomplete: "四项任务还没有全部选好角色", duplicate: "同一位赛马娘不能参加两个任务，请换一位", budget: "编队超出预算了，请调整后再确认", ended: "编队已经结束，可以查看结果", market: "请从本局牌池中为任务选择角色", noGrid: "这些资料还凑不齐九格，请重新准备角色资料", noDraft: "这组牌暂时凑不出足够的编队，请重新准备资料",
    pending: "这个任务还没选角色", notInPool: "选中的角色不在本局牌池里", reused: "「{{name}}」被 {{count}} 个任务重复使用，同一角色只能出场一次", overspent: "总价 {{spent}} 超过预算 {{budget}}，请换更便宜的角色",
  },
  "zh-Hant": {
    stopped: "本局已停止作答，可以查看結果", chooseCell: "請先選一個空格", occupied: "這格已經有人囉，選另一格吧", used: "她已經上場囉，每位賽馬娘只能占一格", candidate: "請從本局的候選角色中選擇", mismatch: "她的兩項適性還沒同時符合這格的條件，換一位試試吧，這次不扣次數", fourTasks: "請為四項任務各選一位賽馬娘", incomplete: "四項任務還沒全部選好角色", duplicate: "同一位賽馬娘不能參加兩項任務，請換一位", budget: "編隊超出預算了，請調整後再確認", ended: "編隊已結束，可以查看結果", market: "請從本局的角色卡中選人執行任務", noGrid: "目前的資料還無法填滿九格，請重新準備角色資料", noDraft: "這組角色卡還無法組成足夠的隊伍，請重新準備資料",
    pending: "這項任務還沒選角色", notInPool: "選中的角色不在本局的角色卡中", reused: "「{{name}}」重複安排在 {{count}} 項任務，同一位只能上場一次", overspent: "總花費 {{spent}} 超過預算 {{budget}}，請換成費用較低的角色",
  },
  ja: {
    stopped: "回答は終了しました。結果を確認できます", chooseCell: "先に空いているマスを選んでください", occupied: "このマスは埋まっています。別のマスを選ぼう", used: "そのウマ娘は出走済みです。一人につき一マスだけ埋められます", candidate: "今回の候補からウマ娘を選んでください", mismatch: "二つの適性がこのマスの条件を満たしていません。回数は減らないので、別のウマ娘を選んでみよう", fourTasks: "四つの課題に一人ずつ編成してください", incomplete: "まだウマ娘が決まっていない課題があります", duplicate: "同じウマ娘を二つの課題に編成できません。別のウマ娘を選んでください", budget: "編成コストが予算を超えています。調整してから確定してください", ended: "編成は終了しました。結果を確認できます", market: "今回の候補カードから課題に合うウマ娘を選んでください", noGrid: "九マスを埋められるデータがそろっていません。キャラクターデータを準備し直してください", noDraft: "このカードでは十分な編成を組めません。データを準備し直してください",
    pending: "まだウマ娘が決まっていません", notInPool: "選んだウマ娘は今回の候補に含まれていません", reused: "「{{name}}」を {{count}} 個の課題に重複して編成しています。一人につき一つの課題を担当できます", overspent: "合計コスト {{spent}} が予算 {{budget}} を超えています。コストの低いウマ娘に変更してください",
  },
} as const;
export type AptitudeNoticeKey = keyof typeof aptitudeNotices["zh-Hans"];
registerMessages("aptitudeNotices", aptitudeNotices);
export class AptitudeNotice extends FeatureNotice {
  constructor(key: AptitudeNoticeKey) { super("aptitudeNotices", key); }
}
