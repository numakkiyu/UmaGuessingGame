import i18n, { matchLanguage, type Language } from ".";
// Display-only names: regional race catalogue plus historical result records.
// Preserve historical names; never identify or grade races with a translated label.
export const raceNameAliases = {
  "东京优骏": [
    "東京優駿（日本打比）",
    "東京優駿（日本ダービー）"
  ],
  "东京大赏典": [
    "東京大賞典",
    "東京大賞典"
  ],
  "中山纪念": [
    "中山紀念",
    "中山記念"
  ],
  "中日报杯": [
    "中日新聞盃",
    "中日新聞杯"
  ],
  "主角锦标": [
    "主角錦標賽",
    "プリンシパルステークス"
  ],
  "二月锦标": [
    "二月錦標賽",
    "フェブラリーステークス"
  ],
  "产经体育4岁雌马特别赛": [
    "產經體育賞四歲雌馬特別賽",
    "サンケイスポーツ賞4歳牝馬特別"
  ],
  "产经赏All Comers": [
    "產經賞",
    "産経賞オールカマー"
  ],
  "京成杯": [
    "京成盃",
    "京成杯"
  ],
  "京王杯春季杯": [
    "京王盃春季盃",
    "京王杯スプリングカップ"
  ],
  "京都大赏典": [
    "京都大賞典",
    "京都大賞典"
  ],
  "京都报杯": [
    "京都新聞盃",
    "京都新聞杯"
  ],
  "伊丽莎白二世女王杯": [
    "富衛保險女皇盃",
    "クイーンエリザベス2世カップ"
  ],
  "伊丽莎白二世女皇杯": [
    "伊麗莎白女王盃",
    "エリザベス女王杯"
  ],
  "优骏牝马": [
    "優駿牝馬（奧克斯）",
    "優駿牝馬（オークス）"
  ],
  "天皇赏(春)": [
    "天皇賞（春）",
    "天皇賞（春）"
  ],
  "天皇赏(秋)": [
    "天皇賞（秋）",
    "天皇賞（秋）"
  ],
  "天鹅锦标": [
    "天鵝錦標賽",
    "スワンステークス"
  ],
  "安田纪念": [
    "安田紀念",
    "安田記念"
  ],
  "宝冢纪念": [
    "寶塚紀念",
    "宝塚記念"
  ],
  "富士TV春季锦标": [
    "富士電視台賞春季錦標賽",
    "フジテレビ賞スプリングステークス"
  ],
  "小仓纪念": [
    "小倉紀念",
    "小倉記念"
  ],
  "德比伯爵挑战者杯": [
    "打比伯爵挑戰盃",
    "ダービー卿チャレンジトロフィー"
  ],
  "报知杯弥生赏": [
    "報知盃彌生賞",
    "報知杯弥生賞"
  ],
  "新潟3岁锦标": [
    "新潟三歲錦標賽",
    "新潟3歳ステークス"
  ],
  "日刊スポーツ賞金杯": [
    "日刊體育賞金盃",
    "日刊スポーツ賞金杯"
  ],
  "日本杯": [
    "日本盃",
    "ジャパンカップ"
  ],
  "日本泥地杯": [
    "日本盃泥地大賽",
    "ジャパンカップダート"
  ],
  "日经赏": [
    "日經賞",
    "日経賞"
  ],
  "有马纪念": [
    "有馬紀念",
    "有馬記念"
  ],
  "朝日杯三岁锦标": [
    "朝日盃三歲錦標賽",
    "朝日杯3歳ステークス"
  ],
  "樱花赏": [
    "櫻花賞",
    "桜花賞"
  ],
  "每日放送天鹅锦标": [
    "每日放送賞天鵝錦標賽",
    "毎日放送賞スワンステークス"
  ],
  "每日杯": [
    "每日盃",
    "毎日杯"
  ],
  "每日杯3岁锦标": [
    "每日盃三歲錦標賽",
    "デイリー杯3歳ステークス"
  ],
  "每日王冠": [
    "每日王冠",
    "毎日王冠"
  ],
  "産經大阪杯": [
    "產經大阪盃",
    "産経大阪杯"
  ],
  "皋月赏": [
    "皋月賞",
    "皐月賞"
  ],
  "目黑纪念": [
    "目黑紀念",
    "目黒記念"
  ],
  "短波杯三岁锦标": [
    "短波電台盃三歲錦標賽",
    "ラジオたんぱ杯3歳ステークス"
  ],
  "短途马锦标": [
    "短途跑者錦標賽",
    "スプリンターズステークス"
  ],
  "神户报杯": [
    "神戶新聞盃",
    "神戸新聞杯"
  ],
  "维多利亚英里赛": [
    "維多利亞一哩賽",
    "ヴィクトリアマイル"
  ],
  "胡枝子锦标": [
    "萩錦標賽",
    "萩ステークス"
  ],
  "若驹锦标": [
    "幼駒錦標賽",
    "若駒ステークス"
  ],
  "英里冠军杯": [
    "一哩冠軍賽",
    "マイルチャンピオンシップ"
  ],
  "菊花赏": [
    "菊花賞",
    "菊花賞"
  ],
  "迪拜世界杯": [
    "杜拜世界盃",
    "ドバイワールドカップ"
  ],
  "郁金香赏": [
    "鬱金香賞",
    "チューリップ賞"
  ],
  "金鯱赏": [
    "金鯱賞",
    "金鯱賞"
  ],
  "阪神2岁雌马大赛": [
    "阪神兩歲雌馬賽（阪神 JF）",
    "阪神ジュベナイルフィリーズ"
  ],
  "阪神大赏典": [
    "阪神大賞典",
    "阪神大賞典"
  ],
  "高松宫杯": [
    "高松宮盃",
    "高松宮杯"
  ],
  "高松宫纪念": [
    "高松宮紀念",
    "高松宮記念"
  ],
  "鸣尾纪念": [
    "鳴尾紀念",
    "鳴尾記念"
  ]
} as const;
// Ambiguous titles and old names need the reviewed race ID and date together.
const historicalRaceContexts: Record<string, Readonly<Record<string, string>>> = {
  "新潟3岁锦标": {
    "199606040811": "1996-09-01"
  },
  "富士TV春季锦标": {
    "199206030211": "1992-03-29"
  },
  "每日放送天鹅锦标": {
    "200708040811": "2007-10-27"
  },
  "报知杯弥生赏": {
    "199806020411": "1998-03-08",
    "200106020411": "2001-03-04",
    "199006020411": "1990-03-04",
    "199906020411": "1999-03-07",
    "199306020411": "1993-03-07"
  },
  "短波杯三岁锦标": {
    "200009050711": "2000-12-23"
  },
  "朝日杯三岁锦标": {
    "199106050411": "1991-12-08"
  },
  "每日杯3岁锦标": {
    "199608040511": "1996-10-19",
    "198908050311": "1989-11-11"
  },
  "伊丽莎白二世女王杯": {
    "2021H1042508": "2021-04-25"
  },
  "日本泥地杯": {
    "201209050211": "2012-12-02"
  },
  "产经体育4岁雌马特别赛": {
    "199005020411": "1990-04-29"
  },
  "伊丽莎白二世女皇杯": {
    "200708050411": "2007-11-11",
    "200608060411": "2006-11-12"
  },
  "産經大阪杯": {
    "199209020411": "1992-04-05",
    "199309020411": "1993-04-04",
    "198909020411": "1989-04-02"
  },
  "高松宫杯": {
    "199607020211": "1996-05-19",
    "199308040811": "1993-07-11",
    "198907020811": "1989-07-09"
  },
  "产经赏All Comers": {
    "199306040411": "1993-09-19"
  },
  "日刊スポーツ賞金杯": {
    "199306010111": "1993-01-05"
  }
};
export type RaceNameContext = { raceId: string; date: string };
export const courseAliases = { "东京":["東京","東京"],"中山":["中山","中山"],"阪神":["阪神","阪神"],"京都":["京都","京都"] } as const;
function displayAlias(table: Record<string, readonly [string,string]>, value:string, language?:string):string {
  const lang=matchLanguage(language??i18n.resolvedLanguage??"")??"zh-Hans";
  return lang!=="zh-Hans"&&Object.hasOwn(table,value)?table[value]![lang==="ja"?1:0]:value;
}
export function raceName(value: string, language?: string, context?: RaceNameContext): string {
  if (Object.hasOwn(historicalRaceContexts, value)) {
    const dates = historicalRaceContexts[value]!;
    if (!context || !Object.hasOwn(dates, context.raceId) || dates[context.raceId] !== context.date) return value;
  }
  return displayAlias(raceNameAliases, value, language);
}
export const raceCourse=(value:string,language?:string)=>displayAlias(courseAliases,value,language);
export function raceDate(value:string,language?:string):string {
  const lang:Language=matchLanguage(language??i18n.resolvedLanguage??"")??"zh-Hans";
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return value;
  const date=new Date(`${value}T00:00:00Z`);
  if(!Number.isFinite(date.getTime()) || date.toISOString().slice(0,10)!==value)return value;
  return new Intl.DateTimeFormat({"zh-Hans":"zh-CN","zh-Hant":"zh-TW",ja:"ja-JP"}[lang],{dateStyle:"long",timeZone:"UTC"}).format(date);
}
