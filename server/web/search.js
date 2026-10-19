import { h, api, Unauthorized } from "./dom.js";
import { albumCard, artistList, songRow } from "./library.js";

let lastQuery = "";

export function renderSearch(container, opts = {}) {
  const input = h("input", { type: "search", class: "search-box", placeholder: "Search artists, albums, songs", "aria-label": "Search", value: lastQuery, autocapitalize: "none" });
  const out = h("div", { class: "results" });
  container.replaceChildren(input, out);
  let timer = 0;
  let seq = 0;
  const run = async () => {
    const q = input.value.trim();
    lastQuery = q;
    const mine = ++seq;
    if (!q) { out.replaceChildren(h("p", { class: "muted" }, "Type to search your library.")); return; }
    try {
      const r = await api(`/api/music/search?q=${encodeURIComponent(q)}`);
      if (mine !== seq) return;
      const artists = r.artist || [], albums = r.album || [], songs = r.song || [];
      if (!artists.length && !albums.length && !songs.length) { out.replaceChildren(h("p", { class: "muted" }, "Nothing found.")); return; }
      out.replaceChildren(
        artists.length ? h("section", {}, h("h2", {}, "Artists"), artistList(artists)) : null,
        albums.length ? h("section", {}, h("h2", {}, "Albums"), h("div", { class: "grid" }, albums.map(albumCard))) : null,
        songs.length ? h("section", {}, h("h2", {}, "Songs"), h("ol", { class: "songs card" }, songs.map((_, i) => songRow(songs, i, { showArtist: true, showCover: true, onUnauthorized: opts.onUnauthorized })))) : null);
    } catch (e) {
      if (mine !== seq) return;
      if (e instanceof Unauthorized) return opts.onUnauthorized?.();
      out.replaceChildren(h("p", { class: "muted" }, "Search failed."));
    }
  };
  input.addEventListener("input", () => { clearTimeout(timer); timer = setTimeout(run, 250); });
  run();
  input.focus();
}
