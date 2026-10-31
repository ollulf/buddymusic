import { t } from "./i18n.js";
import { h } from "./dom.js";

export const MAX_ARTISTS = 8;

export function artistEditor(list, room, label, track, changed = () => {}, edited = () => {}) {
  const items = h("div", { class: "artist-inputs" });
  const add = h("button", { type: "button", class: "ghost add-artist", onclick: () => {
    list.push("");
    render();
    changed();
    items.lastElementChild?.querySelector("input")?.focus();
  } }, t("upload.addArtist"));
  const box = h("div", { class: "field artists" }, h("span", {}, label), items, add);
  function render() {
    items.replaceChildren(...list.map((v, i) => h("div", { class: "artist-item" },
      h("input", { type: "text", value: v, maxlength: "300", placeholder: t("upload.optional"),
        "aria-label": t("upload.extraArtist", { n: i + 2 }),
        oninput: (e) => { list[i] = e.target.value; edited(); } }),
      h("button", { type: "button", class: "icon", title: t("upload.remove"),
        onclick: () => { list.splice(i, 1); render(); changed(); } }, "×"))));
    add.hidden = list.length >= room();
    box.classList.toggle("empty", list.length === 0);
    if (track) track([...items.querySelectorAll("input, button"), add]);
  }
  render();
  return { box, render };
}

export function artistNames(list) {
  const out = [];
  const seen = new Set();
  for (const raw of list) {
    const v = String(raw ?? "").trim();
    if (!v || seen.has(v.toLowerCase())) continue;
    seen.add(v.toLowerCase());
    out.push(v);
  }
  return out.slice(0, MAX_ARTISTS);
}

export function splitArtists(s) {
  return artistNames(String(s ?? "").split(";"));
}
