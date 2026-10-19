import { h, api, Unauthorized } from "./dom.js";
import { albumCard, songRow } from "./library.js";
import { isStarred } from "./favs.js";
import * as player from "./player.js";

export async function renderFavorites(container, opts = {}) {
  container.replaceChildren(h("h1", {}, "Favorites"), h("div", { class: "muted" }, "Loading…"));
  try {
    const data = await api("/api/music/starred");
    const songs = (data.song || []).filter((s) => isStarred(s, "song"));
    const albums = (data.album || []).filter((al) => isStarred(al, "album"));
    const live = () => songs.filter((s) => isStarred(s, "song"));
    container.replaceChildren(
      h("h1", {}, "Favorites"),
      h("section", {},
        h("h2", {}, `Songs (${songs.length})`),
        songs.length
          ? [h("div", { class: "actions fav-actions" },
              h("button", { class: "primary", onclick: () => { const l = live(); if (l.length) player.playList(l, 0); } }, "Play all"),
              h("button", { onclick: () => { const l = live(); if (l.length) { player.playList(l, Math.floor(Math.random() * l.length)); if (!player.settings().shuffle) player.toggleShuffle(); } } }, "Shuffle"),
              h("button", { onclick: () => { const l = live(); if (l.length) player.addToQueue(l); } }, "Add to queue")),
            h("ol", { class: "songs card" }, songs.map((_, i) => songRow(songs, i, { showArtist: true, showCover: true, onUnauthorized: opts.onUnauthorized })))]
          : h("p", { class: "card muted" }, "No favorite songs yet. Tap the heart next to a song to add it.")),
      h("section", {},
        h("h2", {}, `Albums (${albums.length})`),
        albums.length
          ? h("div", { class: "grid" }, albums.map(albumCard))
          : h("p", { class: "card muted" }, "No favorite albums yet. Use the heart on an album page.")));
  } catch (e) {
    if (e instanceof Unauthorized) return opts.onUnauthorized?.();
    container.replaceChildren(h("h1", {}, "Favorites"),
      h("div", { class: "card muted" }, e.message === "navidrome-unreachable" ? "Navidrome is not answering." : "Could not load favorites."));
  }
}
