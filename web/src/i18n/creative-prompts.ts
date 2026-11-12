import i18n,{matchLanguage} from ".";
import type {CreativePrompt} from "@/lib/api/modes-g09-g12";
// Exact source matching keeps older/unknown authored rules unchanged, including literal constraint words.
export const creativePrompts=[
 {
  "id": "sc-01",
  "source": "下雨天的校门口",
  "zh-Hant": "下雨天的校門口",
  "ja": "雨の日の校門前"
 },
 {
  "id": "sc-02",
  "source": "训练结束后空掉的跑道",
  "zh-Hant": "訓練結束後空無一人的跑道",
  "ja": "トレーニングを終え、誰もいなくなったコース"
 },
 {
  "id": "sc-03",
  "source": "食堂快关门的时候",
  "zh-Hant": "餐廳快打烊的時候",
  "ja": "食堂が閉まる少し前"
 },
 {
  "id": "sc-04",
  "source": "借出去的笔记还没还",
  "zh-Hant": "借出的筆記還沒還回來",
  "ja": "貸したノートがまだ返ってこない"
 },
 {
  "id": "sc-05",
  "source": "第一次值日的清晨",
  "zh-Hant": "第一次值日的清晨",
  "ja": "初めて日直をする日の朝"
 },
 {
  "id": "sc-06",
  "source": "宿舍熄灯前的十分钟",
  "zh-Hant": "宿舍熄燈前的十分鐘",
  "ja": "寮の消灯まで、あと10分"
 },
 {
  "id": "sc-07",
  "source": "教室后排靠窗的位置",
  "zh-Hant": "教室後排靠窗的座位",
  "ja": "教室の一番後ろ、窓際の席"
 },
 {
  "id": "sc-08",
  "source": "自动贩卖机前找零钱",
  "zh-Hant": "在自動販賣機前找零錢",
  "ja": "自動販売機の前で小銭を探して"
 },
 {
  "id": "sc-09",
  "source": "操场看台上等一个人",
  "zh-Hant": "在操場看台等一個人",
  "ja": "校庭の観客席で、誰かを待ちながら"
 },
 {
  "id": "sc-10",
  "source": "文化祭收摊的傍晚",
  "zh-Hant": "學園祭收攤的傍晚",
  "ja": "学園祭の片付けをする夕暮れ"
 },
 {
  "id": "sc-11",
  "source": "雨停后的自行车棚",
  "zh-Hant": "雨停後的單車棚",
  "ja": "雨上がりの駐輪場"
 },
 {
  "id": "sc-12",
  "source": "周末早班的图书馆",
  "zh-Hant": "週末一早的圖書館",
  "ja": "週末、開館したばかりの図書館"
 },
 {
  "id": "sc-13",
  "source": "换季整理衣柜的下午",
  "zh-Hant": "換季整理衣櫃的下午",
  "ja": "衣替えでクローゼットを片付ける午後"
 },
 {
  "id": "sc-14",
  "source": "走廊尽头的公告栏前",
  "zh-Hant": "走廊盡頭的公布欄前",
  "ja": "廊下の突き当たりにある掲示板の前"
 },
 {
  "id": "sc-15",
  "source": "等末班车的时候",
  "zh-Hant": "等末班車的時候",
  "ja": "最終バスを待つひととき"
 },
 {
  "id": "sc-16",
  "source": "雪停后第一节课前",
  "zh-Hant": "雪停後第一堂課之前",
  "ja": "雪がやんだ朝、1時間目が始まる前"
 },
 {
  "id": "sc-17",
  "source": "社团活动室的午休",
  "zh-Hant": "社團教室的午休時間",
  "ja": "部室で過ごす昼休み"
 },
 {
  "id": "sc-18",
  "source": "把伞借出去之后",
  "zh-Hant": "把傘借出去之後",
  "ja": "傘を貸した、その後で"
 },
 {
  "id": "sc-19",
  "source": "体检当天的走廊",
  "zh-Hant": "健康檢查當天的走廊",
  "ja": "健康診断の日の廊下"
 },
 {
  "id": "sc-20",
  "source": "新学期发课本的时候",
  "zh-Hant": "新學期發課本的時候",
  "ja": "新学期、教科書を配るとき"
 },
 {
  "id": "sc-21",
  "source": "半夜肚子饿了",
  "zh-Hant": "半夜肚子餓了",
  "ja": "真夜中にお腹がすいて"
 },
 {
  "id": "sc-22",
  "source": "临时被叫去代打",
  "zh-Hant": "臨時被找去當代打",
  "ja": "急きょ代役を頼まれて"
 },
 {
  "id": "sc-23",
  "source": "捡到东西送去失物招领",
  "zh-Hant": "撿到東西，送到失物招領處",
  "ja": "拾った物を、落とし物窓口へ"
 },
 {
  "id": "sc-24",
  "source": "深夜便利店的关东煮",
  "zh-Hant": "深夜便利商店的關東煮",
  "ja": "深夜のコンビニ、おでんの前で"
 },
 {
  "id": "ct-01",
  "source": "整段不能出现「跑」字",
  "zh-Hant": "整段不能出現「跑」字",
  "ja": "文章に漢字の「跑」を使わない"
 },
 {
  "id": "ct-02",
  "source": "必须以一个问题结尾",
  "zh-Hant": "必須以一個問題作結",
  "ja": "最後は問いかけで締める"
 },
 {
  "id": "ct-03",
  "source": "必须包含一个具体的数字",
  "zh-Hant": "必須包含一個明確的數字",
  "ja": "具体的な数字を一つ以上入れる"
 },
 {
  "id": "ct-04",
  "source": "只能写一件事，不能转折",
  "zh-Hant": "只能寫一件事，不能轉折",
  "ja": "一つの出来事だけを書き、話を転換しない"
 },
 {
  "id": "ct-05",
  "source": "必须用第二人称「你」",
  "zh-Hant": "必須用第二人稱「你」",
  "ja": "二人称で書き、指定語「你」（あなた）を入れる"
 },
 {
  "id": "ct-06",
  "source": "不能出现任何人的名字",
  "zh-Hant": "不能出現任何人的名字",
  "ja": "誰の名前も出さない"
 },
 {
  "id": "ct-07",
  "source": "必须提到一件道具",
  "zh-Hant": "必須提到一件道具",
  "ja": "小道具を一つ以上登場させる"
 },
 {
  "id": "ct-08",
  "source": "只能写一句话，不得分段",
  "zh-Hant": "只能寫一句話，不得分段",
  "ja": "改行せず、一文だけで書く"
 },
 {
  "id": "ct-09",
  "source": "必须出现一个时间点",
  "zh-Hant": "必須出現一個時間點",
  "ja": "いつのことか分かる時刻を入れる"
 },
 {
  "id": "ct-10",
  "source": "不能使用感叹号",
  "zh-Hant": "不能使用驚嘆號",
  "ja": "感嘆符（！や!）を使わない"
 },
 {
  "id": "ct-11",
  "source": "必须以「后来」开头",
  "zh-Hant": "必須以原題指定的「后来」開頭",
  "ja": "指定語「后来」（その後）で書き始める"
 },
 {
  "id": "ct-12",
  "source": "必须包含一句对话",
  "zh-Hant": "必須包含一句對話",
  "ja": "せりふを一つ以上入れる"
 }
] as const;
export function localizeCreativePrompt(prompt:CreativePrompt,language=i18n.language):CreativePrompt&{original:boolean} {
 const locale=matchLanguage(language)??"zh-Hans";if(locale==="zh-Hans")return {...prompt,original:false};
 const scene=creativePrompts.find(p=>p.id===prompt.sceneId&&p.source===prompt.scene),constraint=creativePrompts.find(p=>p.id===prompt.constraintId&&p.source===prompt.constraint);
 return {...prompt,scene:scene?.[locale]??prompt.scene,constraint:constraint?.[locale]??prompt.constraint,original:!scene||!constraint};
}
