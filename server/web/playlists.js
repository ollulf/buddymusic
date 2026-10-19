import { h, api, apiJSON, fmtTime, Unauthorized } from "./dom.js";
import { songRow } from "./library.js";
import * as player from "./player.js";

let me = "";
export const setPlaylistUser = (user) => { me = (user || "").toLowerCase(); };
const mine = (pl) => (pl.owner || "").toLowerCase() === me;

const ERRORS = {
  "not-owner": "Only the owner can change this playlist.",
  "not-found": "This playlist no longer exists.",
  "navidrome-unreachable": "Navidrome is not answering.",
};

function report(e, opts, what) {
  if (e instanceof Unauthorized) { opts.onUnauthorized?.(); return; }
  alert(`${what} failed: ${ERRORS[e.message] || e.message}`);
}

const listPlaylists = async () => (await api("/api/music/playlists")).playlist || [];

function shuffle(songs) {
  if (!songs.length) return;
  player.playList(songs, Math.floor(Math.random() * songs.length));
  if (!player.settings().shuffle) player.toggleShuffle();
}

async function createPlaylist(name, songIds, opts) {
  try {
    return await apiJSON("POST", "/api/music/playlists", { name, songIds });
  } catch (e) {
    report(e, opts, "Creating the playlist");
    return null;
  }
}

function deleteButton(pl, opts, after) {
  return h("button", { class: "icon more danger", title: "Delete playlist", "aria-label": `Delete playlist ${pl.name}`, onclick: async () => {
    if (!confirm(`Delete the playlist "${pl.name}"?\n\nThe songs stay in the library; only the playlist goes away.`)) return;
    try {
      await apiJSON("DELETE", `/api/music/playlist/${encodeURIComponent(pl.id)}`);
      after();
    } catch (e) { report(e, opts, "Deleting"); }
  } }, "✕");
}

function playlistRow(pl, opts, reload) {
  const own = mine(pl);
  const sub = [`${pl.songCount || 0} songs`, pl.duration ? fmtTime(pl.duration) : null, own ? null : `by ${pl.owner}`].filter(Boolean).join(" · ");
  return h("li", { class: "pl-row" },
    h("a", { class: "pl-main", href: `#/playlists/${encodeURIComponent(pl.id)}` },
      h("span", { class: "s-meta" },
        h("span", { class: "s-title" }, pl.name || "Untitled", own && pl.public ? h("span", { class: "badge" }, "Public") : null),
        h("span", { class: "s-sub" }, sub))),
    h("button", { class: "icon more", title: "Play", "aria-label": `Play ${pl.name}`, onclick: async () => {
      try {
        const full = await api(`/api/music/playlist/${encodeURIComponent(pl.id)}`);
        if (full.entry?.length) player.playList(full.entry, 0);
      } catch (e) { report(e, opts, "Loading"); }
    } }, "▶"),
    own ? deleteButton(pl, opts, reload) : null);
}

async function renderOverview(container, opts) {
  const reload = () => renderOverview(container, opts);
  const newBtn = h("button", { class: "primary", onclick: async () => {
    const name = prompt("Name of the new playlist");
    if (!name || !name.trim()) return;
    const pl = await createPlaylist(name.trim(), [], opts);
    if (pl) location.hash = `#/playlists/${encodeURIComponent(pl.id)}`;
  } }, "New playlist");
  container.replaceChildren(h("div", { class: "pl-top" }, h("h1", {}, "Playlists"), newBtn), h("div", { class: "muted" }, "Loading…"));
  try {
    const all = await listPlaylists();
    const own = all.filter(mine);
    const friends = all.filter((pl) => !mine(pl));
    container.replaceChildren(
      h("div", { class: "pl-top" }, h("h1", {}, "Playlists"), newBtn),
      h("section", {},
        h("h2", {}, `My playlists (${own.length})`),
        own.length ? h("ul", { class: "songs card" }, own.map((pl) => playlistRow(pl, opts, reload)))
          : h("p", { class: "card muted" }, "No playlists yet. Create one, or use the ☰ button next to any song.")),
      h("section", {},
        h("h2", {}, `From friends (${friends.length})`),
        friends.length ? h("ul", { class: "songs card" }, friends.map((pl) => playlistRow(pl, opts, reload)))
          : h("p", { class: "card muted" }, "Nobody has shared a public playlist yet.")));
  } catch (e) {
    if (e instanceof Unauthorized) return opts.onUnauthorized?.();
    container.replaceChildren(h("h1", {}, "Playlists"), h("div", { class: "card muted" }, ERRORS[e.message] || "Could not load playlists."));
  }
}

