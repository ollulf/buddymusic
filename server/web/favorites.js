import { t } from "./i18n.js";
import { h, api, Unauthorized } from "./dom.js";
import { albumCard, songRow } from "./library.js";
import { isStarred } from "./favs.js";
import * as player from "./player.js";

export async function renderFavorites(container, opts = {}) {
  container.replaceChildren(h("h1", {}, t("tab.favorites")), h("div", { class: "muted" }, t("common.loading")));
  try {
    const data = await api("/api/music/starred");
    const songs = (data.song || []).filter((s) => isStarred(s, "song"));
    const albums = (data.album || []).filter((al) => isStarred(al, "album"));
    const live = () => songs.filter((s) => isStarred(s, "song"));
    container.replaceChildren(
      h("h1", {}, t("tab.favorites")),
      h("section", {},
        h("h2", {}, t("favorites.songs", { n: songs.length })),
        songs.length
          ? [h("div", { class: "actions fav-actions" },
              h("button", { class: "primary", onclick: () => { const l = live(); if (l.length) player.playList(l, 0); } }, t("favorites.playAll")),
              h("button", { onclick: () => { const l = live(); if (l.length) { player.playList(l, Math.floor(Math.random() * l.length)); if (!player.settings().shuffle) player.toggleShuffle(); } } }, t("player.shuffle")),
              h("button", { onclick: () => { const l = live(); if (l.length) player.addToQueue(l); } }, t("song.addToQueue"))),
            h("ol", { class: "songs card" }, songs.map((_, i) => songRow(songs, i, { showArtist: true, showCover: true, onUnauthorized: opts.onUnauthorized })))]
          : h("p", { class: "card muted" }, t("favorites.noSongs"))),
      h("section", {},
        h("h2", {}, t("favorites.albums", { n: albums.length })),
        albums.length
          ? h("div", { class: "grid" }, albums.map(albumCard))
          : h("p", { class: "card muted" }, t("favorites.noAlbums"))));
  } catch (e) {
    if (e instanceof Unauthorized) return opts.onUnauthorized?.();
    container.replaceChildren(h("h1", {}, t("tab.favorites")),
      h("div", { class: "card muted" }, e.message === "navidrome-unreachable" ? t("common.navidrome") : t("favorites.loadFailed")));
  }
}
