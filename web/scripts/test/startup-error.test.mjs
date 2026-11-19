import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { JSDOM } from "jsdom";
const script=readFileSync(new URL("../../public/startup-error.js",import.meta.url),"utf8");
const gate=readFileSync(new URL("../../public/browser-support.js",import.meta.url),"utf8");
function page(language="zh-CN",saved){
  const dom=new JSDOM('<html><head></head><body><div id="root"><button>old page</button></div></body></html>',{url:"https://game.example/matchmaking",runScripts:"outside-only"});
  Object.defineProperty(dom.window.navigator,"languages",{value:[language]});
  if(saved)dom.window.localStorage.setItem("umaguess:language",saved);
  return dom;
}
for(const[language,title]of [["zh-CN","页面还没准备好"],["zh-TW","頁面還沒準備好"],["ja-JP","画面を読み込めませんでした"]])test(`startup recovery works without React or application CSS: ${language}`,()=>{
 const dom=page(language);dom.window.eval(script);const doc=dom.window.document;
 assert.equal(doc.querySelector('[role="alertdialog"] h1').textContent,title);
 assert(doc.querySelector("style").textContent.includes("position:fixed"));
 assert.equal(doc.querySelector("#root").getAttribute("aria-hidden"),"true");
 const retry=doc.querySelector("#startup-fault button"),home=doc.querySelector("#startup-fault a");
 assert.equal(doc.activeElement,retry);retry.dispatchEvent(new dom.window.KeyboardEvent("keydown",{key:"Tab",bubbles:true,cancelable:true}));assert.equal(doc.activeElement,home);
 dom.window.eval(script);assert.equal(doc.querySelectorAll('[role="alertdialog"]').length,1);
 for(const img of doc.querySelectorAll("#startup-fault img"))img.dispatchEvent(new dom.window.Event("error"));assert(doc.querySelector("#startup-fault button"));
 dom.window.close();
});
test("explicit language preference wins over the browser; unavailable storage remains usable",()=>{
 const saved=page("ja-JP","zh-Hans");saved.window.eval(script);assert.equal(saved.window.document.querySelector("#startup-fault").lang,"zh-Hans");saved.window.close();
 const denied=page("zh-HK");Object.defineProperty(denied.window,"localStorage",{get(){throw Error("denied")}});denied.window.eval(script);assert.equal(denied.window.document.querySelector("#startup-fault").lang,"zh-Hant");denied.window.close();
});
test("module and stylesheet failures request a single independent recovery UI",()=>{
 const dom=page();const {window}=dom;Object.defineProperty(window.navigator,"userAgent",{value:"Chrome/157.0.0.0"});
 Object.assign(window,{fetch(){},TextEncoder,structuredClone,CSS:{supports:()=>true}});
 const entry=window.document.createElement("script");entry.setAttribute("data-game-entry","/assets/game.js");entry.type="application/json";
 const style=window.document.createElement("link");style.setAttribute("data-game-style","/assets/game.css");window.document.head.append(entry,style);
 window.eval(gate);const module=window.document.querySelector('script[type="module"]');assert(module);
 module.dispatchEvent(new window.Event("error"));style.dispatchEvent(new window.Event("error"));
 assert.equal(window.document.querySelectorAll('script[src="/startup-error.js"]').length,1);dom.window.close();
});