async function renderPlaylist(container, id, opts) {
  const back = h("a", { class: "back", href: "#/playlists" }, "‹ Playlists");
  container.replaceChildren(back, h("div", { class: "muted" }, "Loading…"));
  let pl;
  try {
    pl = await api(`/api/music/playlist/${encodeURIComponent(id)}`);
  } catch (e) {
    if (e instanceof Unauthorized) return opts.onUnauthorized?.();
    container.replaceChildren(back, h("div", { class: "card muted" }, ERRORS[e.message] || "Could not load the playlist."));
    return;
  }
  const own = mine(pl);
  const songs = pl.entry || [];
  const reload = () => renderPlaylist(container, id, opts);
  const patch = async (body) => {
    try { await apiJSON("PATCH", `/api/music/playlist/${encodeURIComponent(pl.id)}`, body); }
    catch (e) { report(e, opts, "Saving"); }
    reload();
  };
  const saveSongs = async (list) => {
    try { await apiJSON("PUT", `/api/music/playlist/${encodeURIComponent(pl.id)}/songs`, { songIds: list.map((s) => s.id) }); }
    catch (e) { report(e, opts, "Saving"); }
    reload();
  };
  const move = (i, d) => {
    const list = songs.slice();
    [list[i], list[i + d]] = [list[i + d], list[i]];
    saveSongs(list);
  };
  const total = songs.reduce((t, s) => t + (s.duration || 0), 0);
  const publicBox = h("input", { type: "checkbox", checked: !!pl.public, onchange: (e) => patch({ public: e.target.checked }) });
  const rows = songs.map((_, i) => songRow(songs, i, {
    showArtist: true, showCover: true, onUnauthorized: opts.onUnauthorized,
    extra: own ? [
      h("button", { class: "icon more", title: "Move up", "aria-label": "Move up", disabled: i === 0, onclick: () => move(i, -1) }, "↑"),
      h("button", { class: "icon more", title: "Move down", "aria-label": "Move down", disabled: i === songs.length - 1, onclick: () => move(i, 1) }, "↓"),
      h("button", { class: "icon more danger", title: "Remove from playlist", "aria-label": "Remove from playlist", onclick: () => saveSongs(songs.filter((_, j) => j !== i)) }, "−"),
    ] : null,
  }));
  container.replaceChildren(
    back,
    h("section", { class: "pl-head" },
      h("h1", {}, pl.name || "Untitled", own && pl.public ? h("span", { class: "badge" }, "Public") : null),
      h("div", { class: "muted" }, [own ? "Your playlist" : `By ${pl.owner}`, `${songs.length} songs`, fmtTime(total)].join(" · ")),
      h("div", { class: "actions" },
        h("button", { class: "primary", disabled: !songs.length, onclick: () => player.playList(songs, 0) }, "Play all"),
        h("button", { disabled: !songs.length, onclick: () => shuffle(songs) }, "Shuffle"),
        h("button", { disabled: !songs.length, onclick: () => player.addToQueue(songs) }, "Add to queue"),
        own ? h("button", { onclick: () => {
          const name = prompt("New name", pl.name || "");
          if (name && name.trim() && name.trim() !== pl.name) patch({ name: name.trim() });
        } }, "Rename") : null,
        own ? null : h("button", { onclick: async () => {
          const copy = await createPlaylist(pl.name || "Copy", songs.map((s) => s.id), opts);
          if (copy) location.hash = `#/playlists/${encodeURIComponent(copy.id)}`;
        } }, "Copy to my playlists"),
        own ? h("button", { class: "danger", onclick: async () => {
          if (!confirm(`Delete the playlist "${pl.name}"?\n\nThe songs stay in the library; only the playlist goes away.`)) return;
          try {
            await apiJSON("DELETE", `/api/music/playlist/${encodeURIComponent(pl.id)}`);
            location.hash = "#/playlists";
          } catch (e) { report(e, opts, "Deleting"); }
        } }, "Delete playlist") : null),
      own ? h("label", { class: "pl-public" }, publicBox, h("span", {}, "Public: every buddymusic user can see and play it (read-only)")) : null),
    songs.length ? h("ol", { class: "songs card" }, rows)
      : h("p", { class: "card muted" }, own ? "Empty. Add songs with the ☰ button next to any song." : "This playlist is empty."));
}

