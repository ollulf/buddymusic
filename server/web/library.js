import { t } from "./i18n.js";
import { h, api, apiSend, apiJSON, cover, fmtTime, Unauthorized, setPageTitle } from "./dom.js";
import * as player from "./player.js";
import { heartButton, isStarred, toggleStar } from "./favs.js";
import { addToPlaylistButton, openAddSheet, toast } from "./playlists.js";
import { rowMenuButton, swipeRow } from "./rowactions.js";
import { flyFrom } from "./fly.js";
import { holo, sparkles, unholo } from "./holo.js";
import { openAlbumEdit } from "./albumedit.js";
import { openRecommendSheet } from "./recs.js";

const deletedSongs = new Set();
const deletedAlbums = new Set();
const liveAlbums = (list) => (list || []).filter((al) => !deletedAlbums.has(al.id));

const LISTS = [["newest", t("library.new")], ["recent", t("library.recent")], ["random", t("library.random")], ["artists", t("library.artists")]];

const EAGER_COVERS = 12;

function caseCover(al, size, cls, eager = false) {
  const box = cover(al.coverArt || al.id, size, `${cls} jc`, eager);
  if (al.id) {
    box.dataset.album = al.id;
    if (player.nowAlbum() === al.id) box.classList.add("now");
  }
  if (al.vinyl || vinylAlbums.has(al.id)) box.append(h("span", { class: "vinyl-tag", title: t("library.vinylTag") }, "VINYL"));
  return box;
}

let newSince = Infinity;
let ownAlbums = new Set();
let openedAlbums = new Set();
let renewedAlbums = new Map();
let vinylAlbums = new Set();
const albumKey = (artist, name) => `${(artist || "").trim().toLowerCase()}|${(name || "").trim().toLowerCase()}`;

export function setVinylAlbums(ids) {
  vinylAlbums = new Set(ids || []);
}

export function setNewAlbums(since, own, opened, renewed) {
  newSince = Number(since) > 0 ? Number(since) * 1000 : Infinity;
  ownAlbums = new Set(own || []);
  openedAlbums = new Set(opened || []);
  renewedAlbums = new Map(Object.entries(renewed || {}).map(([id, at]) => [id, Number(at) * 1000]));
}

function markAlbumOpened(al) {
  if (!al.id || !isNewAlbum(al)) return;
  openedAlbums.add(al.id);
  for (const card of document.querySelectorAll("a.album.is-new")) {
    if (card.getAttribute("href") !== `#/library/album/${encodeURIComponent(al.id)}`) continue;
    card.classList.remove("is-new");
    card.querySelector(".new-ribbon")?.remove();
    card.querySelector(".sparkles")?.remove();
    unholo(card.querySelector(".holo"));
  }
  fetch(`/api/music/album/${encodeURIComponent(al.id)}/opened`, { method: "POST" }).catch(() => {});
}

function isNewAlbum(al) {
  if (openedAlbums.has(al.id)) return false;
  if (renewedAlbums.get(al.id) > newSince) return true;
  const created = Date.parse(al.created || "");
  return created > newSince && !ownAlbums.has(albumKey(al.artist, al.name || al.title));
}

export function albumCard(al, i = EAGER_COVERS) {
  const fresh = isNewAlbum(al);
  const art = caseCover(al, 300, "art", i < EAGER_COVERS);
  if (fresh) art.append(holo());
  return h("a", { class: fresh ? "album is-new" : "album", href: `#/library/album/${encodeURIComponent(al.id)}` },
    art,
    fresh ? sparkles() : null,
    fresh ? h("span", { class: "new-ribbon", "aria-label": t("library.newSince") }, h("span", {}, t("library.newBadge"))) : null,
    h("span", { class: "a-name" }, al.name || al.title || t("library.unknownAlbum")),
    h("span", { class: "a-sub" }, [al.artist, al.year].filter(Boolean).join(" · ")));
}

function markNow(li) {
  const cur = player.current();
  const on = !!cur && cur.id === li.dataset.song;
  li.classList.toggle("now-playing", on);
  if (on) li.setAttribute("aria-current", "true"); else li.removeAttribute("aria-current");
}

player.onChange(() => {
  for (const li of document.querySelectorAll("li.song-row[data-song]")) markNow(li);
});

function artistNames(list, fallback) {
  const names = Array.isArray(list) ? list.map((a) => (a?.name || "").trim()).filter(Boolean) : [];
  return names.length ? names : (fallback ? [fallback.trim()] : []);
}

