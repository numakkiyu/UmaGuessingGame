import i18n, {matchLanguage} from ".";
import type {TradeObjective} from "@/lib/api/trade";
export const tradeCards = [
  {
    "id": "speed",
    "source": "速度档案",
    "zh-Hant": "速度檔案",
    "ja": "スピード資料"
  },
  {
    "id": "stamina",
    "source": "耐力档案",
    "zh-Hant": "耐力檔案",
    "ja": "スタミナ資料"
  },
  {
    "id": "power",
    "source": "力量档案",
    "zh-Hant": "力量檔案",
    "ja": "パワー資料"
  },
  {
    "id": "wit",
    "source": "智慧档案",
    "zh-Hant": "智慧檔案",
    "ja": "賢さ資料"
  },
  {
    "id": "charm",
    "source": "魅力档案",
    "zh-Hant": "魅力檔案",
    "ja": "魅力資料"
  },
  {
    "id": "grit",
    "source": "毅力档案",
    "zh-Hant": "毅力檔案",
    "ja": "意志資料"
  },
  {
    "id": "grace",
    "source": "仪态档案",
    "zh-Hant": "儀態檔案",
    "ja": "身だしなみ資料"
  },
  {
    "id": "insight",
    "source": "洞察档案",
    "zh-Hant": "洞察檔案",
    "ja": "洞察資料"
  },
  {
    "id": "rhythm",
    "source": "节奏档案",
    "zh-Hant": "節奏檔案",
    "ja": "リズム資料"
  },
  {
    "id": "bond",
    "source": "羁绊档案",
    "zh-Hant": "羈絆檔案",
    "ja": "絆資料"
  }
] as const;
export const tradeObjectives = [
  {
    "id": "ob-01",
    "source": "最终持有速度与耐力档案各一张",
    "kinds": [
      "speed",
      "stamina"
    ],
    "points": 50,
    "zh-Hant": "結束時持有速度檔案與耐力檔案各一張",
    "ja": "終了時に「スピード資料」と「スタミナ資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-02",
    "source": "最终持有力量与耐力档案各一张",
    "kinds": [
      "power",
      "stamina"
    ],
    "points": 50,
    "zh-Hant": "結束時持有力量檔案與耐力檔案各一張",
    "ja": "終了時に「パワー資料」と「スタミナ資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-03",
    "source": "最终持有力量与智慧档案各一张",
    "kinds": [
      "power",
      "wit"
    ],
    "points": 50,
    "zh-Hant": "結束時持有力量檔案與智慧檔案各一張",
    "ja": "終了時に「パワー資料」と「賢さ資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-04",
    "source": "最终持有魅力与智慧档案各一张",
    "kinds": [
      "charm",
      "wit"
    ],
    "points": 50,
    "zh-Hant": "結束時持有魅力檔案與智慧檔案各一張",
    "ja": "終了時に「魅力資料」と「賢さ資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-05",
    "source": "最终持有魅力与毅力档案各一张",
    "kinds": [
      "charm",
      "grit"
    ],
    "points": 50,
    "zh-Hant": "結束時持有魅力檔案與毅力檔案各一張",
    "ja": "終了時に「魅力資料」と「意志資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-06",
    "source": "最终持有仪态与毅力档案各一张",
    "kinds": [
      "grace",
      "grit"
    ],
    "points": 50,
    "zh-Hant": "結束時持有儀態檔案與毅力檔案各一張",
    "ja": "終了時に「身だしなみ資料」と「意志資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-07",
    "source": "最终持有仪态与洞察档案各一张",
    "kinds": [
      "grace",
      "insight"
    ],
    "points": 50,
    "zh-Hant": "結束時持有儀態檔案與洞察檔案各一張",
    "ja": "終了時に「身だしなみ資料」と「洞察資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-08",
    "source": "最终持有洞察与节奏档案各一张",
    "kinds": [
      "insight",
      "rhythm"
    ],
    "points": 50,
    "zh-Hant": "結束時持有洞察檔案與節奏檔案各一張",
    "ja": "終了時に「洞察資料」と「リズム資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-09",
    "source": "最终持有羁绊与节奏档案各一张",
    "kinds": [
      "bond",
      "rhythm"
    ],
    "points": 50,
    "zh-Hant": "結束時持有羈絆檔案與節奏檔案各一張",
    "ja": "終了時に「絆資料」と「リズム資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-10",
    "source": "最终持有羁绊与速度档案各一张",
    "kinds": [
      "bond",
      "speed"
    ],
    "points": 50,
    "zh-Hant": "結束時持有羈絆檔案與速度檔案各一張",
    "ja": "終了時に「絆資料」と「スピード資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-11",
    "source": "最终持有力量与速度档案各一张",
    "kinds": [
      "power",
      "speed"
    ],
    "points": 50,
    "zh-Hant": "結束時持有力量檔案與速度檔案各一張",
    "ja": "終了時に「パワー資料」と「スピード資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-12",
    "source": "最终持有耐力与智慧档案各一张",
    "kinds": [
      "stamina",
      "wit"
    ],
    "points": 50,
    "zh-Hant": "結束時持有耐力檔案與智慧檔案各一張",
    "ja": "終了時に「スタミナ資料」と「賢さ資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-13",
    "source": "最终持有魅力与力量档案各一张",
    "kinds": [
      "charm",
      "power"
    ],
    "points": 50,
    "zh-Hant": "結束時持有魅力檔案與力量檔案各一張",
    "ja": "終了時に「魅力資料」と「パワー資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-14",
    "source": "最终持有毅力与智慧档案各一张",
    "kinds": [
      "grit",
      "wit"
    ],
    "points": 50,
    "zh-Hant": "結束時持有毅力檔案與智慧檔案各一張",
    "ja": "終了時に「意志資料」と「賢さ資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-15",
    "source": "最终持有魅力与仪态档案各一张",
    "kinds": [
      "charm",
      "grace"
    ],
    "points": 50,
    "zh-Hant": "結束時持有魅力檔案與儀態檔案各一張",
    "ja": "終了時に「魅力資料」と「身だしなみ資料」を一枚ずつ持つ"
  },
  {
    "id": "ob-16",
    "source": "最终持有毅力与洞察档案各一张",
    "kinds": [
      "grit",
      "insight"
    ],
    "points": 50,
    "zh-Hant": "結束時持有毅力檔案與洞察檔案各一張",
    "ja": "終了時に「意志資料」と「洞察資料」を一枚ずつ持つ"
  }
] as const;
export function tradeCardLabel(id:string, source:string, language=i18n.language):{text:string;original:boolean} {
 const locale=matchLanguage(language)??"zh-Hans", known=tradeCards.find(c=>c.id===id&&c.source===source);
 return {text:known&&locale!=="zh-Hans"?known[locale]:source,original:!known&&locale!=="zh-Hans"};
}
export function tradeObjectiveText(objective:TradeObjective,language=i18n.language):{text:string;original:boolean} {
 const locale=matchLanguage(language)??"zh-Hans",known=tradeObjectives.find(o=>o.id===objective.objectiveId&&o.source===objective.text&&o.points===objective.points&&o.kinds.length===objective.kinds.length&&o.kinds.every((k,i)=>k===objective.kinds[i]));
 return {text:known&&locale!=="zh-Hans"?known[locale]:objective.text,original:!known&&locale!=="zh-Hans"};
}
