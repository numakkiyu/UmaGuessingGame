(function () {
  "use strict";
  if (document.getElementById("startup-fault")) return;
  var language = "zh-Hans", hasPreference = false;
  try { var saved = window.localStorage.getItem("umaguess:language"); if (/^(zh-Hans|zh-Hant|ja)$/.test(saved)) { language = saved; hasPreference = true; } } catch (error) { /* Browser preference remains available. */ }
  var preferred = navigator.languages || [navigator.language];
  if (!hasPreference) for (var i = 0; i < preferred.length; i++) {
    var tag = String(preferred[i] || "").toLowerCase();
    if (/^ja/.test(tag)) { language = "ja"; break; }
    if (/^zh/.test(tag)) { language = /hant|tw|hk|mo/.test(tag) && !/hans/.test(tag) ? "zh-Hant" : "zh-Hans"; break; }
  }
  var messages = {
    "zh-Hans": ["遇到一点小状况", "页面还没准备好", "有些内容暂时没能加载，请重新打开页面再试一次", "重新加载", "返回游玩大厅"],
    "zh-Hant": ["遇到一點小狀況", "頁面還沒準備好", "部分內容暫時無法載入，請重新開啟頁面再試一次", "重新載入", "返回遊戲大廳"],
    "ja": ["うまくいかなかったようです", "画面を読み込めませんでした", "一部の内容を読み込めませんでした。もう一度ページを開いてみてください", "再読み込み", "ロビーへ戻る"]
  };
  var text = messages[language];
  var style = document.createElement("style");
  // Keep the recovery panel independent of application styles and chunks.
  style.textContent = "#startup-fault{position:fixed;inset:0;z-index:1000;overflow:auto;background:#edf3edeb;display:flex;align-items:safe center;justify-content:center;padding:190px 24px 80px;box-sizing:border-box;font-family:system-ui,sans-serif;color:#776451}#startup-fault *{box-sizing:border-box}#startup-fault .fault-scene{position:relative;isolation:isolate;width:100%;max-width:580px;flex-shrink:0}#startup-fault .fault-panel{position:relative;z-index:2;border:1px solid #e2a17f;border-top:7px solid #df7650;border-radius:23px;background:#fffdf8;box-shadow:0 5px 0 #e9bca0,0 28px 85px #203a252e;overflow:hidden}#startup-fault .fault-copy{padding:24px}#startup-fault small{color:#ac6c47;font-size:13px}#startup-fault h1{margin:12px 0;font-size:23px;color:#8d3c2b;line-height:1.5;overflow-wrap:anywhere}#startup-fault p{margin:0;font-size:14px;line-height:1.85;overflow-wrap:anywhere}#startup-fault .fault-actions{min-height:130px;padding:18px 22px 22px 140px;background:#fff5e7;border-top:1px solid #f2dec7;display:flex;flex-wrap:wrap;justify-content:flex-end;align-items:center;gap:10px}#startup-fault button,#startup-fault a{display:inline-flex;align-items:center;justify-content:center;padding:12px 16px;min-height:44px;border:1px solid #ad422c;border-radius:12px;background:#cf593d;color:#fff;text-decoration:none;font:700 14px/1.6 system-ui,sans-serif;cursor:pointer;overflow-wrap:anywhere}#startup-fault a{background:#fffdf8;border-color:#e8cdb6;color:#876047}#startup-fault :focus-visible{outline:3px solid #3977b2;outline-offset:3px}#startup-fault img{position:absolute;object-fit:contain;pointer-events:none}#startup-fault .fault-back,#startup-fault .fault-hands{width:168px;height:200px;top:-175px;right:30px;object-position:center bottom;z-index:1}#startup-fault .fault-hands{z-index:3;clip-path:inset(87.5% 0 0 0)}#startup-fault .fault-front{width:162px;height:190px;bottom:-64px;left:-30px;z-index:4} @media(max-width:767px){#startup-fault{padding:160px 18px 70px}#startup-fault h1{font-size:19px}#startup-fault .fault-copy{padding:20px 16px}#startup-fault .fault-actions{padding:16px 12px 18px 100px;min-height:148px;flex-direction:column;align-items:stretch}#startup-fault .fault-back,#startup-fault .fault-hands{width:132px;height:158px;top:-138.25px;right:28px}#startup-fault .fault-front{width:110px;height:140px;bottom:-30px;left:-15px}}";
  document.head.appendChild(style);
  var root = document.createElement("div");root.id = "startup-fault";root.lang = language;root.setAttribute("role", "alertdialog");root.setAttribute("aria-modal", "true");root.setAttribute("aria-labelledby", "startup-fault-title");root.setAttribute("aria-describedby", "startup-fault-detail");
  var scene = document.createElement("div");scene.className = "fault-scene";
  var pairs = [["nicenature", "ikunodictus"], ["twinturbo", "matikanetannhauser"], ["mejiropalmer", "daitakuhelios"], ["rice", "mcqueen"], ["specialweek", "teio"]];
  var pair = pairs[Math.floor(Math.random() * pairs.length)];
  function art(name, position) { var img = document.createElement("img");img.src = "/assets/ui/errors/" + name + ".webp";img.alt = "";img.className = position;img.onerror = function () { img.style.display = "none"; };scene.appendChild(img); }
  art(pair[1], "fault-back");
  var panel = document.createElement("section");panel.className = "fault-panel";
  var copy = document.createElement("div");copy.className = "fault-copy";
  var eyebrow = document.createElement("small");eyebrow.textContent = text[0];copy.appendChild(eyebrow);
  var title = document.createElement("h1");title.id = "startup-fault-title";title.textContent = text[1];copy.appendChild(title);
  var detail = document.createElement("p");detail.id = "startup-fault-detail";detail.textContent = text[2];copy.appendChild(detail);panel.appendChild(copy);
  var actions = document.createElement("div");actions.className = "fault-actions";
  var retry = document.createElement("button");retry.type = "button";retry.textContent = text[3];retry.onclick = function () { retry.disabled = true; window.location.reload(); };actions.appendChild(retry);
  var home = document.createElement("a");home.href = "/";home.textContent = text[4];actions.appendChild(home);panel.appendChild(actions);scene.appendChild(panel);art(pair[1], "fault-hands");art(pair[0], "fault-front");root.appendChild(scene);
  for (var c = 0; c < document.body.children.length; c++) { var child = document.body.children[c];if (child.tagName !== "SCRIPT") { child.setAttribute("inert", "");child.setAttribute("aria-hidden", "true"); } }
  document.body.appendChild(root);retry.focus();root.onkeydown = function (event) { if (event.key === "Tab") { event.preventDefault();(document.activeElement === retry ? home : retry).focus(); } };
}());
