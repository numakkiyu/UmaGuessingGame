import i18n from ".";
import type {RapportQuestion} from "@/lib/api/modes-g09-g12";
// Match the exact authored source, not just an ID that an older pack may reuse.
export const rapportQuestions = [
 {
  "id": "rp-01",
  "source": {
   "prompt": "训练结束后最想做的事是？",
   "options": [
    "去食堂加一餐",
    "回宿舍补觉",
    "再跑一组加练",
    "和大家一起看电影"
   ]
  },
  "zh-Hant": {
   "prompt": "訓練結束後最想做甚麼？",
   "options": [
    "去餐廳再吃一餐",
    "回宿舍補眠",
    "再多練一組",
    "和大家一起看電影"
   ]
  },
  "ja": {
   "prompt": "トレーニングが終わったら、一番したいことは？",
   "options": [
    "食堂でもうひと食べ",
    "寮に戻ってひと眠り",
    "もう1セット追加で練習",
    "みんなと映画を見る"
   ]
  }
 },
 {
  "id": "rp-02",
  "source": {
   "prompt": "赛前最需要的心理准备是？",
   "options": [
    "安静独处",
    "听一首固定的歌",
    "和搭档聊两句",
    "把路线再走一遍"
   ]
  },
  "zh-Hant": {
   "prompt": "賽前最需要哪一種心理準備？",
   "options": [
    "安靜地獨處",
    "聽一首固定的歌",
    "和搭檔聊幾句",
    "再走一遍路線"
   ]
  },
  "ja": {
   "prompt": "レース前、気持ちを整えるなら？",
   "options": [
    "静かに一人で過ごす",
    "いつもの1曲を聴く",
    "相棒と少し話す",
    "コースをもう一度歩く"
   ]
  }
 },
 {
  "id": "rp-03",
  "source": {
   "prompt": "如果只能选一种奖励，会选？",
   "options": [
    "多一天休息",
    "一份限定甜点",
    "新的训练装备",
    "现场看一场比赛"
   ]
  },
  "zh-Hant": {
   "prompt": "如果只能選一種獎勵，你會選？",
   "options": [
    "多休息一天",
    "一份限定甜點",
    "新的訓練裝備",
    "到現場看一場比賽"
   ]
  },
  "ja": {
   "prompt": "ご褒美を一つだけ選べるなら？",
   "options": [
    "休みをもう1日",
    "限定スイーツ",
    "新しいトレーニング用品",
    "レースを現地で観戦"
   ]
  }
 },
 {
  "id": "rp-04",
  "source": {
   "prompt": "遇到状态低谷时，更希望？",
   "options": [
    "别人别来打扰",
    "有人陪着待着",
    "被直接指出问题",
    "换个环境散散心"
   ]
  },
  "zh-Hant": {
   "prompt": "狀態低潮時，你比較希望？",
   "options": [
    "別人先不要打擾",
    "有人陪在身邊",
    "有人直接指出問題",
    "換個環境散心"
   ]
  },
  "ja": {
   "prompt": "調子が落ち込んでいるとき、どうしてほしい？",
   "options": [
    "そっとしておいてほしい",
    "誰かにそばにいてほしい",
    "問題点をはっきり指摘してほしい",
    "場所を変えて気分転換したい"
   ]
  }
 },
 {
  "id": "rp-05",
  "source": {
   "prompt": "在团队里最愿意承担的角色是？",
   "options": [
    "定计划的人",
    "盯细节的人",
    "鼓励大家的人",
    "默默补位的人"
   ]
  },
  "zh-Hant": {
   "prompt": "在團隊中，你最想擔任甚麼角色？",
   "options": [
    "擬定計劃的人",
    "留意細節的人",
    "鼓勵大家的人",
    "默默補位的人"
   ]
  },
  "ja": {
   "prompt": "チームで引き受けたい役割は？",
   "options": [
    "計画を立てる役",
    "細かいところを確認する役",
    "みんなを励ます役",
    "さりげなくフォローする役"
   ]
  }
 },
 {
  "id": "rp-06",
  "source": {
   "prompt": "比赛当天早餐会选？",
   "options": [
    "照常吃",
    "只喝点东西",
    "多吃一点",
    "和平时一样但更早吃"
   ]
  },
  "zh-Hant": {
   "prompt": "比賽當天的早餐，你會選？",
   "options": [
    "照平常吃",
    "只喝點東西",
    "多吃一點",
    "吃得和平常一樣，但早一點吃"
   ]
  },
  "ja": {
   "prompt": "レース当日の朝ごはん、どうする？",
   "options": [
    "いつも通り食べる",
    "飲み物だけにする",
    "少し多めに食べる",
    "量はいつも通り、時間を早める"
   ]
  }
 },
 {
  "id": "rp-07",
  "source": {
   "prompt": "空闲的半天最想去？",
   "options": [
    "训练场",
    "图书馆",
    "商店街",
    "什么都不做"
   ]
  },
  "zh-Hant": {
   "prompt": "有半天空檔，你最想怎麼過？",
   "options": [
    "去訓練場",
    "去圖書館",
    "逛商店街",
    "甚麼都不做"
   ]
  },
  "ja": {
   "prompt": "半日自由に使えるなら？",
   "options": [
    "トレーニング場へ行く",
    "図書館へ行く",
    "商店街へ行く",
    "何もせずに過ごす"
   ]
  }
 },
 {
  "id": "rp-08",
  "source": {
   "prompt": "收到礼物时更在意？",
   "options": [
    "实用",
    "心意",
    "惊喜",
    "包装"
   ]
  },
  "zh-Hant": {
   "prompt": "收到禮物時，你比較在意？",
   "options": [
    "實用性",
    "心意",
    "驚喜感",
    "包裝"
   ]
  },
  "ja": {
   "prompt": "プレゼントをもらうとき、一番重視するのは？",
   "options": [
    "実用的なこと",
    "気持ちがこもっていること",
    "サプライズがあること",
    "ラッピングがすてきなこと"
   ]
  }
 },
 {
  "id": "rp-09",
  "source": {
   "prompt": "最不擅长应对的是？",
   "options": [
    "突然被点名",
    "长时间的等待",
    "人多吵闹",
    "被夸奖"
   ]
  },
  "zh-Hant": {
   "prompt": "你最不擅長應付哪一種情況？",
   "options": [
    "突然被點名",
    "等很久",
    "人多又吵鬧",
    "被稱讚"
   ]
  },
  "ja": {
   "prompt": "一番苦手な場面は？",
   "options": [
    "急に指名される",
    "長く待たされる",
    "人が多くて騒がしい",
    "褒められる"
   ]
  }
 },
 {
  "id": "rp-10",
  "source": {
   "prompt": "复盘时会先看？",
   "options": [
    "自己的失误",
    "对手的强项",
    "整体节奏",
    "最后一个弯道"
   ]
  },
  "zh-Hant": {
   "prompt": "回顧比賽時，你會先看？",
   "options": [
    "自己的失誤",
    "對手的強項",
    "整體節奏",
    "最後一個彎道"
   ]
  },
  "ja": {
   "prompt": "レースを振り返るとき、最初に見るのは？",
   "options": [
    "自分のミス",
    "相手の強み",
    "全体のペース",
    "最後のコーナー"
   ]
  }
 },
 {
  "id": "rp-11",
  "source": {
   "prompt": "如果换一种训练方式，最想试？",
   "options": [
    "换场地",
    "换距离",
    "换跑法",
    "换搭档"
   ]
  },
  "zh-Hant": {
   "prompt": "如果改變訓練方式，你最想試？",
   "options": [
    "換場地",
    "換距離",
    "換跑法",
    "換搭檔"
   ]
  },
  "ja": {
   "prompt": "トレーニングに変化をつけるなら？",
   "options": [
    "場所を変える",
    "距離を変える",
    "走り方を変える",
    "相棒を変える"
   ]
  }
 },
 {
  "id": "rp-12",
  "source": {
   "prompt": "最希望别人记住你的哪一点？",
   "options": [
    "成绩",
    "努力",
    "性格",
    "某一场比赛"
   ]
  },
  "zh-Hant": {
   "prompt": "你最希望別人記住自己哪一點？",
   "options": [
    "成績",
    "努力",
    "個性",
    "某一場比賽"
   ]
  },
  "ja": {
   "prompt": "自分のどんなところを覚えていてほしい？",
   "options": [
    "成績",
    "努力",
    "性格",
    "あるレースでの姿"
   ]
  }
 },
 {
  "id": "rp-13",
  "source": {
   "prompt": "遇到不擅长的距离时会？",
   "options": [
    "提前加练",
    "比赛中调整",
    "靠搭档补位",
    "接受结果"
   ]
  },
  "zh-Hant": {
   "prompt": "遇到不擅長的距離時，你會？",
   "options": [
    "提前加練",
    "在比賽中調整",
    "靠搭檔補位",
    "接受結果"
   ]
  },
  "ja": {
   "prompt": "苦手な距離のレースに出るなら？",
   "options": [
    "事前に練習を増やす",
    "レース中に調整する",
    "相棒のフォローに頼る",
    "結果を受け入れる"
   ]
  }
 },
 {
  "id": "rp-14",
  "source": {
   "prompt": "休息日最不可能做的事是？",
   "options": [
    "睡到中午",
    "主动加练",
    "整理房间",
    "看比赛录像"
   ]
  },
  "zh-Hant": {
   "prompt": "休息日，你最不可能做甚麼？",
   "options": [
    "睡到中午",
    "主動加練",
    "整理房間",
    "看比賽影片"
   ]
  },
  "ja": {
   "prompt": "休日に一番しそうにないことは？",
   "options": [
    "昼まで寝る",
    "自分から追加練習する",
    "部屋を片づける",
    "レース映像を見る"
   ]
  }
 }
] as const;
export function localizeRapportQuestion(question:RapportQuestion,locale=i18n.language):RapportQuestion & {original:boolean} {
 const item=rapportQuestions.find(q=>q.id===question.questionId&&q.source.prompt===question.prompt&&q.source.options.length===question.options.length&&q.source.options.every((label,i)=>question.options[i]?.optionId===`${q.id}-o${i+1}`&&question.options[i]?.label===label));
 if(!item)return {...question,original:locale!=="zh-Hans"};
 if(locale!=="zh-Hant"&&locale!=="ja")return {...question,original:false};
 const translated=item[locale];
 const options=question.options.map((o,i)=>({...o,label:translated.options[i]??o.label}));
 // Even a malformed historic reveal cannot pair an arbitrary label with an ID.
 const index=question.options.findIndex(o=>o.optionId===question.reveal?.chosenOptionId&&o.label===question.reveal?.chosenLabel);
 return {...question,prompt:translated.prompt,options,original:false,reveal:question.reveal?{...question.reveal,chosenLabel:index>=0?options[index]!.label:question.reveal.chosenLabel}:undefined};
}
