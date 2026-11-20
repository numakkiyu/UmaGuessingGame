import { concentrationEntry } from "./concentration-entry";
import { registerMessages } from ".";

export const concentrationCopy = {
  "zh-Hans": {
    ...concentrationEntry["zh-Hans"],
    rating: "评级", skipReveal: "跳过动画", correct: "答对", noPoints: "未得分",
    baseTitle: "角色名称", bonusTitle: "加分项", optional: "选填", month: "生日月份", day: "生日日期", monthUnit: "月", dayUnit: "日", intro: "随机抽取 20 位角色，看立绘填写名字、声优和生日等资料",
    start: "开始测试", resume: "继续作答", results: "查看结果", again: "再测一次", home: "返回单人游玩",
    pool: "{{count}} 位角色", format: "20 题 · 满分 400 分",
    nickname: "玩家",
    rulesTitle: "玩法与计分", rules: [
      "每局随机抽取 20 位角色，每题 20 分，满分 400 分。没有时间限制，交卷前可返回修改",
      "角色名可填收录的中文译名或日文名。有多种中文译名时，角色名与其他中文译名各 6 分，两栏填写不同名称；只有一种译名时，填对角色名得 12 分",
      "日文名 4 分、英文名 2 分、声优 1 分、生日与身高各 0.5 分。生日分月、日填写，身高单位为厘米。声优按官网现任名单，可填收录的中日文姓名，艺名以官网为准",
      "浓度为角色识别率、档案正确率与总分比例的平均值。档案正确率只计算角色名之外的资料得分。95% 起为 SSS，90% 起为 SS，80% 起为 S，60% 起为 A，40% 起为 B，30% 起为 C，其余为 D",
      "作答随填写保存，刷新或切换页面后可以继续。清理浏览器数据会删除记录"
    ],
    portrait: "角色全身立绘", done: "已填写 {{count}} / {{total}} 题", elapsed: "用时", previous: "上一题", next: "下一题", submit: "交卷看结果", confirm: "确认交卷？共 {{total}} 题，其中 {{blank}} 题留空。交卷后将揭晓答案", nameHint: "中文译名或日文名", translationHint: "其他译名，没有可留空", draft: "作答自动保存", storageError: "作答暂时无法保存，请保持本页打开，完成后可下载成绩单", restoreError: "上次的记录无法读取，可以重新开始", imageError: "立绘暂时未能加载，可以重试或稍后返回本题", retry: "重新加载立绘", finalTitle: "你的浓度测试结果", certificate: "马娘浓度成绩单", concentration: "马娘浓度", accuracy: "角色识别率", completeness: "档案正确率", relevance: "总分比例", score: "得分", review: "逐题对照", reviewHint: "展开题目，查看你的作答、参考答案和得分", yourAnswer: "你的作答", correctAnswer: "参考答案", noAnswer: "留空", source: "查看资料来源", official: "赛马娘官方网站", wiki: "哔哩哔哩游戏百科", moegirl: "萌娘百科", download: "下载成绩单", downloaded: "成绩单已准备下载", replayConfirm: "重新开始将替换当前保存的作答。要继续吗？", fields: { name: "角色名", translation: "其他中文译名", japanese: "日文名", english: "英文名", cv: "声优", birthday: "生日", height: "身高" },
  },
  "zh-Hant": {
    ...concentrationEntry["zh-Hant"],
    rating: "評級", skipReveal: "略過動畫", correct: "答對", noPoints: "未得分",
    baseTitle: "角色名稱", bonusTitle: "加分項", optional: "選填", month: "生日月份", day: "生日日期", monthUnit: "月", dayUnit: "日", intro: "隨機抽出 20 位角色，看立繪填寫名字、聲優和生日等資料",
    start: "開始測試", resume: "繼續作答", results: "查看結果", again: "再測一次", home: "返回單人遊玩",
    pool: "{{count}} 位角色", format: "20 題 · 滿分 400 分",
    nickname: "玩家",
    rulesTitle: "玩法與計分", rules: [
      "每局隨機抽出 20 位角色，每題 20 分，滿分 400 分。沒有時間限制，交卷前可返回修改",
      "角色名稱可填收錄的中文譯名或日文名稱。有多種中文譯名時，角色名稱與其他中文譯名各 6 分，兩欄填寫不同名稱；只有一種譯名時，答對角色名稱得 12 分",
      "日文名稱 4 分、英文名稱 2 分、聲優 1 分、生日與身高各 0.5 分。生日分月、日填寫，身高單位為公分。聲優依官網現任名單，可填收錄的中日文姓名，藝名以官網為準",
      "濃度為角色辨識率、資料正確率與總分比例的平均值。資料正確率只計算角色名稱以外的資料得分。95% 起為 SSS，90% 起為 SS，80% 起為 S，60% 起為 A，40% 起為 B，30% 起為 C，其餘為 D",
      "填寫時自動儲存，重新整理或切換頁面後可繼續。清除瀏覽器資料會刪除紀錄"
    ],
    portrait: "角色全身立繪", done: "已填寫 {{count}} / {{total}} 題", elapsed: "作答時間", previous: "上一題", next: "下一題", submit: "交卷看結果", confirm: "確定交卷？共 {{total}} 題，其中 {{blank}} 題留白。交卷後會公布答案", nameHint: "中文譯名或日文名稱", translationHint: "其他譯名，沒有可留白", draft: "作答自動儲存", storageError: "目前無法儲存作答，請保持本頁開啟，完成後可下載成績單", restoreError: "無法讀取上次的紀錄，可以重新開始", imageError: "立繪暫時無法載入，可以重試或稍後返回本題", retry: "重新載入立繪", finalTitle: "你的濃度測試結果", certificate: "賽馬娘濃度成績單", concentration: "賽馬娘濃度", accuracy: "角色辨識率", completeness: "資料正確率", relevance: "總分比例", score: "得分", review: "逐題對照", reviewHint: "展開題目，查看你的作答、參考答案與得分", yourAnswer: "你的作答", correctAnswer: "參考答案", noAnswer: "留白", source: "查看資料來源", official: "賽馬娘官方網站", wiki: "嗶哩嗶哩遊戲百科", moegirl: "萌娘百科", download: "下載成績單", downloaded: "成績單已準備下載", replayConfirm: "重新開始會取代目前儲存的作答，要繼續嗎？", fields: { name: "角色名稱", translation: "其他中文譯名", japanese: "日文名稱", english: "英文名稱", cv: "聲優", birthday: "生日", height: "身高" },
  },
  ja: {
    ...concentrationEntry["ja"],
    rating: "評価", skipReveal: "演出をスキップ", correct: "正解", noPoints: "得点なし",
    baseTitle: "名前", bonusTitle: "プロフィール", optional: "任意", month: "誕生月", day: "誕生日の日", monthUnit: "月", dayUnit: "日", intro: "ランダムに選ばれた 20 人の全身イラストを見て、名前や声優、誕生日を答えよう",
    start: "テストを始める", resume: "回答を続ける", results: "結果を見る", again: "もう一度挑戦", home: "ひとりで遊ぶへ戻る",
    pool: "{{count}} 人を収録", format: "20 問 · 400 点満点",
    nickname: "プレイヤー",
    rulesTitle: "遊び方と採点", rules: [
      "毎回ランダムに選ばれた 20 人を出題します。1 問 20 点、400 点満点。時間制限はなく、提出前なら前の問題に戻って修正できます",
      "名前には収録済みの中国語訳名か日本語名を書けます。中国語訳名が複数ある場合は、名前と別訳名で各 6 点。2 つの欄には異なる名前を書いてください。訳名が 1 種類の場合、名前の正解だけで 12 点です",
      "日本語名 4 点、英語名 2 点、声優 1 点、誕生日と身長は各 0.5 点。誕生日は月と日を分け、身長はセンチメートルで入力します。声優は公式サイトの現行キャストが対象で、収録済みの中国語・日本語表記に対応。芸名は公式サイトの表記に合わせてください",
      "知識度は名前の正答率、プロフィール正答率、総得点率の平均です。プロフィール正答率は名前以外の項目の得点から計算します。95% 以上は SSS、90% 以上は SS、80% 以上は S、60% 以上は A、40% 以上は B、30% 以上は C、それ未満は D です",
      "回答は入力中に自動保存され、再読み込みやページ移動後も続けられます。ブラウザーのデータを消去すると記録も消えます"
    ],
    portrait: "ウマ娘の全身イラスト", done: "{{total}} 問中 {{count}} 問を記入", elapsed: "経過時間", previous: "前の問題", next: "次の問題", submit: "提出して結果を見る", confirm: "回答を提出しますか？全 {{total}} 問のうち {{blank}} 問が空欄です。提出後に正解を表示します", nameHint: "日本語名または中国語訳名", translationHint: "別の中国語訳名（なければ空欄）", draft: "回答を自動保存", storageError: "回答を保存できません。このページを開いたまま続けてください。終了後に成績をダウンロードできます", restoreError: "前回の記録を読み込めません。新しく始められます", imageError: "画像を読み込めません。再試行するか、後でこの問題に戻ってください", retry: "画像を再読み込み", finalTitle: "知識テストの結果", certificate: "ウマ娘知識テスト 成績表", concentration: "ウマ娘知識度", accuracy: "名前の正答率", completeness: "プロフィール正答率", relevance: "総得点率", score: "得点", review: "答え合わせ", reviewHint: "問題を開くと、自分の回答、正解、得点を確認できます", yourAnswer: "自分の回答", correctAnswer: "正解", noAnswer: "空欄", source: "出典を確認", official: "ウマ娘公式サイト", wiki: "bilibili ゲーム百科", moegirl: "萌娘百科", download: "成績表をダウンロード", downloaded: "成績表のダウンロードを準備しました", replayConfirm: "新しく始めると、保存中の回答を置き換えます。続けますか？", fields: { name: "名前", translation: "別の中国語訳名", japanese: "日本語名", english: "英語名", cv: "声優", birthday: "誕生日", height: "身長" },
  },
} as const;
registerMessages("concentration", concentrationCopy);
