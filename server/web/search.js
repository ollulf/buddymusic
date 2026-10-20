import { h, api, Unauthorized } from "./dom.js";
import { albumCard, artistList, songRow } from "./library.js";

let query = "";
let target = null;
let box = null;
let runOpts = {};
let seq = 0;

const onSearchView = () => /^#\/?search(\/|$)/.test(location.hash);

async function run() {
  if (!target) return;
  const out = target;
  const q = query;
  const mine = ++seq;
  if (q.length < 2) { out.replaceChildren(h("p", { class: "muted" }, "Type in the search box at the top to search your library.")); return; }
  try {
    const r = await api(`/api/music/search?q=${encodeURIComponent(q)}`);
    if (mine !== seq) return;
    const artists = r.artist || [], albums = r.album || [], songs = r.song || [];
    if (!artists.length && !albums.length && !songs.length) { out.replaceChildren(h("p", { class: "muted" }, "Nothing found.")); return; }
    out.replaceChildren(
      artists.length ? h("section", {}, h("h2", {}, "Artists"), artistList(artists)) : null,
      albums.length ? h("section", {}, h("h2", {}, "Albums"), h("div", { class: "grid" }, albums.map(albumCard))) : null,
      songs.length ? h("section", {}, h("h2", {}, "Songs"), h("ol", { class: "songs card" }, songs.map((_, i) => songRow(songs, i, { showArtist: true, showCover: true, onUnauthorized: runOpts.onUnauthorized })))) : null);
  } catch (e) {
    if (mine !== seq) return;
    if (e instanceof Unauthorized) return runOpts.onUnauthorized?.();
    out.replaceChildren(h("p", { class: "muted" }, "Search failed."));
  }
}

export function searchBox() {
  const input = h("input", { type: "search", class: "search-box", placeholder: "Search", "aria-label": "Search artists, albums, songs", title: "Search artists, albums, songs", value: query, autocapitalize: "none", autocomplete: "off", enterkeyhint: "search" });
  box = input;
  let timer = 0;
  const go = () => {
    clearTimeout(timer);
    const q = input.value.trim();
    if (q.length === 1 || (q === query && onSearchView())) return;
    query = q;
    if (onSearchView()) run();
    else if (q) location.hash = "#/search";
  };
  input.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(go, 250); });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); go(); }
    if (e.key === "Escape") {
      e.stopPropagation();
      clearTimeout(timer);
      input.value = "";
      query = "";
      input.blur();
      if (onSearchView()) run();
    }
  });
  return h("div", { class: "hsearch", role: "search" }, input);
}

export function renderSearch(container, opts = {}) {
  target = container;
  runOpts = opts;
  run();
  if (query.length < 2) box?.focus();
}