export function renderPlaylists(container, parts, opts = {}) {
  const [id] = parts;
  if (id) return renderPlaylist(container, id, opts);
  return renderOverview(container, opts);
}

export async function openAddSheet(songs, opts = {}) {
  const ids = songs.map((s) => s.id);
  const list = h("ul", { class: "sheet-list" }, h("li", { class: "muted" }, "Loading…"));
  const name = h("input", { class: "search-box", placeholder: "New playlist name", "aria-label": "New playlist name" });
  const status = h("div", { class: "muted", role: "status" });
  const dlg = h("dialog", { class: "sheet card" },
    h("h2", {}, songs.length === 1 ? `Add "${songs[0].title || "song"}" to…` : `Add ${songs.length} songs to…`),
    list,
    h("form", { class: "sheet-new", onsubmit: async (e) => {
      e.preventDefault();
      if (!name.value.trim()) return name.focus();
      const pl = await createPlaylist(name.value.trim(), ids, opts);
      if (pl) { dlg.close(); toast(`Added to ${pl.name}`); }
    } }, name, h("button", { class: "primary", type: "submit" }, "Create")),
    status,
    h("button", { class: "ghost", type: "button", onclick: () => dlg.close() }, "Cancel"));
  dlg.addEventListener("close", () => dlg.remove());
  document.body.append(dlg);
  dlg.showModal();
  try {
    const own = (await listPlaylists()).filter(mine);
    list.replaceChildren(...(own.length ? own.map((pl) => h("li", {}, h("button", { class: "sheet-item", onclick: async () => {
      try {
        await apiJSON("POST", `/api/music/playlist/${encodeURIComponent(pl.id)}/songs`, { songIds: ids });
        dlg.close();
        toast(`Added to ${pl.name}`);
      } catch (e) { report(e, opts, "Adding"); }
    } }, h("span", {}, pl.name), h("span", { class: "muted" }, `${pl.songCount || 0} songs`)))) : [h("li", { class: "muted" }, "No playlists yet. Name a new one below.")]));
  } catch (e) {
    if (e instanceof Unauthorized) { dlg.close(); return opts.onUnauthorized?.(); }
    list.replaceChildren(h("li", { class: "muted" }, "Could not load your playlists."));
  }
}

function toast(text) {
  const t = h("div", { class: "toast", role: "status" }, text);
  document.body.append(t);
  setTimeout(() => t.remove(), 2200);
}

export const addToPlaylistButton = (songs, opts = {}, cls = "icon more") =>
  h("button", { class: cls, title: "Add to playlist", "aria-label": "Add to playlist", onclick: () => openAddSheet(songs, opts) }, cls.includes("icon") ? "☰" : "Add to playlist");