function guestLine(s, al) {
  const album = new Set(artistNames(al.artists, al.artist).map((n) => n.toLowerCase()));
  const own = artistNames(s.artists, s.artist);
  const extra = own.filter((n) => !album.has(n.toLowerCase()));
  if (!extra.length) return "";
  if (Array.isArray(s.artists) && s.artists.length) return "feat. " + extra.join(", ");
  return s.artist || "";
}

function albumGuests(al, songs) {
  const seen = new Set(artistNames(al.artists, al.artist).map((n) => n.toLowerCase()));
  const out = [];
  for (const s of songs || []) {
    const list = Array.isArray(s.artists) && s.artists.length
      ? s.artists.filter((a) => a?.name).map((a) => ({ id: a.id, name: a.name.trim() }))
      : (s.artist || "").split(/\s*;\s*/).filter(Boolean).map((name) => ({ name }));
    for (const a of list) {
      const key = a.name.toLowerCase();
      if (!a.name || seen.has(key)) continue;
      seen.add(key);
      out.push(a);
    }
  }
  return out;
}

function artistLink(a) {
  return h("a", { class: "muted", href: a.id ? `#/library/artist/${encodeURIComponent(a.id)}` : "#/library/artists" }, a.name);
}

function albumArtistLinks(al, songs) {
  const list = Array.isArray(al.artists) && al.artists.length ? al.artists : [{ id: al.artistId, name: al.artist }];
  const join = (els) => els.flatMap((el, i) => i ? [h("span", { class: "muted" }, ", "), el] : [el]);
  const links = join(list.filter((a) => a?.name).map(artistLink));
  const guests = albumGuests(al, songs).map(artistLink);
  const tail = guests.length ? [h("span", { class: "muted" }, " · feat. "), ...join(guests)] : [];
  return h("div", { class: "album-artists" }, ...links, ...tail);
}

export function songRow(songs, i, opts = {}) {
  const s = songs[i];
  const fav = () => isStarred(s, "song");
  const toggleFav = () => {
    const on = !fav();
    toggleStar(s, "song", opts);
    toast(t(on ? "song.faved" : "song.unfaved", { title: s.title || t("player.unknownTitle") }));
  };
  const queue = () => {
    flyFrom(li);
    player.addToQueue([s]);
  };
  const next = () => {
    flyFrom(li);
    player.playNext([s]);
  };
  const menu = rowMenuButton(() => [
    { icon: fav() ? "♥" : "♡", label: t(fav() ? "song.unfav" : "song.fav"), run: toggleFav },
    { icon: "⤵", label: t("song.playNext"), run: next },
    { icon: "+", label: t("song.addToQueue"), run: queue },
    { icon: "☰", label: `${t("playlists.addTo")}…`, run: () => openAddSheet([s], opts) },
    s.id ? { icon: "✉", label: `${t("recs.recommendTo")}…`, run: () => openRecommendSheet(s, opts) } : null,
    ...(opts.menu || []),
    opts.onDelete ? { icon: "✕", label: t("library.deleteTrack"), danger: true, run: () => opts.onDelete(s) } : null,
  ]);
  const fg = h("div", { class: "row-fg" },
    h("button", { class: "song-main", onclick: () => player.playList(songs, i) },
      h("span", { class: "s-no" }, opts.showCover ? cover(s.coverArt, 64, "art tiny") : String(s.track || i + 1)),
      h("span", { class: "s-meta" },
        h("span", { class: "s-title" }, s.title || t("player.unknownTitle")),
        opts.showArtist ? h("span", { class: "s-sub" }, [s.artist, s.album].filter(Boolean).join(" · "))
          : opts.album && guestLine(s, opts.album) ? h("span", { class: "s-sub" }, guestLine(s, opts.album)) : null),
      h("span", { class: "s-dur" }, fmtTime(s.duration))),
    menu);
  const li = h("li", { class: "song-row" }, fg);
  if (s.id) { li.dataset.song = s.id; markNow(li); }
  swipeRow(li, fg, {
    right: { icon: "+", label: () => t("song.swipeQueue"), run: queue },
    left: { icon: "♥", label: () => t(fav() ? "song.unfav" : "song.swipeFav"), run: toggleFav },
  });
  return li;
}

export function artistList(artists) {
  return h("ul", { class: "artist-list" }, artists.map((ar) => h("li", {},
    h("a", { href: `#/library/artist/${encodeURIComponent(ar.id)}` },
      h("span", {}, ar.name),
      h("span", { class: "muted" }, ar.featured && !ar.albumCount
        ? t("library.featuredCount", { n: ar.appearsOnCount || 0 })
        : t("library.albumCount", { n: ar.albumCount || 0 }))))));
}

