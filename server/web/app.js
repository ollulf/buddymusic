import { renderUpload } from "./upload.js";

const root = document.getElementById("app");

function header() {
  const el = document.createElement("header");
  el.className = "top";
  const name = document.createElement("strong");
  name.textContent = "buddymusic";
  const tab = document.createElement("span");
  tab.className = "tab on";
  tab.textContent = "Upload";
  el.append(name, tab);
  return el;
}

function boot() {
  const main = document.createElement("main");
  root.replaceChildren(header(), main);
  renderUpload(main);
}

boot();
