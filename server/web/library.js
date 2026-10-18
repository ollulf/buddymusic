import { h, api, cover, fmtTime, Unauthorized } from "./dom.js";
import * as player from "./player.js";

const LISTS = [["newest", "New"], ["recent", "Recently played"], ["random", "Random"], ["artists", "Artists"]];

export function albumCard(al) {
  return h("a", { class: "album", href: `#/library/album/${encodeURIComponent(al.id)}` },
    cover(al.coverArt || al.id, 300),
    h("span", { class: "a-name" }, al.name || al.title || "Unknown album"),
    h("span", { class: "a-sub" }, [al.artist, al.year].filter(Boolean).join(" · ")));
}

export function songRow(songs, i, opts = {}) {
  const s = songs[i];
  return h("li", { class: "song-row" },
    h("button", { class: "song-main", onclick: () => player.playList(songs, i) },
      h("span", { class: "s-no" }, opts.showCover ? cover(s.coverArt, 64, "art tiny") : String(s.track || i + 1)),
      h("span", { class: "s-meta" },
        h("span", { class: "s-title" }, s.title || "Unknown title"),
        opts.showArtist ? h("span", { class: "s-sub" }, [s.artist, s.album].filter(Boolean).join(" · ")) : null),
      h("span", { class: "s-dur" }, fmtTime(s.duration))),
    h("button", { class: "icon more", title: "Play next", "aria-label": "Play next", onclick: () => player.playNext([s]) }, "⤵"),
    h("button", { class: "icon more", title: "Add to queue", "aria-label": "Add to queue", onclick: () => player.addToQueue([s]) }, "+"));
}

export function artistList(artists) {
  return h("ul", { class: "artist-list" }, artists.map((ar) => h("li", {},
    h("a", { href: `#/library/artist/${encodeURIComponent(ar.id)}` },
      h("span", {}, ar.name),
      h("span", { class: "muted" }, `${ar.albumCount || 0} albums`)))));
}

function failBox(e, opts) {
  if (e instanceof Unauthorized) { opts.onUnauthorized?.(); return h("div"); }
  return h("div", { class: "card muted" }, e.message === "navidrome-unreachable" ? "Navidrome is not answering." : "Could not load the library.");
}

async function renderLists(container, kind, opts) {
  const chips = h("nav", { class: "chips-nav" },
    LISTS.map(([k, label]) => h("a", { class: `pill${k === kind ? " on" : ""}`, href: `#/library/${k}` }, label)));
  const body = h("div", { class: "muted" }, "Loading…");
  container.replaceChildren(chips, body);
  try {
    if (kind === "artists") {
      const data = await api("/api/music/artists");
      const groups = data.index || [];
      const sections = groups.map((g) => h("section", {}, h("h2", {}, g.name), artistList(g.artist || [])));
      body.replaceWith(groups.length ? h("div", { class: "artists" }, sections) : h("div", { class: "card muted" }, "No artists yet."));
      return;
    }
    const data = await api(`/api/music/albums?type=${kind}&size=60`);
    const albums = data.album || [];
    body.replaceWith(albums.length ? h("div", { class: "grid" }, albums.map(albumCard))
      : h("div", { class: "card muted" }, "No albums yet. Upload some music in the Upload tab."));
  } catch (e) {
    body.replaceWith(failBox(e, opts));
  }
}

async function renderAlbum(container, id, opts) {
  container.replaceChildren(h("div", { class: "muted" }, "Loading…"));
  try {
    const al = await api(`/api/music/album/${encodeURIComponent(id)}`);
    const songs = (al.song || []).map((s) => ({ ...s, coverArt: s.coverArt || al.coverArt }));
    const total = songs.reduce((t, s) => t + (s.duration || 0), 0);
    container.replaceChildren(
      h("a", { class: "back", href: "#/library" }, "‹ Library"),
      h("section", { class: "album-head" },
        cover(al.coverArt || al.id, 600, "art big"),
        h("div", { class: "album-info" },
          h("h1", {}, al.name || "Unknown album"),
          h("a", { class: "muted", href: al.artistId ? `#/library/artist/${encodeURIComponent(al.artistId)}` : "#/library/artists" }, al.artist || ""),
          h("div", { class: "muted" }, [al.year, `${songs.length} songs`, fmtTime(total)].filter(Boolean).join(" · ")),
          h("div", { class: "actions" },
            h("button", { class: "primary", onclick: () => player.playList(songs, 0), disabled: !songs.length }, "Play"),
            h("button", { onclick: () => { player.playList(songs, Math.floor(Math.random() * songs.length)); if (!player.settings().shuffle) player.toggleShuffle(); }, disabled: !songs.length }, "Shuffle"),
            h("button", { onclick: () => player.addToQueue(songs), disabled: !songs.length }, "Add to queue")))),
      h("ol", { class: "songs card" }, songs.map((_, i) => songRow(songs, i))));
  } catch (e) {
    container.replaceChildren(h("a", { class: "back", href: "#/library" }, "‹ Library"), failBox(e, opts));
  }
}

async function renderArtist(container, id, opts) {
  container.replaceChildren(h("div", { class: "muted" }, "Loading…"));
  try {
    const ar = await api(`/api/music/artist/${encodeURIComponent(id)}`);
    container.replaceChildren(
      h("a", { class: "back", href: "#/library/artists" }, "‹ Artists"),
      h("h1", {}, ar.name || "Unknown artist"),
      h("div", { class: "grid" }, (ar.album || []).map(albumCard)));
  } catch (e) {
    container.replaceChildren(h("a", { class: "back", href: "#/library/artists" }, "‹ Artists"), failBox(e, opts));
  }
}

export function renderLibrary(container, parts, opts = {}) {
  const [kind, id] = parts;
  if (kind === "album" && id) return renderAlbum(container, id, opts);
  if (kind === "artist" && id) return renderArtist(container, id, opts);
  return renderLists(container, LISTS.some(([k]) => k === kind) ? kind : "newest", opts);
}
