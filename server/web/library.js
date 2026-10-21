import { t } from "./i18n.js";
import { h, api, apiSend, cover, fmtTime, Unauthorized } from "./dom.js";
import * as player from "./player.js";
import { heartButton } from "./favs.js";
import { addToPlaylistButton } from "./playlists.js";

const deletedSongs = new Set();
const deletedAlbums = new Set();
const liveAlbums = (list) => (list || []).filter((al) => !deletedAlbums.has(al.id));

const LISTS = [["newest", t("library.new")], ["recent", t("library.recent")], ["random", t("library.random")], ["artists", t("library.artists")]];

function caseCover(al, size, cls) {
  const box = cover(al.coverArt || al.id, size, `${cls} jc`);
  if (al.id) {
    box.dataset.album = al.id;
    if (player.nowAlbum() === al.id) box.classList.add("now");
  }
  return box;
}

let newSince = Infinity;
let ownAlbums = new Set();
let openedAlbums = new Set();
const albumKey = (artist, name) => `${(artist || "").trim().toLowerCase()}|${(name || "").trim().toLowerCase()}`;

export function setNewAlbums(since, own, opened) {
  newSince = Number(since) > 0 ? Number(since) * 1000 : Infinity;
  ownAlbums = new Set(own || []);
  openedAlbums = new Set(opened || []);
}

function markAlbumOpened(al) {
  if (!al.id || !isNewAlbum(al)) return;
  openedAlbums.add(al.id);
  for (const card of document.querySelectorAll("a.album.is-new")) {
    if (card.getAttribute("href") !== `#/library/album/${encodeURIComponent(al.id)}`) continue;
    card.classList.remove("is-new");
    card.querySelector(".new-ribbon")?.remove();
  }
  fetch(`/api/music/album/${encodeURIComponent(al.id)}/opened`, { method: "POST" }).catch(() => {});
}

function isNewAlbum(al) {
  const created = Date.parse(al.created || "");
  return created > newSince && !openedAlbums.has(al.id) && !ownAlbums.has(albumKey(al.artist, al.name || al.title));
}

export function albumCard(al) {
  const fresh = isNewAlbum(al);
  return h("a", { class: fresh ? "album is-new" : "album", href: `#/library/album/${encodeURIComponent(al.id)}` },
    caseCover(al, 300, "art"),
    fresh ? h("span", { class: "new-ribbon", "aria-label": t("library.newSince") }, h("span", {}, t("library.newBadge"))) : null,
    h("span", { class: "a-name" }, al.name || al.title || t("library.unknownAlbum")),
    h("span", { class: "a-sub" }, [al.artist, al.year].filter(Boolean).join(" · ")));
}

export function songRow(songs, i, opts = {}) {
  const s = songs[i];
  return h("li", { class: "song-row" },
    h("button", { class: "song-main", onclick: () => player.playList(songs, i) },
      h("span", { class: "s-no" }, opts.showCover ? cover(s.coverArt, 64, "art tiny") : String(s.track || i + 1)),
      h("span", { class: "s-meta" },
        h("span", { class: "s-title" }, s.title || t("player.unknownTitle")),
        opts.showArtist ? h("span", { class: "s-sub" }, [s.artist, s.album].filter(Boolean).join(" · ")) : null),
      h("span", { class: "s-dur" }, fmtTime(s.duration))),
    heartButton(s, "song", { cls: "more", onUnauthorized: opts.onUnauthorized }),
    h("button", { class: "icon more", title: t("song.playNext"), "aria-label": t("song.playNext"), onclick: () => player.playNext([s]) }, "⤵"),
    h("button", { class: "icon more", title: t("song.addToQueue"), "aria-label": t("song.addToQueue"), onclick: () => player.addToQueue([s]) }, "+"),
    addToPlaylistButton([s], opts),
    opts.extra || null,
    opts.onDelete ? h("button", { class: "icon more danger", title: t("library.deleteTrack"), "aria-label": t("library.deleteTrack"), onclick: () => opts.onDelete(s) }, "✕") : null);
}

export function artistList(artists) {
  return h("ul", { class: "artist-list" }, artists.map((ar) => h("li", {},
    h("a", { href: `#/library/artist/${encodeURIComponent(ar.id)}` },
      h("span", {}, ar.name),
      h("span", { class: "muted" }, t("library.albumCount", { n: ar.albumCount || 0 }))))));
}

function failBox(e, opts) {
  if (e instanceof Unauthorized) { opts.onUnauthorized?.(); return h("div"); }
  return h("div", { class: "card muted" }, e.message === "navidrome-unreachable" ? t("common.navidrome") : t("library.loadFailed"));
}

async function renderLists(container, kind, opts) {
  const chips = h("nav", { class: "chips-nav" },
    LISTS.map(([k, label]) => h("a", { class: `pill${k === kind ? " on" : ""}`, href: `#/library/${k}` }, label)));
  const body = h("div", { class: "muted" }, t("common.loading"));
  container.replaceChildren(chips, body);
  try {
    if (kind === "artists") {
      const data = await api("/api/music/artists");
      const groups = data.index || [];
      const sections = groups.map((g) => h("section", {}, h("h2", {}, g.name), artistList(g.artist || [])));
      body.replaceWith(groups.length ? h("div", { class: "artists" }, sections) : h("div", { class: "card muted" }, t("library.noArtists")));
      return;
    }
    const data = await api(`/api/music/albums?type=${kind}&size=60`);
    const albums = liveAlbums(data.album);
    body.replaceWith(albums.length ? h("div", { class: "grid" }, albums.map(albumCard))
      : h("div", { class: "card muted" }, t("library.noAlbums")));
  } catch (e) {
    body.replaceWith(failBox(e, opts));
  }
}

