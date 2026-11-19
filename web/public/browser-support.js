(function () {
  "use strict";

  // Keep this entry in ES5 so unsupported browsers can still show the notice.
  var ua = navigator.userAgent || "";
  var appleMobile = /iPad|iPhone|iPod/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1);
  var match;
  var supported = true;
  if (/MSIE|Trident\//.test(ua) || /Edge\//.test(ua)) {
    supported = false;
  } else if (appleMobile) {
    match = /OS (\d+)[_.](\d+)/.exec(ua) || /Version\/(\d+)\.(\d+)/.exec(ua);
    supported = !!match && (Number(match[1]) > 17 || (Number(match[1]) === 17 && Number(match[2]) >= 4));
  } else if ((match = /Edg(?:A|iOS)?\/(\d+)/.exec(ua))) {
    supported = Number(match[1]) >= 111;
  } else if ((match = /(?:Chrome|Chromium)\/(\d+)/.exec(ua))) {
    supported = Number(match[1]) >= 111;
  } else if ((match = /Firefox\/(\d+)/.exec(ua))) {
    supported = Number(match[1]) >= 128;
  } else if (/Safari\//.test(ua) && (match = /Version\/(\d+)\.(\d+)/.exec(ua))) {
    supported = Number(match[1]) > 17 || (Number(match[1]) === 17 && Number(match[2]) >= 4);
  }

  // Storage permission and audio playback are checked in their own flows.
  supported = supported && !!(window.Promise && window.fetch && window.URL && window.AbortController
    && window.TextEncoder && window.structuredClone && Array.prototype.at && String.prototype.replaceAll
    && window.CSS && window.CSS.supports && window.CSS.supports("color", "oklch(50% 0.1 120)")
    && window.CSS.supports("color", "color-mix(in oklab, red, blue)"));
  window.__UMAGUESS_BROWSER_SUPPORTED__ = supported;
  if (supported) {
    var reported = false;
    function showLoadFailure() {
      if (reported) return;
      reported = true;
      var fallback = document.createElement("script");
      fallback.src = "/startup-error.js";
      document.body.appendChild(fallback);
    }
    var entry = document.querySelector("[data-game-entry]");
    var styles = document.querySelectorAll("[data-game-style]");
    for (var i = 0; i < styles.length; i++) {
      styles[i].onerror = showLoadFailure;
      styles[i].setAttribute("rel", "stylesheet");
      styles[i].setAttribute("href", styles[i].getAttribute("data-game-style"));
    }
    if (entry) {
      var game = document.createElement("script");
      game.onerror = showLoadFailure;
      game.type = "module";
      game.src = entry.getAttribute("data-game-entry");
      document.body.appendChild(game);
    }
    return;
  }
  var notice = document.createElement("script");
  notice.src = "/browser-notice.js";
  document.body.appendChild(notice);
}());
