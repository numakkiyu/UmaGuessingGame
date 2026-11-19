(function () {
  "use strict";
  document.body.className += " browser-unsupported";

  var messages = {
    "zh-Hans": {
      title: "更新浏览器，再来一起玩吧", detail: "当前浏览器暂不支持这场游戏，换用新版浏览器，就能继续游玩", versions: "请使用 Safari 17.4+、Firefox 128+、Chrome 111+ 或 Edge 111+，推荐更新到最新版", apple: "iPhone 和 iPad 请将系统更新至 17.4 或以上，再用 Safari 打开", ie: "不再支持 Internet Explorer 与旧版 Edge", refresh: "更新浏览器后再试", downloads: "获取新版浏览器", page: "浏览器更新提示"
    },
    "zh-Hant": {
      title: "更新瀏覽器，再一起玩吧", detail: "目前的瀏覽器暫不支援這款遊戲，換用新版瀏覽器，就能繼續遊玩", versions: "請使用 Safari 17.4+、Firefox 128+、Chrome 111+ 或 Edge 111+，建議更新至最新版", apple: "iPhone 與 iPad 請先將系統更新至 17.4 或以上，再以 Safari 開啟", ie: "不再支援 Internet Explorer 與舊版 Edge", refresh: "更新瀏覽器後再試", downloads: "下載新版瀏覽器", page: "瀏覽器更新提示"
    },
    ja: {
      title: "ブラウザーを更新して、また遊ぼう", detail: "お使いのブラウザーではこのゲームを遊べません。ブラウザーを更新してから、もう一度開いてください", versions: "Safari 17.4 以降、Firefox 128 以降、Chrome 111 以降、Edge 111 以降をご利用ください。最新版への更新をおすすめします", apple: "iPhone・iPad は OS を 17.4 以降に更新し、Safari で開いてください", ie: "Internet Explorer と旧版 Edge はサポートしていません", refresh: "ブラウザーの更新後に再読み込み", downloads: "新しいブラウザーを入手", page: "ブラウザー更新のご案内"
    }
  };
  function matchLanguage(tag) {
    tag = String(tag || "").replace(/_/g, "-").toLowerCase();
    if (/^ja(?:-|$)/.test(tag)) return "ja";
    if (!/^zh(?:-|$)/.test(tag)) return null;
    if (/(?:^|-)hans(?:-|$)/.test(tag)) return "zh-Hans";
    return /(?:^|-)(?:hant|tw|hk|mo)(?:-|$)/.test(tag) ? "zh-Hant" : "zh-Hans";
  }
  function browserLanguage() {
    var preferred = navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language || navigator.userLanguage];
    for (var i = 0; i < preferred.length; i++) {
      var language = matchLanguage(preferred[i]);
      if (language) return language;
    }
    try {
      var saved = window.localStorage.getItem("umaguess:language");
      if (saved === "zh-Hans" || saved === "zh-Hant" || saved === "ja") return saved;
    } catch (error) { /* The notice also works when preferences cannot be saved. */ }
    return "zh-Hans";
  }

  function render(nextLanguage) {
    var text = messages[nextLanguage];
    document.documentElement.lang = nextLanguage;
    document.title = text.page + " · UmaGuessingGame";
    var fields = ["title", "detail", "versions", "apple", "ie", "refresh", "downloads"];
    for (var j = 0; j < fields.length; j++) document.getElementById("browser-" + fields[j]).textContent = text[fields[j]];
    var buttons = document.querySelectorAll("[data-browser-language]");
    for (var k = 0; k < buttons.length; k++) buttons[k].setAttribute("aria-pressed", String(buttons[k].getAttribute("data-browser-language") === nextLanguage));
  }
  render(browserLanguage());
  if (window.addEventListener) window.addEventListener("languagechange", function () { render(browserLanguage()); });
  document.getElementById("browser-support").style.display = "block";
  var buttons = document.querySelectorAll("[data-browser-language]");
  for (var b = 0; b < buttons.length; b++) buttons[b].onclick = function () {
    var next = this.getAttribute("data-browser-language");
    render(next);
    try { window.localStorage.setItem("umaguess:language", next); } catch (error) { /* Keep the selected language for this page. */ }
  };
  document.getElementById("browser-refresh").onclick = function () { window.location.reload(); };
}());