const DELETE_ERRORS = {
  "admin-only": t("delete.adminOnly"),
  "outside-library": t("delete.outside"),
  "file-not-found": t("delete.fileNotFound"),
  "not-found": t("delete.notFound"),
};

async function deleteItem(kind, id, opts) {
  try {
    const res = await apiSend("DELETE", `/api/music/${kind}/${encodeURIComponent(id)}`);
    return res.moved || [];
  } catch (e) {
    if (e instanceof Unauthorized) { opts.onUnauthorized?.(); return null; }
    alert(t("delete.failed", { error: DELETE_ERRORS[e.message] || e.message }));
    return null;
  }
}

async function renderAlbum(container, id, opts) {
  container.replaceChildren(h("div", { class: "muted" }, t("common.loading")));
  try {
    const al = await api(`/api/music/album/${encodeURIComponent(id)}`);
    markAlbumOpened(al);
    const songs = (al.song || []).filter((s) => !deletedSongs.has(s.id)).map((s) => ({ ...s, coverArt: s.coverArt || al.coverArt }));
    const deleteTrack = opts.admin ? async (s) => {
      if (!confirm(s.title ? t("delete.confirmTrack", { title: s.title }) : t("delete.confirmTrackUntitled"))) return;
      if (!(await deleteItem("track", s.id, opts))) return;
      deletedSongs.add(s.id);
      player.removeSongs([s.id]);
      if (songs.length <= 1) { deletedAlbums.add(al.id); location.hash = "#/library"; }
      else renderAlbum(container, id, opts);
    } : null;
    const deleteAlbum = async () => {
      if (!confirm(t("delete.confirmAlbum", { n: songs.length, name: al.name || t("library.unknownAlbum") }))) return;
      if (!(await deleteItem("album", al.id, opts))) return;
      songs.forEach((s) => deletedSongs.add(s.id));
      deletedAlbums.add(al.id);
      player.removeSongs(songs.map((s) => s.id));
      location.hash = "#/library";
    };
    const total = songs.reduce((t, s) => t + (s.duration || 0), 0);
    container.replaceChildren(
      h("a", { class: "back", href: "#/library" }, t("library.back")),
      h("section", { class: "album-head" },
        caseCover(al, 600, "art big"),
        h("div", { class: "album-info" },
          h("h1", {}, al.name || t("library.unknownAlbum")),
          h("a", { class: "muted", href: al.artistId ? `#/library/artist/${encodeURIComponent(al.artistId)}` : "#/library/artists" }, al.artist || ""),
          h("div", { class: "muted" }, [al.year, t("common.songCount", { n: songs.length }), fmtTime(total)].filter(Boolean).join(" · ")),
          Array.isArray(al.uploadedBy) && al.uploadedBy.length
            ? h("div", { class: "uploaded-by" }, t("library.uploadedBy", { names: al.uploadedBy.join(", ") })) : null,
          h("div", { class: "actions" },
            h("button", { class: "primary", onclick: () => player.playList(songs, 0), disabled: !songs.length }, t("player.play")),
            h("button", { onclick: () => { player.playList(songs, Math.floor(Math.random() * songs.length)); if (!player.settings().shuffle) player.toggleShuffle(); }, disabled: !songs.length }, t("player.shuffle")),
            h("button", { onclick: () => player.addToQueue(songs), disabled: !songs.length }, t("song.addToQueue")),
            songs.length ? addToPlaylistButton(songs, opts, "") : null,
            heartButton(al, "album", { cls: "album-heart", onUnauthorized: opts.onUnauthorized }),
            opts.admin ? h("button", { class: "danger", onclick: deleteAlbum }, t("library.deleteAlbum")) : null))),
      h("ol", { class: "songs card" }, songs.map((_, i) => songRow(songs, i, { onDelete: deleteTrack, onUnauthorized: opts.onUnauthorized }))));
  } catch (e) {
    container.replaceChildren(h("a", { class: "back", href: "#/library" }, t("library.back")), failBox(e, opts));
  }
}

async function renderArtist(container, id, opts) {
  container.replaceChildren(h("div", { class: "muted" }, t("common.loading")));
  try {
    const ar = await api(`/api/music/artist/${encodeURIComponent(id)}`);
    container.replaceChildren(
      h("a", { class: "back", href: "#/library/artists" }, t("library.backArtists")),
      h("h1", {}, ar.name || t("library.unknownArtist")),
      h("div", { class: "grid" }, liveAlbums(ar.album).map(albumCard)));
  } catch (e) {
    container.replaceChildren(h("a", { class: "back", href: "#/library/artists" }, t("library.backArtists")), failBox(e, opts));
  }
}

export function renderLibrary(container, parts, opts = {}) {
  const [kind, id] = parts;
  if (kind === "album" && id) return renderAlbum(container, id, opts);
  if (kind === "artist" && id) return renderArtist(container, id, opts);
  return renderLists(container, LISTS.some(([k]) => k === kind) ? kind : "newest", opts);
}