function failBox(e, opts) {
  if (e instanceof Unauthorized) { opts.onUnauthorized?.(); return h("div"); }
  return h("div", { class: "card muted" }, e.message === "navidrome-unreachable" ? t("common.navidrome") : t("library.loadFailed"));
}

let prefetched = null;

const albumsPath = (kind) => `/api/music/albums?type=${kind}&size=60`;

export function prefetchLibrary(parts) {
  const kind = parts[0] || "newest";
  if (kind === "artists" || !LISTS.some(([k]) => k === kind)) return;
  const promise = api(albumsPath(kind));
  promise.catch(() => {});
  prefetched = { kind, promise };
}

export function dropLibraryPrefetch() {
  prefetched = null;
}

function albumsFor(kind) {
  const p = prefetched;
  prefetched = null;
  return p && p.kind === kind ? p.promise : api(albumsPath(kind));
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
    const data = await albumsFor(kind);
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
  "move-failed": t("delete.moveFailed"),
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
    const renewAlbum = async () => {
      if (!confirm(t("library.confirmRenew", { name: al.name || t("library.unknownAlbum") }))) return;
      try {
        await apiJSON("POST", `/api/admin/album/${encodeURIComponent(al.id)}/renew`);
        openedAlbums.add(al.id);
        toast(t("library.renewed"));
      } catch (e) {
        if (e instanceof Unauthorized) opts.onUnauthorized?.();
        else toast(t("library.renewFailed"));
      }
    };
    const total = songs.reduce((t, s) => t + (s.duration || 0), 0);
    setPageTitle(al.name || t("library.unknownAlbum"), container);
    container.replaceChildren(
      h("a", { class: "back", href: "#/library" }, t("library.back")),
      h("section", { class: "album-head" },
        caseCover(al, 600, "art big"),
        h("div", { class: "album-info" },
          albumArtistLinks(al, songs),
          h("div", { class: "muted" }, [al.year, t("common.songCount", { n: songs.length }), fmtTime(total)].filter(Boolean).join(" · ")),
          Array.isArray(al.uploadedBy) && al.uploadedBy.length
            ? h("div", { class: "uploaded-by" }, t("library.uploadedBy", { names: al.uploadedBy.join(", ") })) : null,
          h("div", { class: "actions" },
            h("button", { class: "primary", onclick: () => player.playList(songs, 0), disabled: !songs.length }, t("player.play")),
            h("button", { onclick: () => { player.playList(songs, Math.floor(Math.random() * songs.length)); if (!player.settings().shuffle) player.toggleShuffle(); }, disabled: !songs.length }, t("player.shuffle")),
            h("button", { onclick: () => player.addToQueue(songs), disabled: !songs.length }, t("song.addToQueue")),
            songs.length ? addToPlaylistButton(songs, opts, "") : null,
            heartButton(al, "album", { cls: "album-heart", onUnauthorized: opts.onUnauthorized }),
            opts.admin ? h("button", { onclick: (e) => openAlbumEdit(al, songs, { returnTo: e.currentTarget, onUnauthorized: opts.onUnauthorized, onSaved: (body) => { if (body && "vinyl" in body) { if (body.vinyl) vinylAlbums.add(al.id); else vinylAlbums.delete(al.id); } renderAlbum(container, id, opts); } }) }, t("library.editAlbum")) : null,
            opts.admin ? h("button", { onclick: renewAlbum }, t("library.renewAlbum")) : null,
            opts.admin ? h("button", { class: "danger", onclick: deleteAlbum }, t("library.deleteAlbum")) : null))),
      h("ol", { class: "songs card" }, songs.map((_, i) => songRow(songs, i, { album: al, onDelete: deleteTrack, onUnauthorized: opts.onUnauthorized }))));
  } catch (e) {
    container.replaceChildren(h("a", { class: "back", href: "#/library" }, t("library.back")), failBox(e, opts));
  }
}

async function renderArtist(container, id, opts) {
  container.replaceChildren(h("div", { class: "muted" }, t("common.loading")));
  try {
    const ar = await api(`/api/music/artist/${encodeURIComponent(id)}`);
    setPageTitle(ar.name || t("library.unknownArtist"), container);
    const appears = liveAlbums(ar.appearsOn);
    container.replaceChildren(
      h("a", { class: "back", href: "#/library/artists" }, t("library.backArtists")),
      h("div", { class: "grid" }, liveAlbums(ar.album).map(albumCard)),
      ...(appears.length ? [h("h2", { class: "appears-on" }, t("library.appearsOn")), h("div", { class: "grid" }, appears.map(albumCard))] : []));
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
