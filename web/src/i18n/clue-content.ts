import i18n, {matchLanguage} from ".";
import type {ClueCard,ClueItem} from "@/lib/api/modes-g09-g12";
export const clueCards = [
  {
    "id": "dc-01",
    "source": "档案 A：出赛记录",
    "zh-Hant": "檔案 A：出賽紀錄",
    "ja": "資料 A：出走記録"
  },
  {
    "id": "dc-02",
    "source": "档案 B：训练日志",
    "zh-Hant": "檔案 B：訓練日誌",
    "ja": "資料 B：トレーニング日誌"
  },
  {
    "id": "dc-03",
    "source": "档案 C：宿舍值日表",
    "zh-Hant": "檔案 C：宿舍值日表",
    "ja": "資料 C：寮の当番表"
  },
  {
    "id": "dc-04",
    "source": "档案 D：赛事报名表",
    "zh-Hant": "檔案 D：賽事報名表",
    "ja": "資料 D：レース申込書"
  },
  {
    "id": "dc-05",
    "source": "档案 E：体能测试表",
    "zh-Hant": "檔案 E：體能測試表",
    "ja": "資料 E：体力測定表"
  },
  {
    "id": "dc-06",
    "source": "档案 F：课程出席表",
    "zh-Hant": "檔案 F：課程出席表",
    "ja": "資料 F：授業の出席簿"
  },
  {
    "id": "dc-07",
    "source": "档案 G：器材借用单",
    "zh-Hant": "檔案 G：器材借用單",
    "ja": "資料 G：用具貸出表"
  },
  {
    "id": "dc-08",
    "source": "档案 H：场地预约表",
    "zh-Hant": "檔案 H：場地預約表",
    "ja": "資料 H：施設の予約表"
  },
  {
    "id": "dc-09",
    "source": "档案 I：陪练安排表",
    "zh-Hant": "檔案 I：陪練安排表",
    "ja": "資料 I：挑戰パートナー表"
  },
  {
    "id": "dc-10",
    "source": "档案 J：补给清单",
    "zh-Hant": "檔案 J：補給清單",
    "ja": "資料 J：補給品リスト"
  },
  {
    "id": "dc-11",
    "source": "档案 K：赛后复盘表",
    "zh-Hant": "檔案 K：賽後檢討表",
    "ja": "資料 K：レース振り返り表"
  },
  {
    "id": "dc-12",
    "source": "档案 L：出行计划表",
    "zh-Hant": "檔案 L：出行計畫表",
    "ja": "資料 L：外出予定表"
  }
] as const;
export function clueCardLabel(card:ClueCard,language=i18n.language):{text:string;original:boolean} {const lang=matchLanguage(language)??"zh-Hans",known=clueCards.find(c=>c.id===card.cardId&&c.source===card.label);return {text:known&&lang!=="zh-Hans"?known[lang]:card.label,original:lang!=="zh-Hans"&&!known};}
/** Only localise a known generated statement if its text and all semantic parameters agree. */
export function clueText(clue:ClueItem,cards:ClueCard[],language=i18n.language):{text:string;original:boolean} {
 const lang=matchLanguage(language)??"zh-Hans",members=clue.cards.map(id=>cards.find(c=>c.cardId===id));if(members.some(c=>!c)||new Set(clue.cards).size!==clue.cards.length)return {text:clue.text,original:lang!=="zh-Hans"};
 const source=members.map(c=>c!.label),labels=members.map(c=>clueCardLabel(c!,language));let expected:string|undefined,text:string|undefined;
 switch(clue.kind){
 case "position": {const legacy=clue.text.match(/ 在第 ([1-6]) 位$/),position=clue.position??(legacy?Number(legacy[1]):null);if(source.length!==1||typeof position!=="number"||!Number.isInteger(position)||position<1||position>6)break;expected=`${source[0]} 在第 ${position} 位`;text=lang==="ja"?`${labels[0]!.text}は${position}番目`:`${labels[0]!.text}在第 ${position} 位`;break;}
 case "before":if(source.length===2){expected=`${source[0]} 排在 ${source[1]} 前面`;text=lang==="ja"?`${labels[0]!.text}は${labels[1]!.text}より前`:`${labels[0]!.text}排在${labels[1]!.text}前面`;}break;
 case "adjacent":if(source.length===2){expected=`${source[0]} 与 ${source[1]} 相邻`;text=lang==="ja"?`${labels[0]!.text}と${labels[1]!.text}は隣り合う`:`${labels[0]!.text}與${labels[1]!.text}相鄰`;}break;
 case "not_with":if(source.length===2){expected=`${source[0]} 与 ${source[1]} 不相邻`;text=lang==="ja"?`${labels[0]!.text}と${labels[1]!.text}は隣り合わない`:`${labels[0]!.text}與${labels[1]!.text}不相鄰`;}break;
 case "group":if(source.length===3){expected=`${source.join("、")} 连续排在一起`;text=lang==="ja"?`${labels.map(c=>c.text).join("・")}は、間を空けずにひとまとまりに並ぶ。並び順は自由です`:`${labels.map(c=>c.text).join("、")}連續排在一起，彼此順序不限`;}break;
 }
 if(lang==="zh-Hans")return {text:clue.text,original:false};
 if(!expected||expected!==clue.text||!text)return {text:clue.text,original:true};return {text,original:labels.some(c=>c.original)};
}
